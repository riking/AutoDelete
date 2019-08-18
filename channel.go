package autodelete

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricBulkDeleteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "autodelete",
			Name:      "bulk_delete_errors",
			Help:      "distinct error codes from BulkDelete",
		},
		[]string{fieldErrorCode},
	)
	metricSingleDeleteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "autodelete",
			Name:      "single_delete_errors",
			Help:      "distinct error codes from MessageDelete",
		},
		[]string{fieldErrorCode},
	)
	metricBacklogErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "autodelete",
			Name:      "channel_messages_errors",
			Help:      "distinct error codes from LoadBacklog's ChannelMessages",
		},
		[]string{fieldErrorCode},
	)
	metricPinsErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "autodelete",
			Name:      "get_pins_errors",
			Help:      "distinct error codes from ChannelPins",
		},
		[]string{fieldErrorCode},
	)
)

func init() {
	prometheus.MustRegister(metricBulkDeleteFailures)
	prometheus.MustRegister(metricSingleDeleteFailures)
	prometheus.MustRegister(metricBacklogErrors)
	prometheus.MustRegister(metricPinsErrors)
}

type smallMessage struct {
	MessageID string
	PostedAt  time.Time

	// implicit in which ManagedChannel this is a member of
	//ChannelID string
}

const minTimeBetweenDeletion = time.Second * 5
const minTimeBetweenLoadBacklog = time.Second * 30
const backlogReloadLimit = 100
const backlogAutoReloadPreFraction = 0.8
const backlogAutoReloadDeleteFraction = 0.25

type ManagedChannel struct {
	// Atomic variables for use by the metrics collectors.
	// Must be the first fields of the struct.
	NumKeepMessages int64
	NumLiveMessages int64

	bot     *Bot
	Channel *discordgo.Channel
	GuildID string

	mu              sync.Mutex
	backlogMu       sync.Mutex // only for LoadBacklog()
	minNextDelete   time.Time  // channel cannot get sent to deletion before this time
	lastLoadBacklog time.Time  // last LoadBacklog call
	// Messages posted to the channel get deleted after
	MessageLiveTime time.Duration
	MaxMessages     int
	KeepMessages    []string
	// if lower than CriticalMsgSequence, need to send one
	LastSentUpdate int
	IsDonor        bool
	needsExport    bool
	// if false, need to check channel history for messages
	isStarted    chan struct{}
	liveMessages []smallMessage
	keepLookup   map[string]bool
	loadFailures time.Duration
}

func (c *ManagedChannel) Export() ManagedChannelMarshal {
	c.mu.Lock()
	defer c.mu.Unlock()

	return ManagedChannelMarshal{
		ID:             c.Channel.ID,
		GuildID:        c.Channel.GuildID,
		LiveTime:       c.MessageLiveTime,
		MaxMessages:    c.MaxMessages,
		LastSentUpdate: c.LastSentUpdate,
		KeepMessages:   c.KeepMessages,
		IsDonor:        c.IsDonor,
	}
}

func InitChannel(b *Bot, chConf ManagedChannelMarshal) (*ManagedChannel, error) {
	disCh, err := b.s.Channel(chConf.ID)
	if err != nil {
		return nil, err
	}
	needsExport := false
	if disCh.GuildID != chConf.GuildID {
		needsExport = true
	}
	return &ManagedChannel{
		bot:             b,
		Channel:         disCh,
		GuildID:         disCh.GuildID,
		minNextDelete:   time.Now(),
		MessageLiveTime: chConf.LiveTime,
		MaxMessages:     chConf.MaxMessages,
		LastSentUpdate:  chConf.LastSentUpdate,
		KeepMessages:    chConf.KeepMessages,
		NumKeepMessages: len(chConf.KeepMessages),
		IsDonor:         chConf.IsDonor,
		needsExport:     needsExport,
		isStarted:       make(chan struct{}),
		liveMessages:    nil,
		keepLookup:      make(map[string]bool),
	}, nil
}

