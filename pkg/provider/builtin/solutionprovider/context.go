// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
)

type ancestorStackKey struct{}

type loaderKey struct{}

type registryKey struct{}

type officialProvidersKey struct{}

type pluginFetcherKey struct{}

type pluginConfigKey struct{}

type clientOptsKey struct{}

// WithLoaderCtx returns a context carrying a Loader for the solution provider.
func WithLoaderCtx(ctx context.Context, l Loader) context.Context {
	return context.WithValue(ctx, loaderKey{}, l)
}

// LoaderFromContext retrieves the Loader from the context.
func LoaderFromContext(ctx context.Context) Loader {
	l, _ := ctx.Value(loaderKey{}).(Loader)
	return l
}

// WithProviderRegistry returns a context carrying a provider Registry for
// the solution provider to use when executing sub-solutions.
func WithProviderRegistry(ctx context.Context, r *provider.Registry) context.Context {
	return context.WithValue(ctx, registryKey{}, r)
}

// ProviderRegistryFromContext retrieves the provider Registry from context.
func ProviderRegistryFromContext(ctx context.Context) *provider.Registry {
	r, _ := ctx.Value(registryKey{}).(*provider.Registry)
	return r
}

// WithOfficialProvidersCtx returns a context carrying an official provider Registry.
func WithOfficialProvidersCtx(ctx context.Context, r *official.Registry) context.Context {
	return context.WithValue(ctx, officialProvidersKey{}, r)
}

// OfficialProvidersFromContext retrieves the official provider Registry from context.
func OfficialProvidersFromContext(ctx context.Context) *official.Registry {
	r, _ := ctx.Value(officialProvidersKey{}).(*official.Registry)
	return r
}

// WithPluginFetcherCtx returns a context carrying a plugin Fetcher.
func WithPluginFetcherCtx(ctx context.Context, f *plugin.Fetcher) context.Context {
	return context.WithValue(ctx, pluginFetcherKey{}, f)
}

// PluginFetcherFromContext retrieves the plugin Fetcher from context.
func PluginFetcherFromContext(ctx context.Context) *plugin.Fetcher {
	f, _ := ctx.Value(pluginFetcherKey{}).(*plugin.Fetcher)
	return f
}

// WithPluginConfigCtx returns a context carrying plugin configuration.
func WithPluginConfigCtx(ctx context.Context, cfg *plugin.ProviderConfig) context.Context {
	return context.WithValue(ctx, pluginConfigKey{}, cfg)
}

// PluginConfigFromContext retrieves the plugin configuration from context.
func PluginConfigFromContext(ctx context.Context) *plugin.ProviderConfig {
	cfg, _ := ctx.Value(pluginConfigKey{}).(*plugin.ProviderConfig)
	return cfg
}

// WithClientOptionsCtx returns a context carrying plugin client options.
func WithClientOptionsCtx(ctx context.Context, opts []plugin.ClientOption) context.Context {
	return context.WithValue(ctx, clientOptsKey{}, opts)
}

// ClientOptionsFromContext retrieves the plugin client options from context.
func ClientOptionsFromContext(ctx context.Context) []plugin.ClientOption {
	opts, _ := ctx.Value(clientOptsKey{}).([]plugin.ClientOption)
	return opts
}

// ChildClientTracker tracks plugin clients started during a single execution
// so they can be cleaned up without affecting other concurrent executions
// sharing the same SolutionProvider singleton.
type ChildClientTracker struct {
	mu      sync.Mutex
	clients []*plugin.Client
}

// NewChildClientTracker creates a new per-execution client tracker.
func NewChildClientTracker() *ChildClientTracker {
	return &ChildClientTracker{}
}

// Add appends plugin clients to the tracker for deferred cleanup.
func (t *ChildClientTracker) Add(clients ...*plugin.Client) {
	t.mu.Lock()
	t.clients = append(t.clients, clients...)
	t.mu.Unlock()
}

// Close kills all tracked plugin clients and clears the list.
func (t *ChildClientTracker) Close() {
	t.mu.Lock()
	clients := t.clients
	t.clients = nil
	t.mu.Unlock()
	for _, c := range clients {
		c.Kill()
	}
}

type childClientTrackerKey struct{}

// WithChildClientTracker returns a context carrying a per-execution client tracker.
func WithChildClientTracker(ctx context.Context, t *ChildClientTracker) context.Context {
	return context.WithValue(ctx, childClientTrackerKey{}, t)
}

// ChildClientTrackerFromContext retrieves the per-execution client tracker from context.
func ChildClientTrackerFromContext(ctx context.Context) *ChildClientTracker {
	t, _ := ctx.Value(childClientTrackerKey{}).(*ChildClientTracker)
	return t
}

// WithAncestorStack returns a new context with the given ancestor stack.
// This is used to track the chain of solution invocations for circular reference detection.
func WithAncestorStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, ancestorStackKey{}, stack)
}

// AncestorStackFromContext retrieves the ancestor stack from context.
// Returns nil if no stack is set (root-level execution).
func AncestorStackFromContext(ctx context.Context) []string {
	stack, _ := ctx.Value(ancestorStackKey{}).([]string)
	return stack
}

// PushAncestor adds a canonical name to the ancestor stack and returns the updated context.
// Returns an error if the name already exists in the stack (circular reference detected).
func PushAncestor(ctx context.Context, name string) (context.Context, error) {
	stack := AncestorStackFromContext(ctx)

	for _, ancestor := range stack {
		if ancestor == name {
			chain := make([]string, len(stack)+1)
			copy(chain, stack)
			chain[len(stack)] = name
			return ctx, fmt.Errorf("solution: circular reference detected: %s", strings.Join(chain, " \u2192 "))
		}
	}

	newStack := make([]string, len(stack), len(stack)+1)
	copy(newStack, stack)
	newStack = append(newStack, name)

	return WithAncestorStack(ctx, newStack), nil
}

// CheckDepth validates that the current nesting depth does not exceed the maximum allowed.
// Depth is derived from the length of the ancestor stack.
func CheckDepth(ctx context.Context, maxDepth int) error {
	stack := AncestorStackFromContext(ctx)
	if len(stack) >= maxDepth {
		return fmt.Errorf("solution: max nesting depth %d exceeded: %s", maxDepth, strings.Join(stack, " \u2192 "))
	}
	return nil
}

// Canonicalize normalizes a source reference into a canonical name for ancestor tracking.
// File paths are resolved to absolute paths, catalog references and URLs are used as-is.
func Canonicalize(ctx context.Context, source string) string {
	// URLs - use as-is
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return source
	}

	// Relative or absolute file paths - resolve to absolute
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.Contains(source, string(filepath.Separator)) {
		abs, err := provider.AbsFromContext(ctx, source)
		if err != nil {
			return source // fallback to raw value
		}
		return abs
	}

	// Catalog references (bare name or name@version) - use as-is
	return source
}
