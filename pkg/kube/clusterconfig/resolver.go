// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package clusterconfig implements a config-driven kube.ClusterResolver so the
// stock scafctl binary can resolve a cluster by name for `kube login` without
// an embedder-provided resolver.
//
// Resolution reuses the auth hostname inventory engine (fetch + CEL transform +
// TTL cache) for the dynamic tier, and adds a richer static-alias tier that
// carries per-cluster connection details (server, default handler, auth type,
// audience). Precedence, highest to lowest:
//
//  1. Static alias (kube.clusters.aliases) -- wins over inventory.
//  2. Dynamic inventory (kube.clusters.resolver).
//
// Concrete-URL passthrough and explicit --server/--handler flags are handled by
// the caller (pkg/kube/login), which layers them above this resolver.
package clusterconfig

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/kube"
)

// resolverHandler namespaces the inventory cache key and satisfies the hostname
// engine's loop guard. Cluster resolution has no auth handler of its own, so a
// stable non-auth-handler token is used.
const resolverHandler = "kube"

// ErrClusterNotFound is returned when a selector matches neither a static alias
// nor a dynamic inventory entry.
var ErrClusterNotFound = fmt.Errorf("cluster not found")

// Resolver resolves cluster selectors from configuration. It implements
// kube.ClusterResolver.
type Resolver struct {
	cfg  config.ClusterResolutionConfig
	deps hostname.Deps
}

// Option customizes a Resolver.
type Option func(*Resolver)

// WithDeps overrides the inventory engine collaborators (fetch/token/transform/
// cache). Primarily for tests; production uses hostname.DefaultDeps.
func WithDeps(deps hostname.Deps) Option {
	return func(r *Resolver) { r.deps = deps }
}

// New builds a config-driven cluster resolver. When no options are supplied it
// uses the production hostname engine collaborators (HTTP fetch, cached token,
// CEL transform, on-disk inventory cache).
func New(cfg config.ClusterResolutionConfig, opts ...Option) *Resolver {
	r := &Resolver{cfg: cfg, deps: hostname.DefaultDeps()}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Enabled reports whether any resolution source is configured. Callers use it
// to decide whether to attach the resolver at all.
func (r *Resolver) Enabled() bool {
	return len(r.cfg.Aliases) > 0 || r.cfg.Resolver != nil
}

// Resolve returns the ClusterInfo for the named cluster. Static aliases win
// over the dynamic inventory.
func (r *Resolver) Resolve(ctx context.Context, name string) (*kube.ClusterInfo, error) {
	if name == "" {
		return nil, fmt.Errorf("cluster name is required")
	}

	if alias, ok := r.cfg.Aliases[name]; ok {
		if alias.Server == "" {
			return nil, fmt.Errorf("cluster alias %q is missing a server", name)
		}
		return aliasToInfo(name, alias), nil
	}

	if r.cfg.Resolver != nil {
		hc := &config.HostnameConfig{Resolver: r.cfg.Resolver}
		entry, err := hostname.ResolveEntryWith(ctx, hc, resolverHandler, name, r.deps)
		if err != nil {
			// Normalize an inventory miss to ErrClusterNotFound so callers can
			// detect "no such cluster" uniformly, regardless of whether the
			// miss came from the alias tier or the inventory tier.
			if errors.Is(err, hostname.ErrSelectorNotFound) {
				return nil, fmt.Errorf("%w: %q", ErrClusterNotFound, name)
			}
			return nil, fmt.Errorf("resolve cluster %q from inventory: %w", name, err)
		}
		return entryToInfo(entry), nil
	}

	return nil, fmt.Errorf("%w: %q", ErrClusterNotFound, name)
}

// List returns all known clusters (static aliases plus dynamic inventory
// entries) for shell completion. Static aliases take precedence over inventory
// entries of the same name. When the inventory cannot be fetched, the static
// aliases gathered so far are returned along with the error so completion can
// degrade gracefully.
func (r *Resolver) List(ctx context.Context) ([]kube.ClusterInfo, error) {
	out, seen := r.aliasInfos()

	if r.cfg.Resolver != nil {
		entries, err := hostname.ResolveInventory(ctx, r.cfg.Resolver, resolverHandler, r.deps)
		if err != nil {
			return out, fmt.Errorf("list clusters from inventory: %w", err)
		}
		out = appendEntries(out, seen, entries)
	}

	return out, nil
}

// ListCached returns known clusters without any network I/O: static aliases
// always, plus dynamic inventory entries only when they were already cached by
// a prior resolution. It is intended for shell completion, where blocking on a
// network fetch would stall the shell.
func (r *Resolver) ListCached(ctx context.Context) []kube.ClusterInfo {
	out, seen := r.aliasInfos()

	if r.cfg.Resolver != nil {
		if entries, ok := hostname.CachedInventory(ctx, r.cfg.Resolver, resolverHandler, r.deps); ok {
			out = appendEntries(out, seen, entries)
		}
	}

	return out
}

// aliasInfos returns the static aliases as ClusterInfos (sorted by name) plus a
// set of the names already included, for inventory de-duplication.
func (r *Resolver) aliasInfos() ([]kube.ClusterInfo, map[string]bool) {
	seen := make(map[string]bool, len(r.cfg.Aliases))
	out := make([]kube.ClusterInfo, 0, len(r.cfg.Aliases))

	aliasNames := make([]string, 0, len(r.cfg.Aliases))
	for name := range r.cfg.Aliases {
		aliasNames = append(aliasNames, name)
	}
	sort.Strings(aliasNames)
	for _, name := range aliasNames {
		out = append(out, *aliasToInfo(name, r.cfg.Aliases[name]))
		seen[name] = true
	}
	return out, seen
}

// appendEntries appends inventory entries not already present (static aliases
// win over inventory entries of the same name).
func appendEntries(out []kube.ClusterInfo, seen map[string]bool, entries []hostname.Entry) []kube.ClusterInfo {
	for i := range entries {
		if seen[entries[i].Name] {
			continue
		}
		out = append(out, *entryToInfo(&entries[i]))
		seen[entries[i].Name] = true
	}
	return out
}

// aliasToInfo maps a static ClusterAlias to a ClusterInfo.
func aliasToInfo(name string, a config.ClusterAlias) *kube.ClusterInfo {
	return &kube.ClusterInfo{
		Name:            name,
		APIServerURL:    a.Server,
		ConsoleURL:      a.ConsoleURL,
		AuthType:        kube.AuthType(a.AuthType),
		OIDCAudience:    a.OIDCAudience,
		DefaultHandler:  a.DefaultHandler,
		CAData:          a.CAData,
		InsecureSkipTLS: a.InsecureSkipTLS,
	}
}

// entryToInfo maps a resolved inventory Entry to a ClusterInfo.
func entryToInfo(e *hostname.Entry) *kube.ClusterInfo {
	return &kube.ClusterInfo{
		Name:            e.Name,
		APIServerURL:    e.URL,
		ConsoleURL:      e.ConsoleURL,
		AuthType:        kube.AuthType(e.AuthType),
		OIDCAudience:    e.Audience,
		DefaultHandler:  e.DefaultHandler,
		CAData:          e.CAData,
		InsecureSkipTLS: e.InsecureSkipTLS,
	}
}
