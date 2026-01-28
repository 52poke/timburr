package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/tidwall/gjson"

	"github.com/52poke/timburr/utils"
)

type ServerConfig struct {
	BrokerList   string
	Listen       string
	TopicKey     string
	DefaultTopic string
}

type TimburrServer struct {
	producer *kafka.Writer
	server   *http.Server
	config   *ServerConfig
}

func NewTimburrServer(config *ServerConfig) (*TimburrServer, error) {
	s := &TimburrServer{
		config: config,
	}
	if err := s.setup(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *TimburrServer) setup() error {
	brokers := utils.SplitBrokers(s.config.BrokerList)
	s.producer = kafka.NewWriter(kafka.WriterConfig{
		Brokers:  brokers,
		Balancer: &kafka.LeastBytes{},
	})

	s.server = &http.Server{Addr: s.config.Listen, Handler: s}
	return nil
}

func (s *TimburrServer) sendMessage(data gjson.Result) error {
	topic := data.Get(s.config.TopicKey).String()
	if topic == "" {
		topic = s.config.DefaultTopic
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.producer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: []byte(data.Raw),
	})
	if err != nil {
		slog.Warn("http produce error", "err", err)
	}
	return err
}

func (s *TimburrServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("read request failed"))
		return
	}

	json := gjson.Parse(string(body))
	if json.IsArray() {
		json.ForEach(func(key, value gjson.Result) bool {
			if err = s.sendMessage(value); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("send to kafka error: " + err.Error()))
				return false
			}
			return true
		})
	} else {
		if err = s.sendMessage(json); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("send to kafka error: " + err.Error()))
			return
		}
	}

	if err == nil {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *TimburrServer) Start() error {
	slog.Info("server start", "listen", s.config.Listen)
	return s.server.ListenAndServe()
}

func (s *TimburrServer) Close() error {
	if err := s.producer.Close(); err != nil {
		return err
	}
	return s.server.Close()
}
