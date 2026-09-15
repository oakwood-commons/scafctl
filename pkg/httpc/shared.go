// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// maxCachedClients bounds how many distinct configurations shared clients are
// held for. Application configuration is loaded once per process, so in
// practice there are one or two. The bound only guards against an embedder that
// swaps configuration repeatedly; past it, callers get an unshared client
// rather than growing the map without limit.
const maxCachedClients = 8

// Client shapes. Each shape is built differently, so a configuration key alone
// is not enough to decide two callers can share -- the shape forms part of the
// key.
const (
	shapeFetch = "fetch"
)

var (
	sharedClientsMu sync.Mutex
	sharedClients   = make(map[string]*Client, 2)
)

// sharedClient returns the client stored under key, calling build on first use.
//
// Clients are reused because each one owns its own connection pool. Building a
// new client per call discards the open connection and pays a fresh TLS
// handshake every time, which measured roughly 5x the cost of reusing one, and
// leaks the abandoned pool for the life of the process since nothing closes it.
//
// Reuse is keyed on the configuration, so a client is only ever handed to a
// caller governed by the same rules that were in force when its connections
// were opened. A connection cannot be inherited by a request that should have
// been refused. Callers with different configurations share nothing.
func sharedClient(key string, build func() *Client) *Client {
	sharedClientsMu.Lock()
	defer sharedClientsMu.Unlock()

	if client, ok := sharedClients[key]; ok {
		return client
	}

	client := build()
	if len(sharedClients) < maxCachedClients {
		sharedClients[key] = client
	}
	return client
}

// FetchClient returns the shared client for retrieving a URL under the
// destination-address policy carried on ctx.
//
// The returned client is shared. Do NOT call Close on it: that would shut down
// connections still in use by other callers. Its idle connections persist for
// the life of the process, which is the normal lifetime for a shared HTTP
// client.
func FetchClient(ctx context.Context) *Client {
	cfg := config.FromContext(ctx)
	key := shapeFetch + "|" + configKey(cfg)

	return sharedClient(key, func() *Client {
		return NewClient(&ClientConfig{
			Timeout:           settings.DefaultHTTPTimeout,
			RetryMax:          settings.DefaultHTTPRetryMax,
			RetryWaitMin:      settings.DefaultHTTPRetryWaitMinimum,
			RetryWaitMax:      settings.DefaultHTTPRetryWaitMaximum,
			EnableCache:       false,
			EnableCompression: true,
			// A nil policy denies private, loopback, and link-local
			// addresses, so a missing or unusable configuration fails closed.
			IPPolicy: PolicyFromContext(ctx),
		})
	})
}

// configKey returns a stable identifier for the HTTP client configuration on
// cfg, or a distinct value when there is none.
func configKey(cfg *config.Config) string {
	if cfg == nil {
		// Matches PolicyFromContext's fail-closed default for a missing config.
		return "nocfg"
	}
	return httpConfigKey(&cfg.HTTPClient)
}

// httpConfigKey returns a stable identifier for an HTTP client configuration.
//
// It serializes the whole struct rather than naming individual fields so that
// adding a setting cannot silently produce a key collision, which would hand a
// caller a client built under different rules. Go's JSON encoder emits struct
// fields in declaration order, so the result is stable for a given build.
//
// JSON alone is not sufficient: `omitempty` erases the difference between an
// absent field and an empty one. That distinction is load-bearing for
// AllowedPrivateCIDRs -- an empty list means "no exceptions" and overrides
// AllowPrivateIPs, while an absent list leaves it in force -- so it is encoded
// explicitly. A future field whose absent and empty forms mean different things
// needs the same treatment.
//
// Slice order is preserved deliberately: two allowlists differing only in order
// describe the same policy, but treating them as distinct costs at most one
// extra cached client, whereas normalizing on every call costs every call.
func httpConfigKey(cfg *config.HTTPClientConfig) string {
	if cfg == nil {
		return "nilhttp"
	}

	var cidrs string
	switch {
	case cfg.AllowedPrivateCIDRs == nil:
		cidrs = "unset"
	case len(cfg.AllowedPrivateCIDRs) == 0:
		cidrs = "empty"
	default:
		cidrs = "set"
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		// Unreachable for this struct, but a key that never matches is the
		// safe failure: callers get their own client rather than someone
		// else's.
		return "unkeyable-" + strconv.FormatInt(int64(len(cfg.AllowedPrivateCIDRs)), 10)
	}
	return string(raw) + "|cidrs=" + cidrs
}
