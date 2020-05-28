package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/bwmarrin/discordgo"
	autodelete "github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

var flagPrintMessages = flag.Bool("messages", false, "print recent messages")
var flagUnconfiguredChannels = flag.Bool("all", false, "print channels that are not configured, too")

func main() {
	var conf autodelete.Config

	flag.Parse()

	confBytes, err := ioutil.ReadFile("config.yml")
	if err != nil {
		fmt.Println("Please copy config.yml.example to config.yml and fill out the values")
		return
	}
	err = yaml.Unmarshal(confBytes, &conf)
	if err != nil {
		fmt.Println("yaml error:", err)
		return
	}
	if conf.BotToken == "" {
		fmt.Println("bot token must be specified")
	}
	if conf.Shards == 0 {
		conf.Shards = 1
	}

	s, err := discordgo.New("Bot " + conf.BotToken)
	if err != nil {
		fmt.Println("bot create error:", err)
		return
	}

	for _, guildID := range flag.Args() {
		guild, err := s.Guild(guildID)
		if err != nil {
			fmt.Printf("error fetching %s: %v", guildID, err)
			continue
		}
		guildIDNumeric, err := strconv.ParseInt(guildID, 10, 64)
		if err != nil {
			panic(err)
		}
		owner, err := s.User(guild.OwnerID)
		if err != nil {
			fmt.Printf("error fetching user(%s): %v", guild.OwnerID, err)
			continue
		}

		fmt.Printf("%s: name [%s] owner [%s] shard_discriminant [%d]\n", guild.ID, guild.Name, owner.String(), (guildIDNumeric>>22)%int64(conf.Shards))

		channelList, err := s.GuildChannels(guildID)
		if err != nil {
			fmt.Printf("error fetching channels: %v", err)
		}
		for _, v := range channelList {
			if *flagUnconfiguredChannels || isConfigured(v.ID) {
				fmt.Printf("  %s: #%s\n", v.ID, v.Name)
				for _, perm := range v.PermissionOverwrites {
					if 0 != (perm.Allow & discordgo.PermissionManageMessages) {
						fmt.Printf("    %s %s: perms +%x -%x\n", perm.Type, perm.ID, perm.Allow, perm.Deny)
					} else if 0 != perm.Deny {
						fmt.Printf("    %s %s: perms +%x -%x\n", perm.Type, perm.ID, perm.Allow, perm.Deny)
					}
				}
			}
		}

	}
}

func isConfigured(channelID string) bool {
	_, err := os.Stat(fmt.Sprintf("data/%s.yml", channelID))
	if os.IsNotExist(err) {
		return false
	} else if err == nil {
		return true
	} else {
		fmt.Printf("[ERR] %v", err)
		return false
	}
}
