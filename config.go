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
	ErrorLogCh   string `yaml:"error_log"`
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
		// err = b.loadChannel(chID)
		// if err != nil {
		//	fmt.Println("error loading configuration from", n, ":", err)
		// }
	}
	return nil
}

func (b *Bot) loadChannel(channelID string) error {
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
		/*
			if rErr, ok := err.(*discordgo.RESTError); ok {
				if rErr.Message != nil {
					if rErr.Message.Code == 50001 { // Missing access

					}
				}
			}
		*/
		fmt.Println("Loading backlog for", channelID, err)
		return err
	}
	return nil
}
