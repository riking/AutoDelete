package autodelete

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/prometheus/client_golang/prometheus"

	topk "github.com/riking/AutoDelete/go-prometheus-topk"
)

const minTimeBetweenDeletion = time.Second * 5
const minTimeBetweenLoadBacklog = time.Millisecond * 30
const (
	backlogAutoReloadPreFraction    = 0.8
	backlogAutoReloadDeleteFraction = 0.25
	backlogChunkLimit               = 100
)

var (
	backlogLimitNonDonor = 200
	backlogLimitDonor    = 1000
)

var (
	mBacklogLoadLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "backlog_load_seconds",
		Help:      "Latency of LoadBacklog calls.",
		Buckets:   bucketsDiscordAPI,
	})
	mPinLoadLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "pins_load_seconds",
		Help:      "Latency of loadPins calls.",
		Buckets:   bucketsDiscordAPI,
	})
	mNextDeletionTimes = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "next_deletion_time_seconds",
		Help:      "Time until next message in channel is due to be deleted.",
		Buckets:   bucketsDeletionTimes,
	})
	mNoNextDeletionTimeCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "next_deletion_time_none_total",
		Help:      "Number of times that '10 days from now' is returned from GetNextDeletionTime",
	})
	mReapLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "reap_seconds",
		Help:      "Latency of message deletion (Reap) calls.",
		Buckets:   bucketsDiscordAPI,
	})
	mDeletionChunks = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "message_reaps_chunksize",
		Help:      "Number of messages deleted per Reap call. Total count deleted is sum(this).",
		Buckets:   bucketsMessageCounts,
	})
	mReapErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "message_reap_errors_total",
		Help:      "Number of errors encountered when deleting messages",
	}, []string{"error_code"})
	mSingleMessageReapErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "message_reap_single_errors_total",
		Help:      "Number of errors encountered when single-deleting messages",
	}, []string{"error_code"})
	mTopDeletionChannels = topk.NewTopK(topk.TopKOpts{
		Namespace:          nsAutodelete,
		Name:               "message_reaps_by_channel",
		Help:               "Top-K of channels with the most messages deleted",
		Buckets:            50,
		ReportingThreshold: 150,
	}, []string{"channel_id"})
	mTopDeletionGuilds = topk.NewTopK(topk.TopKOpts{
		Namespace:          nsAutodelete,
		Name:               "message_reaps_by_guild",
		Help:               "Top-K of guilds with the most messages deleted",
		Buckets:            50,
		ReportingThreshold: 150,
	}, []string{"guild_id"})
)

func init() {
	prometheus.MustRegister(mBacklogLoadLatency)
	prometheus.MustRegister(mPinLoadLatency)
	prometheus.MustRegister(mNextDeletionTimes)
	prometheus.MustRegister(mNoNextDeletionTimeCount)
	prometheus.MustRegister(mReapLatency)
	prometheus.MustRegister(mDeletionChunks)
	prometheus.MustRegister(mReapErrors)
	prometheus.MustRegister(mSingleMessageReapErrors)
	prometheus.MustRegister(mTopDeletionChannels)
	prometheus.MustRegister(mTopDeletionGuilds)
}

type smallMessage struct {
	MessageID string
	PostedAt  time.Time
}

// A ManagedChannel holds all the AutoDelete-related state for a Discord channel.
type ManagedChannel struct {
	bot         *Bot
	ChannelID   string
	ChannelName string
	GuildID     string

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

	// If true, this ManagedChannel has been disabled; the Bot might have a
	// new version. The reaper thread should throw it out.
	// Observed in the return of collectMessagesToDelete.
	killBit bool

	// if false, need to check channel history for messages
	isStarted chan struct{}
	// liveMessages contains a list of message IDs and the timestamp they
	// were posted at, listing the candidates for deletion in this channel.
	// It should always be sorted with the oldest messages at index 0 and
	// the newer messages at higher indices.
	liveMessages []smallMessage
	// Set of message IDs that need to be kept and not deleted.
	keepLookup map[string]bool
	// Used in queue.go for exponential backoff
	loadFailures time.Duration
}

