package autodelete

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type smallMessage struct {
	MessageID string
	PostedAt  time.Time

	// implicit in which ManagedChannel this is a member of
	//ChannelID string
}

type ManagedChannel struct {
	bot     *Bot
	Channel *discordgo.Channel

	mu sync.Mutex
	// Messages posted to the channel get deleted after
	MessageLiveTime time.Duration
	MaxMessages     int
	// if false, need to check channel history for messages
	isStarted    chan struct{}
	liveMessages []smallMessage
}

func (c *ManagedChannel) Export() managedChannelMarshal {
	c.mu.Lock()
	defer c.mu.Unlock()

	return managedChannelMarshal{
		ID:          c.Channel.ID,
		GuildID:     c.Channel.GuildID,
		LiveTime:    c.MessageLiveTime,
		MaxMessages: c.MaxMessages,
	}
}

func InitChannel(b *Bot, chConf managedChannelMarshal) (*ManagedChannel, error) {
	disCh, err := b.s.Channel(chConf.ID)
	if err != nil {
		return nil, err
	}
	return &ManagedChannel{
		bot:             b,
		Channel:         disCh,
		MessageLiveTime: chConf.LiveTime,
		MaxMessages:     chConf.MaxMessages,
		isStarted:       make(chan struct{}),
		liveMessages:    nil,
	}, nil
}

func (c *ManagedChannel) LoadBacklog() error {
	msgs, err := c.bot.s.ChannelMessages(c.Channel.ID, 100, "", "", "")
	if err != nil {
		return err
	}
	fmt.Println("backlog for", c.Channel.ID, "len =", len(msgs))

	defer c.bot.QueueReap(c) // requires mutex unlocked
	c.mu.Lock()
	defer c.mu.Unlock()
	c.liveMessages = make([]smallMessage, len(msgs))
	for i, v := range msgs {
		c.liveMessages[len(msgs) - 1 - i].MessageID = v.ID
		c.liveMessages[len(msgs) - 1 - i].PostedAt, err = v.Timestamp.Parse()
		if err != nil {
			panic("Timestamp format change")
		}
	}

	// mark as ready for AddMessage()
	select {
	case <-c.isStarted:
	default:
		close(c.isStarted)
	}
	return nil
}

func (b *Bot) LoadAllBacklogs() {
	b.mu.RLock()
	for _, v := range b.channels {
		go v.LoadBacklog()
	}
	b.mu.RUnlock()
}

func (c *ManagedChannel) AddMessage(m *discordgo.Message) {
	<-c.isStarted

	needReap := false
	c.mu.Lock()
	if len(c.liveMessages) == 0 {
		needReap = true
	} else if len(c.liveMessages) == c.MaxMessages {
		needReap = true
	}
	c.liveMessages = append(c.liveMessages, smallMessage{
		MessageID: m.ID,
		PostedAt:  time.Now(),
	})
	c.mu.Unlock()

	if needReap {
		fmt.Println("channel", c.Channel.ID, "needs reap")
		c.bot.QueueReap(c)
	}
}

func (c *ManagedChannel) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.MessageLiveTime > 0 || c.MaxMessages > 0
}

func (c *ManagedChannel) SetLiveTime(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MessageLiveTime = d
}

func (c *ManagedChannel) SetMaxMessages(max int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MaxMessages = max
}

func (c *ManagedChannel) GetNextDeletionTime(ifNone time.Time) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.liveMessages) > c.MaxMessages {
		return time.Now()
	}
	if len(c.liveMessages) > 0 {
		return c.liveMessages[0].PostedAt.Add(c.MessageLiveTime)
	}
	return ifNone
}

func (c *ManagedChannel) Reap() error {
	msgs := c.collectMessagesToDelete()
	if len(msgs) == 0 {
		fmt.Println("no messages to clean")
		return nil
	}

	for len(msgs) > 50 {
		c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs[:50])
		msgs = msgs[50:]
	}
	return c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs)
}

func (c *ManagedChannel) collectMessagesToDelete() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete []string

	if c.MaxMessages > 0 {
		for len(c.liveMessages) > c.MaxMessages {
			toDelete = append(toDelete, c.liveMessages[0].MessageID)
			c.liveMessages = c.liveMessages[1:]
		}
	}
	if c.MessageLiveTime > 0 {
		cutoff := time.Now().Add(-c.MessageLiveTime)
		for len(c.liveMessages) > 0 && c.liveMessages[0].PostedAt.Before(cutoff) {
			toDelete = append(toDelete, c.liveMessages[0].MessageID)
			c.liveMessages = c.liveMessages[1:]
		}
	}

	return toDelete
}
