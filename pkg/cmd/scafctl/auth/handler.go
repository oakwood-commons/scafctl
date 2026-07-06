// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package auth provides CLI commands for authentication management.
package auth

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/handlerdep"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// handlerContextKey is used for test injection of handlers.
// This is package-private and only used for testing.
type handlerContextKey struct{}

// withTestHandler injects a handler into context for testing.
// This is not exported and should only be used in tests.
func withTestHandler(ctx context.Context, h auth.Handler) context.Context {
	return context.WithValue(ctx, handlerContextKey{}, h)
}

// handlerFromContext retrieves a test-injected handler from context.
// Returns nil if no handler was injected.
func handlerFromContext(ctx context.Context) auth.Handler {
	if h, ok := ctx.Value(handlerContextKey{}).(auth.Handler); ok {
		return h
	}
	return nil
}

// getHandler retrieves an auth handler from the registry or test context.
// For production use, it looks up the handler by name from the auth registry.
// For tests, it returns the test-injected handler.
func getHandler(ctx context.Context, handlerName string) (auth.Handler, error) {
	// Check for test-injected handler first
	if h := handlerFromContext(ctx); h != nil {
		return h, nil
	}

	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("%w: no auth registry in context", auth.ErrHandlerNotFound)
	}
	return registry.Get(handlerName)
}

// listHandlers returns the names of eagerly-registered (installed) handlers.
// Use this for bulk operations (status, list, logout --all, diagnose) that
// call getHandler on each name — it avoids triggering lazy plugin resolution.
func listHandlers(ctx context.Context) []string {
	if h := handlerFromContext(ctx); h != nil {
		return []string{h.Name()}
	}

	registry := auth.RegistryFromContext(ctx)
	if registry == nil {
		return nil
	}
	return registry.List()
}

