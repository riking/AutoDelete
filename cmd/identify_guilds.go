package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"sort"
	"strconv"

	"github.com/bwmarrin/discordgo"
	autodelete "github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

var flagShardCount = flag.Int("shards", 4, "number of shards")
var flagPrintRoles = flag.Bool("printroles", false, "print server roles")

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
		if *flagPrintRoles {
			printRoles(s, guild)
		}
	}
}

// Print out the roles with permission to manage AutoDelete (server-wide) and
// their members.
func printRoles(s *discordgo.Session, guild *discordgo.Guild) {
	var sortedRoles = make(discordgo.Roles, len(guild.Roles))
	copy(sortedRoles, guild.Roles)
	sort.Sort(sortedRoles)
	var managerRoles = make(discordgo.Roles, 0)

	for _, role := range sortedRoles {
		if (role.Permissions&discordgo.PermissionManageMessages != 0) && !role.Managed {
			fmt.Printf("  [%s] perms %08x\n", role.Name, role.Permissions)
			managerRoles = append(managerRoles, role)
		}
	}
	printRoleMembers(s, guild, managerRoles)
}

func printRoleMembers(s *discordgo.Session, guild *discordgo.Guild, roles discordgo.Roles) {
	var pageID string
	var roleIDMap = make(map[string]*discordgo.Role)

	for _, r := range roles {
		roleIDMap[r.ID] = r
	}
	for {
		members, err := s.GuildMembers(guild.ID, pageID, 500)
		if err != nil {
			fmt.Printf("error fetching members [guild %s]: %v\n", guild.ID, err)
			return
		}
		if len(members) == 0 {
			// completed
			return
		}
		for _, m := range members {
			var firstRole *discordgo.Role
			for _, r := range m.Roles {
				if roleIDMap[r] != nil {
					firstRole = roleIDMap[r]
					break
				}
			}
			if firstRole != nil {
				// Found a manager, print it out
				fmt.Printf("\r  User %s %s has role [%s]  \n", m.User.ID, m.User.String(), firstRole.Name)
			}
			pageID = m.User.ID
		}
		if len(members) >= 450 {
			fmt.Printf(".")
		}
	}
}
