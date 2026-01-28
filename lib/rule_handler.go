package lib

import (
	"regexp"

	"github.com/52poke/timburr/utils"
)

func ruleTopics(rule utils.RuleConfig) []string {
	var topics []string
	if len(rule.Topics) > 0 {
		topics = rule.Topics
	} else {
		topics = []string{rule.Topic}
	}
	return topics
}

func isBasicRule(rule utils.RuleConfig) bool {
	topics := ruleTopics(rule)
	basicRule := true
	for _, topic := range topics {
		if matched, _ := regexp.MatchString("^\\/.+\\/$", topic); matched {
			basicRule = false
			break
		}
	}
	return basicRule
}

func filterTopics(rule utils.RuleConfig, allTopics []string) []string {
	filtered := []string{}
	topics := ruleTopics(rule)

	for _, topic := range allTopics {
		matched := false
		for _, t := range topics {
			if regexRule, _ := regexp.MatchString("^\\/.+\\/$", t); regexRule {
				matched, _ = regexp.MatchString(t[1:len(t)-1], topic)
				if matched {
					matched = true
					break
				}
			} else if t == topic {
				matched = true
				break
			}
		}
		for _, t := range rule.ExcludeTopics {
			if topic == t {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, topic)
		}
	}
	return filtered
}
