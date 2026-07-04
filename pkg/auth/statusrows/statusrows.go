// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package statusrows contains the domain logic for expanding an auth handler's
// status into per-cluster (per-instance) sessions. It is consumed by the CLI
// auth status/list commands (and reusable by embedders) so the command layer
// stays thin: the command builds display rows, this package decides which
// clusters, which representative token per cluster, which session is active,
// and how each cluster is labeled.
package statusrows

import (
	"context"
	"sort"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// InstanceSession is one per-cluster cached session selected for display.
type InstanceSession struct {
	// ClusterLabel is the display label for the cluster: the reverse-mapped
	// alias when the token hostname matches a configured/resolved alias, else
	// the trimmed host.
	ClusterLabel string
	// Active marks the most recently used session (newest CachedAt).
	Active bool
	// Token is the representative cached token for the cluster.
	Token *auth.CachedTokenInfo
}

// Expand returns one session per cached cluster when the handler advertises
// CapInstanceHostname and exposes per-instance cached tokens (each carrying a
// Hostname). It returns nil when the handler is not an instance handler, is
// unauthenticated, does not implement TokenLister, or has no hostname-bearing
// cached tokens -- callers then render the handler's single base row unchanged.
//
// Sessions are one per cluster (deduplicated by Hostname): when a handler
// returns several tokens for the same instance -- e.g. a user login plus minted
// service-account tokens -- a single representative token is chosen (a
// user/login flow wins over a machine flow; ties break by most-recent CachedAt)
// so a service token is never rendered as if it were the login session. Sessions
// are ordered deterministically by hostname, with Active marking the
// most-recently-used cluster.
func Expand(ctx context.Context, handlerName string, handler auth.Handler, status *auth.Status) []InstanceSession {
	if status == nil || !status.Authenticated {
		return nil
	}
	if !auth.HasCapability(handler.Capabilities(), auth.CapInstanceHostname) {
		return nil
	}
	lister, ok := handler.(auth.TokenLister)
	if !ok {
		return nil
	}
	tokens, err := lister.ListCachedTokens(ctx)
	if err != nil || len(tokens) == 0 {
		return nil
	}

	// One representative token per instance (deduplicated by hostname).
	instanceTokens := dedupInstanceTokens(tokens)
	if len(instanceTokens) == 0 {
		return nil
	}

	// Deterministic order by hostname.
	sort.Slice(instanceTokens, func(i, j int) bool {
		return instanceTokens[i].Hostname < instanceTokens[j].Hostname
	})

	aliases := InstanceAliases(ctx, handlerName)
	activeIdx := mostRecentTokenIndex(instanceTokens)

	sessions := make([]InstanceSession, 0, len(instanceTokens))
	for i, tk := range instanceTokens {
		sessions = append(sessions, InstanceSession{
			ClusterLabel: ClusterLabel(aliases, tk.Hostname),
			Active:       i == activeIdx,
			Token:        tk,
		})
	}
	return sessions
}

// ClusterLabel returns the display label for a cluster URL: the reverse-mapped
// alias selector when rawURL matches a configured/resolved alias, else the
// trimmed display host.
func ClusterLabel(aliases map[string]string, rawURL string) string {
	if label, ok := hostname.AliasForURL(aliases, rawURL); ok {
		return label
	}
	return hostname.DisplayHostFromURL(rawURL)
}

// dedupInstanceTokens returns one representative cached token per hostname. When
// a handler returns several tokens for the same instance (e.g. a user login plus
// minted service-account tokens), a user/login flow is preferred over a machine
// flow, with ties broken by the most-recent CachedAt, so each cluster renders as
// a single session reflecting the login. Tokens without a hostname are skipped.
func dedupInstanceTokens(tokens []*auth.CachedTokenInfo) []*auth.CachedTokenInfo {
	byHost := make(map[string]*auth.CachedTokenInfo)
	for _, tk := range tokens {
		if tk == nil || tk.Hostname == "" {
			continue
		}
		if current, ok := byHost[tk.Hostname]; !ok || preferInstanceToken(tk, current) {
			byHost[tk.Hostname] = tk
		}
	}
	out := make([]*auth.CachedTokenInfo, 0, len(byHost))
	for _, tk := range byHost {
		out = append(out, tk)
	}
	return out
}

// preferInstanceToken reports whether candidate is a better per-cluster
// representative than current: a user/login flow beats a machine flow, otherwise
// the newer CachedAt wins.
func preferInstanceToken(candidate, current *auth.CachedTokenInfo) bool {
	cu, ru := isUserFlow(candidate.Flow), isUserFlow(current.Flow)
	if cu != ru {
		return cu
	}
	return candidate.CachedAt.After(current.CachedAt)
}

// isUserFlow reports whether a flow represents an interactive user/login session
// (as opposed to a machine/service credential).
func isUserFlow(f auth.Flow) bool {
	switch f {
	case auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowPAT:
		return true
	default:
		return false
	}
}

// mostRecentTokenIndex returns the index of the cached token with the newest
// CachedAt timestamp, used to mark the "(active)" cluster session. It returns
// -1 when no token carries a usable timestamp (no marker is applied).
func mostRecentTokenIndex(tokens []*auth.CachedTokenInfo) int {
	idx := -1
	var latest time.Time
	for i, tk := range tokens {
		if tk.CachedAt.IsZero() {
			continue
		}
		if idx == -1 || tk.CachedAt.After(latest) {
			latest = tk.CachedAt
			idx = i
		}
	}
	return idx
}

// InstanceAliases returns a selector->URL map for reverse-mapping a cluster URL
// back to its short name. It merges static hostname aliases with the resolver's
// already-cached inventory entries (no network fetch), so both statically
// aliased and dynamically resolved fleets render the short selector. Static
// aliases win over resolver entries on selector collisions. Returns nil when
// nothing is configured.
func InstanceAliases(ctx context.Context, handlerName string) map[string]string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return nil
	}
	hc, ok := cfg.Auth.Handlers[handlerName]
	if !ok || hc.Hostname == nil {
		return nil
	}

	merged := make(map[string]string, len(hc.Hostname.Aliases))
	// Reverse-map from any cached resolver inventory (network-free, stale-OK): a
	// cluster resolved via `kube login` is cached under a different key than the
	// openshift-auth resolver, so union all cached inventories for labeling.
	// Static aliases overwrite on selector collision.
	if hc.Hostname.Resolver != nil || len(hc.Hostname.Aliases) > 0 {
		for _, e := range hostname.AllCachedInventoryEntries() {
			if e.Name != "" && e.URL != "" {
				if _, ok := merged[e.Name]; !ok {
					merged[e.Name] = e.URL
				}
			}
		}
	}
	for sel, url := range hc.Hostname.Aliases {
		merged[sel] = url
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
