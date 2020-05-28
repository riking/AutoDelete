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

	state := discordgo.NewState()
	s.State = state

	for _, guildID := range flag.Args() {
		guild, err := s.Guild(guildID)
		if err != nil {
			fmt.Printf("error fetching %s: %v", guildID, err)
			continue
		}
		fetchRoles(s, guild)

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
			if (*flagUnconfiguredChannels && v.Type != discordgo.ChannelTypeGuildVoice) || isConfigured(v.ID) {
				fmt.Printf("  %s: #%s\n", v.ID, v.Name)
				var managers, denied []*discordgo.PermissionOverwrite
				for _, perm := range v.PermissionOverwrites {
					if 0 != (perm.Allow & discordgo.PermissionManageMessages) {
						managers = append(managers, perm)
					} else if 0 != perm.Deny {
						denied = append(denied, perm)
					}
				}
				for _, v := range managers {
					printInterestingPermissions(s, guild, v)
				}
				for _, v := range denied {
					printInterestingPermissions(s, guild, v)
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

var roleMap = make(map[string]*discordgo.Role)

func fetchRoles(s *discordgo.Session, g *discordgo.Guild) {
	roles, err := s.GuildRoles(g.ID)
	if err != nil {
		fmt.Printf("err fetching roles(%s): %v", g.ID, err)
		return
	}
	for _, v := range roles {
		roleMap[v.ID] = v
	}
}

func printInterestingPermissions(s *discordgo.Session, g *discordgo.Guild, perm *discordgo.PermissionOverwrite) {
	switch perm.Type {
	case "member":
		user, err := s.User(perm.ID)
		if err == nil {
			fmt.Printf("   user %s (%s): perms +%x -%x\n", user, perm.ID, perm.Allow, perm.Deny)
		} else {
			fmt.Printf("   user %s (err %v): perms +%x -%x\n", perm.ID, err, perm.Allow, perm.Deny)
		}
	case "role":
		role, ok := roleMap[perm.ID]
		if ok {
			fmt.Printf("   role %s (%s): perms +%x -%x\n", role.Name, perm.ID, perm.Allow, perm.Deny)
		} else {
			fmt.Printf("   role %s (err): perms +%x -%x\n", perm.ID, perm.Allow, perm.Deny)
		}
	default:
		fmt.Printf("   %s %s: perms +%x -%x\n", perm.Type, perm.ID, perm.Allow, perm.Deny)
	}
}
