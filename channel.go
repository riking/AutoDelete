package autodelete

import (
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
	isStarted    bool
	liveMessages []smallMessage
}

type managedChannelMarshal struct {
	ID            string `yaml:"id"`
	GuildID       string `yaml:"guild_id"`
	ConfMessageID string `yaml:"conf_message_id"`
	LiveTime      time.Duration `yaml:"live_time"`
	MaxMessages   int `yaml:"max_messages"`
}

func (c *ManagedChannel) MarshalYAML() (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return managedChannelMarshal{
		ID:          c.Channel.ID,
		GuildID:     c.Channel.GuildID,
		LiveTime:    c.MessageLiveTime,
		MaxMessages: c.MaxMessages,
	}, nil
}

func LoadChannel(b *Bot, chConf managedChannelMarshal) (*ManagedChannel, error) {
	disCh, err := b.s.Channel(chConf.ID)
	if err != nil {
		return nil, err
	}
	return &ManagedChannel{
		bot:             b,
		Channel:         disCh,
		MessageLiveTime: chConf.LiveTime,
		MaxMessages:     chConf.MaxMessages,
		isStarted:       false,
		liveMessages:    nil,
	}, nil
}

func (c *ManagedChannel) LoadBacklog() error {
	msgs, err := c.bot.s.ChannelMessages(c.Channel.ID, 100, "", "", "")
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.liveMessages = make([]smallMessage, len(msgs))
	for i, v := range msgs {
		c.liveMessages[i].MessageID = v.ID
		c.liveMessages[i].PostedAt, err = v.Timestamp.Parse()
		if err != nil {
			panic("Timestamp format change")
		}
	}
	c.isStarted = true
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

func (c *ManagedChannel) Reap() (error) {
	msgs := c.collectMessagesToDelete()
	if len(msgs) == 0 {
		return nil
	}

	for len(msgs) > 50 {
		c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs[:50])
		msgs = msgs[50:]
	}
	return c.bot.s.ChannelMessagesBulkDelete(c.Channel.ID, msgs)
}

func (c *ManagedChannel) collectMessagesToDelete() ([]string) {
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
