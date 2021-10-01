package autodelete

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/prometheus/client_golang/prometheus"
)

type Bot struct {
	Config
	storage    Storage
	donorRoles map[string]bool

	s  *discordgo.Session
	me *discordgo.User

	mu       sync.RWMutex
	channels map[string]*ManagedChannel

	// The reapQueue for deleting messages.
	reaper *reapQueue
	// The reapQueue for channels that encountered a rate-limit error when we
	// tried to load them.
	loadRetries *reapQueue
}

func New(c Config) *Bot {
	b := &Bot{
		Config:      c,
		storage:     &DiskStorage{},
		donorRoles:  makeSet(c.DonorRoleIDs),
		channels:    make(map[string]*ManagedChannel),
		reaper:      newReapQueue(4, queueReap),
		loadRetries: newReapQueue(12, queueLoad),
	}
	prometheus.MustRegister(reapqCollector{[]*reapQueue{b.reaper, b.loadRetries}})
	go reapScheduler(b.reaper, b.reapWorker)
	go reapScheduler(b.loadRetries, b.loadWorker)
	if c.BacklogLengthLimit != 0 {
		backlogLimitNonDonor = c.BacklogLengthLimit
	}
	if c.DonorBacklogLimit != 0 {
		backlogLimitDonor = c.DonorBacklogLimit
	}
	return b
}

type Config struct {
	ClientID     string `yaml:"clientid"`
	ClientSecret string `yaml:"clientsecret"`
	BotToken     string `yaml:"bottoken"`
	// discord user ID
	AdminUser string `yaml:"adminuser"`
	// 0: do not use sharding
	Shards int `yaml:"shards"`
	// discord channel ID
	ErrorLogCh string `yaml:"errorlog"`
	HTTP       struct {
		Listen string `yaml:"listen"`
		Public string `yaml:"public"`
	} `yaml:"http"`

	StatusMessage *string `yaml:"statusmessage"`

	// discord guild ID
	DonorGuild string `yaml:"donor_guild"`
	// discord role IDs
	DonorRoleIDs []string `yaml:"donor_roles"`

	BacklogLengthLimit int `yaml:"backlog_limit"`
	DonorBacklogLimit  int `yaml:"backlog_limit_donor"`
}

type BansFile struct {
	Guilds []string `yaml:"guilds"`
}

type ManagedChannelMarshal struct {
	ID      string `yaml:"id"`
	GuildID string `yaml:"guild_id"`

	LiveTime       time.Duration `yaml:"live_time"`
	MaxMessages    int           `yaml:"max_messages"`
	LastSentUpdate int           `yaml:"last_critical_msg"`
	HasPins        bool          `yaml:"has_pins,omitempty"`
	IsDonor        bool          `yaml:"is_donor,omitempty"`

	// ConfMessageID is deprecated.
	ConfMessageID string   `yaml:"conf_message_id,omitempty"`
	KeepMessages  []string `yaml:"keep_messages"`
}

var errNegativeConfigValues = fmt.Errorf("negative configuration values")

func internalMigrateConfig(c ManagedChannelMarshal) ManagedChannelMarshal {
	if c.ConfMessageID != "" {
		c.KeepMessages = []string{c.ConfMessageID}
		c.ConfMessageID = ""
	}
	return c
}

func (b *Bot) ReportToLogChannel(msg string) {
	_, err := b.s.ChannelMessageSend(b.Config.ErrorLogCh, msg)
	if err != nil {
		fmt.Println("error while reporting to error log:", err)
	}
	fmt.Println("[LOG]", msg)
}

func (b *Bot) SaveAllChannelConfigs() []error {
	var wg sync.WaitGroup
	errCh := make(chan error)

	b.mu.RLock()
	for channelID := range b.channels {
		wg.Add(1)
		go func(channelID string) {
			errCh <- b.SaveChannelConfig(channelID)
			wg.Done()
		}(channelID)
	}
	b.mu.RUnlock()

	go func() {
		wg.Wait()
		close(errCh)
	}()

	var errs []error
	for v := range errCh {
		if v != nil {
			errs = append(errs, v)
		}
	}
	return errs
}

