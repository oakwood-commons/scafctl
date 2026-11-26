// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// unkeyableSeq makes each un-marshalable configuration produce a distinct cache
// key, so such a caller never shares a client with anyone else. See
// HTTPConfigKey.
var unkeyableSeq atomic.Uint64

// DefaultClientPoolCap bounds how many distinct configurations a ClientPool
// retains by default. Application configuration is loaded once per process, so
// in practice there are one or two entries; past the bound an existing entry is
// evicted (and closed) to store the new client rather than growing without
// limit.
const DefaultClientPoolCap = 16

// Client shapes. Each shape is built differently, so a configuration key alone
// is not enough to decide two callers can share -- the shape forms part of the
// key.
const (
	shapeFetch = "fetch"
	shapeApp   = "app"
)

var defaultPool = NewClientPool(DefaultClientPoolCap)

// ClientPool is a bounded, concurrency-safe cache of *Client instances keyed
// on configuration.
//
// Pooling *Client instances directly -- rather than sharing a raw
// *http.Transport across per-call NewClient constructions -- is required
// because upstream clones *http.Transport on every NewClient call so each
// client owns an isolated idle-connection pool enforced by its dial-time
// IPPolicy.
type ClientPool struct {
	mu       sync.Mutex
	clients  map[string]*Client
	capacity int
}

// NewClientPool returns a ClientPool bounded to capacity entries. A zero or
// negative capacity defaults to DefaultClientPoolCap.
func NewClientPool(capacity int) *ClientPool {
	if capacity <= 0 {
		capacity = DefaultClientPoolCap
	}
	return &ClientPool{
		clients:  make(map[string]*Client, 2),
		capacity: capacity,
	}
}

// GetOrBuild returns the client stored under key, calling build on first use.
//
// Clients are reused because each one owns its own connection pool. Building a
// new client per call discards open connections, pays a fresh TCP/TLS
// handshake every time, and abandons an idle pool unless closed.
//
// Reuse must be keyed on every configuration input that governs dial policy,
// proxy selection, caching, timeouts, and retries so a client is only handed
// to callers governed by identical rules.
//
// At capacity, an existing entry is evicted and closed rather than returning an
// unowned client:
//
//   - Close is a cleanup hint, not a lifecycle terminator: upstream reaps
//     currently-idle connections without marking the client unusable, so a
//     borrower whose request races an eviction dials fresh and completes
//     normally.
//   - Any idle pool opened by such a post-eviction request self-reaps after
//     IdleConnTimeout (90s) on the cloned DefaultTransport.
func (p *ClientPool) GetOrBuild(key string, build func() *Client) *Client {
	var evicted *Client
	defer func() {
		if evicted != nil {
			_ = evicted.Close()
		}
	}()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.capacity <= 0 {
		p.capacity = DefaultClientPoolCap
	}
	if p.clients == nil {
		p.clients = make(map[string]*Client, 2)
	}

	if client, ok := p.clients[key]; ok {
		return client
	}

	client := build()
	if client == nil {
		return nil
	}

	if len(p.clients) >= p.capacity {
		for k, victim := range p.clients {
			delete(p.clients, k)
			evicted = victim
			break
		}
	}
	p.clients[key] = client

	return client
}

// Close closes every pooled client and clears the pool. The pool remains safe
// for subsequent GetOrBuild calls, which populate fresh clients on demand.
func (p *ClientPool) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	entries := p.clients
	p.clients = make(map[string]*Client)
	p.mu.Unlock()

	var errs []error
	for _, client := range entries {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Len returns the number of clients currently held in the pool.
func (p *ClientPool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.clients)
}

// FetchClient returns the shared non-caching client for retrieving a URL under
// the application HTTP client configuration carried on ctx.
//
// All settings from cfg.HTTPClient (timeouts, retries, circuit breaker,
// compression, max response body size, dial-time IPPolicy, and trusted-proxy
// transport) are honored while EnableCache is forced false so callers never
// serve stale or header-insensitive cached responses.
//
// The returned client is shared. Do NOT call Close on it.
func FetchClient(ctx context.Context) *Client {
	return FetchClientFromConfig(config.FromContext(ctx))
}