func InitChannel(b *Bot, chConf ManagedChannelMarshal) (*ManagedChannel, error) {
	disCh, err := b.Channel(chConf.ID)
	if err != nil {
		return nil, err
	}
	needsExport := false
	if disCh.GuildID != chConf.GuildID {
		needsExport = true
	}
	return &ManagedChannel{
		bot:             b,
		ChannelID:       disCh.ID,
		ChannelName:     disCh.Name,
		GuildID:         disCh.GuildID,
		minNextDelete:   time.Now(),
		MessageLiveTime: chConf.LiveTime,
		MaxMessages:     chConf.MaxMessages,
		LastSentUpdate:  chConf.LastSentUpdate,
		KeepMessages:    chConf.KeepMessages,
		IsDonor:         chConf.IsDonor,
		needsExport:     needsExport,
		isStarted:       make(chan struct{}),
		liveMessages:    nil,
		keepLookup:      make(map[string]bool),
	}, nil
}

func (c *ManagedChannel) Export() ManagedChannelMarshal {
	c.mu.Lock()
	defer c.mu.Unlock()

	return ManagedChannelMarshal{
		ID:             c.ChannelID,
		GuildID:        c.GuildID,
		LiveTime:       c.MessageLiveTime,
		MaxMessages:    c.MaxMessages,
		LastSentUpdate: c.LastSentUpdate,
		KeepMessages:   c.KeepMessages,
		IsDonor:        c.IsDonor,
	}
}

func (c *ManagedChannel) String() string {
	return fmt.Sprintf("%s #%s", c.ChannelID, c.ChannelName)
}

func (c *ManagedChannel) IsDisabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killBit
}

// Remove this channel from all relevant datastructures.
//
// Must be called with no locks held. Takes Bot, self, and reapq locks.
// Can be called on a fake ManagedChannel instance (e.g. (&ManagedChannel{ChannelID: ...}).Disable()), so the only member assumed valid is bot and ChannelID.
func (c *ManagedChannel) Disable() {
	// first: block anything from finding us
	c.bot.mu.Lock()
	delete(c.bot.channels, c.ChannelID)
	c.bot.mu.Unlock()

	// reset internal state
	c.mu.Lock()
	c.liveMessages = nil
	c.keepLookup = nil

	c.killBit = true // ensure reapq gets our drop message
	c.mu.Unlock()

	// drop from reapq
	c.bot.CancelReap(c)
}

// Get a discord Channel. Results are cached in the library State.
func (b *Bot) Channel(channelID string) (*discordgo.Channel, error) {
	ch, _ := b.s.State.Channel(channelID)
	if ch != nil {
		return ch, nil
	}
	ch, err := b.s.Channel(channelID)
	if err != nil {
		return ch, err
	}
	b.s.State.ChannelAdd(ch)
	return ch, nil
}

const useRatelimitWorkaround = true
const useAlternateRatelimiter = false

var alternateRL = discordgo.NewRatelimiter()

func (c *ManagedChannel) loadPins() ([]*discordgo.Message, error) {
	// timing note: should always be cached
	disCh, err := c.bot.Channel(c.ChannelID)
	if err != nil {
		return nil, err
	}

	if disCh.LastPinTimestamp == "" {
		return nil, nil
	}

	//<-pinsGlobalRatelimit

	timer := prometheus.NewTimer(mPinLoadLatency)
	defer timer.ObserveDuration()

	// https://github.com/bwmarrin/discordgo/issues/712
	// This is, in fact, not a hack - it is the actual ratelimit bucket.
	if useRatelimitWorkaround {
		fmt.Printf("[load] %s: loading pins\n", c)
		// Inlined ChannelMessagesPinned with the ratelimit bucket replaced
		var body []byte
		var err error
		if useAlternateRatelimiter {
			// the string "//reactions//" gets a special ratelimit applied
			body, err = c.bot.s.RequestWithLockedBucket("GET", discordgo.EndpointChannelMessagesPins(c.ChannelID), "application/json", nil, alternateRL.LockBucket(fmt.Sprintf("/custom/pins/%s//reactions//x", c.ChannelID)), 0)
		} else {
			body, err = c.bot.s.RequestWithBucketID("GET", discordgo.EndpointChannelMessagesPins(c.ChannelID), nil, "/custom/pinsGlobal")
		}
		if err != nil {
			return nil, err
		}
		var st []*discordgo.Message
		err = json.Unmarshal(body, &st)
		return st, err
	} else {
		return c.bot.s.ChannelMessagesPinned(c.ChannelID)
	}
}

func (c *ManagedChannel) LoadBacklogNow() {
	err := c.LoadBacklog()
	if isRetryableLoadError(err) {
		c.bot.QueueLoadBacklog(c, QOSLoadError)
	}
}

