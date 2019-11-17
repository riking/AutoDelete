package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/bwmarrin/discordgo"
	autodelete "github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

type loggingRoundTripper struct {
	http.RoundTripper
}

func (l *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.RoundTripper.RoundTrip(req)
	if resp != nil {
		req.Write(os.Stderr)
		resp.Header.Write(os.Stderr)
	}
	return resp, err
}

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
		return
	}

	s, err := discordgo.New("Bot " + conf.BotToken)
	if err != nil {
		fmt.Println("bot create error:", err)
		return
	}

	client := &http.Client{
		Transport: &loggingRoundTripper{http.DefaultTransport},
	}
	s.Client = client

	s.ChannelMessagesPinned("566749851585609758")
}
