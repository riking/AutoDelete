package autodelete

import "github.com/bwmarrin/discordgo"

const textHelp = `Commands:
  @AutoDelete start [duration: 30m] [count: 10] - starts this channel for message auto-deletion
      Duration or message count can be specified as ` + "`-`" + ` to not use that, but at least one must be specified.
  @AutoDelete stop - stop auto-deletion in this channel
  @AutoDelete help - prints this help message`

func CommandHelp(b *Bot, m *discordgo.Message) {
	b.s.ChannelMessageSend(m.ChannelID, textHelp)
}

func CommandStart(b *Bot, m *discordgo.Message) {

}

var commands = map[string]func(b *Bot, m *discordgo.Message){
	"help": CommandHelp,
	"start": CommandStart,
}