// FetchClientFromConfig returns the shared non-caching client for cfg (nil uses
// secure defaults). All cfg.HTTPClient settings are applied while EnableCache
// is forced false.
//
// The returned client is shared. Do NOT call Close on it.
func FetchClientFromConfig(cfg *config.Config) *Client {
	key := shapeFetch + "|" + ConfigKey(cfg)

	return defaultPool.GetOrBuild(key, func() *Client {
		var httpCfg *config.HTTPClientConfig
		if cfg != nil {
			httpCfg = &cfg.HTTPClient
		}
		clientCfg := httpClientConfigFromAppConfig(httpCfg, logr.Discard())
		clientCfg.EnableCache = false
		return NewClient(clientCfg)
	})
}

// AppClient returns the shared client honoring the full application HTTP
// client configuration carried on ctx, including response caching.
//
// The returned client is shared. Do NOT call Close on it.
func AppClient(ctx context.Context) *Client {
	return AppClientFromConfig(config.FromContext(ctx))
}

// AppClientFromConfig returns the shared client honoring cfg.HTTPClient (nil
// uses secure defaults), including EnableCache.
//
// The returned client is shared. Do NOT call Close on it.
func AppClientFromConfig(cfg *config.Config) *Client {
	key := shapeApp + "|" + ConfigKey(cfg)

	return defaultPool.GetOrBuild(key, func() *Client {
		var httpCfg *config.HTTPClientConfig
		if cfg != nil {
			httpCfg = &cfg.HTTPClient
		}
		return NewClient(httpClientConfigFromAppConfig(httpCfg, logr.Discard()))
	})
}

// ConfigKey returns a stable identifier for the HTTP client configuration on
// cfg, prefixed with the active application name so embedders (for example
// "mycli") never collide on default cache paths or cache key prefixes.
func ConfigKey(cfg *config.Config) string {
	return configKeyForApp(paths.AppName(), paths.HTTPCacheDir(), cfg)
}

func configKeyForApp(appName, defaultCacheDir string, cfg *config.Config) string {
	prefix := appName + "|"
	if cfg == nil {
		// Matches PolicyFromContext's fail-closed default for a missing config.
		return prefix + defaultCacheDir + "|nocfg"
	}
	if cfg.HTTPClient.CacheDir == "" {
		return prefix + defaultCacheDir + "|" + HTTPConfigKey(&cfg.HTTPClient)
	}
	return prefix + HTTPConfigKey(&cfg.HTTPClient)
}

// HTTPConfigKey returns a stable identifier for an HTTP client configuration.
//
// It serializes the whole struct rather than naming individual fields so that
// adding a setting cannot silently produce a key collision, which would hand a
// caller a client built under different rules. Go's JSON encoder emits struct
// fields in declaration order, so the result is stable for a given build.
//
// JSON's `omitempty` erases the difference between an absent field and an empty
// one, which would be fatal here: for AllowedPrivateCIDRs an empty list means
// "no exceptions" and overrides AllowPrivateIPs, while an absent list leaves it
// in force. That field is a *[]string precisely so the two survive -- a nil
// pointer is omitted, a pointer to an empty slice encodes as "[]". Any future
// field whose absent and empty forms differ needs the same treatment, or it
// will collide here and hand a caller a client built under other rules.
//
// Slice order is preserved deliberately: two allowlists differing only in order
// describe the same policy, but treating them as distinct costs at most one
// extra cached client, whereas normalizing on every call costs every call.
func HTTPConfigKey(cfg *config.HTTPClientConfig) string {
	if cfg == nil {
		return "nilhttp"
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		// Unreachable for this struct (it holds no channels or funcs), but a
		// key that never matches another caller's is the safe failure: they get
		// their own client rather than one built under rules we could not read.
		// The cache's own eviction keeps this from growing without bound.
		return "unkeyable-" + strconv.FormatUint(unkeyableSeq.Add(1), 10)
	}
	return string(raw)
}
