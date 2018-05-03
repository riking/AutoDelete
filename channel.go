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
	ConfMessageID   string
	// if lower than CriticalMsgSequence, need to send one
	LastSentUpdate int
	// if false, need to check channel history for messages
	isStarted    chan struct{}
	liveMessages []smallMessage
}

func (c *ManagedChannel) Export() managedChannelMarshal {
	c.mu.Lock()
	defer c.mu.Unlock()

	return managedChannelMarshal{
		ID:             c.Channel.ID,
		LiveTime:       c.MessageLiveTime,
		MaxMessages:    c.MaxMessages,
		LastSentUpdate: c.LastSentUpdate,
		ConfMessageID:  c.ConfMessageID,
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
		LastSentUpdate:  chConf.LastSentUpdate,
		ConfMessageID:   chConf.ConfMessageID,
		isStarted:       make(chan struct{}),
		liveMessages:    nil,
	}, nil
}

func (c *ManagedChannel) LoadBacklog() error {
	msgs, err := c.bot.s.ChannelMessages(c.Channel.ID, 100, "", "", "")
	if err != nil {
		fmt.Println("could not load backlog for", c.Channel.ID, err)
		return err
	}
	fmt.Println("backlog for", c.Channel.ID, "len =", len(msgs))

	defer c.bot.QueueReap(c) // requires mutex unlocked
	c.mu.Lock()
	defer c.mu.Unlock()
	c.liveMessages = make([]smallMessage, 0, len(msgs))
	for i := len(msgs); i > 0; i-- {
		v := msgs[i-1]
		if v.ID == c.ConfMessageID {
			continue
		}
		ts, err := v.Timestamp.Parse()
		if err != nil {
			panic("Timestamp format change")
		}
		if ts.IsZero() {
			continue
		}
		c.liveMessages = append(c.liveMessages, smallMessage{
			MessageID: v.ID,
			PostedAt:  ts,
		})
	}

	// mark as ready for AddMessage()
	select {
	case <-c.isStarted:
	default:
		close(c.isStarted)
		fmt.Println("Initialized auto-deletion for", c.Channel.Name, c.Channel.ID)
	}
	return nil
}

func (b *Bot) LoadAllBacklogs() {
	b.mu.RLock()
	for _, v := range b.channels {
		if v != nil {
			go v.LoadBacklog()
		}
	}
	b.mu.RUnlock()
}

func (c *ManagedChannel) AddMessage(m *discordgo.Message) {
	<-c.isStarted
	needReap := false

	c.mu.Lock()
	if m.ID == c.ConfMessageID {
		c.mu.Unlock()
		return
	}

	if len(c.liveMessages) == 0 {
		needReap = true
	} else if c.MaxMessages > 0 && len(c.liveMessages) == c.MaxMessages {
		needReap = true
	}

	c.liveMessages = append(c.liveMessages, smallMessage{
		MessageID: m.ID,
		PostedAt:  time.Now(),
	})
	c.mu.Unlock()

	if needReap {
		fmt.Println("channel", c.Channel.ID, "needs reap, new message:")
		fmt.Println(m)
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

func (c *ManagedChannel) GetNextDeletionTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.liveMessages) == 0 {
		return time.Now().Add(240 * time.Hour)
	}

	if c.liveMessages[0].MessageID == c.ConfMessageID {
		c.liveMessages = c.liveMessages[1:]
	}
	if c.MaxMessages > 0 && len(c.liveMessages) > c.MaxMessages {
		return time.Now()
	}
	if c.MessageLiveTime != 0 {
		return c.liveMessages[0].PostedAt.Add(c.MessageLiveTime)
	}
	return time.Now().Add(240 * time.Hour)
}

const errCodeBulkDeleteOld = 50034

func (c *ManagedChannel) Reap() error {
	msgs := c.collectMessagesToDelete()
	if len(msgs) == 0 {
		fmt.Println("no messages to clean")
		return nil
	}
	var err error

nobulk:
	switch {
	case true:
		for len(msgs) > 50 {
			err := c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs[:50])
			if rErr, ok := err.(*discordgo.RESTError); ok {
				if rErr.Message != nil && rErr.Message.Code == errCodeBulkDeleteOld {
					break nobulk
				}
				return err
			} else if err != nil {
				return err
			}
			msgs = msgs[50:]
		}
		err = c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs)
		if rErr, ok := err.(*discordgo.RESTError); ok {
			if rErr.Message != nil && rErr.Message.Code == errCodeBulkDeleteOld {
				break nobulk
			}
			return err
		} else if err != nil {
			return err
		}
		return nil
	}

	// single delete required
	// Spin up a separate goroutine - this could take a while
	go func() {
		for _, msg := range msgs {
			err = c.bot.s.ChannelMessageDelete(c.Channel.ID, msg)
			if err != nil {
				fmt.Println("Error in single-message delete:", err, c.Channel.ID, msg)
			}
		}
		// re-load the backlog in case this surfaced more things to delete
		c.LoadBacklog()
	}()
	return nil
}

func (c *ManagedChannel) collectMessagesToDelete() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete []string

	if c.MaxMessages > 0 {
		for len(c.liveMessages) > c.MaxMessages {
			if c.liveMessages[0].MessageID != c.ConfMessageID {
				toDelete = append(toDelete, c.liveMessages[0].MessageID)
			}
			c.liveMessages = c.liveMessages[1:]
		}
	}
	if c.MessageLiveTime > 0 {
		cutoff := time.Now().Add(-c.MessageLiveTime)
		for len(c.liveMessages) > 0 && c.liveMessages[0].PostedAt.Before(cutoff) {
			if c.liveMessages[0].MessageID != c.ConfMessageID {
				toDelete = append(toDelete, c.liveMessages[0].MessageID)
			}
			c.liveMessages = c.liveMessages[1:]
		}
	}

	return toDelete
}
