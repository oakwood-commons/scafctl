// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package hostname resolves a user-supplied hostname selector (e.g. "cluster-01")
// into a concrete endpoint URL for an auth handler at login time.
//
// Resolution precedence, highest to lowest:
//
//  1. The selector is already a concrete http(s):// URL -> returned unchanged.
//  2. No hostname config exists for the handler -> selector returned unchanged
//     (preserves plain-hostname handlers such as GitHub Enterprise).
//  3. A static alias (auth.handlers.<name>.hostname.aliases) matches -> its URL.
//  4. A dynamic resolver (auth.handlers.<name>.hostname.resolver) fetches an
//     inventory, normalizes it with an org-owned CEL transform into a list of
//     {name, url} entries, and the selector is looked up by name.
//
// Resolve returns just the endpoint URL. ResolveEntry returns the full Entry,
// including optional per-cluster OIDC metadata (audience, authType, caData,
// defaultHandler, consoleUrl, insecureSkipTls) that a config-driven inventory
// can surface for
// kube login.
//
// scafctl core stays shape-blind: all inventory normalization lives in the
// org-owned CEL transform. The host only validates that the transform yields
// the canonical list<{name, url}> contract.
package hostname

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// Entry is one normalized inventory record. The CEL transform must yield a
// list of these. Only name and url are required; the remaining fields carry
// optional per-cluster OIDC metadata that mirrors kube.ClusterInfo so a
// config-driven inventory can feed both auth login and kube login.
type Entry struct {
	// Name is the selector used to look up this entry (required).
	Name string `json:"name"`

	// URL is the concrete endpoint the selector resolves to (required).
	URL string `json:"url"`

	// Audience is the OIDC audience/client ID a token for this cluster targets.
	Audience string `json:"audience,omitempty"`

	// AuthType is the login method for this cluster: "" (auto), "oauth", or
	// "oidc". It mirrors kube.AuthType.
	AuthType string `json:"authType,omitempty"`

	// DefaultHandler is the auth handler used to authenticate to this cluster
	// when the caller does not pass an explicit --handler. It lets a fleet
	// inventory drive `kube login <cluster>` without naming a handler.
	DefaultHandler string `json:"defaultHandler,omitempty"`

	// CAData is a PEM-encoded CA bundle for the endpoint (preferred over
	// InsecureSkipTLS).
	CAData string `json:"caData,omitempty"`

	// ConsoleURL is an optional web console URL for the cluster.
	ConsoleURL string `json:"consoleUrl,omitempty"`

	// InsecureSkipTLS disables endpoint TLS verification (dev only).
	InsecureSkipTLS bool `json:"insecureSkipTls,omitempty"`
}

// FetchFunc retrieves the raw inventory body from the resolver source. The
// bearer token is empty for unauthenticated sources.
type FetchFunc func(ctx context.Context, src config.HostnameResolverSource, bearer string) ([]byte, error)

// TokenFunc retrieves a bearer token for the given auth provider and scope
// using cached, non-interactive credentials only.
type TokenFunc func(ctx context.Context, provider, scope string) (string, error)

// TransformFunc normalizes the raw inventory body into a list of entries using
// the given CEL expression.
type TransformFunc func(ctx context.Context, cel string, body []byte) ([]Entry, error)

// InventoryCache caches resolved inventories across invocations. A nil cache
// disables caching.
type InventoryCache interface {
	Get(ctx context.Context, key string) ([]Entry, bool)
	Set(ctx context.Context, key string, entries []Entry, ttl time.Duration)
}

// Deps carries injectable collaborators. Zero-valued fields fall back to the
// production defaults (httpc fetch, tokenprovider token, celexp transform).
type Deps struct {
	Fetch     FetchFunc
	Token     TokenFunc
	Transform TransformFunc
	Cache     InventoryCache
}

func (d Deps) withDefaults() Deps {
	if d.Fetch == nil {
		d.Fetch = defaultFetch
	}
	if d.Token == nil {
		d.Token = defaultToken
	}
	if d.Transform == nil {
		d.Transform = defaultTransform
	}
	return d
}

// DefaultDeps returns the production collaborators used by Resolve/ResolveEntry:
// httpc fetch, tokenprovider token, celexp transform, and an on-disk inventory
// cache. Callers that resolve an inventory outside the auth-handler config path
// (for example kube cluster resolution) can reuse the same engine via
// ResolveEntryWith with these deps.
func DefaultDeps() Deps {
	return Deps{Cache: newDiskCache()}.withDefaults()
}

// Resolve turns a hostname selector into a concrete endpoint URL for the given
// handler, pulling config and collaborators from the context. It returns the
// selector unchanged when it is already a URL or when the handler has no
// hostname config.
func Resolve(ctx context.Context, handler, selector string) (string, error) {
	e, err := ResolveEntry(ctx, handler, selector)
	if err != nil {
		return "", err
	}
	return e.URL, nil
}

// ResolveEntry resolves a hostname selector into a full inventory Entry (the
// endpoint URL plus any optional per-cluster OIDC metadata) for the given
// handler, pulling config and collaborators from the context. For concrete
// URLs and handlers without hostname config it returns an Entry whose Name and
// URL are the selector itself.
func ResolveEntry(ctx context.Context, handler, selector string) (*Entry, error) {
	var cfg *config.HostnameConfig
	if c := config.FromContext(ctx); c != nil {
		if hc, ok := c.Auth.Handlers[handler]; ok {
			cfg = hc.Hostname
		}
	}
	return ResolveEntryWith(ctx, cfg, handler, selector, Deps{Cache: newDiskCache()})
}

