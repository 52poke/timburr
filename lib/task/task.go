package task

import (
	"context"
	"sync"
)

// Executor can execute a certain from a kafka message
type Executor interface {
	Execute(ctx context.Context, message []byte) error
}

// Type is an enum for task types
type Type int16

const (
	// JobRunnerTask executes a MediaWiki job via event bus
	JobRunnerTask Type = iota
	// PurgeTask purges the front-end cache of a URL
	PurgeTask
)

var getexMutex sync.Mutex
var taskMap = make(map[Type]Executor)

func (t Type) String() string {
	switch t {
	case JobRunnerTask:
		return "job-runner"
	case PurgeTask:
		return "purge"
	}
	return "unknown"
}

// GetExecutor returns a task executor for a task type
func (t Type) GetExecutor() Executor {
	getexMutex.Lock()
	defer getexMutex.Unlock()

	if executor, ok := taskMap[t]; ok {
		return executor
	}

	var executor Executor
	switch t {
	case JobRunnerTask:
		executor = DefaultJobRunnerExecutor()
	case PurgeTask:
		executor = DefaultPurgeExecutor()
	}
	taskMap[t] = executor
	return executor
}

// TypeFromString returns a task type from a string
func TypeFromString(s string) Type {
	switch s {
	case JobRunnerTask.String():
		return JobRunnerTask
	case PurgeTask.String():
		return PurgeTask
	default:
		return JobRunnerTask
	}
}
