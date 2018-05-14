package autodelete

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
)

func (b *Bot) ConnectDiscord() error {
	s, err := discordgo.New("Bot " + b.BotToken)
	if err != nil {
		return err
	}
	b.s = s
	s.AddHandler(b.OnReady)
	s.AddHandler(b.OnResume)
	s.AddHandler(b.OnChannelCreate)
	s.AddHandler(b.OnChannelPins)
	s.AddHandler(b.HandleMentions)
	s.AddHandler(b.OnMessage)
	me, err := s.User("@me")
	if err != nil {
		return errors.Wrap(err, "get me")
	}
	b.me = me

	err = s.Open()
	if err != nil {
		return errors.Wrap(err, "open socket")
	}
	return nil
}

func (b *Bot) HandleMentions(s *discordgo.Session, m *discordgo.MessageCreate) {
	found := false
	for _, v := range m.Message.Mentions {
		if v.ID == b.me.ID {
			found = true
			break
		}
	}
	if !found {
		return
	}

	split := strings.Fields(m.Message.Content)
	if split[0] == b.me.Mention() && len(split) > 1 {
		cmd := split[1]
		fun, ok := commands[cmd]
		if ok {
			fmt.Println("got command:", split)
			go fun(b, m.Message, split[2:])
			return
		}
	}
	fmt.Println("got non-command mention:", m.Message.Content)
}

func (b *Bot) OnMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.mu.RLock()
	mCh, ok := b.channels[m.Message.ChannelID]
	b.mu.RUnlock()

	if !ok {
		b.loadChannel(m.Message.ChannelID)
		b.mu.RLock()
		mCh = b.channels[m.Message.ChannelID]
		b.mu.RUnlock()
	}

	if mCh != nil {
		mCh.AddMessage(m.Message)
	}
}

func (b *Bot) OnChannelCreate(s *discordgo.Session, ch *discordgo.ChannelCreate) {
	// No action, need a config message
}

func (b *Bot) OnChannelPins(s *discordgo.Session, ev *discordgo.ChannelPinsUpdate) {
	b.mu.RLock()
	mCh, ok := b.channels[ev.ChannelID]
	b.mu.RUnlock()
	if !ok || mCh == nil {
		return
	}

	fmt.Println("Channel pins changed for", mCh.Channel.ID, mCh.Channel.Name, "- new lpts", ev.LastPinTimestamp)
}

func (b *Bot) OnReady(s *discordgo.Session, m *discordgo.Ready) {
	b.ReportToLogChannel("AutoDelete started.")
	err := s.UpdateStatus(0, "in the garbage")
	if err != nil {
		fmt.Println("error setting game:", err)
	}

	go func() {
		err := b.LoadChannelConfigs()
		if err != nil {
			fmt.Println("error loading configs:", err)
		}

		b.LoadAllBacklogs()
	}()
}

func (b *Bot) OnResume(s *discordgo.Session, r *discordgo.Resumed) {
	fmt.Println("Reconnected!")
	go func() {
		time.Sleep(3 * time.Second)
		b.LoadAllBacklogs()
	}()
	go s.UpdateStatus(0, "in the garbage")
}
