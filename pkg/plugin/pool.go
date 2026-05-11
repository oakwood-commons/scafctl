// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Sentinel errors for Pool operations.
var (
	ErrPoolClosed       = errors.New("plugin pool is closed")
	ErrPoolFull         = errors.New("plugin pool at maximum capacity")
	ErrPluginNotAllowed = errors.New("plugin not in allowlist")
	ErrExternalDisabled = errors.New("external plugins disabled")
)

// PoolErrorHTTPStatus maps pool errors to appropriate HTTP status codes.
// Returns 0 if the error is not a recognized pool error.
func PoolErrorHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrPluginNotAllowed), errors.Is(err, ErrExternalDisabled):
		return 403 // Forbidden — policy rejection
	case errors.Is(err, ErrPoolFull):
		return 503 // Service Unavailable — capacity exhaustion
	case errors.Is(err, ErrPoolClosed):
		return 503
	case errors.Is(err, context.Canceled):
		return 499 // Client Closed Request
	case errors.Is(err, context.DeadlineExceeded):
		return 504 // Gateway Timeout
	default:
		return 502 // Bad Gateway — upstream plugin failure
	}
}

// entryState represents the lifecycle state of a pool entry.
type entryState int

const (
	entryStarting entryState = iota
	entryReady
	entryDead
)

// poolEntry tracks a single managed plugin process.
type poolEntry struct {
	mu       sync.Mutex
	client   *Client
	state    entryState
	lastUsed time.Time
	refCount int32 // atomic; tracks in-flight requests using this plugin
	dep      solution.PluginDependency
	result   FetchResult
	// registeredProviders holds the provider names this entry successfully
	// registered in the shared registry. Only these names are unregistered
	// on eviction/death — avoids removing providers owned by other plugins.
	registeredProviders []string
	// ready is closed when the entry transitions from entryStarting to
	// entryReady or entryDead, unblocking waiters.
	ready chan struct{}
	// err holds any error from the spawn attempt (only valid after ready is closed).
	err error
}

// poolOptions holds Pool configuration.
type poolOptions struct {
	idleTimeout     time.Duration
	maxPlugins      int
	healthInterval  time.Duration
	clock           func() time.Time // for testing
	allowedPlugins  map[string]bool  // nil means allow all
	disableExternal bool             // reject all non-adopted plugins
	clientOpts      []ClientOption   // extra options for spawned plugin clients
	sanitizeEnv     bool             // prepend WithSanitizedEnv() on spawn
}

// defaultPoolOptions returns sensible defaults.
func defaultPoolOptions() poolOptions {
	return poolOptions{
		idleTimeout:    5 * time.Minute,
		maxPlugins:     50,
		healthInterval: 30 * time.Second,
		clock:          time.Now,
		sanitizeEnv:    true,
	}
}

// PoolOption configures pool behavior.
type PoolOption func(*poolOptions)

// WithIdleTimeout sets how long an unused plugin stays alive before eviction.
// Zero disables idle eviction.
func WithIdleTimeout(d time.Duration) PoolOption {
	return func(o *poolOptions) { o.idleTimeout = d }
}

// WithMaxPlugins sets the maximum number of external plugin processes.
// Zero means unlimited.
func WithMaxPlugins(n int) PoolOption {
	return func(o *poolOptions) { o.maxPlugins = n }
}

// WithHealthCheckInterval sets the background health check frequency.
// Zero disables background checks (health is verified on use).
//
// NOTE: Background health checks are not yet implemented. This option
// stores the interval for future use. Currently, dead plugins are only
// detected when a caller invokes Ping or when a request fails.
func WithHealthCheckInterval(d time.Duration) PoolOption {
	return func(o *poolOptions) { o.healthInterval = d }
}

// withClock overrides the time source (for testing).
func withClock(fn func() time.Time) PoolOption {
	return func(o *poolOptions) { o.clock = fn }
}