func (b *Bot) SaveChannelConfig(channelID string) error {
	b.mu.RLock()
	manCh := b.channels[channelID]
	b.mu.RUnlock()
	if manCh == nil {
		return nil
	}

	return b.saveChannelConfig(manCh.Export())
}

func (b *Bot) saveChannelConfig(conf ManagedChannelMarshal) error {
	return b.storage.SaveChannel(conf)
}

func (b *Bot) deleteChannelConfig(chID string) error {
	// i love layering violations
	(&ManagedChannel{bot: b, ChannelID: chID}).Disable()

	err := b.storage.DeleteChannel(chID)
	if err != nil {
		fmt.Println("failed to delete channel config for", chID, ":", err)
		// continue
	}

	return err
}

// Change the config to the provided one.
func (b *Bot) setChannelConfig(conf ManagedChannelMarshal) error {
	err := b.saveChannelConfig(conf)
	if err != nil {
		return err
	}

	b.mu.Lock()
	delete(b.channels, conf.ID)
	b.mu.Unlock()

	return b.loadChannel(conf.ID, QOSInteractive)
}

func (b *Bot) handleCriticalPermissionsErrors(channelID string, srcErr error) bool {
	if srcErr == errNegativeConfigValues {
		fmt.Printf("[LOG] Disabled due to negative config values in %s\n", channelID)
		return true
	}

	if rErr, ok := srcErr.(*discordgo.RESTError); ok && rErr != nil && rErr.Message != nil {
		shouldRemoveChannel := false
		shouldNotifyChannel := false
		var logMsg string

		switch rErr.Message.Code {
		case discordgo.ErrCodeUnknownChannel, discordgo.ErrCodeMissingAccess:
			shouldRemoveChannel = true
			logMsg = fmt.Sprintf("Removed unknown channel ID %s", channelID)
		case discordgo.ErrCodeMissingPermissions:
			shouldRemoveChannel = true
			shouldNotifyChannel = true
			channelObj, _ := b.Channel(channelID)
			if channelObj != nil {
				guildObj, _ := b.s.State.Guild(channelObj.GuildID)
				if guildObj != nil {
					logMsg = fmt.Sprintf("AutoDelete disabled from channel #%s (%s) (server %s (%s)) due to missing critical permissions", channelObj.Name, channelID, guildObj.Name, channelObj.GuildID)
				} else {
					logMsg = fmt.Sprintf("AutoDelete disabled from channel #%s (%s) (server ID %s) due to missing critical permissions", channelObj.Name, channelID, channelObj.GuildID)
				}
			} else {
				logMsg = fmt.Sprintf("AutoDelete disabled from channel (%s) (server unknown) due to missing critical permissions", channelID)
			}
		}

		if shouldRemoveChannel {
			b.ReportToLogChannel(logMsg)
			if shouldNotifyChannel {
				_, err := b.s.ChannelMessageSend(channelID, logMsg)
				fmt.Println("error reporting removal to channel", channelID, ":", err)
			}
			b.deleteChannelConfig(channelID)
			return true
		}
	}
	return false
}

func (b *Bot) IsInShard(guildID string) bool {
	n, err := strconv.ParseInt(guildID, 10, 64)
	if err != nil {
		return true // fail safe
	}
	return b.isInShardNumeric(n)
}

func (b *Bot) isInShardNumeric(guildID int64) bool {
	if b.s.ShardCount <= 1 {
		return true
	}
	shardSpecifier := (guildID >> 22)
	return (shardSpecifier % int64(b.s.ShardCount)) == int64(b.s.ShardID)
}

func (b *Bot) LoadChannelConfigs() error {
	channels, err := b.storage.ListChannels()
	if err != nil {
		return err
	}

	chanCh := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chID := range chanCh {
				b.initialLoadChannel(chID)
			}
		}()
	}

	for _, chID := range channels {
		chanCh <- chID
	}
	close(chanCh)
	wg.Wait()
	return nil
}

