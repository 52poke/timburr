package lib

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/52poke/timburr/lib/task"
	"github.com/52poke/timburr/utils"
	"github.com/segmentio/kafka-go"
	"golang.org/x/time/rate"
)

// BasicSubscription can subscribe to kafka topics and handle task to task runner
type BasicSubscription struct {
	config     *SubscriptionConfig
	rule       utils.RuleConfig
	mutex      sync.Mutex
	reader     *kafka.Reader
	subscribed bool
	cancel     context.CancelFunc
	limiter    *rate.Limiter
}

func (sub *BasicSubscription) topics() []string {
	return ruleTopics(sub.rule)
}

// Subscribe creates a new kafka consumer and start to poll messages
func (sub *BasicSubscription) Subscribe() error {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	if sub.subscribed {
		return nil
	}

	topics := sub.topics()
	sub.reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:     utils.SplitBrokers(sub.config.BrokerList),
		GroupID:     sub.config.GroupIDPrefix + sub.rule.Name,
		GroupTopics: topics,
		StartOffset: kafka.FirstOffset,
	})
	sub.subscribed = true
	if sub.rule.RateLimit > 0 {
		if sub.rule.RateInterval == 0 {
			sub.rule.RateInterval = 1000
		}
		interval := time.Duration(sub.rule.RateInterval) * time.Millisecond
		limit := rate.Limit(float64(sub.rule.RateLimit) / interval.Seconds())
		sub.limiter = rate.NewLimiter(limit, sub.rule.RateLimit)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sub.cancel = cancel
	go sub.consume(ctx)
	slog.Info("subscribed to topics", "topics", topics, "rule", sub.rule.Name)
	return nil
}

func (sub *BasicSubscription) consume(ctx context.Context) {
	for {
		msg, err := sub.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("consume message error", "err", err)
			continue
		}
		if sub.limiter != nil {
			if err := sub.limiter.Wait(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				slog.Warn("rate limit wait error", "err", err)
				continue
			}
		}
		sub.mutex.Lock()
		sub.handleMessage(ctx, msg.Value)
		sub.mutex.Unlock()
		if err := sub.reader.CommitMessages(ctx, msg); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("commit message error", "err", err)
		}
	}
}

func (sub *BasicSubscription) handleMessage(ctx context.Context, km []byte) error {
	executor := task.TypeFromString(sub.rule.TaskType).GetExecutor()
	err := executor.Execute(ctx, km)
	if err != nil {
		slog.Warn("execute message error", "err", err)
	}
	return err
}

// Unsubscribe stops polling messages
func (sub *BasicSubscription) Unsubscribe() {
	sub.mutex.Lock()
	defer sub.mutex.Unlock()
	if !sub.subscribed {
		return
	}
	sub.subscribed = false
	if sub.cancel != nil {
		sub.cancel()
		sub.cancel = nil
	}
	if sub.reader != nil {
		if err := sub.reader.Close(); err != nil {
			slog.Warn("close reader failed", "err", err)
		}
		sub.reader = nil
	}
}
