package adpub

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultAdChainDepthLimit = 50000
	defaultEntriesDepthLimit = 1000
)

type config struct {
	adChainDepthLimit int64
	entriesDepthLimit int64
	maxSyncRetry      uint64
	syncRetryBackoff  time.Duration
	topic             string
}

// Option is a function that sets a value in a config.
type Option func(*config) error

// getOpts creates a config and applies Options to it.
func getOpts(opts []Option) (config, error) {
	cfg := config{
		adChainDepthLimit: defaultAdChainDepthLimit,
		entriesDepthLimit: defaultEntriesDepthLimit,
		topic:             "/indexer/ingest/mainnet",
		syncRetryBackoff:  500 * time.Millisecond,
	}

	for i, opt := range opts {
		if err := opt(&cfg); err != nil {
			return config{}, fmt.Errorf("option %d failed: %s", i, err)
		}
	}
	return cfg, nil
}

// WithAdChainDepthLimit sets the depth limit when syncing an advertisement
// chain. Setting to 0 means no limit.
func WithAdChainDepthLimit(limit int64) Option {
	return func(c *config) error {
		if limit < 0 {
			return errors.New("ad chain depth limit cannot be negative")
		}
		c.adChainDepthLimit = limit
		return nil
	}
}

// WithSyncRetryBackoff sets the length of time to wait before retrying a faild
// sync. Defaults to 500ms if unset.
func WithSyncRetryBackoff(d time.Duration) Option {
	return func(c *config) error {
		c.syncRetryBackoff = d
		return nil
	}
}

// WithMaxSyncRetry sets the maximum number of times to retry a failed sync.
// Defaults to 10 if unset.
func WithMaxSyncRetry(r uint64) Option {
	return func(c *config) error {
		c.maxSyncRetry = r
		return nil
	}
}

// WithTopicName sets the topic name on which the provider announces advertised
// content. Defaults to '/indexer/ingest/mainnet'.
func WithTopicName(topic string) Option {
	return func(c *config) error {
		c.topic = topic
		return nil
	}
}

// WithEntriesDepthLimit sets the depth limit when syncing an
// advertisement entries chain. Setting to 0 means no limit.
func WithEntriesDepthLimit(depthLimit int64) Option {
	return func(c *config) error {
		if depthLimit < 0 {
			return errors.New("ad entries depth limit cannot be negative")
		}
		c.entriesDepthLimit = depthLimit
		return nil
	}
}