func (c *ManagedChannel) LoadBacklog() error {
	timer := prometheus.NewTimer(mBacklogLoadLatency)
	defer timer.ObserveDuration()

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
		fmt.Println("[WARN] Cancelling LoadBacklog for", c, "due to <30s elapsed")
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
	msgsA, err := c.bot.s.ChannelMessages(c.ChannelID, backlogChunkLimit, "", "", "")
	if err != nil {
		fmt.Println("[ERR ] could not load backlog for", c, err)
		return err
	}
	msgs := msgsA
	limit := backlogLimitNonDonor
	if c.IsDonor {
		limit = backlogLimitDonor
	}
	for len(msgsA) == backlogChunkLimit && len(msgs) < limit {
		fmt.Println("[TEST] Loading extended backlog for", c, len(msgsA))
		before := msgs[len(msgs)-1].ID

		msgsA, err = c.bot.s.ChannelMessages(c.ChannelID, backlogChunkLimit, before, "", "")
		if err != nil {
			fmt.Println("[ERR ] could not load backlog for", c, err)
			return err
		}

		msgs = append(msgs, msgsA...)
	}

	pins, pinsErr := c.loadPins()
	if pinsErr != nil {
		fmt.Println("[ERR ] could not load pins for", c, pinsErr)

		// experiment with a notice
		//c.bot.s.ChannelMessageSend(c.ChannelID,
		//	":warning: Failed to load channel pins, may accidentally delete them",
		//)
		return pinsErr
	}

	defer c.bot.QueueReap(c) // requires mutex unlocked
	c.mu.Lock()
	defer c.mu.Unlock()

	c.keepLookup = make(map[string]bool)
	for i := range pins {
		c.keepLookup[pins[i].ID] = true
	}
	for _, v := range c.KeepMessages {
		c.keepLookup[v] = true
	}

	c.mergeBacklog(msgs)

	// mark as ready for AddMessage()
	inited := "reloaded"
	select {
	case <-c.isStarted:
	default:
		close(c.isStarted)
		inited = "initialized"
	}
	fmt.Printf("[load] %s %s, %d msgs %d keeps\n", c.String(), inited, len(c.liveMessages), len(c.keepLookup))
	return nil
}

func (c *ManagedChannel) mergeBacklog(msgs []*discordgo.Message) {
	var (
		oldLiveMessages = c.liveMessages
		newLiveMessages = make([]smallMessage, 0, len(msgs))
		iOld            int
	)
	sort.Sort(liveMessagesSort(oldLiveMessages))
	// Iterate backwards so we put oldest first
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
		// equal messages will break this loop
		for iOld < len(oldLiveMessages) && ts.After(oldLiveMessages[iOld].PostedAt) {
			newLiveMessages = append(newLiveMessages, oldLiveMessages[iOld])
			iOld++
		}
		// handle equal messages
		if iOld < len(oldLiveMessages) && oldLiveMessages[iOld].MessageID == v.ID {
			iOld++
		}
		newLiveMessages = append(newLiveMessages, smallMessage{
			MessageID: v.ID,
			PostedAt:  ts,
		})
	}
	sort.Sort(liveMessagesSort(newLiveMessages))
	c.liveMessages = newLiveMessages
}

type liveMessagesSort []smallMessage

