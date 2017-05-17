package autodelete

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Config
	db *sql.DB
	s  *discordgo.Session

	mu sync.RWMutex
	channels map[string]*ManagedChannel
}

type Config struct {
	ClientID     string `yaml:"clientid"`
	ClientSecret string `yaml:"clientsecret"`
	BotToken     string `yaml:"bottoken"`
	Database struct {
		Driver string `yaml:"driver"`
		URL    string `yaml:"url"`
	} `yaml:"db,flow"`
}

type GuildConfig struct {
	Channels map[string]managedChannelMarshal
}

const pathGuildConfig = "./data/%s.yml"

func (b *Bot) SaveChannelConfig() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, v := range b.channels {
		file := fmt.Sprintf(pathGuildConfig, v.Channel.ID)
	}
}