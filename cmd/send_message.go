package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/bwmarrin/discordgo"
	autodelete "github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

var flagReason = flag.String("reason", "(MISSING)", "explanation of what server they are responsible for")
var flagMessageFile = flag.String("file", "message.txt", "file with the message to send; must contain one %s")

var messageContent = `[A message from the developer]
Reason: %s`

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
	if *flagReason == "(MISSING)" {
		fmt.Println("please provide a message reason.")
		return
	}

	s, err := discordgo.New("Bot " + conf.BotToken)
	if err != nil {
		fmt.Println("bot create error:", err)
		return
	}

	messageBytes, err := ioutil.ReadFile(*flagMessageFile)
	if err != nil {
		fmt.Println("could not read message file:", err)
		return
	}
	messageContent = fmt.Sprintf(string(messageBytes), *flagReason)

	for _, userID := range flag.Args() {
		channel, err := s.UserChannelCreate(userID)
		if err != nil {
			fmt.Printf("failed to open DM channel with %s: %v\n", userID, err)
			return
		}
		fmt.Printf("Sending to %s (ch id %s)\n", userID, channel.ID)
		_, err = s.ChannelMessageSend(channel.ID, messageContent)
		if err != nil {
			fmt.Printf("failed to send: %v\n", err)
			continue
		}
	}
}
