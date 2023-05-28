package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime/debug"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	autodelete "github.com/riking/AutoDelete"
	"gopkg.in/yaml.v2"
)

const (
	defaultConfigFile = "config.yml"
)

var (
	flagShardID        = flag.Int("shard", -1, "shard ID of this bot")
	flagNoHTTP         = flag.Bool("nohttp", false, "skip HTTP handler")
	flagMetricsPort    = flag.Int("metrics", 6130, "port for metrics listener; shard ID is added")
	flagMetricsListen  = flag.String("metricslisten", "127.0.0.4", "address to listen on for metrics handler")
	flagConfigFile     = flag.String("config", defaultConfigFile, "configuration file path")
)

func main() {
	flag.Parse()

	config, err := readConfig(*flagConfigFile)
	if err != nil {
		log.Fatalf("failed to read config file: %v", err)
	}

	if config.BotToken == "" {
		log.Fatal("bot token must be specified")
	}

	if config.Shards > 0 && *flagShardID == -1 {
		log.Fatal("this AutoDelete instance is configured to be sharded; please specify --shard=n")
	}

	if *flagShardID > config.Shards {
		log.Fatal("error: shard number is greater than shard count")
	}

	b := autodelete.New(config)

	err = b.ConnectDiscord(*flagShardID, config.Shards)
	if err != nil {
		log.Fatalf("failed to connect to Discord: %v", err)
	}

	go periodicallyFreeOSMemory()

	if !*flagNoHTTP {
		go startMetricServer(*flagMetricsListen, *flagMetricsPort+*flagShardID)
		go startHTTPServer(config.HTTP.Listen, b)

		fmt.Printf("URL: %s%s\n", config.HTTP.Public, "/discord_auto_delete/oauth/start")
	} else {
		select {}
	}
}

func readConfig(filePath string) (autodelete.Config, error) {
	var config autodelete.Config

	configBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %w", err)
	}

	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return config, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
}

func periodicallyFreeOSMemory() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		debug.FreeOSMemory()
	}
}

func startMetricServer(listenAddress string, port int) {
	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}))

	addr := fmt.Sprintf("%s:%d", listenAddress, port)
	metricServer := &http.Server{
		Handler: mux,
		Addr:    addr,
	}

	err := metricServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Printf("failed to start metric server: %v", err)
		os.Exit(1)
	}
}

func startHTTPServer
