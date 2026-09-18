// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// unkeyableSeq makes each un-marshalable configuration produce a distinct cache
// key, so such a caller never shares a client with anyone else. See
// httpConfigKey.
var unkeyableSeq atomic.Uint64

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

	// At the bound, evict an existing entry rather than handing back a client
	// nobody owns. FetchClient's contract forbids callers from closing the
	// result, so an uncached client would abandon its transport and idle pool
	// on every call -- the exact leak this cache exists to prevent.
	//
	// A borrower holds no lease, so an evicted (and Closed) client can still
	// sit in a caller's hands -- a FetchClient(ctx).Get(...) pair can even
	// straddle the eviction. That race is bounded and deliberately NOT fixed
	// with an acquire/release API:
	//
	//   - Close is a cleanup hint, not a lifecycle terminator: upstream it
	//     only reaps currently-idle connections. It does not mark the client
	//     unusable, and a dial of a new connection proceeds unaffected, so
	//     the borrower's post-eviction request completes normally.
	//   - The worst the race can leave behind is one extra idle pool: the
	//     borrower's post-Close request dials fresh, and those connections
	//     then idle on a client nobody will close again. Every pooled
	//     transport here is a DefaultTransport clone, so IdleConnTimeout
	//     (90s) is preserved: that pool self-reaps, after which the transport
	//     holds no resources and is garbage-collectible.
	//   - The bound is reached only by an embedder rotating through more than
	//     maxCachedClients distinct policies (a CLI process holds one or
	//     two), so the lingering cost is a handful of sockets for at most 90
	//     seconds, in a process already swapping policies frequently.
	//
	// Closing at map removal still earns its keep in the common case -- idle
	// sockets reaped immediately instead of 90 seconds later. A lease API
	// would close the race window entirely but would change FetchClient's
	// exported shape for every caller; against a self-reaping 90-second pool,
	// the documented tradeoff wins. (General client-pooling work is tracked
	// in #847.)
	//
	// Go map iteration order is unspecified, which is an acceptable victim
	// choice here -- every entry is equivalent, and the bound is reached only
	// by a process rotating through more distinct policies than it keeps
	// clients.
	if len(sharedClients) >= maxCachedClients {
		for k, victim := range sharedClients {
			delete(sharedClients, k)
			_ = victim.Close()
			break
		}
	}
	sharedClients[key] = client

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
			// Proxy routing bypasses the dial-time check above (the proxy is
			// what actually gets dialed), so it stays disabled here too
			// unless httpClient.trustedProxy explicitly opts in.
			Transport: ProxyAwareTransport(TrustedProxyFromContext(ctx)),
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
func httpConfigKey(cfg *config.HTTPClientConfig) string {
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
