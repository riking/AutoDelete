package main

import (
	"fmt"
	"flag"
	"io/ioutil"
	"net/http"
	rdebug "runtime/debug"

	"github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

var flagShardCount = flag.Int("shard", -1, "shard ID of this bot")
var flagNoHttp = flag.Bool("nohttp", false, "skip http handler")

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
	}
	if conf.Shards > 0 && *flagShardCount == -1 {
		fmt.Println("This AutoDelete instance is configured to be sharded; please specify --shard=n")
		return
	}
	if *flagShardCount > conf.Shards {
		fmt.Println("error: shard nbr is > shard count")
		return
	}

	b := autodelete.New(conf)

	err = b.ConnectDiscord(*flagShardCount, conf.Shards)
	if err != nil {
		fmt.Println("connect error:", err)
		return
	}

	go func() {
		for {
			time.Sleep(time.Hour*1)
			rdebug.FreeOSMemory()
		}
	}()

	if !*flagNoHttp {
		fmt.Printf("url: %s%s\n", conf.HTTP.Public, "/discord_auto_delete/oauth/start")
		http.HandleFunc("/discord_auto_delete/oauth/start", b.HTTPOAuthStart)
		http.HandleFunc("/discord_auto_delete/oauth/callback", b.HTTPOAuthCallback)
		err = http.ListenAndServe(conf.HTTP.Listen, nil)
		fmt.Println("exiting main()", err)
	} else {
		select{}
	}
}