// WithAllowedPlugins restricts which external plugins may be loaded via
// Ensure. Plugins not in this list are rejected with ErrPluginNotAllowed.
// A nil or empty list means all plugins are allowed (no restriction).
// Adopted (pre-loaded) plugins bypass this check.
func WithAllowedPlugins(names []string) PoolOption {
	return func(o *poolOptions) {
		if len(names) == 0 {
			o.allowedPlugins = nil
			return
		}
		o.allowedPlugins = make(map[string]bool, len(names))
		for _, n := range names {
			o.allowedPlugins[n] = true
		}
	}
}

// WithDisableExternal rejects all plugin load attempts via Ensure.
// Only pre-loaded (Adopt) plugins are usable. This is the safe default
// for API server deployments.
func WithDisableExternal(disabled bool) PoolOption {
	return func(o *poolOptions) { o.disableExternal = disabled }
}

// WithClientOptions sets additional ClientOption values that are passed to
// every plugin client spawned by the pool. Use this to inject host-side
// dependencies such as auth registries (via WithHostDeps).
func WithClientOptions(opts ...ClientOption) PoolOption {
	return func(o *poolOptions) { o.clientOpts = append(o.clientOpts, opts...) }
}

// WithSanitizeEnv controls whether spawned plugin clients get a sanitized
// environment (true) or inherit the host environment (false). Defaults to true.
// API server deployments should use true; MCP interactive sessions may use
// false so plugins can access host credentials (SSH_AUTH_SOCK, tokens, etc.).
func WithSanitizeEnv(sanitize bool) PoolOption {
	return func(o *poolOptions) { o.sanitizeEnv = sanitize }
}

// PoolStats holds pool metrics.
type PoolStats struct {
	Active  int // Entries with refCount > 0
	Idle    int // Entries in ready state with refCount == 0
	Dead    int // Entries that crashed and await eviction or re-spawn
	Total   int // All entries
	Evicted int // Cumulative evictions since pool creation
}

// Pool manages shared, long-lived plugin processes with lazy initialization
// and idle eviction. It is safe for concurrent use.
//
// Official providers pre-loaded at startup can be added via Adopt; external
// plugins declared in bundle.plugins are loaded on-demand via Ensure.
type Pool struct {
	mu       sync.Mutex
	entries  map[string]*poolEntry // keyed by plugin name
	fetcher  *Fetcher
	registry *provider.Registry
	opts     poolOptions
	closed   bool
	evicted  int
	stopOnce sync.Once
	stop     chan struct{}
	logger   logr.Logger
}

// NewPool creates a plugin pool backed by the given fetcher and registry.
// The fetcher may be nil if only Adopt (pre-loaded) plugins are used.
func NewPool(fetcher *Fetcher, registry *provider.Registry, logger logr.Logger, opts ...PoolOption) *Pool {
	o := defaultPoolOptions()
	for _, opt := range opts {
		opt(&o)
	}
	p := &Pool{
		entries:  make(map[string]*poolEntry),
		fetcher:  fetcher,
		registry: registry,
		opts:     o,
		stop:     make(chan struct{}),
		logger:   logger,
	}
	if o.idleTimeout > 0 {
		go p.evictionLoop()
	}
	return p
}

// Adopt registers an already-running plugin client into the pool so that its
// lifecycle (idle eviction, shutdown) is managed by the pool. This is used
// for official providers pre-loaded at startup.
func (p *Pool) Adopt(name string, client *Client, dep solution.PluginDependency, registeredProviders []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	ready := make(chan struct{})
	close(ready)
	p.entries[name] = &poolEntry{
		client:              client,
		state:               entryReady,
		lastUsed:            p.opts.clock(),
		dep:                 dep,
		registeredProviders: registeredProviders,
		ready:               ready,
	}
}

// Ensure guarantees that all plugins in deps are running and registered in
// the provider registry. For plugins already in the pool and healthy, this
// is a no-op. For new plugins, it fetches, spawns, and registers them.
func (p *Pool) Ensure(ctx context.Context, deps []solution.PluginDependency) error {
	for _, dep := range deps {
		if dep.Kind != solution.PluginKindProvider {
			continue
		}
		if err := p.ensureOne(ctx, dep); err != nil {
			return err
		}
	}
	return nil
}

