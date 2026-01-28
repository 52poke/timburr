package utils

import "strings"

// SplitBrokers parses a comma-separated broker list into a cleaned slice.
func SplitBrokers(brokerList string) []string {
	parts := strings.Split(brokerList, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		broker := strings.TrimSpace(part)
		if broker != "" {
			brokers = append(brokers, broker)
		}
	}
	return brokers
}