// ResolveWith is the dependency-injected core of Resolve. It is exported for
// tests and embedders that supply their own collaborators.
func ResolveWith(ctx context.Context, cfg *config.HostnameConfig, handler, selector string, deps Deps) (string, error) {
	e, err := ResolveEntryWith(ctx, cfg, handler, selector, deps)
	if err != nil {
		return "", err
	}
	return e.URL, nil
}

// ResolveEntryWith is the dependency-injected core of ResolveEntry. It is
// exported for tests and embedders that supply their own collaborators.
func ResolveEntryWith(ctx context.Context, cfg *config.HostnameConfig, handler, selector string, deps Deps) (*Entry, error) {
	deps = deps.withDefaults()

	// 1. Concrete URL passthrough.
	if isConcreteURL(selector) {
		return &Entry{Name: selector, URL: selector}, nil
	}

	// 2. No hostname config for this handler.
	if cfg == nil {
		return &Entry{Name: selector, URL: selector}, nil
	}

	// 3. Static alias wins over the dynamic resolver.
	if u, ok := cfg.Aliases[selector]; ok {
		return &Entry{Name: selector, URL: u}, nil
	}

	// 4. Dynamic resolver.
	if cfg.Resolver != nil {
		entries, err := resolveInventory(ctx, cfg.Resolver, handler, deps)
		if err != nil {
			return nil, err
		}
		for i := range entries {
			if entries[i].Name == selector {
				e := entries[i]
				return &e, nil
			}
		}
		return nil, fmt.Errorf("%w: %q (available: %s)", ErrSelectorNotFound, selector, availableNames(cfg, entries))
	}

	// 5. Not found in aliases and no resolver configured.
	return nil, fmt.Errorf("%w: %q (available: %s)", ErrSelectorNotFound, selector, availableNames(cfg, nil))
}

// ResolveInventory fetches, transforms, and caches the endpoint inventory for
// the given resolver config and returns all entries. It is the list-all
// counterpart to ResolveEntryWith's single-selector lookup, for callers that
// enumerate entries (e.g. kube cluster-name shell completion). The handler
// namespaces the cache key and guards against a resolver depending on the
// handler it resolves; pass a stable non-auth-handler token (e.g. "kube") for
// non-auth callers.
func ResolveInventory(ctx context.Context, rc *config.HostnameResolverConfig, handler string, deps Deps) ([]Entry, error) {
	return resolveInventory(ctx, rc, handler, deps.withDefaults())
}

// CachedInventory returns the inventory entries already cached for the resolver
// config, without triggering a network fetch. ok is false when nothing is
// cached (or caching is disabled). It lets latency-sensitive callers such as
// shell completion enumerate previously-resolved entries without blocking on
// I/O. The handler must match the one used at resolution time so the cache key
// aligns.
func CachedInventory(ctx context.Context, rc *config.HostnameResolverConfig, handler string, deps Deps) ([]Entry, bool) {
	deps = deps.withDefaults()
	if deps.Cache == nil {
		return nil, false
	}
	return deps.Cache.Get(ctx, cacheKey(handler, rc))
}

// resolveInventory fetches, transforms, and caches the endpoint inventory.
func resolveInventory(ctx context.Context, rc *config.HostnameResolverConfig, handler string, deps Deps) ([]Entry, error) {
	// Loop guard: a resolver must not depend on the handler it resolves.
	if rc.Source.AuthProvider != "" && rc.Source.AuthProvider == handler {
		return nil, fmt.Errorf("%w: source authProvider %q equals the handler being resolved", ErrResolverLoop, handler)
	}

	key := cacheKey(handler, rc)
	if deps.Cache != nil {
		if entries, ok := deps.Cache.Get(ctx, key); ok {
			return entries, nil
		}
	}

	var bearer string
	if rc.Source.AuthProvider != "" {
		tok, err := deps.Token(ctx, rc.Source.AuthProvider, rc.Source.AuthScope)
		if err != nil {
			return nil, fmt.Errorf("%w: auth provider %q: run 'auth login %s' first: %w",
				ErrNoCredentials, rc.Source.AuthProvider, rc.Source.AuthProvider, err)
		}
		bearer = tok
	}

	body, err := deps.Fetch(ctx, rc.Source, bearer)
	if err != nil {
		return nil, fmt.Errorf("fetching hostname inventory: %w", err)
	}

	entries, err := deps.Transform(ctx, rc.Transform, body)
	if err != nil {
		return nil, err
	}

	if deps.Cache != nil {
		if ttl := parseTTL(rc.TTL); ttl > 0 {
			deps.Cache.Set(ctx, key, entries, ttl)
		}
	}
	return entries, nil
}

// isConcreteURL reports whether selector is already an absolute http(s) URL.
func isConcreteURL(selector string) bool {
	u, err := url.Parse(selector)
	if err != nil {
		return false
	}
	return (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// parseTTL parses an optional cache TTL. Empty, "0", or unparseable values
// disable caching (return 0).
func parseTTL(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// availableNames returns a sorted, comma-separated list of known selectors from
// the static aliases and the resolved inventory, for error messages.
func availableNames(cfg *config.HostnameConfig, entries []Entry) string {
	seen := make(map[string]struct{})
	if cfg != nil {
		for name := range cfg.Aliases {
			seen[name] = struct{}{}
		}
	}
	for _, e := range entries {
		seen[e.Name] = struct{}{}
	}
	if len(seen) == 0 {
		return "none"
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