// listActiveHandlers returns the handler names to enumerate for root ("no
// handler") bulk operations (status, list, diagnose): the union of
// eagerly-registered handlers and locally-installed auth-handler plugins.
//
// Unlike listHandlers it also surfaces third-party plugin handlers the user
// actually uses (e.g. openshift) that are resolved lazily by name and so are not
// eagerly registered. The installed-plugin cache is a host-owned, handler-
// agnostic signal that requires no config pin, no fragile credential-file
// parsing, and no network fetch (only already-cached plugins are included).
//
// Policy applies: installed third-party plugins are omitted when
// settings.disableThirdPartyAuthHandlers is set (official handlers still surface
// via the eager registry). Results are deduplicated and sorted.
func listActiveHandlers(ctx context.Context, binaryName string) []string {
	if h := handlerFromContext(ctx); h != nil {
		return []string{h.Name()}
	}

	seen := make(map[string]struct{})
	if registry := auth.RegistryFromContext(ctx); registry != nil {
		for _, name := range registry.List() {
			seen[name] = struct{}{}
		}
	}

	for _, name := range installedAuthHandlerNames(ctx, binaryName) {
		seen[name] = struct{}{}
	}

	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// installedAuthHandlerNames returns the names of auth-handler plugins present in
// the local plugin cache (i.e. already fetched/used). This is a host-owned,
// handler-agnostic, network-free signal for surfacing lazily-resolved
// third-party handlers (e.g. openshift). It returns nil when third-party
// resolution is disabled by policy or nothing is cached. Names are not sorted.
func installedAuthHandlerNames(ctx context.Context, binaryName string) []string {
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Settings.DisableThirdPartyAuthHandlers {
		return nil
	}
	cache := plugin.NewCache(settings.PluginCacheDirFor(binaryName))
	cached, err := cache.List()
	if err != nil {
		return nil
	}
	var names []string
	for _, p := range cached {
		if name, kind := plugin.PluginKindFromCacheKey(p.Name); kind == solution.PluginKindAuthHandler && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// listKnownHandlers returns the names of all registered and officially-known
// handlers. It merges eagerly-registered handlers with the official handler
// registry so that lazy-resolvable handlers appear in validation error messages
// and shell completions. Do NOT iterate this set with getHandler — official
// names that are not yet installed would trigger network I/O.
func listKnownHandlers(ctx context.Context) []string {
	if h := handlerFromContext(ctx); h != nil {
		return []string{h.Name()}
	}

	seen := make(map[string]struct{})

	if registry := auth.RegistryFromContext(ctx); registry != nil {
		for _, name := range registry.List() {
			seen[name] = struct{}{}
		}
	}

	if official := authofficial.RegistryFromContext(ctx); official != nil {
		for _, name := range official.Names() {
			seen[name] = struct{}{}
		}
	}

	// Config-pinned third-party handlers (auth.handlers.<name>.plugin) are also
	// lazily resolvable and should surface in completions and error hints --
	// but only when policy actually allows resolving them. handlerdep.IsKnown
	// honors settings.disableThirdPartyAuthHandlers, so pins are omitted when
	// third-party resolution is disabled.
	if appCfg := config.FromContext(ctx); appCfg != nil {
		for name, hc := range appCfg.Auth.Handlers {
			if hc.Plugin != nil && handlerdep.IsKnown(ctx, name) {
				seen[name] = struct{}{}
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// listUnconfiguredOfficialHandlers returns official handler names that are NOT
// eagerly registered. These are available via lazy resolution but the user has
// not yet used them. Use this for hint messages in CLI output.
func listUnconfiguredOfficialHandlers(ctx context.Context) []string {
	official := authofficial.RegistryFromContext(ctx)
	if official == nil {
		return nil
	}

	var eager map[string]struct{}
	if registry := auth.RegistryFromContext(ctx); registry != nil {
		names := registry.List()
		eager = make(map[string]struct{}, len(names))
		for _, n := range names {
			eager[n] = struct{}{}
		}
	}

	var unconfigured []string
	for _, name := range official.Names() {
		if _, ok := eager[name]; !ok {
			unconfigured = append(unconfigured, name)
		}
	}
	return unconfigured
}

// isHandlerRegistered checks if a handler name is registered or known to be
// lazily resolvable (official allowlist or a config pin under
// auth.handlers.<name>.plugin). It does not probe catalogs, so a bare catalog
// name is not reported as known here.
func isHandlerRegistered(ctx context.Context, name string) bool {
	// Test-injected handlers match any name (since tests inject a single mock)
	if h := handlerFromContext(ctx); h != nil {
		return true // Let the test handler respond regardless of name
	}

	if registry := auth.RegistryFromContext(ctx); registry != nil && registry.Has(name) {
		return true
	}

	// Official handlers and config-pinned third-party handlers are lazily
	// resolvable via the registry fallback.
	return handlerdep.IsKnown(ctx, name)
}

// validateHandlerName checks if a handler name is plausibly resolvable and
// returns a formatted error if not. A name that is registered, official, or
// config-pinned passes immediately. Any other non-empty name is deferred to
// getHandler, which performs the authoritative existence check by resolving the
// handler against configured catalogs via the registry fallback. Only an empty
// name or a name that policy forbids (e.g. third-party resolution disabled) is
// rejected here.
func validateHandlerName(ctx context.Context, handlerName string) error {
	if handlerName == "" {
		return fmt.Errorf("auth handler name is required")
	}
	if isHandlerRegistered(ctx, handlerName) {
		return nil
	}
	// Not eagerly known. Defer to getHandler unless policy forbids resolving
	// this name (handlerdep.Resolve returns an error when the matching
	// official/third-party resolution is disabled).
	if _, _, err := handlerdep.Resolve(ctx, handlerName); err != nil {
		handlers := listKnownHandlers(ctx)
		if len(handlers) == 0 {
			return fmt.Errorf("unknown auth handler: %s: %w", handlerName, err)
		}
		return fmt.Errorf("unknown auth handler: %s (registered: %v): %w", handlerName, handlers, err)
	}
	return nil
}
