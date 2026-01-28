package utils

import (
	"github.com/jinzhu/configor"
)

// RuleConfig is the configuration of a rule
type RuleConfig struct {
	Name          string   `yaml:"name"`
	Topic         string   `yaml:"topic"`
	Topics        []string `yaml:"topics"`
	ExcludeTopics []string `yaml:"excludeTopics"`
	Filter        string   `yaml:"filter"`
	TaskType      string   `yaml:"taskType"`
	RateLimit     int      `yaml:"rateLimit"`
	RateInterval  int64    `yaml:"rateInterval"`
}

// PurgeEntryConfig defines how to generate purge requests for different hosts
type PurgeEntryConfig struct {
	Host      string            `yaml:"host"`
	PathMatch string            `yaml:"pathMatch"`
	Method    string            `yaml:"method"`
	URIs      []string          `yaml:"uris"`
	Headers   map[string]string `yaml:"headers"`
	Variants  []string          `toml:"variants"`
}

// Config is the configuration of timburr
var Config = struct {
	Kafka struct {
		BrokerList string `yaml:"brokerList"`
	} `yaml:"kafka"`

	Options struct {
		GroupIDPrefix                string `yaml:"groupIDPrefix"`
		MetadataWatchRefreshInterval int64  `yaml:"metadataWatchRefreshInterval"`
		Listen                       string `yaml:"listen"`
		TopicKey                     string `yaml:"topicKey"`
		DefaultTopic                 string `yaml:"defaultTopic"`
	} `yaml:"options"`

	JobRunner struct {
		Endpoint      string   `yaml:"endpoint"`
		ExcludeFields []string `yaml:"excludeFields"`
	} `yaml:"jobRunner"`

	Purge struct {
		Expiry   int64              `yaml:"expiry"`
		Entries  []PurgeEntryConfig `yaml:"entries"`
		CFToken  string             `yaml:"cfToken"`
		CFZoneID string             `yaml:"cfZoneID"`
	} `yaml:"purge"`

	Rules []RuleConfig `yaml:"rules"`
}{}

// InitConfig initializes the configuration
func InitConfig() error {
	return configor.New(&configor.Config{ENVPrefix: "TIMBURR"}).Load(&Config, "conf/config.yml")
}
