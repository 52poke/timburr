package lib

import (
	"github.com/52poke/timburr/utils"
)

// SubscriptionConfig is the configuration for all subscriptions
type SubscriptionConfig struct {
	BrokerList    string
	GroupIDPrefix string
}

// Subscription contains basic subscription and regex subscription
type Subscription interface {
	Subscribe() error
	Unsubscribe()
}

// NewSubscription creates a new subscription with configuration and rule
func NewSubscription(config *SubscriptionConfig, rule utils.RuleConfig) Subscription {
	var s Subscription
	if isBasicRule(rule) {
		s = &BasicSubscription{
			rule:   rule,
			config: config,
		}
	} else {
		s = &RegexSubscription{
			rule:   rule,
			config: config,
		}
	}
	return s
}
