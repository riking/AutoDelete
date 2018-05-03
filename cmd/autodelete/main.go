package main

import "fmt"
import (
	"io/ioutil"
	"net/http"

	"github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

func main() {
	var conf autodelete.Config

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

	b := autodelete.New(conf)

	err = b.ConnectDiscord()
	if err != nil {
		fmt.Println("connect error:", err)
		return
	}

	fmt.Printf("url: %s%s\n", conf.HTTP.Public, "/discord_auto_delete/oauth/start")
	http.HandleFunc("/discord_auto_delete/oauth/start", b.HTTPOAuthStart)
	http.HandleFunc("/discord_auto_delete/oauth/callback", b.HTTPOAuthCallback)
	http.ListenAndServe(conf.HTTP.Listen, nil)
}
