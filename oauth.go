package autodelete

import (
	"fmt"
	"net/http"
	"strconv"

	"strings"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/oauth2"
)

var oauthConfig *oauth2.Config

func (b *Bot) oauthConfig() *oauth2.Config {
	if oauthConfig != nil {
		return oauthConfig
	}
	oauthConfig = &oauth2.Config{
		ClientID:     b.ClientID,
		ClientSecret: b.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discordgo.EndpointOauth2 + "authorize",
			TokenURL: discordgo.EndpointOauth2 + "token",
		},
		Scopes:      []string{"bot"},
		RedirectURL: fmt.Sprintf("%s%s", b.HTTP.Public, "/discord_auto_delete/oauth/callback"),
	}
	return oauthConfig
}

func (b *Bot) OAuthStartURL() string {
	return b.oauthConfig().AuthCodeURL("not_necessary_for_discord",
		oauth2.SetAuthURLParam("permissions", strconv.Itoa(discordgo.PermissionManageMessages)))
}

func (b *Bot) HTTPOAuthStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", b.OAuthStartURL())
	w.WriteHeader(http.StatusFound)
}

func (b *Bot) HTTPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	if r.Form.Get("code") == "" {
		http.Error(w, "no authcode", 400)
		return
	}

	t, err := b.oauthConfig().Exchange(r.Context(), r.Form.Get("code"))
	if err != nil && strings.Contains(err.Error(), "invalid_client") {
		fmt.Fprint(w, "OK, bot joined\nUse '@AutoDelete setup' to get started")
		return
	} else if err != nil {
		fmt.Printf("%T %v", err, err)
		http.Error(w, "An error occured and the bot could not join the server.", http.StatusUnprocessableEntity)
		return
	}
	if guildInfo, ok := t.Extra("guild").(map[string]interface{}); ok {
		if guildID, ok := guildInfo["id"].(string); ok {
			if banned, err := b.storage.IsBanned(guildID); banned {
				b.s.GuildLeave(guildID)
				http.Error(w, "AutoDelete is not available on this server.", http.StatusForbidden)
				fmt.Printf("[INFO] join attempt for banned server %s\n", guildID)
				return
			} else if err != nil {
				fmt.Printf("[ERR] Could not check banlist: %T %v", err, err)
				http.Error(w, "An error occured and the bot may not have joined the server.", http.StatusUnprocessableEntity)
				return
			}
		} else {
			fmt.Printf("[ERR] Unexpected type for guild.id: got %T\n", guildInfo["id"])
		}
	} else {
		fmt.Printf("[ERR] Unexpected type for guild: got %T\n", t.Extra("guild"))
	}

	fmt.Println(t)
	w.WriteHeader(200)
	fmt.Fprint(w, "OK, bot joined\nUse '@AutoDelete setup' to get started")
}
