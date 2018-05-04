package autodelete

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/yaml.v2"
)

type Bot struct {
	Config
	s  *discordgo.Session
	me *discordgo.User

	mu       sync.RWMutex
	channels map[string]*ManagedChannel

	reaper *reapQueue
}

func New(c Config) *Bot {
	b := &Bot{
		Config:   c,
		channels: make(map[string]*ManagedChannel),
		reaper:   newReapQueue(),
	}
	go b.reapWorker()
	return b
}

type Config struct {
	ClientID     string `yaml:"clientid"`
	ClientSecret string `yaml:"clientsecret"`
	BotToken     string `yaml:"bottoken"`
	ErrorLogCh   string `yaml:"errorlog"`
	HTTP         struct {
		Listen string `yaml:"listen"`
		Public string `yaml:"public"`
	} `yaml:"http"`
	//Database struct {
	//	Driver string `yaml:"driver"`
	//	URL    string `yaml:"url"`
	//} `yaml:"db,flow"`
}

type managedChannelMarshal struct {
	ID             string        `yaml:"id"`
	ConfMessageID  string        `yaml:"conf_message_id"`
	LiveTime       time.Duration `yaml:"live_time"`
	MaxMessages    int           `yaml:"max_messages"`
	LastSentUpdate int           `yaml:"last_critical_msg"`
}

const pathChannelConfDir = "./data"
const pathChannelConfig = "./data/%s.yml"

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

func (b *Bot) saveChannelConfig(conf managedChannelMarshal) error {
	by, err := yaml.Marshal(conf)
	if err != nil {
		panic(err)
	}
	fileName := fmt.Sprintf(pathChannelConfig, conf.ID)
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	f.Write(by)
	err = f.Close()
	if err != nil {
		return err
	}
	return nil
}

func (b *Bot) deleteChannelConfig(chID string) error {
	fileName := fmt.Sprintf(pathChannelConfig, chID)
	err := os.Remove(fileName)
	if err != nil {
		return err
	}

	b.mu.Lock()
	delete(b.channels, chID)
	b.mu.Unlock()
	return nil
}

// Change the config to the provided one.
func (b *Bot) setChannelConfig(conf managedChannelMarshal) error {
	err := b.saveChannelConfig(conf)
	if err != nil {
		return err
	}

	b.mu.Lock()
	delete(b.channels, conf.ID)
	b.mu.Unlock()

	return b.loadChannel(conf.ID)
}

func (b *Bot) LoadChannelConfigs() error {
	files, err := ioutil.ReadDir(pathChannelConfDir)
	if err != nil {
		return err
	}
	for _, v := range files {
		n := v.Name()
		if !strings.HasSuffix(n, ".yml") {
			continue
		}
		chID := strings.TrimSuffix(n, ".yml")
		err = b.loadChannel(chID)

		errHandled := false
		if rErr, ok := err.(*discordgo.RESTError); ok && rErr != nil && rErr.Message != nil {
			shouldRemoveChannel := false
			shouldNotifyChannel := false
			var logMsg string

			switch rErr.Message.Code {
			case discordgo.ErrCodeUnknownChannel, discordgo.ErrCodeMissingAccess:
				shouldRemoveChannel = true
				logMsg = fmt.Sprintf("Removed unknown channel ID %s", chID)
			case discordgo.ErrCodeMissingPermissions:
				shouldRemoveChannel = true
				shouldNotifyChannel = true
				channelObj, _ := b.s.Channel(chID)
				if channelObj != nil {
					guildObj, _ := b.s.Guild(channelObj.GuildID)
					if guildObj != nil {
						logMsg = fmt.Sprintf("AutoDelete disabled from channel #%s (%s) (server %s (%s)) due to missing critical permissions", channelObj.Name, chID, guildObj.Name, channelObj.GuildID)
					} else {
						logMsg = fmt.Sprintf("AutoDelete disabled from channel #%s (%s) (server ID %s) due to missing critical permissions", channelObj.Name, chID, channelObj.GuildID)
					}
				} else {
					logMsg = fmt.Sprintf("AutoDelete disabled from channel (%s) (server unknown) due to missing critical permissions", chID)
				}
			}

			if shouldRemoveChannel {
				b.ReportToLogChannel(logMsg)
				if shouldNotifyChannel {
					_, err := b.s.ChannelMessageSend(chID, logMsg)
					fmt.Println("error reporting removal to channel", chID, ":", err)
				}
				b.deleteChannelConfig(chID)
				errHandled = true
			}
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

	fileName := fmt.Sprintf(pathChannelConfig, channelID)
	f, err := os.Open(fileName)
	if os.IsNotExist(err) {
		b.mu.Lock()
		b.channels[channelID] = nil
		b.mu.Unlock()
		return nil
	} else if err != nil {
		return err
	}
	by, err := ioutil.ReadAll(f)
	f.Close()
	if err != nil {
		return err
	}
	var conf managedChannelMarshal
	err = yaml.Unmarshal(by, &conf)
	if err != nil {
		return err
	}

	conf.ID = channelID
	mCh, err := InitChannel(b, conf)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.channels[channelID] = mCh
	b.mu.Unlock()

	err = mCh.LoadBacklog()
	if err != nil {
		fmt.Println("Loading backlog for", channelID, err)
		return err
	}
	return nil
}
