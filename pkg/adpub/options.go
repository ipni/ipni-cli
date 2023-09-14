package adpub

import (
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
)

const (
	defaultEntriesDepthLimit = 1000
)

type config struct {
	entriesDepthLimit int64
	p2pHost           host.Host
	maxSyncRetry      uint64
	syncRetryBackoff  time.Duration
	topic             string
}

// Option is a function that sets a value in a config.
type Option func(*config) error

// getOpts creates a config and applies Options to it.
func getOpts(opts []Option) (config, error) {
	cfg := config{
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

// WithLibp2pHost configures the client to use an existing libp2p host.
func WithLibp2pHost(h host.Host) Option {
	return func(c *config) error {
		c.p2pHost = h
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
