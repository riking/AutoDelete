package autodelete

import "github.com/bwmarrin/discordgo"

func (b *Bot) OnMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if s != b.s {
		return
	}

}

func (b *Bot) OnReady(s *discordgo.Session, m *discordgo.Ready) {

}