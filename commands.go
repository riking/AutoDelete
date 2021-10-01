package autodelete

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const textHelp = `Commands:
  @AutoDelete set [duration: 30m] [count: 10] - starts this channel for message auto-deletion
      Duration or message count can be specified as ` + "`-`" + ` to not use that, but at least one must be specified. Use "set 0 0" to disable the bot.
  @AutoDelete help - prints this help message
For more help, check <https://github.com/riking/AutoDelete> or join the help server: <https://discord.gg/FUGn8yE>`

const emojiBusy = `🔄`
const emojiDone = `✅`

func (b *Bot) GetMsgChGuild(m *discordgo.Message) (*discordgo.Channel, *discordgo.Guild) {
	ch, err := b.Channel(m.ChannelID)
	if err != nil {
		return nil, nil
	}
	guild, err := b.s.State.Guild(ch.GuildID)
	if err != nil {
		return nil, nil
	}
	return ch, guild
}

func CommandHelp(b *Bot, m *discordgo.Message, rest []string) {
	b.s.ChannelMessageSend(m.ChannelID, textHelp)
}

func CommandAdminHelp(b *Bot, m *discordgo.Message, rest []string) {
	plainContent, err := m.ContentWithMoreMentionsReplaced(b.s)
	if err != nil {
		plainContent = m.Content
	}
	ch, guild := b.GetMsgChGuild(m)
	if guild == nil {
		return
	}
	b.ReportToLogChannel(fmt.Sprintf(
		"Adminhelp command from %s (%s#%s) in #%s (ch id %s) of '%s' (guild id %s):\n%s",
		m.Author.Mention(), m.Author.Username, m.Author.Discriminator,
		ch.Name, m.ChannelID,
		guild.Name, guild.ID,
		plainContent,
	))
}

func CommandAdminSay(b *Bot, m *discordgo.Message, rest []string) {
	if len(rest) == 0 {
		return
	}
	channelID := rest[0]

	if m.Author.ID != b.Config.AdminUser {
		return
	}

	ch, err := b.Channel(channelID)
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

func CommandSetDonor(b *Bot, m *discordgo.Message, rest []string) {
	var channelID string
	if len(rest) == 0 {
		channelID = m.ChannelID
	} else {
		channelID = rest[0]
	}

	if m.Author.ID != b.Config.AdminUser {
		b.s.ChannelMessageSend(m.ChannelID, "patron checking not yet implemented")
		return
	}

	b.mu.RLock()
	mCh, ok := b.channels[channelID]
	b.mu.RUnlock()

	if !ok {
		b.s.ChannelMessageSend(m.ChannelID, "not currently deleting in that channel")
		return
	}

	mCh.mu.Lock()
	mCh.IsDonor = true
	mCh.mu.Unlock()

	b.saveChannelConfig(mCh.Export())

	b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("set %v as a donor channel", channelID))
	b.QueueLoadBacklog(mCh, QOSInteractive)
}

func (b *Bot) isDonor(userID string) (bool, error) {
	if b.Config.DonorGuild == "" {
		return false, nil
	}
	member, err := b.s.GuildMember(b.Config.DonorGuild, userID)
	if err != nil {
		return false, err
	}
	for _, r := range member.Roles {
		if b.donorRoles[r] {
			return true, nil
		}
	}
	return false, nil
}

func CommandCheck(b *Bot, m *discordgo.Message, rest []string) {
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

	mCh, err := b.GetChannel(m.ChannelID, QOSInteractive)
	if err != nil {
		b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error checking settings: %v", err))
		return
	}

	if mCh == nil {
		b.s.ChannelMessageSend(m.ChannelID, "This channel is not set up for deletion.")
		return
	}

	duration := mCh.MessageLiveTime
	count := mCh.MaxMessages
	keeps := mCh.KeepMessages

	var msg bytes.Buffer
	msg.WriteString("Settings: Messages in this channel will ")
	if duration != 0 && count != 0 {
		fmt.Fprintf(&msg, "be deleted after %s or %d messages, whichever comes first.", duration, count)
	} else if duration != 0 {
		fmt.Fprintf(&msg, "be deleted after %s.", duration)
	} else if count != 0 {
		fmt.Fprintf(&msg, "be deleted after %d other messages.", count)
	} else {
		fmt.Fprintf(&msg, "[BUG?] not be auto-deleted (but are still being incorrectly tracked???).")
	}

	if len(keeps) > 1 {
		fmt.Fprintf(&msg, " I am aware of %d pinned messages.", len(keeps)-1)
	}

	b.s.ChannelMessageSend(m.ChannelID, msg.String())
}