// EnsureAndAcquire ensures all plugins are available and acquires them
// (increments their refcounts), preventing idle eviction for the duration
// of the caller's work. Returns a release function that must be called when
// the caller is done using the plugins (typically via defer).
func (p *Pool) EnsureAndAcquire(ctx context.Context, deps []solution.PluginDependency) (release func(), err error) {
	if err := p.Ensure(ctx, deps); err != nil {
		return nil, err
	}

	var acquired []string
	for _, dep := range deps {
		if dep.Kind != solution.PluginKindProvider {
			continue
		}
		if p.Acquire(dep.Name) {
			acquired = append(acquired, dep.Name)
		}
	}

	return func() {
		for _, name := range acquired {
			p.Release(name)
		}
	}, nil
}

// ensureOne handles a single plugin dependency.
func (p *Pool) ensureOne(ctx context.Context, dep solution.PluginDependency) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPoolClosed
	}

	// Already in pool (pre-loaded or previously loaded)?
	if entry, ok := p.entries[dep.Name]; ok {
		p.mu.Unlock()
		err := p.waitAndValidate(ctx, entry)
		if err != nil {
			// Remove dead entries so the plugin can be re-spawned on retry.
			p.mu.Lock()
			if entry.state == entryDead {
				p.unregisterEntry(entry)
				delete(p.entries, dep.Name)
			}
			p.mu.Unlock()
		}
		return err
	}

	// Check if already registered (builtin or pre-loaded without Adopt)
	if p.registry.Has(dep.Name) {
		p.mu.Unlock()
		return nil
	}

	// Security: reject if external plugins are disabled entirely.
	if p.opts.disableExternal {
		p.mu.Unlock()
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrExternalDisabled)
	}

	// Security: reject if allowlist is configured and plugin is not on it.
	if p.opts.allowedPlugins != nil && !p.opts.allowedPlugins[dep.Name] {
		p.mu.Unlock()
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrPluginNotAllowed)
	}

	// Capacity check
	if p.opts.maxPlugins > 0 && len(p.entries) >= p.opts.maxPlugins {
		p.mu.Unlock()
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrPoolFull)
	}

	// Create a placeholder entry; we'll spawn outside the lock.
	entry := &poolEntry{
		state: entryStarting,
		dep:   dep,
		ready: make(chan struct{}),
	}
	p.entries[dep.Name] = entry
	p.mu.Unlock()

	// Spawn the plugin (may take time: fetch + exec + gRPC handshake).
	p.spawn(ctx, entry)

	if entry.err != nil {
		// Remove failed entry
		p.mu.Lock()
		delete(p.entries, dep.Name)
		p.mu.Unlock()
		return fmt.Errorf("plugin %q: %w", dep.Name, entry.err)
	}
	return nil
}

