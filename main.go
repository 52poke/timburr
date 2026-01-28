package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/52poke/timburr/lib"
	"github.com/52poke/timburr/server"
	"github.com/52poke/timburr/utils"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := utils.InitConfig(); err != nil {
		slog.Error("config init failed", "err", err)
		panic(err)
	}

	server, err := server.NewTimburrServer(&server.ServerConfig{
		BrokerList:   utils.Config.Kafka.BrokerList,
		Listen:       utils.Config.Options.Listen,
		TopicKey:     utils.Config.Options.TopicKey,
		DefaultTopic: utils.Config.Options.DefaultTopic,
	})
	if err != nil {
		slog.Error("create server failed", "err", err)
		panic(err)
	}

	go server.Start()

	sub := lib.DefaultSubscriber()
	for _, rule := range utils.Config.Rules {
		if err := sub.Subscribe(rule); err != nil {
			slog.Error("subscribe rule failed", "rule", rule.Name, "err", err)
			panic(err)
		}
	}

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	<-sigchan
	sub.Unsubscribe()
	server.Close()
}
