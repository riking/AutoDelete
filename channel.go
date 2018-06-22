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
	HasPins        bool
	IsDonor        bool
	// if false, need to check channel history for messages
	isStarted    chan struct{}
	liveMessages []smallMessage
	pinMessages  []smallMessage
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
		HasPins:        c.HasPins,
		IsDonor:        c.IsDonor,
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
		HasPins:         chConf.HasPins,
		IsDonor:         chConf.IsDonor,
		isStarted:       make(chan struct{}),
		liveMessages:    nil,
	}, nil
}

func (c *ManagedChannel) loadPins() ([]*discordgo.Message, error) {
	c.mu.Lock()
	hasPins := c.HasPins
	c.mu.Unlock()
	if !hasPins {
		return nil, nil
	}
	return c.bot.s.ChannelMessagesPinned(c.Channel.ID)
}

func (c *ManagedChannel) LoadBacklog() error {
	msgs, err := c.bot.s.ChannelMessages(c.Channel.ID, 100, "", "", "")
	if err != nil {
		fmt.Println("could not load backlog for", c.Channel.ID, err)
		return err
	}
	pins, pinsErr := c.loadPins()
	if pinsErr != nil {
		fmt.Println("could not load pins for", c.Channel.ID, pinsErr)
		//return err
	}

	defer c.bot.QueueReap(c) // requires mutex unlocked
	c.mu.Lock()
	defer c.mu.Unlock()

	var newPinMessages []smallMessage
	var quickPinLookup = make(map[string]struct{})
	if pinsErr != nil {
		newPinMessages = c.pinMessages
		for _, v := range newPinMessages {
			quickPinLookup[v.MessageID] = struct{}{}
		}
	} else {
		newPinMessages = make([]smallMessage, 0, len(pins))
		for i := range pins {
			ts, err := pins[i].Timestamp.Parse()
			if err != nil {
				panic("Timestamp format change")
			}
			if ts.IsZero() {
				continue
			}
			newPinMessages = append(newPinMessages, smallMessage{
				MessageID: pins[i].ID,
				PostedAt:  ts,
			})
			quickPinLookup[pins[i].ID] = struct{}{}
		}
	}

	c.liveMessages = make([]smallMessage, 0, len(msgs))
	// Iterate backwards so we swap the order
	for i := len(msgs); i > 0; i-- {
		v := msgs[i-1]

		// Check for non-deletion
		keep := false
		if v.ID == c.ConfMessageID {
			keep = true
		}
		if _, found := quickPinLookup[v.ID]; found {
			keep = true
		}
		if keep {
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
	inited := "reloaded"
	select {
	case <-c.isStarted:
	default:
		close(c.isStarted)
		inited = "initialized"
	}
	fmt.Printf("[load] %s #%s %s, %d msgs %d pins\n", c.Channel.ID, c.Channel.Name, inited, len(c.liveMessages), len(c.pinMessages))
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

	// if m.Type == discordgo.MessageTypeChannelPinnedMessage {
	//	fmt.Println("[DEBUG]", "Got pinning message", m)
	// }

	c.mu.Lock()
	// Check for nondeletion
	// don't need a pin check here, it's a brand new message
	keep := false
	if m.ID == c.ConfMessageID {
		keep = true
	}
	if keep {
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
		c.bot.QueueReap(c)
	}
}

// UpdatePins gets called in two situations - a pin was added, a pin was
// removed, or more than one of those happened too fast for us to notice.
func (c *ManagedChannel) UpdatePins(hasPins bool) {
	c.mu.Lock()
	oldHasPins := c.HasPins
	c.HasPins = hasPins
	c.mu.Unlock()

	if oldHasPins != hasPins {
		c.bot.saveChannelConfig(c.Export())
	}

	if hasPins {
		c.LoadBacklog()
	}

	/*
		pins, err := c.bot.s.ChannelMessagesPinned(c.Channel.ID)
		if err != nil {
			fmt.Println("could not load pins for", c.Channel.ID, err)
			return
		}
		newPins := make(map[string]struct{})
		remPins := make(map[string]struct{})

		for _, v := range pins {
			newPins[v.ID] = struct{}{}
		}
		c.mu.Lock()
		for _, v := range c.pinMessages {
			_, matched := newPins[v.MessageID]
			if matched {
				delete(newPins, v.MessageID)
			} else {
				remPins[v.MessageID] = struct{}{}
			}
		}
		c.mu.Unlock()

		fmt.Println("pins update for", c.Channel.ID, c.Channel.Name, "-", len(newPins), "added,", len(remPins), "removed")
	*/
	// if len(remPins) > 0 || len(newPins) > 0 {
	// c.LoadBacklog()
	// }
	// Doesn't work -- AddMessage works chronologically
	// for msgID := range remPins {
	// 	msg := c.b.s.ChannelMessage(c.Channel.ID, msgID)
	// 	c.AddMessage(msg)
	// }
}

// DoNotDeleteMessage marks a message ID as not for deletion.
// only called from UpdatePins()
func (c *ManagedChannel) DoNotDeleteMessage(msgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := -1

	for i, v := range c.liveMessages {
		if v.MessageID == msgID {
			idx = i
		}
	}
	if idx == -1 {
		fmt.Println("[BUG] DoNotDeleteMessage called with non-live message")
		return
	}
	lenMinus1 := len(c.liveMessages) - 1
	// Delete item
	copy(c.liveMessages[idx:], c.liveMessages[idx+1:])
	c.liveMessages[lenMinus1] = smallMessage{}
	c.liveMessages = c.liveMessages[:lenMinus1]
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

func (c *ManagedChannel) Reap(msgs []string) (int, error) {
	var err error
	count := 0

nobulk:
	switch {
	case true:
		for len(msgs) > 50 {
			err := c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs[:50])
			if rErr, ok := err.(*discordgo.RESTError); ok {
				if rErr.Message != nil && rErr.Message.Code == errCodeBulkDeleteOld {
					break nobulk
				}
				return count, err
			} else if err != nil {
				return count, err
			}
			msgs = msgs[50:]
			count += 50
		}
		err = c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs)
		count += len(msgs)
		if rErr, ok := err.(*discordgo.RESTError); ok {
			if rErr.Message != nil && rErr.Message.Code == errCodeBulkDeleteOld {
				break nobulk
			}
			return count, err
		} else if err != nil {
			return count, err
		}
		return count, nil
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
	return -1, nil
}

func (c *ManagedChannel) collectMessagesToDelete() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete []string
	var oldest time.Time
	var zero time.Time

	if c.MaxMessages > 0 {
		for len(c.liveMessages) > c.MaxMessages {
			if c.liveMessages[0].MessageID != c.ConfMessageID {
				toDelete = append(toDelete, c.liveMessages[0].MessageID)
				if oldest == zero {
					oldest = c.liveMessages[0].PostedAt
				}
			}
			c.liveMessages = c.liveMessages[1:]
		}
	}
	if c.MessageLiveTime > 0 {
		cutoff := time.Now().Add(-c.MessageLiveTime)
		for len(c.liveMessages) > 0 && c.liveMessages[0].PostedAt.Before(cutoff) {
			if c.liveMessages[0].MessageID != c.ConfMessageID {
				toDelete = append(toDelete, c.liveMessages[0].MessageID)
				if oldest == zero {
					oldest = c.liveMessages[0].PostedAt
				}
			}
			c.liveMessages = c.liveMessages[1:]
		}
		// Collect additional messages within 1.5sec of deleted message
		if oldest != zero {
			cutoff = oldest.Add(1500 * time.Millisecond)
			for len(c.liveMessages) > 0 && c.liveMessages[0].PostedAt.Before(cutoff) {
				if c.liveMessages[0].MessageID != c.ConfMessageID {
					toDelete = append(toDelete, c.liveMessages[0].MessageID)
				}
				c.liveMessages = c.liveMessages[1:]
			}
		}
	}

	return toDelete
}