func (c *ManagedChannel) loadPins() ([]*discordgo.Message, error) {
	disCh, err := c.bot.s.Channel(c.Channel.ID)
	if err != nil {
		return nil, err
	}
	if disCh.LastPinTimestamp == nil {
		return nil, nil
	} else {
		fmt.Println("[load]", "loading pins for", c.Channel.ID)
		// Inlined ChannelMessagesPinned with the ratelimit bucket replaced
		body, err := c.bot.s.RequestWithBucketID("GET", discordgo.EndpointChannelMessagesPins(c.Channel.ID), nil, "/custom/pinsGlobal")
		if err != nil {
			return nil, err
		}
		var st []*discordgo.Message
		err = json.Unmarshal(body, &st)
		return st, err
	}
}

func (c *ManagedChannel) LoadBacklogNow() {
	err := c.LoadBacklog()
	if isRetryableLoadError(err) {
		c.bot.QueueLoadBacklog(c, true)
	}
}

func (c *ManagedChannel) LoadBacklog() error {
	// prevent reentrancy, even during web requests
	c.backlogMu.Lock()
	defer c.backlogMu.Unlock()

	// Early exit if we got multiple calls
	earlyExit := false
	c.mu.Lock()
	if c.lastLoadBacklog.Add(minTimeBetweenLoadBacklog).After(time.Now()) {
		earlyExit = true
	} else {
		c.lastLoadBacklog = time.Now()
	}
	c.mu.Unlock()
	if earlyExit {
		fmt.Println("[WARN] Cancelling LoadBacklog for", c.Channel.ID, "due to <30s elapsed")
		return nil
	}
	// Clear the progress flag if we set it
	// Set time even on errors
	defer func() {
		c.mu.Lock()
		c.lastLoadBacklog = time.Now()
		c.mu.Unlock()
	}()

	// Load messages & pins
	msgs, err := c.bot.s.ChannelMessages(c.Channel.ID, 100, "", "", "")
	reportDiscordError(metricBacklogErrors, err)
	if err != nil {
		fmt.Println("[ERR ] could not load backlog for", c.Channel.ID, err)
		return err
	}
	pins, pinsErr := c.loadPins()
	reportDiscordError(metricPinsErrors, err)
	if pinsErr != nil {
		fmt.Println("[ERR ] could not load pins for", c.Channel.ID, pinsErr)

		// experiment with a notice
		//c.bot.s.ChannelMessageSend(c.Channel.ID,
		//	":warning: Failed to load channel pins, may accidentally delete them",
		//)
		return pinsErr
	}

	defer c.bot.QueueReap(c) // requires mutex unlocked
	c.mu.Lock()
	defer c.mu.Unlock()

	c.loadFailures = 0
	c.keepLookup = make(map[string]bool)
	for i := range pins {
		c.keepLookup[pins[i].ID] = true
	}
	for _, v := range c.KeepMessages {
		c.keepLookup[v] = true
	}

	c.liveMessages = make([]smallMessage, 0, len(msgs))
	// Iterate backwards so we swap the order
	for i := len(msgs); i > 0; i-- {
		v := msgs[i-1]

		// Check for non-deletion
		if c.keepLookup[v.ID] {
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

	// metric export
	atomic.StoreInt64(&c.NumKeepMessages, len(c.keepLookup))
	atomic.StoreInt64(&c.NumLiveMessages, len(c.liveMessages))
	fmt.Printf("[load] %s #%s %s, %d msgs %d keeps\n", c.Channel.ID, c.Channel.Name, inited, len(c.liveMessages), len(c.keepLookup))
	return nil
}

func (b *Bot) LoadAllBacklogs() {
	b.mu.RLock()
	for _, v := range b.channels {
		if v != nil {
			go v.LoadBacklogNow()
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
	if c.keepLookup[m.ID] {
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
	atomic.StoreInt64(&c.NumLiveMessages, len(c.liveMessages))
	c.mu.Unlock()

	if needReap {
		c.bot.QueueReap(c)
	}
}

// UpdatePins gets called in two situations - a pin was added, a pin was
// removed, or more than one of those happened too fast for us to notice.
func (c *ManagedChannel) UpdatePins(newLpts string) {
	var dropMsgs []string
	defer func() {
		// This is not the best, as the pins will be deleted
		// non-chronologically, but it avoids chopping the backlog back to 100
		// messages.
		for _, v := range dropMsgs {
			msg, err := c.bot.s.ChannelMessage(c.Channel.ID, v)
			if err == nil {
				c.AddMessage(msg)
			}
		}
	}()
	c.mu.Lock()
	defer c.mu.Unlock()

	pins, err := c.bot.s.ChannelMessagesPinned(c.Channel.ID)
	if err != nil {
		fmt.Println("[pins] could not load pins for", c.Channel.ID, err)
		return
	}

	newKeep := make(map[string]bool)

	for _, v := range pins {
		newKeep[v.ID] = true
	}
	for _, v := range c.KeepMessages {
		newKeep[v] = true
	}

	for id := range c.keepLookup {
		if !newKeep[id] {
			dropMsgs = append(dropMsgs, id)
		}
	}

	fmt.Println("[pins] update for", c.Channel.ID, c.Channel.Name, "-", len(newKeep), "keep", len(dropMsgs), "drop")
	c.keepLookup = newKeep
	atomic.StoreInt64(&c.NumKeepMessages, len(c.keepLookup))
	atomic.StoreInt64(&c.NumLiveMessages, len(c.liveMessages))
	// deferred function calls AddMessage for each of dropMsgs
}

// DoNotDeleteMessage marks a message ID as not for deletion.
//
// - only called from UpdatePins()
// - does not update Num* fields, UpdatePins will do that
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

	for len(c.liveMessages) > 0 {
		// Recheck keepLookup
		if c.keepLookup[c.liveMessages[0].MessageID] {
			c.liveMessages = c.liveMessages[1:]
			continue
		}
		break
	}
	if len(c.liveMessages) == 0 {
		return time.Now().Add(240 * time.Hour)
	}

	if c.MaxMessages > 0 && len(c.liveMessages) > c.MaxMessages {
		return c.minNextDelete
	}
	if c.MessageLiveTime != 0 {
		ts := c.liveMessages[0].PostedAt.Add(c.MessageLiveTime)
		if ts.Before(c.minNextDelete) {
			return c.minNextDelete
		}
		return ts
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
			reportDiscordError(metricBulkDeleteFailures, err)
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
		reportDiscordError(metricBulkDeleteFailures, err)
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
			reportDiscordError(metricSingleDeleteFailures, err)
			if err != nil {
				fmt.Println("Error in single-message delete:", err, c.Channel.ID, msg)
			}
		}
		// re-load the backlog in case this surfaced more things to delete
		c.bot.QueueLoadBacklog(c, false)
	}()
	return -1, nil
}

var (
	metricMessagesDeleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "autodelete",
			Name:      "messages_deleted_total",
			Help:      "counts how many messages collectMessagesToDelete returns",
		},
		[]string{"horizon_recheck"},
	)
	horizonRecheckNo  = prometheus.Labels{"horizon_recheck": "no"}
	horizonRecheckYes = prometheus.Labels{"horizon_recheck": "yes"}
)

func init() { prometheus.MustRegister(metricMessagesDeleted) }

// returns and removes the messages that need to be deleted right now.
//
// also sets the minNextDelete and returns whether we think there could be more
// messages past the backlog horizon
func (c *ManagedChannel) collectMessagesToDelete() ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.minNextDelete = time.Now().Add(minTimeBetweenDeletion)

	var toDelete []string
	var oldest time.Time
	var zero time.Time

	nLiveMessages := len(c.liveMessages)

	if c.MaxMessages > 0 {
		for len(c.liveMessages) > c.MaxMessages {
			if !c.keepLookup[c.liveMessages[0].MessageID] {
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
			if !c.keepLookup[c.liveMessages[0].MessageID] {
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
				if !c.keepLookup[c.liveMessages[0].MessageID] {
					toDelete = append(toDelete, c.liveMessages[0].MessageID)
				}
				c.liveMessages = c.liveMessages[1:]
			}
		}
	}

	atomic.StoreInt64(&c.NumLiveMessages, len(c.liveMessages))

	horizonRecheck := ((nLiveMessages >= backlogReloadLimit*backlogAutoReloadPreFraction) &&
		(len(toDelete) > backlogReloadLimit*backlogAutoReloadDeleteFraction))
	if horizonRecheck {
		metricMessagesDeleted.With(horizonRecheckYes).Add(len(toDelete))
	} else {
		metricMessagesDeleted.With(horizonRecheckNo).Add(len(toDelete))
	}

	return toDelete, horizonRecheck
}
