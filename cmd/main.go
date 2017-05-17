package main

import "fmt"
import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"github.com/riking/AutoDelete"
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

}
