package dtrack

import (
	"github.com/libp2p/go-libp2p/core/host"
)

type config struct {
	depthLimit int64
	p2pHost    host.Host
}

type Option func(*config)

// getOpts creates a config and applies Options to it.
func getOpts(opts []Option) config {
	cfg := config{
		depthLimit: 5000,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithDepthLimit configures the advertisement chain depth limit.
func WithDepthLimit(limit int64) Option {
	return func(c *config) {
		c.depthLimit = limit
	}
}

// WithP2pHost configures the libp2p host to use for connection to the
// advertisement publisher.
func WithP2pHost(p2pHost host.Host) Option {
	return func(c *config) {
		c.p2pHost = p2pHost
	}
}
