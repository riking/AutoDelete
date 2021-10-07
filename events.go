package autodelete

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"runtime"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
)

const userAgent = "AutoDelete (https://github.com/riking/AutoDelete, v1.4)"

type userAgentSetter struct {
	t http.RoundTripper
}

func (u *userAgentSetter) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return u.t.RoundTrip(req)
}

func (b *Bot) ConnectDiscord(shardID, shardCount int) error {
	s, err := discordgo.New("Bot " + b.BotToken)
	if err != nil {
		return err
	}
	b.s = s
	state := discordgo.NewState()
	state.TrackChannels = true
	state.TrackEmojis = false
	state.TrackMembers = false
	state.TrackRoles = false
	state.TrackVoice = false
	state.TrackPresences = false
	state.MaxMessageCount = 0
	s.State = state

	s.Identify.Compress = true
	s.Identify.Properties.OS = runtime.GOOS
	s.Identify.Properties.Browser = "github.com/riking/AutoDelete"
	s.Identify.Properties.Device = "github.com/riking/AutoDelete"
	if b.Config.StatusMessage != nil {
		s.Identify.Presence.Game.Name = *b.Config.StatusMessage
		s.Identify.Presence.Game.Type = discordgo.ActivityTypeGame
	}
	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions | discordgo.IntentsDirectMessages |
		discordgo.IntentsDirectMessageReactions

	// Configure the HTTP client
	s.UserAgent = userAgent
	runtimeCookieJar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	transport := &userAgentSetter{t: http.DefaultTransport}
	s.Client = &http.Client{
		Timeout:   20 * time.Second,
		Jar:       runtimeCookieJar,
		Transport: transport,
	}

	if shardID == 0 {
		gb, err := s.GatewayBot()
		if err != nil {
			return err
		}
		fmt.Println("shard count recommendation: ", gb.Shards)
		if !(shardCount == 0 && gb.Shards == 1) && (int(float64(shardCount)*2.5) < gb.Shards) {
			return errors.Errorf("need to increase shard count: have %d, want %d", shardCount, gb.Shards)
		}
	}
	if shardCount != 0 {
		s.Identify.Shard = &[2]int{shardID, shardCount}
	}
	s.ShardID = shardID
	s.ShardCount = shardCount

	// Add event handlers
	s.AddHandler(b.OnReady)
	s.AddHandler(b.OnResume)
	s.AddHandler(b.OnChannelDelete)
	s.AddHandler(b.OnGuildRemove)
	s.AddHandler(b.OnChannelPins)
	s.AddHandler(b.HandleMentions)
	s.AddHandler(b.OnMessage)
	me, err := s.User("@me")
	if err != nil {
		fmt.Println("get me:", err)
		return errors.Wrap(err, "get me")
	}
	b.me = me

	err = s.Open()
	if err != nil {
		return errors.Wrap(err, "open socket")
	}
	return nil
}

func (b *Bot) HandleMentions(s *discordgo.Session, m *discordgo.MessageCreate) {
	found := false
	for _, v := range m.Message.Mentions {
		if v.ID == b.me.ID {
			found = true
			break
		}
	}
	// TODO allow mentioning the bot role
	// for _, roleID := range m.Message.MentionRoles {
	// looks like <&1234roleid6789>
	// }
	if !found {
		return
	}

	if len(m.Message.Content) == 0 {
		return
	}
	split := strings.Fields(m.Message.Content)
	if len(split) == 0 {
		return
	}
	plainMention := "<@" + b.me.ID + ">"
	nickMention := "<@!" + b.me.ID + ">"

	ch, guild := b.GetMsgChGuild(m.Message)
	if guild == nil {
		fmt.Printf("[ cmd] got mention from %s (%s#%s) in unknown channel %s: %s\n",
			m.Author.Mention(), m.Author.Username, m.Author.Discriminator,
			m.Message.ChannelID, m.Message.Content)
		return
	}

	if ((split[0] == plainMention) ||
		(split[0] == nickMention)) && len(split) > 1 {
		cmd := split[1]
		cmd = strings.ToLower(cmd)
		fun, ok := commands[cmd]
		if ok {
			fmt.Printf("[ cmd] got command from %s (%s#%s) in %s (id %s) guild %s (id %s):\n  %v\n",
				m.Message.Author.Mention(), m.Message.Author.Username, m.Message.Author.Discriminator,
				ch.Name, ch.ID, guild.Name, guild.ID,
				split)
			go fun(b, m.Message, split[2:])
			return
		}
	}
	fmt.Printf("[ cmd] got non-command from %s (%s#%s) in %s (id %s) guild %s (id %s):\n  %s\n",
		m.Message.Author.Mention(), m.Message.Author.Username, m.Message.Author.Discriminator,
		ch.Name, ch.ID, guild.Name, guild.ID,
		m.Message.Content)
}

