package autodelete

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
)

const textHelp = `Commands:
  @AutoDelete set [duration: 30m] [count: 10] - starts this channel for message auto-deletion
      Duration or message count can be specified as ` + "`-`" + ` to not use that, but at least one must be specified.
  @AutoDelete stop - stop auto-deletion in this channel
  @AutoDelete help - prints this help message`

func CommandHelp(b *Bot, m *discordgo.Message, rest []string) {
	b.s.ChannelMessageSend(m.ChannelID, textHelp)
}

func CommandModify(b *Bot, m *discordgo.Message, rest []string) {
	var duration time.Duration
	var count int
	var anySet bool

	const perm = discordgo.PermissionManageMessages

	apermissions, err := b.s.UserChannelPermissions(m.Author.ID, m.MessageID)
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

	newManagedChannel := managedChannelMarshal{
		ID:            m.ChannelID,
		ConfMessageID: m.ID,
		LiveTime:      duration,
		MaxMessages:   count,
	}

	err := b.setChannelConfig(newManagedChannel)
	if err != nil {
		fmt.Println("Error:", err)
		b.s.ChannelMessageSend(m.ChannelID, "Encountered error, settings may or may not have saved.\n"+err.Error())
	} else {
		if duration != 0 && count != 0 {
			b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s or %d messages, whichever comes first.", duration, count))
		} else if duration != 0 {
			b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s.", duration))

		} else if count != 0 {
			b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %d other messages.", count))
		} else {
			b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will not be auto-deleted.", count))
		}
	}
}

var commands = map[string]func(b *Bot, m *discordgo.Message, rest []string){
	"help": CommandHelp,
	"set":  CommandModify,
}