// waitAndValidate waits for an entry to become ready and checks health.
func (p *Pool) waitAndValidate(ctx context.Context, entry *poolEntry) error {
	// Wait for entry to be ready (handles concurrent Ensure for same plugin).
	select {
	case <-entry.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.state == entryDead {
		// Dead entries will be re-spawned on next Ensure after eviction.
		return fmt.Errorf("plugin %q is dead: %w", entry.dep.Name, entry.err)
	}

	// Touch last-used time
	entry.lastUsed = p.opts.clock()
	return nil
}

// buildSpawnClientOpts builds the ClientOption slice used when spawning a
// plugin process. When sanitize is true, WithSanitizedEnv() is prepended.
func buildSpawnClientOpts(sanitize bool, extraOpts []ClientOption) []ClientOption {
	if sanitize {
		opts := make([]ClientOption, 1, 1+len(extraOpts))
		opts[0] = WithSanitizedEnv()
		opts = append(opts, extraOpts...)
		return opts
	}
	if len(extraOpts) == 0 {
		return nil
	}
	opts := make([]ClientOption, len(extraOpts))
	copy(opts, extraOpts)
	return opts
}

// spawn fetches and starts a plugin, updating the entry state.
func (p *Pool) spawn(ctx context.Context, entry *poolEntry) {
	defer close(entry.ready)

	if p.fetcher == nil {
		entry.failWith(errors.New("plugin fetcher not available"))
		return
	}

	// Fetch binary
	results, err := p.fetcher.FetchPlugins(ctx, []solution.PluginDependency{entry.dep}, nil)
	if err != nil {
		entry.failWith(fmt.Errorf("fetching: %w", err))
		return
	}

	if len(results) == 0 {
		entry.failWith(errors.New("no fetch results returned"))
		return
	}

	result := results[0]

	// Spawn client, optionally sanitizing the environment.
	clientOpts := buildSpawnClientOpts(p.opts.sanitizeEnv, p.opts.clientOpts)
	client, err := NewClient(result.Path, clientOpts...)
	if err != nil {
		entry.failWith(fmt.Errorf("starting process: %w", err))
		return
	}

	// Register providers into the shared registry
	providers, err := client.GetProviders(ctx)
	if err != nil {
		client.Kill()
		entry.failWith(fmt.Errorf("getting providers: %w", err))
		return
	}

	var registered []string
	for _, provName := range providers {
		wrapper, wErr := NewProviderWrapper(client, provName, WithContext(ctx))
		if wErr != nil {
			p.logger.V(1).Info("failed to create provider wrapper",
				"plugin", entry.dep.Name, "provider", provName, "error", wErr)
			continue
		}
		if rErr := p.registry.Register(wrapper); rErr != nil {
			p.logger.V(1).Info("provider not registered (name taken)",
				"plugin", entry.dep.Name, "provider", provName, "error", rErr)
			continue
		}
		// Configure the provider so the plugin receives the host service ID.
		// Without this call, the plugin cannot dial back to the host for auth
		// tokens or other host services.
		if cErr := wrapper.Configure(ctx, ProviderConfig{}); cErr != nil {
			p.logger.V(1).Info("failed to configure plugin provider",
				"plugin", entry.dep.Name, "provider", provName, "error", cErr)
		}
		registered = append(registered, provName)
	}

	entry.mu.Lock()
	entry.client = client
	entry.result = result
	entry.registeredProviders = registered
	entry.state = entryReady
	entry.lastUsed = p.opts.clock()
	entry.mu.Unlock()

	p.logger.V(0).Info("plugin loaded into pool",
		"plugin", entry.dep.Name, "version", result.Version, "providers", providers)
}

// failWith marks the entry as dead with the given error.
func (e *poolEntry) failWith(err error) {
	e.mu.Lock()
	e.state = entryDead
	e.err = err
	e.mu.Unlock()
}

// Acquire increments the reference count for a plugin, preventing eviction.
// Returns false if the plugin is not in the pool or is dead.
func (p *Pool) Acquire(name string) bool {
	p.mu.Lock()
	entry, ok := p.entries[name]
	p.mu.Unlock()
	if !ok {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.state != entryReady {
		return false
	}
	atomic.AddInt32(&entry.refCount, 1)
	entry.lastUsed = p.opts.clock()
	return true
}

// Release decrements the reference count for a plugin.
func (p *Pool) Release(name string) {
	p.mu.Lock()
	entry, ok := p.entries[name]
	p.mu.Unlock()
	if !ok {
		return
	}
	atomic.AddInt32(&entry.refCount, -1)
}

// Ping checks if a plugin is alive by issuing a lightweight RPC.
func (p *Pool) Ping(ctx context.Context, name string) bool {
	p.mu.Lock()
	entry, ok := p.entries[name]
	p.mu.Unlock()
	if !ok {
		return false
	}
	entry.mu.Lock()
	client := entry.client
	state := entry.state
	entry.mu.Unlock()

	if state != entryReady || client == nil {
		return false
	}

	// Use GetProviders as a health probe (lightweight RPC).
	_, err := client.GetProviders(ctx)
	if err != nil {
		p.markDead(name, entry, err)
		return false
	}
	return true
}

// markDead transitions an entry to dead state and unregisters its providers.
func (p *Pool) markDead(name string, entry *poolEntry, reason error) {
	entry.mu.Lock()
	if entry.state == entryDead {
		entry.mu.Unlock()
		return
	}
	entry.state = entryDead
	entry.err = reason
	client := entry.client
	registered := entry.registeredProviders
	entry.mu.Unlock()

	for _, pName := range registered {
		p.registry.Unregister(pName)
	}
	if client != nil {
		client.Kill()
	}

	p.logger.V(0).Info("plugin marked dead", "plugin", name, "reason", reason)
}

// unregisterEntry unregisters tracked providers and kills the client.
func (p *Pool) unregisterEntry(entry *poolEntry) {
	entry.mu.Lock()
	registered := entry.registeredProviders
	client := entry.client
	entry.mu.Unlock()

	for _, pName := range registered {
		p.registry.Unregister(pName)
	}
	if client != nil {
		client.Kill()
	}
}

// evictionLoop periodically scans for idle or dead entries and removes them.
func (p *Pool) evictionLoop() {
	ticker := time.NewTicker(p.opts.idleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.evict()
		}
	}
}

// evict removes idle and dead entries.
func (p *Pool) evict() {
	now := p.opts.clock()
	p.mu.Lock()
	var toEvict []string
	for name, entry := range p.entries {
		entry.mu.Lock()
		idle := entry.state == entryReady &&
			atomic.LoadInt32(&entry.refCount) == 0 &&
			p.opts.idleTimeout > 0 &&
			now.Sub(entry.lastUsed) > p.opts.idleTimeout
		dead := entry.state == entryDead
		entry.mu.Unlock()

		if idle || dead {
			toEvict = append(toEvict, name)
		}
	}
	p.mu.Unlock()

	for _, name := range toEvict {
		p.mu.Lock()
		entry, ok := p.entries[name]
		if !ok {
			p.mu.Unlock()
			continue
		}
		delete(p.entries, name)
		p.evicted++
		p.mu.Unlock()

		entry.mu.Lock()
		client := entry.client
		entry.mu.Unlock()

		if client != nil {
			p.unregisterEntry(entry)
		}
		p.logger.V(1).Info("evicted plugin from pool", "plugin", name)
	}
}

// Stats returns current pool metrics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	var stats PoolStats
	stats.Total = len(p.entries)
	stats.Evicted = p.evicted

	for _, entry := range p.entries {
		entry.mu.Lock()
		switch entry.state {
		case entryStarting:
			stats.Active++ // starting counts as active (spawn in progress)
		case entryReady:
			if atomic.LoadInt32(&entry.refCount) > 0 {
				stats.Active++
			} else {
				stats.Idle++
			}
		case entryDead:
			stats.Dead++
		}
		entry.mu.Unlock()
	}
	return stats
}

// SanitizeEnv reports whether this pool sanitizes the environment for spawned
// plugin clients. See [WithSanitizeEnv].
func (p *Pool) SanitizeEnv() bool {
	return p.opts.sanitizeEnv
}

// ClientOptsLen returns the number of extra client options configured on the pool.
// This is primarily useful for testing that WithClientOptions was wired correctly.
func (p *Pool) ClientOptsLen() int {
	return len(p.opts.clientOpts)
}

// Shutdown kills all managed plugin processes. Called once on server stop.
func (p *Pool) Shutdown() {
	p.stopOnce.Do(func() {
		close(p.stop)
	})

	p.mu.Lock()
	p.closed = true
	entries := make(map[string]*poolEntry, len(p.entries))
	for k, v := range p.entries {
		entries[k] = v
	}
	p.entries = make(map[string]*poolEntry)
	p.mu.Unlock()

	for _, entry := range entries {
		entry.mu.Lock()
		client := entry.client
		entry.mu.Unlock()
		if client != nil {
			client.Kill()
		}
	}
}