func (s liveMessagesSort) Len() int      { return len(s) }
func (s liveMessagesSort) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s liveMessagesSort) Less(i, j int) bool {
	return s[i].PostedAt.Before(s[j].PostedAt)
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
			msg, err := c.bot.s.ChannelMessage(c.ChannelID, v)
			if err == nil {
				c.AddMessage(msg)
			}
		}
	}()
	c.mu.Lock()
	defer c.mu.Unlock()

	pins, err := c.bot.s.ChannelMessagesPinned(c.ChannelID)
	if err != nil {
		fmt.Println("[pins] could not load pins for", c, err)
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

	fmt.Println("[pins] update for", c, "-", len(newKeep), "keep", len(dropMsgs), "drop")
	c.keepLookup = newKeep
	// deferred function calls AddMessage for each of dropMsgs
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
	return !c.killBit && (c.MessageLiveTime > 0 || c.MaxMessages > 0)
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

func (c *ManagedChannel) GetNextDeletionTime() (deadline time.Time) {
	defer func() {
		x := time.Until(deadline)
		if 863900*time.Second <= x && x <= 864100*time.Second {
			mNoNextDeletionTimeCount.Inc()
		} else if x <= 0 {
			mNextDeletionTimes.Observe(0)
		} else {
			mNextDeletionTimes.Observe(float64(time.Until(deadline)) / float64(time.Second))
		}
	}()
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
		ts := c.liveMessages[c.MaxMessages].PostedAt
		if ts.Before(c.minNextDelete) {
			return c.minNextDelete
		}
		return ts
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

type isTemporary interface {
	error
	Temporary() bool
}

func (c *ManagedChannel) Reap(msgs []string) (int, error) {
	var err error
	count := 0

	timer := prometheus.NewTimer(mReapLatency)
	defer timer.ObserveDuration()
	mDeletionChunks.Observe(float64(len(msgs)))
	mTopDeletionChannels.WithLabelValues(c.ChannelID).Observe(float64(len(msgs)))
	mTopDeletionGuilds.WithLabelValues(c.GuildID).Observe(float64(len(msgs)))

	if len(msgs) == 0 {
		// All messages were deleted by other bots first
		return 0, nil
	}

nobulk:
	switch {
	case true:
		for len(msgs) > 50 {
			err := c.bot.s.ChannelMessagesBulkDelete(c.ChannelID, msgs[:50])
			if rErr, ok := err.(*discordgo.RESTError); ok {
				if rErr.Message != nil {
					mReapErrors.With(prometheus.Labels{"error_code": strconv.Itoa(rErr.Message.Code)}).Inc()
					if rErr.Message.Code == errCodeBulkDeleteOld {
						break nobulk
					}
				}
				return count, err
			} else if tErr, ok := err.(isTemporary); ok && tErr.Temporary() {
				// Temporary error, try again
				mReapErrors.With(prometheus.Labels{"error_code": fmt.Sprintf("other(%T)", err)}).Inc()
				time.Sleep(50 * time.Millisecond)
				continue
			} else if err != nil {
				mReapErrors.With(prometheus.Labels{"error_code": fmt.Sprintf("other(%T)", err)}).Inc()
				return count, err
			}
			msgs = msgs[50:]
			count += 50
		}
		for {
			err = c.bot.s.ChannelMessagesBulkDelete(c.ChannelID, msgs)
			count += len(msgs)
			if rErr, ok := err.(*discordgo.RESTError); ok {
				if rErr.Message != nil {
					mReapErrors.With(prometheus.Labels{"error_code": strconv.Itoa(rErr.Message.Code)}).Inc()
					if rErr.Message.Code == errCodeBulkDeleteOld {
						break nobulk
					}
				}
				return count, err
			} else if tErr, ok := err.(isTemporary); ok && tErr.Temporary() {
				// Temporary error, try again
				mReapErrors.With(prometheus.Labels{"error_code": fmt.Sprintf("other(%T)", err)}).Inc()
				time.Sleep(50 * time.Millisecond)
				continue
			} else if err != nil {
				mReapErrors.With(prometheus.Labels{"error_code": fmt.Sprintf("other(%T)", err)}).Inc()
				return count, err
			}
			return count, nil
		}
	}

	// single-message delete required
	// Spin up a separate goroutine - this could take a while
	go func() {
		for _, msg := range msgs {
			err = c.bot.s.ChannelMessageDelete(c.ChannelID, msg)
			if rErr, ok := err.(*discordgo.RESTError); ok && rErr.Message != nil {
				mSingleMessageReapErrors.With(prometheus.Labels{"error_code": strconv.Itoa(rErr.Message.Code)}).Inc()
				fmt.Printf("[ERR ] %s: single-message delete: %v (on %v)\n", c, err, msg)
			} else if err != nil {
				mSingleMessageReapErrors.With(prometheus.Labels{"error_code": fmt.Sprintf("other(%T)", err)}).Inc()
				fmt.Printf("[ERR ] %s: single-message delete: %v (on %v)\n", c, err, msg)
			}
		}
		// re-load the backlog in case this surfaced more things to delete
		c.bot.QueueLoadBacklog(c, QOSSingleMessageDelete)
	}()
	return -1, nil
}

// returns and removes the messages that need to be deleted right now.
//
// also sets the minNextDelete and returns whether we think there could be more
// messages past the backlog horizon
func (c *ManagedChannel) collectMessagesToDelete() (m []string, needsQueueBacklog, isDisabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.minNextDelete = time.Now().Add(minTimeBetweenDeletion)

	// Mechanism for getting channels dropped from the reaper
	if c.killBit {
		return nil, false, true
	}

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

	return toDelete, ((nLiveMessages >= backlogChunkLimit*backlogAutoReloadPreFraction) &&
		(len(toDelete) > backlogChunkLimit*backlogAutoReloadDeleteFraction)), false
}
