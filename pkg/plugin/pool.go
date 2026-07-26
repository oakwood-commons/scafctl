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
	// pendingKill is set when teardown was requested (eviction or death) while
	// the entry still had active references (refCount > 0). The client's
	// providers are unregistered immediately so no new lookups resolve to it,
	// but the process Kill is deferred until the final Release drains the last
	// reference. This prevents tearing down a plugin mid-request.
	pendingKill bool
}

// poolOptions holds Pool configuration.
type poolOptions struct {
	idleTimeout     time.Duration
	maxPlugins      int
	healthInterval  time.Duration
	spawnTimeout    time.Duration    // bounds the whole spawn (fetch+handshake+configure)
	clock           func() time.Time // for testing
	allowedPlugins  map[string]bool  // nil means allow all
	disableExternal bool             // reject all non-adopted plugins
	clientOpts      []ClientOption   // extra options for spawned plugin clients
	sanitizeEnv     bool             // prepend WithSanitizedEnv() on spawn
	baseConfig      ProviderConfig   // host-static config delivered to each pooled provider at load
}

// defaultPoolOptions returns sensible defaults.
func defaultPoolOptions() poolOptions {
	return poolOptions{
		idleTimeout:    5 * time.Minute,
		maxPlugins:     50,
		healthInterval: 30 * time.Second,
		spawnTimeout:   2 * time.Minute,
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

// WithSpawnTimeout bounds the entire spawn sequence for a single plugin --
// fetch, process start, gRPC handshake, provider registration, and configure.
// Spawns run under the pool-lifetime context (not the caller's request
// context), so this timeout is the only bound on how long a lazy load may take.
// Zero disables the bound (spawn is limited only by pool shutdown).
func WithSpawnTimeout(d time.Duration) PoolOption {
	return func(o *poolOptions) { o.spawnTimeout = d }
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

// WithBaseProviderConfig sets the host-static ProviderConfig delivered to every
// pooled provider via the one-time ConfigureProvider call at load. Use it to
// carry host runtime metadata (build info, entrypoint, command, args) and the
// binary name into long-lived pool-mode hosts so plugins that read
// ProviderConfig.Settings behave the same as under one-shot per-call hosts.
//
// The config must contain only host-static data (constant for the pool's
// lifetime). Per-solution values vary per request and are delivered on the
// per-execution path instead, never re-configured on the shared wrapper.
func WithBaseProviderConfig(cfg ProviderConfig) PoolOption {
	return func(o *poolOptions) { o.baseConfig = cfg }
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
//
// The pool owns a lifetime context (derived from the context passed to
// NewPool). All long-lived plugin setup -- fetching binaries, starting
// processes, registering provider wrappers, and configuring them -- runs under
// this context, NOT the per-request context that happened to trigger the lazy
// load. This ensures a registered wrapper is never backed by a client whose
// initialization context died with a transient request. Per-request contexts
// scope only per-operation Execute/ExecuteStream calls.
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

	// ctx is the pool-lifetime context; cancel tears it down on Shutdown.
	// Long-lived plugin setup (spawn) derives its context from ctx so request
	// cancellation cannot invalidate a registered wrapper.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPool creates a plugin pool backed by the given fetcher and registry.
// The fetcher may be nil if only Adopt (pre-loaded) plugins are used.
//
// ctx is the pool-lifetime context: it governs the background eviction loop and
// all long-lived plugin initialization. Pass a long-lived context (e.g. the
// server context) so plugin loads survive individual requests; the pool
// cancels its derived child context on Shutdown. A nil ctx defaults to
// context.Background().
func NewPool(ctx context.Context, fetcher *Fetcher, registry *provider.Registry, logger logr.Logger, opts ...PoolOption) *Pool {
	o := defaultPoolOptions()
	for _, opt := range opts {
		opt(&o)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// cancel is retained in p.cancel and invoked by Shutdown; gosec cannot see
	// the deferred ownership transfer across the struct field.
	poolCtx, cancel := context.WithCancel(ctx) //nolint:gosec // G118: cancel stored in p.cancel, called by Shutdown
	p := &Pool{
		entries:  make(map[string]*poolEntry),
		fetcher:  fetcher,
		registry: registry,
		opts:     o,
		stop:     make(chan struct{}),
		logger:   logger,
		ctx:      poolCtx,
		cancel:   cancel,
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
		if _, err := p.ensureOne(ctx, dep, false); err != nil {
			return err
		}
	}
	return nil
}

// EnsureAndAcquire ensures all plugins are available and acquires them
// (increments their refcounts), preventing idle eviction for the duration
// of the caller's work. Returns a release function that must be called when
// the caller is done using the plugins (typically via defer).
//
// Acquisition happens atomically with readiness validation (under the entry
// lock), closing the window where a just-readied plugin could be evicted or
// torn down before the caller records its reference.
func (p *Pool) EnsureAndAcquire(ctx context.Context, deps []solution.PluginDependency) (release func(), err error) {
	var acquired []string
	for _, dep := range deps {
		if dep.Kind != solution.PluginKindProvider {
			continue
		}
		ok, eErr := p.ensureOne(ctx, dep, true)
		if eErr != nil {
			// Release anything already acquired before returning the error.
			for _, name := range acquired {
				p.Release(name)
			}
			return nil, eErr
		}
		if ok {
			acquired = append(acquired, dep.Name)
		}
	}

	return func() {
		for _, name := range acquired {
			p.Release(name)
		}
	}, nil
}

// ensureOne handles a single plugin dependency. When acquire is true, a
// reference is atomically taken on the ready entry (under the entry lock) so
// eviction cannot tear it down before the caller records the reference; the
// returned bool reports whether a reference was actually taken (false for
// builtins/pre-registered providers that are not pool-managed).
func (p *Pool) ensureOne(ctx context.Context, dep solution.PluginDependency, acquire bool) (bool, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false, ErrPoolClosed
	}

	// Already in pool (pre-loaded or previously loaded)?
	if entry, ok := p.entries[dep.Name]; ok {
		p.mu.Unlock()
		return p.waitForEntry(ctx, dep.Name, entry, acquire)
	}

	// Check if already registered (builtin or pre-loaded without Adopt)
	if p.registry.Has(dep.Name) {
		p.mu.Unlock()
		return false, nil
	}

	// Security: reject if external plugins are disabled entirely.
	if p.opts.disableExternal {
		p.mu.Unlock()
		return false, fmt.Errorf("plugin %q: %w", dep.Name, ErrExternalDisabled)
	}

	// Security: reject if allowlist is configured and plugin is not on it.
	if p.opts.allowedPlugins != nil && !p.opts.allowedPlugins[dep.Name] {
		p.mu.Unlock()
		return false, fmt.Errorf("plugin %q: %w", dep.Name, ErrPluginNotAllowed)
	}

	// Capacity check
	if p.opts.maxPlugins > 0 && len(p.entries) >= p.opts.maxPlugins {
		p.mu.Unlock()
		return false, fmt.Errorf("plugin %q: %w", dep.Name, ErrPoolFull)
	}

	// Create a placeholder entry; the spawn runs in the background under the
	// pool-lifetime context (not the caller's request context) so a cancelled
	// or short-lived request cannot invalidate a registered plugin wrapper.
	entry := &poolEntry{
		state: entryStarting,
		dep:   dep,
		ready: make(chan struct{}),
	}
	p.entries[dep.Name] = entry
	p.mu.Unlock()

	spawnCtx, cancel := p.newSpawnContext()
	go p.spawn(spawnCtx, entry, cancel)

	return p.waitForEntry(ctx, dep.Name, entry, acquire)
}

// waitForEntry waits for an entry to become ready (bounded by the caller's
// context), optionally acquiring a reference, and removes the entry if the
// spawn failed so a later Ensure can retry. A context cancellation leaves the
// entry intact -- the background spawn continues under the pool context.
func (p *Pool) waitForEntry(ctx context.Context, name string, entry *poolEntry, acquire bool) (bool, error) {
	acquired, err := p.waitAndValidate(ctx, entry, acquire)
	if err != nil {
		p.mu.Lock()
		entry.mu.Lock()
		dead := entry.state == entryDead
		refs := atomic.LoadInt32(&entry.refCount)
		entry.mu.Unlock()
		// Only remove entries that genuinely failed and are unreferenced.
		// Referenced dead entries stay in the map so their final Release can
		// finalize the deferred teardown.
		if dead && refs == 0 {
			delete(p.entries, name)
			p.mu.Unlock()
			p.teardownEntry(entry)
		} else {
			p.mu.Unlock()
		}
	}
	return acquired, err
}

// newSpawnContext derives a context for a background spawn from the pool
// lifetime context, bounded by the configured spawn timeout. It is fully
// decoupled from any request context so request cancellation cannot abort a
// plugin load in progress; the pool context (cancelled on Shutdown) and the
// spawn timeout are the only bounds.
func (p *Pool) newSpawnContext() (context.Context, context.CancelFunc) {
	if p.opts.spawnTimeout > 0 {
		return context.WithTimeout(p.ctx, p.opts.spawnTimeout)
	}
	return context.WithCancel(p.ctx)
}

// waitAndValidate waits for an entry to become ready and checks health. When
// acquire is true and the entry is ready, a reference is taken under the entry
// lock (atomic with the readiness check) and the returned bool is true.
func (p *Pool) waitAndValidate(ctx context.Context, entry *poolEntry, acquire bool) (bool, error) {
	// Wait for entry to be ready (handles concurrent Ensure for same plugin).
	select {
	case <-entry.ready:
	case <-ctx.Done():
		return false, ctx.Err()
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.state == entryDead {
		// Dead entries will be re-spawned on next Ensure after eviction.
		return false, fmt.Errorf("plugin %q is dead: %w", entry.dep.Name, entry.err)
	}

	// Touch last-used time
	entry.lastUsed = p.opts.clock()
	if acquire {
		atomic.AddInt32(&entry.refCount, 1)
		return true, nil
	}
	return false, nil
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

// spawn fetches and starts a plugin, updating the entry state. It runs in a
// background goroutine under a pool-derived context; cancel releases that
// context when the spawn completes.
func (p *Pool) spawn(ctx context.Context, entry *poolEntry, cancel context.CancelFunc) {
	defer cancel()
	defer close(entry.ready)

	if p.fetcher == nil {
		entry.failWith(errors.New("plugin fetcher not available"))
		return
	}

	// Fetch binary
	results, err := p.fetcher.FetchPlugins(ctx, []solution.PluginDependency{entry.dep}, nil)
	// Release cache pins — placed before error check so partial results
	// are released on failure. After exec, the OS has the binary mapped
	// in memory so the on-disk file can be safely evicted by the LRU cache.
	defer func() {
		for i := range results {
			if results[i].Release != nil {
				results[i].Release()
			}
		}
	}()
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
		// tokens or other host services. The base config also carries
		// host-static runtime metadata (build info, entrypoint, command, args)
		// so pooled plugins that read ProviderConfig.Settings match per-call
		// hosts; it is empty by default.
		if cErr := wrapper.Configure(ctx, p.opts.baseConfig); cErr != nil {
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

// Release decrements the reference count for a plugin. If this drains the last
// reference on an entry whose teardown was deferred (pendingKill), the client is
// killed now.
func (p *Pool) Release(name string) {
	p.mu.Lock()
	entry, ok := p.entries[name]
	p.mu.Unlock()
	if !ok {
		return
	}
	entry.mu.Lock()
	newCount := atomic.AddInt32(&entry.refCount, -1)
	var client *Client
	if newCount <= 0 && entry.pendingKill {
		client = entry.client
		entry.client = nil
		entry.pendingKill = false
	}
	entry.mu.Unlock()
	if client != nil {
		client.Kill()
	}
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
// The underlying client is killed immediately if unreferenced, or deferred to
// the final Release if requests are still in flight.
func (p *Pool) markDead(name string, entry *poolEntry, reason error) {
	entry.mu.Lock()
	if entry.state == entryDead {
		entry.mu.Unlock()
		return
	}
	entry.state = entryDead
	entry.err = reason
	entry.mu.Unlock()

	p.teardownEntry(entry)
	p.logger.V(0).Info("plugin marked dead", "plugin", name, "reason", reason)
}

// teardownEntry unregisters an entry's providers from the shared registry so no
// new lookups resolve to it, then tears down the client. If the entry still has
// active references (refCount > 0), the process Kill is deferred: pendingKill is
// set and the final Release performs the Kill. Otherwise the client is killed
// immediately. Safe to call multiple times.
func (p *Pool) teardownEntry(entry *poolEntry) {
	entry.mu.Lock()
	registered := entry.registeredProviders
	entry.registeredProviders = nil
	client := entry.client
	if client == nil {
		entry.mu.Unlock()
		p.unregisterProviders(registered)
		return
	}
	if atomic.LoadInt32(&entry.refCount) > 0 {
		// Deferred teardown: keep the client so the last Release can kill it,
		// but stop new lookups by unregistering providers now.
		entry.pendingKill = true
		entry.mu.Unlock()
		p.unregisterProviders(registered)
		return
	}
	// No references: safe to kill now. Clear the client to prevent double-kill.
	entry.client = nil
	entry.mu.Unlock()
	p.unregisterProviders(registered)
	client.Kill()
}

// unregisterProviders removes the given provider names from the shared registry.
func (p *Pool) unregisterProviders(names []string) {
	for _, pName := range names {
		p.registry.Unregister(pName)
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
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.evict()
		}
	}
}

// evict removes idle and dead entries that have no active references. Entries
// still in use (refCount > 0) are left in place; their teardown is handled by
// the final Release.
func (p *Pool) evict() {
	now := p.opts.clock()
	p.mu.Lock()
	var names []string
	var entries []*poolEntry
	for name, entry := range p.entries {
		entry.mu.Lock()
		refs := atomic.LoadInt32(&entry.refCount)
		idle := entry.state == entryReady &&
			refs == 0 &&
			p.opts.idleTimeout > 0 &&
			now.Sub(entry.lastUsed) > p.opts.idleTimeout
		dead := entry.state == entryDead && refs == 0
		entry.mu.Unlock()

		if idle || dead {
			names = append(names, name)
			entries = append(entries, entry)
		}
	}
	for _, name := range names {
		delete(p.entries, name)
		p.evicted++
	}
	p.mu.Unlock()

	for i, entry := range entries {
		p.teardownEntry(entry)
		p.logger.V(1).Info("evicted plugin from pool", "plugin", names[i])
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

// BaseProviderConfig returns the host-static ProviderConfig delivered to each
// pooled provider at load time. See [WithBaseProviderConfig]. This is primarily
// useful for testing that the base config was wired correctly.
func (p *Pool) BaseProviderConfig() ProviderConfig {
	return p.opts.baseConfig
}

// Shutdown kills all managed plugin processes. Called once on server stop.
func (p *Pool) Shutdown() {
	p.stopOnce.Do(func() {
		close(p.stop)
		if p.cancel != nil {
			p.cancel()
		}
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
		registered := entry.registeredProviders
		entry.registeredProviders = nil
		client := entry.client
		entry.client = nil
		entry.mu.Unlock()
		// Unregister providers before killing so the shared registry is not
		// left holding dead-backed wrappers if it outlives the pool.
		p.unregisterProviders(registered)
		if client != nil {
			client.Kill()
		}
	}
}
