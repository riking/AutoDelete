package autodelete

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Config
	storage Storage

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
		channels:    make(map[string]*ManagedChannel),
		reaper:      newReapQueue(),
		loadRetries: newReapQueue(),
	}
	go reapScheduler(b.reaper, 4, b.reapWorker)
	go reapScheduler(b.loadRetries, 4, b.loadWorker)
	return b
}

type Config struct {
	ClientID     string `yaml:"clientid"`
	ClientSecret string `yaml:"clientsecret"`
	BotToken     string `yaml:"bottoken"`
	Shards       int    `yaml:"shards"`
	ErrorLogCh   string `yaml:"errorlog"`
	AdminHelpID  string `yaml:"adminUserID"`
	HTTP         struct {
		Listen string `yaml:"listen"`
		Public string `yaml:"public"`
	} `yaml:"http"`
	//Database struct {
	//	Driver string `yaml:"driver"`
	//	URL    string `yaml:"url"`
	//} `yaml:"db,flow"`
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
		go func() {
			errCh <- b.SaveChannelConfig(channelID)
			wg.Done()
		}()
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
	err := b.storage.DeleteChannel(chID)
	if err != nil {
		fmt.Println("failed to delete channel config for", chID, ":", err)
		// continue
	}

	b.mu.Lock()
	delete(b.channels, chID)
	b.mu.Unlock()
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

	return b.loadChannel(conf.ID)
}

func (b *Bot) handleCriticalPermissionsErrors(channelID string, srcErr error) bool {
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
			channelObj, _ := b.s.Channel(channelID)
			if channelObj != nil {
				guildObj, _ := b.s.Guild(channelObj.GuildID)
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
	for _, chID := range channels {
		var errHandled = false

		ch, err := b.s.Channel(chID)
		if err != nil {
			errHandled = b.handleCriticalPermissionsErrors(chID, err)
			if errHandled {
				continue
			}
			fmt.Printf("Error loading configuration for channel %s: could not check guild ID: %v\n", chID, err)
			continue
		}
		if !b.IsInShard(ch.GuildID) {
			continue
		}
		err = b.loadChannel(chID)

		errHandled = b.handleCriticalPermissionsErrors(chID, err)

		if os.IsNotExist(err) {
			fmt.Printf("Error loading configuration for %s: configuration file does not exist\n", chID)
			errHandled = true
		}
		if err != nil && !errHandled {
			channelObj, _ := b.s.Channel(chID)
			if channelObj != nil {
				guildObj, _ := b.s.Guild(channelObj.GuildID)
				if guildObj != nil {
					fmt.Printf("Error loading configuration from #%s (%s) (server %s (%s)): %v\n", channelObj.Name, chID, guildObj.Name, channelObj.GuildID, err)
					errHandled = true
				}
			}
		}
		if err != nil && !errHandled {
			fmt.Printf("Error loading configuration for %s: %v\n", chID, err)
			errHandled = true
		}
	}
	return nil
}

func (b *Bot) loadChannel(channelID string) error {
	_, err := b.s.Channel(channelID)
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

	b.QueueLoadBacklog(mCh, true)
	return nil
}
