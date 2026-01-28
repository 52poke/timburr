package task

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/52poke/timburr/utils"
	"github.com/cloudflare/cloudflare-go"
	"golang.org/x/sync/errgroup"
)

// PurgeExecutor purges the front-end cache by URLs
type PurgeExecutor struct {
	expiry   time.Duration
	entries  []utils.PurgeEntryConfig
	client   *http.Client
	cfAPI    *cloudflare.API
	cfZoneID string
}

type requestOptions struct {
	method  string
	url     string
	headers map[string]string
}

// DefaultPurgeExecutor creates a purge executor based on config.yml
func DefaultPurgeExecutor() *PurgeExecutor {
	var cfAPI *cloudflare.API
	var err error
	if utils.Config.Purge.CFToken != "" {
		cfAPI, err = cloudflare.NewWithAPIToken(utils.Config.Purge.CFToken)
		if err != nil {
			slog.Error("cloudflare api invalid", "err", err)
		}
	}
	return NewPurgeExecutor(
		time.Millisecond*time.Duration(utils.Config.Purge.Expiry),
		utils.Config.Purge.Entries,
		cfAPI,
		utils.Config.Purge.CFZoneID,
	)
}

// NewPurgeExecutor creates a new purge executor
func NewPurgeExecutor(expiry time.Duration, entries []utils.PurgeEntryConfig, cfAPI *cloudflare.API, cfZoneID string) *PurgeExecutor {
	return &PurgeExecutor{
		expiry:  expiry,
		entries: entries,
		client: &http.Client{
			Timeout: time.Second * 2,
		},
		cfAPI:    cfAPI,
		cfZoneID: cfZoneID,
	}
}

// Execute sends the corresponding PURGE requests from a kafka message
func (t *PurgeExecutor) Execute(ctx context.Context, message []byte) error {
	type purgeData struct {
		Meta struct {
			URI  string    `json:"uri"`
			Date time.Time `json:"dt"`
		} `json:"meta"`
	}
	var msg purgeData
	err := json.Unmarshal(message, &msg)
	if err != nil {
		return err
	}

	// skip expired message
	if time.Now().After(msg.Meta.Date.Add(t.expiry)) {
		slog.Info("skip purge", "url", msg.Meta.URI)
		return nil
	}

	t.handlePurge(ctx, msg.Meta.URI, msg.Meta.Date)
	return nil
}

func (t *PurgeExecutor) handlePurge(ctx context.Context, item string, date time.Time) {
	ros := []requestOptions{}
	u, err := url.Parse(item)
	if err != nil {
		return
	}
	queries := strings.Split(u.RawQuery, "&")
	lastQuery := ""
	if len(queries) > 0 {
		lastQuery = queries[len(queries)-1]
	}
	pathComponents := strings.Split(u.Path, "/")
	firstPath := ""
	if len(pathComponents) > 1 {
		firstPath = pathComponents[1]
	}

	for _, entry := range t.entries {
		if entry.Host != u.Host {
			continue
		}
		if entry.PathMatch != "" {
			match, err := regexp.MatchString(entry.PathMatch, u.Path)
			if err != nil {
				slog.Warn("invalid pathMatch regex", "host", entry.Host, "pathMatch", entry.PathMatch, "err", err)
				continue
			}
			if !match {
				continue
			}
		}
		variants := make(map[string]bool)
		variants[""] = true
		for _, variant := range entry.Variants {
			variants[variant] = true
		}

		for _, uri := range entry.URIs {
			for variant := range variants {
				if variant != "" && ((lastQuery != "" && variants[lastQuery]) ||
					(firstPath != "" && variants[firstPath])) {
					continue
				}
				ros = append(ros, requestOptions{
					method:  entry.Method,
					url:     strings.ReplaceAll(strings.ReplaceAll(uri, "#url#", u.RequestURI()), "#variants#", variant),
					headers: entry.Headers,
				})

				if firstPath == "wiki" && variant != "" {
					ros = append(ros, requestOptions{
						method:  entry.Method,
						url:     strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(uri, "#url#", u.RequestURI()), "#variants#", ""), "/wiki/", "/"+variant+"/"),
						headers: entry.Headers,
					})
				}
			}
		}
	}

	ros = uniq(ros)
	eg, egCtx := errgroup.WithContext(ctx)
	dateStr := date.Format(time.RFC3339)
	for _, ro := range ros {
		ro := ro
		eg.Go(func() error {
			return t.doRequest(egCtx, ro.method, ro.url, ro.headers, item, dateStr)
		})
	}
	if err := eg.Wait(); err != nil {
		slog.Warn("purge request failed", "err", err)
	}
}

func (t *PurgeExecutor) doRequest(ctx context.Context, method, url string, headers map[string]string, rawURL, dateStr string) error {
	if strings.ToLower(method) == "cloudflare" {
		return t.doCloudFlarePurge(ctx, url)
	}
	ctx, cancel := context.WithTimeout(ctx, t.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		slog.Warn("failed to send purge request", "url", url, "err", err)
		return err
	}
	for k, v := range headers {
		value := strings.ReplaceAll(v, "#url#", rawURL)
		value = strings.ReplaceAll(value, "#date#", dateStr)
		req.Header.Set(k, value)
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
	}
	response, err := t.client.Do(req)
	if err != nil {
		slog.Warn("failed to send purge request", "url", url, "err", err)
		return err
	}
	response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		slog.Info("purge success", "url", url)
	} else if response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusBadRequest && response.StatusCode >= 300 {
		slog.Warn("failed to send purge request", "url", url, "statusCode", response.StatusCode)
	}
	return nil
}

func (t *PurgeExecutor) doCloudFlarePurge(ctx context.Context, url string) error {
	if t.cfAPI == nil {
		slog.Warn("failed to purge cloudflare cache", "url", url)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, t.client.Timeout)
	defer cancel()
	resp, err := t.cfAPI.PurgeCache(ctx, t.cfZoneID, cloudflare.PurgeCacheRequest{
		Files: []string{url},
	})
	if err != nil {
		slog.Warn("failed to purge cloudflare cache", "url", url, "err", err)
		return err
	}
	slog.Info("purge success", "url", url, "response", resp)
	return nil
}

func uniq(input []requestOptions) (res []requestOptions) {
	res = make([]requestOptions, 0, len(input))
	seen := make(map[string]bool)
	for _, val := range input {
		if _, ok := seen[val.url]; !ok {
			seen[val.url] = true
			res = append(res, val)
		}
	}
	return
}
