package autodelete

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
	"strings"
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
			go fun(b, m.Message)
			return
		}
	}
	fmt.Println("got mention:", m.Message.Content)
}

func (b *Bot) OnMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	fmt.Println(m.Message)

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
	fmt.Println("channel create", ch.ID)
	b.loadChannel(ch.ID)
}

func (b *Bot) OnReady(s *discordgo.Session, m *discordgo.Ready) {
	fmt.Println("ready")
	for _, v := range m.Guilds {
		fmt.Println(v.Channels)
	}
}

func (b *Bot) OnResume(s *discordgo.Session, r *discordgo.Resumed) {
	fmt.Println("Reconnected!")
	b.LoadAllBacklogs()
}