package lib

import (
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// MetadataWatcherEvent defines an event, it's either the topics in kafka changes, or an error occurs
type MetadataWatcherEvent struct {
	Topics []string
	Err    error
}

// MetadataWatcher watches the change of topics and notify event channels
type MetadataWatcher struct {
	mutex         sync.RWMutex
	knownTopics   []string
	brokers       []string
	stopChan      chan bool
	eventChannels []chan MetadataWatcherEvent
}

// NewMetadataWatcher creates a new metadata watcher with a broker list
func NewMetadataWatcher(brokers []string, refreshInterval time.Duration) (*MetadataWatcher, error) {
	watcher := &MetadataWatcher{
		brokers:       brokers,
		knownTopics:   []string{},
		eventChannels: []chan MetadataWatcherEvent{},
	}
	stopChan, err := watcher.setup(refreshInterval)
	if err != nil {
		return nil, err
	}
	watcher.stopChan = stopChan
	return watcher, nil
}

func (mw *MetadataWatcher) setup(refreshInterval time.Duration) (chan bool, error) {
	topics, err := mw.GetTopics()
	if err != nil {
		return nil, err
	}
	mw.knownTopics = topics

	ticker := time.NewTicker(refreshInterval)
	stopChan := make(chan bool, 1)

	go func() {
		for {
			select {
			case <-ticker.C:
				topics, err := mw.GetTopics()
				added := false
				if err != nil {
					slog.Warn("get topic error", "err", err)
					mw.emit(nil, err)
					continue
				}
				for _, topic := range topics {
					exist := false
					for _, t := range mw.knownTopics {
						if topic == t {
							exist = true
							break
						}
					}
					if !exist {
						added = true
						break
					}
				}
				if added {
					slog.Info("topic changed", "topics", topics)
					mw.emit(topics, nil)
					mw.knownTopics = topics
				}
			case <-stopChan:
				ticker.Stop()
				return
			}
		}
	}()

	return stopChan, nil
}

func (mw *MetadataWatcher) emit(topics []string, err error) {
	mw.mutex.RLock()
	defer mw.mutex.RUnlock()

	chans := append([]chan MetadataWatcherEvent{}, mw.eventChannels...)
	go func(eventChannels []chan MetadataWatcherEvent, event MetadataWatcherEvent) {
		for _, ch := range eventChannels {
			ch <- event
		}
	}(chans, MetadataWatcherEvent{Topics: topics, Err: err})
}

// AddListener adds a event channel to the metadata watcher
func (mw *MetadataWatcher) AddListener(ch chan MetadataWatcherEvent) {
	mw.mutex.Lock()
	defer mw.mutex.Unlock()
	mw.eventChannels = append(mw.eventChannels, ch)
}

// RemoveListener removes a event channel from the metadata watcher
func (mw *MetadataWatcher) RemoveListener(ch chan MetadataWatcherEvent) {
	mw.mutex.Lock()
	defer mw.mutex.Unlock()
	for i, c := range mw.eventChannels {
		if ch == c {
			mw.eventChannels = append(mw.eventChannels[:i], mw.eventChannels[i+1:]...)
			break
		}
	}
}

// GetTopics lists all topics from kafka
func (mw *MetadataWatcher) GetTopics() ([]string, error) {
	if len(mw.brokers) == 0 {
		return nil, errors.New("no kafka brokers configured")
	}
	var lastErr error
	for _, broker := range mw.brokers {
		conn, err := kafka.Dial("tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}
		partitions, err := conn.ReadPartitions()
		conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		topicsMap := map[string]struct{}{}
		for _, partition := range partitions {
			topicsMap[partition.Topic] = struct{}{}
		}
		topics := make([]string, 0, len(topicsMap))
		for topic := range topicsMap {
			topics = append(topics, topic)
		}
		sort.Strings(topics)
		return topics, nil
	}
	if lastErr == nil {
		lastErr = errors.New("failed to connect to kafka brokers")
	}
	return nil, lastErr
}

// Disconnect closes the metadata watcher
func (mw *MetadataWatcher) Disconnect() error {
	mw.stopChan <- true
	return nil
}