func (b *Bot) OnMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.mu.RLock()
	mCh, ok := b.channels[m.Message.ChannelID]
	b.mu.RUnlock()

	if !ok {
		b.loadChannel(m.Message.ChannelID, QOSNewMessage)
		b.mu.RLock()
		mCh = b.channels[m.Message.ChannelID]
		b.mu.RUnlock()
	}

	if mCh != nil {
		mCh.AddMessage(m.Message)
	}
}

func (b *Bot) OnChannelDelete(s *discordgo.Session, ev *discordgo.ChannelDelete) {
	b.mu.RLock()
	mCh, ok := b.channels[ev.Channel.ID]
	b.mu.RUnlock()
	if !ok || mCh == nil {
		return
	}

	mCh.Disable()
	b.deleteChannelConfig(mCh.ChannelID)
}

func (b *Bot) OnGuildRemove(s *discordgo.Session, ev *discordgo.GuildDelete) {
	guildID := ev.ID

	var toRemove []*ManagedChannel
	(func() {
		b.mu.RLock()
		defer b.mu.RUnlock()
		for _, mCh := range b.channels {
			if mCh != nil && mCh.GuildID == guildID {
				toRemove = append(toRemove, mCh)
			}
		}

	})()

	for _, mCh := range toRemove {
		mCh.Disable()
		b.deleteChannelConfig(mCh.ChannelID)
	}
	fmt.Printf("[LOG] Removed %v channels from guild %v\n", len(toRemove), guildID)
}

func (b *Bot) OnChannelPins(s *discordgo.Session, ev *discordgo.ChannelPinsUpdate) {
	b.mu.RLock()
	mCh, ok := b.channels[ev.ChannelID]
	b.mu.RUnlock()
	if !ok || mCh == nil {
		return
	}

	disCh, err := b.Channel(ev.ChannelID)
	if err != nil {
		fmt.Println("[pins] error fetching channel:", err)
		return
	}

	if ev.LastPinTimestamp == "" {
		disCh.LastPinTimestamp = ""
	} else {
		disCh.LastPinTimestamp = discordgo.Timestamp(ev.LastPinTimestamp)
	}
	fmt.Printf("[pins] got pins update for %s - new lpts %s\n", mCh, ev.LastPinTimestamp)
	mCh.UpdatePins(ev.LastPinTimestamp)
}

func (b *Bot) OnReady(s *discordgo.Session, m *discordgo.Ready) {
	b.ReportToLogChannel(fmt.Sprintf("AutoDelete started (%d/%d).", b.s.ShardID, b.s.ShardCount))
	go func() {
		err := b.LoadChannelConfigs()
		if err != nil {
			fmt.Println("error loading configs:", err)
		}
	}()
}

func (b *Bot) OnResume(s *discordgo.Session, r *discordgo.Resumed) {
	// force ratelimit of reconnects?
	_, _ = s.User("@me")

	if r.Trace != nil {
		b.ReportToLogChannel(fmt.Sprintf("AutoDelete successfully reconnected (%d/%d) with trace data.\n%s", b.s.ShardID, b.s.ShardCount, strings.Join(r.Trace, "\n")))
	} else {
		b.ReportToLogChannel(fmt.Sprintf("AutoDelete successfully reconnected (%d/%d).", b.s.ShardID, b.s.ShardCount))
	}
	go func() {
		time.Sleep(3 * time.Second)
		b.LoadAllBacklogs()
	}()
}
