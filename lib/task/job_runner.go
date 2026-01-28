package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/52poke/timburr/utils"
)

// JobRunnerExecutor executes MediaWiki jobs via event bus
type JobRunnerExecutor struct {
	endpoint      string
	excludeFields []string
	client        *http.Client
}

// DefaultJobRunnerExecutor creates a job runner executor based on config.yml
func DefaultJobRunnerExecutor() *JobRunnerExecutor {
	return NewJobRunnerExecutor(utils.Config.JobRunner.Endpoint, utils.Config.JobRunner.ExcludeFields)
}

// NewJobRunnerExecutor creates a new job runner executor
func NewJobRunnerExecutor(endpoint string, excludeFields []string) *JobRunnerExecutor {
	return &JobRunnerExecutor{
		endpoint:      endpoint,
		excludeFields: excludeFields,
		client: &http.Client{
			Timeout: time.Second * 180,
		},
	}
}

// Execute sends a job in the kafka message to the endpoint
func (t *JobRunnerExecutor) Execute(ctx context.Context, message []byte) error {
	var messageMap map[string]interface{}
	if err := json.Unmarshal(message, &messageMap); err != nil {
		return err
	}
	for _, f := range t.excludeFields {
		delete(messageMap, f)
	}
	rb, err := json.Marshal(messageMap)
	if err != nil {
		return err
	}
	err = t.retryExecute(ctx, rb, 4, time.Second)
	if err != nil {
		return err
	}
	slog.Info("job executed", "payload", messageMap)
	return nil
}

func (t *JobRunnerExecutor) retryExecute(ctx context.Context, message []byte, times int, wait time.Duration) error {
	if err := t.doExecute(ctx, message); err != nil {
		times--
		if times > 0 {
			time.Sleep(wait)
			return t.retryExecute(ctx, message, times, 2*wait)
		}
		return err
	}
	return nil
}

func (t *JobRunnerExecutor) doExecute(ctx context.Context, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, t.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewBuffer(message))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("job runner execute failed, response: %v", string(body))
	}
	return nil
}