func CommandModify(b *Bot, m *discordgo.Message, rest []string) {
	var duration time.Duration
	var count int
	var anySet bool

	const perm = discordgo.PermissionManageMessages

	channel, err := b.Channel(m.ChannelID)
	if err != nil {
		fmt.Println("[ERR ] Could not load channel of mention")
		return
	}

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
	if duration < 0 || count < 0 {
		b.s.ChannelMessageSend(m.ChannelID, "Count and/or duration cannot be negative.")
		return
	}

	var confMessage *discordgo.Message
	doNotReload := false

	if duration != 0 && count != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s or %d messages, whichever comes first.", duration, count))
	} else if duration != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %s.", duration))

	} else if count != 0 {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will be deleted after %d other messages.", count))
	} else {
		confMessage, err = b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Messages in this channel will not be auto-deleted."))
		doNotReload = true
	}

	if err != nil {
		fmt.Println("Error sending config message:", err)
		b.s.ChannelMessageSend(m.ChannelID, "Encountered error, settings were not changed.\n"+err.Error())
		return
	}

	emojiErr := b.s.MessageReactionAdd(m.ChannelID, confMessage.ID, emojiBusy)
	if emojiErr != nil {
		fmt.Println("[Warn]", "could not react to config reply", emojiErr)
	}

	isDonor, err := b.isDonor(m.Author.ID)
	if err != nil {
		fmt.Println("[Warn]", "could not check donor status", err)
	}

	b.mu.RLock()
	mCh := b.channels[m.ChannelID]
	b.mu.RUnlock()

	var newManagedChannel = ManagedChannelMarshal{
		ID:           m.ChannelID,
		GuildID:      channel.GuildID,
		KeepMessages: []string{confMessage.ID},
		LiveTime:     duration,
		MaxMessages:  count,
		HasPins:      channel.LastPinTimestamp != "",
		IsDonor:      isDonor,
	}

	if mCh != nil {
		newManagedChannel = mCh.Export()
		newManagedChannel.LiveTime = duration
		newManagedChannel.MaxMessages = count
	}

	if doNotReload {
		err = b.deleteChannelConfig(m.ChannelID)
		if os.IsNotExist(err) {
			err = nil
		}
	} else {
		err = b.setChannelConfig(newManagedChannel)
	}

	if err != nil {
		fmt.Println("Error:", err)
		b.s.ChannelMessageSend(m.ChannelID, "Encountered error, settings may or may not have saved.\n"+err.Error())
	}
	fmt.Println("[load] Changed settings for channel", m.ChannelID, confMessage.Content)

	if doNotReload {
		if mCh != nil {
			mCh.Disable()
		}
	}

	// Wait for LoadBacklog() to complete by watching isStarted
	go func() {
		channelID := m.ChannelID
		msgID := confMessage.ID
		numMessages := 0

		b.mu.RLock()
		mCh := b.channels[channelID]
		b.mu.RUnlock()
		if mCh != nil {
			select {
			case <-mCh.isStarted:
			case <-time.After(30 * time.Minute):
			}
			// Check for backlog length exceeded
			mCh.mu.Lock()
			numMessages = len(mCh.liveMessages)
			mCh.mu.Unlock()
		}

		// Check for backlog length exceeded
		limit := backlogLimitNonDonor
		if isDonor {
			limit = backlogLimitDonor
		}

		if count > limit {
			b.s.ChannelMessageSend(channelID, fmt.Sprintf("⚠️ The number of messages configured for deletion is over %d. Messages will not be reliably deleted. (Configured: %d)", limit, count))
		} else if numMessages >= limit {
			b.s.ChannelMessageSend(channelID, fmt.Sprintf("⚠️ The number of messages in this channel is over %d. Messages may not be reliably deleted. (Saw: %d)", limit, numMessages))
		}

		// Give done reaction
		b.s.MessageReactionRemove(channelID, msgID, emojiBusy, "@me")
		emojiErr = b.s.MessageReactionAdd(channelID, msgID, emojiDone)
		time.Sleep(30 * time.Second)
		b.s.MessageReactionRemove(channelID, msgID, emojiDone, "@me")
	}()
}

func CommandLeave(b *Bot, m *discordgo.Message, rest []string) {
	var guildID string

	if len(rest) == 0 {
		channel, err := b.Channel(m.ChannelID)
		if err != nil {
			fmt.Println("[cmdE] channel does not exist", m.ChannelID)
			return
		}
		guildID = channel.GuildID
		apermissions, err := b.s.UserChannelPermissions(m.Author.ID, m.ChannelID)
		if err != nil {
			apermissions = 0
		}
		perm := int64(discordgo.PermissionManageServer)
		if apermissions&perm != perm {
			b.s.ChannelMessageSend(m.ChannelID, "Leaving the current server requires MANAGE_SERVER permission.")
			return
		}
	} else if rest[0] == "channel" && len(rest) == 2 {
		if m.Author.ID != b.Config.AdminUser {
			b.s.ChannelMessageSend(m.ChannelID, "Leaving other servers can only be done by the bot controller.")
			return
		}
		channel, err := b.Channel(rest[1])
		if err != nil {
			b.s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Could not find channel %q", rest[0]))
			return
		}
		guildID = channel.GuildID
	} else {
		if m.Author.ID != b.Config.AdminUser {
			b.s.ChannelMessageSend(m.ChannelID, "Leaving other servers can only be done by the bot controller.")
			return
		}
		guildID = rest[0]
	}

	if guildID == b.Config.DonorGuild {
		b.s.ChannelMessageSend(m.ChannelID, "Bot will never voluntarily leave the primary guild")
		return
	}

	fmt.Println("[leav]", guildID, m.Author.String())
	err := b.s.GuildLeave(guildID)
	if err != nil {
		msg := fmt.Sprintf("Error leaving guild ID %s: %v", guildID, err)
		b.s.ChannelMessageSend(m.ChannelID, msg)
		fmt.Println("[cmdE] error leaving:", err)
	} else {
		msg := fmt.Sprintf("Leaving guild ID %s: ok", guildID)
		b.s.ChannelMessageSend(m.ChannelID, msg)
	}
}

var commands = map[string]func(b *Bot, m *discordgo.Message, rest []string){
	"help":  CommandHelp,
	"set":   CommandModify,
	"start": CommandModify,
	"setup": CommandModify,
	"leave": CommandLeave,
	"check": CommandCheck,

	"ahelp":     CommandAdminHelp,
	"adminhelp": CommandAdminHelp,
	"amsg":      CommandAdminHelp,
	"adminmsg":  CommandAdminHelp,
	"support":   CommandAdminHelp,
	"adminsay":  CommandAdminSay,
	"setdonor":  CommandSetDonor,
}
