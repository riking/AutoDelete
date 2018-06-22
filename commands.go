package autodelete

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const textHelp = `Commands:
  @AutoDelete set [duration: 30m] [count: 10] - starts this channel for message auto-deletion
      Duration or message count can be specified as ` + "`-`" + ` to not use that, but at least one must be specified. Use "set 0 0" to disable the bot.
  @AutoDelete help - prints this help message
  @AutoDelete adminhelp [anything...] - forwards your request to the help server
For more help, join the help server: <https://discord.gg/FUGn8yE>`

const adminUserID = `82592645502734336`

func CommandHelp(b *Bot, m *discordgo.Message, rest []string) {
	b.s.ChannelMessageSend(m.ChannelID, textHelp)
}

func CommandAdminHelp(b *Bot, m *discordgo.Message, rest []string) {
	plainContent, err := m.ContentWithMoreMentionsReplaced(b.s)
	if err != nil {
		plainContent = m.Content
	}
	var channelName, guildID, guildName string
	ch, err := b.s.Channel(m.ChannelID)
	if err == nil {
		channelName = ch.Name
		guildID = ch.GuildID
		guild, err := b.s.Guild(ch.GuildID)
		if err == nil {
			guildName = guild.Name
		}
	}
	b.ReportToLogChannel(fmt.Sprintf(
		"Adminhelp command from %s (%s#%s) in #%s (ch id %s) of '%s' (guild id %s):\n%s",
		m.Author.Mention(), m.Author.Username, m.Author.Discriminator,
		channelName, m.ChannelID,
		guildName, guildID,
		plainContent,
	))
}

func CommandAdminSay(b *Bot, m *discordgo.Message, rest []string) {
	channelID := rest[0]

	if m.Author.ID != adminUserID {
		return
	}

	ch, err := b.s.Channel(channelID)
	if err != nil {
		b.s.ChannelMessageSend(m.ChannelID, "channel does not exist")
		return
	}

	b.s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Content: "[ADMIN]",
		Embed: &discordgo.MessageEmbed{
			Title:       "Message from bot administrator",
			Description: strings.Join(rest[1:], " "),
		},
	})
}

func CommandModify(b *Bot, m *discordgo.Message, rest []string) {
	var duration time.Duration
	var count int
	var anySet bool

	const perm = discordgo.PermissionManageMessages

	apermissions, err := b.s.UserChannelPermissions(m.Author.ID, m.ChannelID)
	if err != nil {
		b.s.ChannelMessageSend(m.ChannelID, "could not check your permissions: "+err.Error())
		return
	}
	if apermissions&perm == 0 {
		b.s.ChannelMessageSend(m.ChannelID, "You must have the Manage Messages permission to change AutoDelete settings.")
		return
	}

	for _, v := range rest {
		d, err := time.ParseDuration(v)
		if err == nil {
			duration = d
			anySet = true
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			count = int(n)
			anySet = true
			continue
		}
	}
	if !anySet {
		b.s.ChannelMessageSend(m.ChannelID, "Bad format for `set` command. Provide a count (20) and/or a duration (90m) to purge messages after. Maximum unit is hours.")
		return
	}

	_, err := b.s.ChannelMessagesPinned(m.ChannelID)
	hasPins := err == nil

	var confMessage *discordgo.Message

	if duration != 0 && count != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s or %d messages, whichever comes first.", duration, count))
	} else if duration != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s.", duration))

	} else if count != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %d other messages.", count))
	} else {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will not be auto-deleted."))
	}

	if err != nil {
		fmt.Println("Error sending config message:", err)
		b.s.ChannelMessageSend(m.ChannelID, "Encountered error, settings were not changed.\n"+err.Error())
		return
	}

	newManagedChannel := managedChannelMarshal{
		ID:            m.ChannelID,
		ConfMessageID: confMessage.ID,
		LiveTime:      duration,
		MaxMessages:   count,
		HasPins:       hasPins,
		IsDonor:       false, // TODO
	}

	err = b.setChannelConfig(newManagedChannel)
	if err != nil {
		fmt.Println("Error:", err)
		b.s.ChannelMessageSend(m.ChannelID, "Encountered error, settings may or may not have saved.\n"+err.Error())
	}
}

func CommandLeave(b *Bot, m *discordgo.Message, rest []string) {
	var guildID string

	if len(rest) == 0 {
		channel, err := b.s.Channel(m.ChannelID)
		if err != nil {
			fmt.Println("[cmdE] channel does not exist", m.ChannelID)
			return
		}
		guildID = channel.GuildID
		apermissions, err := b.s.UserChannelPermissions(m.Author.ID, m.ChannelID)
		perm := discordgo.PermissionManageServer
		if apermissions&perm != perm {
			b.s.ChannelMessageSend(m.ChannelID, "Leaving the current server requires MANAGE_SERVER permission.")
		}
	} else {
		if m.Author.ID != adminUserID {
			b.s.ChannelMessageSend(m.ChannelID, "Leaving other servers can only be done by the bot controller.")
			return
		}
		guildID = rest[0]
	}

	msg := fmt.Sprintf("Leaving guild ID %s", guildID)
	b.s.ChannelMessageSend(m.ChannelID, msg)
	fmt.Println("[leav]", msg)
	err := b.s.GuildLeave(guildID)
	if err != nil {
		fmt.Println("[cmdE] error leaving:", err)
	}
}

var commands = map[string]func(b *Bot, m *discordgo.Message, rest []string){
	"help":  CommandHelp,
	"set":   CommandModify,
	"start": CommandModify,
	"setup": CommandModify,
	"leave": CommandLeave,

	"ahelp":     CommandAdminHelp,
	"adminhelp": CommandAdminHelp,
	"amsg":      CommandAdminHelp,
	"adminmsg":  CommandAdminHelp,
	"support":   CommandAdminHelp,
	"adminsay":  CommandAdminSay,
}
