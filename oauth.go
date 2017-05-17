package autodelete

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) OAuthStartURL() string {
	form := url.Values{
		"client_id": []string{b.ClientID},
		"scope": []string{"bot"},
	}
	perms := discordgo.PermissionManageMessages
	form.Set("permissions", strconv.Itoa(perms))
	return discordgo.EndpointOauth2 + "authorize" + "?" + form.Encode()
}

func HTTPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad form", 400)
		return
	}
}