func (b *Bot) initialLoadChannel(chID string) {
	var errHandled = false

	conf, err := b.storage.GetChannel(chID)
	if os.IsNotExist(err) {
		// A delete raced with our load. Ignore.
		return
	} else if err != nil {
		fmt.Printf("[ ERR] Failed to load channel %s from storage: %v\n", chID, err)
		// hope for the best i guess
		return
	}

	if !b.IsInShard(conf.GuildID) {
		return
	}

	err = b.loadChannel(chID, QOSInit)

	errHandled = b.handleCriticalPermissionsErrors(chID, err)

	if os.IsNotExist(err) {
		fmt.Printf("[ ERR] loading configuration for %s: configuration file does not exist\n", chID)
		errHandled = true
	}
	if err != nil && !errHandled {
		channelObj, _ := b.Channel(chID)
		if channelObj != nil {
			guildObj, _ := b.s.State.Guild(channelObj.GuildID)
			if guildObj != nil {
				fmt.Printf("Error loading configuration from #%s (%s) (server %s (%s)): %v\n", channelObj.Name, chID, guildObj.Name, channelObj.GuildID, err)
				errHandled = true
			}
		}
	}
	if err != nil && !errHandled {
		fmt.Printf("[ ERR] Gave up loading channel %s: %v", chID, err)
	}
}

func (b *Bot) GetChannel(channelID string, qos LoadQOS) (*ManagedChannel, error) {
	b.mu.RLock()
	mCh, ok := b.channels[channelID]
	b.mu.RUnlock()

	if ok {
		return mCh, nil
	}

	err := b.loadChannel(channelID, qos)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	b.mu.RLock()
	mCh, _ = b.channels[channelID]
	b.mu.RUnlock()
	return mCh, nil
}

func (b *Bot) loadChannel(channelID string, qos LoadQOS) error {
	// ensure channel exists
	ch, err := b.Channel(channelID)
	if err != nil {
		return err
	}

	conf, err := b.storage.GetChannel(channelID)
	if os.IsNotExist(err) {
		b.mu.Lock()
		b.channels[channelID] = nil
		b.mu.Unlock()
		return os.ErrNotExist
	} else if err != nil {
		return err
	}

	conf.ID = channelID

	if conf.MaxMessages == -1 && conf.LiveTime != 0 {
		// Migration: disallow negative configurations, but treat -1 as 0
		conf.MaxMessages = 0
	}
	if conf.LiveTime < 0 || conf.MaxMessages < 0 {
		// Migration: disallow negative configurations
		err = b.storage.DeleteChannel(channelID)
		if err != nil {
			return err
		}

		absMessages := conf.MaxMessages
		if absMessages < 0 {
			absMessages = -absMessages
		}
		absDuration := conf.LiveTime
		if absDuration < 0 {
			absDuration = -absDuration
		}
		b.s.ChannelMessageSend(channelID, fmt.Sprintf(":warning: AutoDelete is now disabled in this channel due to corrupt configuration: negative values were found. It must be re-enabled manually.\nFound configuration: duration %v, messages %d\nAn administrator can fix this by typing the following command:\n`@%s#%s setup %v %d`", conf.LiveTime, conf.MaxMessages, b.me.Username, b.me.Discriminator, absDuration, absMessages))
		return errNegativeConfigValues
	}

	mCh, err := InitChannel(b, conf)
	if err != nil {
		return err
	}
	if mCh.needsExport {
		fmt.Printf("[migr] Resaving channel %s\n", channelID)
		b.saveChannelConfig(mCh.Export())
		mCh.mu.Lock()
		mCh.needsExport = false
		mCh.mu.Unlock()
	}
	b.mu.Lock()
	// TODO - multiple loadChannels() can happen at the same time (due to incoming messages)
	b.channels[channelID] = mCh
	b.mu.Unlock()

	if ch.LastPinTimestamp == "" {
		b.QueueLoadBacklog(mCh, qos.Upgrade(QOSInitNoPins))
	} else {
		b.QueueLoadBacklog(mCh, qos.Upgrade(QOSInitWithPins))
	}
	return nil
}

func makeSet(s []string) map[string]bool {
	m := make(map[string]bool)
	for _, v := range s {
		m[v] = true
	}
	return m
}
