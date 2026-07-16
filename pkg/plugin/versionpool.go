package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/store"
)

// pluginClient is the minimal surface VersionPool needs from a running plugin
// client: enumerating its providers and terminating it. *Client satisfies it,
// and tests inject in-memory fakes so spawn paths need no real subprocess.
type pluginClient interface {
	GetProviders(ctx context.Context) ([]string, error)
	Kill()
}

// pluginFetcher is the subset of *Fetcher the pool needs to resolve and
// download plugin binaries. Depending on the interface (rather than the
// concrete *Fetcher) lets tests inject canned fetch results without a real
// catalog or network. *Fetcher satisfies it.
type pluginFetcher interface {
	FetchPlugins(ctx context.Context, plugins []solution.PluginDependency, lockPlugins []bundler.LockPlugin) ([]FetchResult, error)
}

// registrableProvider is a provider that can be registered in a provider
// registry and configured with host-side settings. *ProviderWrapper satisfies
// it.
type registrableProvider interface {
	provider.Provider
	Configure(ctx context.Context, cfg ProviderConfig) error
}

// providerRegistry is the subset of *provider.Registry the pool uses to publish
// and retract plugin providers. Depending on the interface (rather than the
// concrete registry) lets tests assert registration without the global
// registry's uniqueness side effects.
type providerRegistry interface {
	Register(p provider.Provider) error
	Has(name string) bool
	Unregister(name string) bool
}

// clientFactory builds a plugin client from a binary path (the first seam).
// The default launches a real process via NewClient; tests inject a factory
// that returns a fake pluginClient.
type clientFactory func(path string, opts ...ClientOption) (pluginClient, error)

// wrapperFactory builds a registrable provider from a client + provider name
// (the second seam — this is what makes the registration loop testable).
type wrapperFactory func(client pluginClient, name string, opts ...WrapperOption) (registrableProvider, error)

// defaultClientFactory adapts NewClient to the clientFactory seam. The returned
// *Client satisfies pluginClient.
func defaultClientFactory(path string, opts ...ClientOption) (pluginClient, error) {
	return NewClient(path, opts...)
}

// defaultWrapperFactory adapts NewProviderWrapper to the wrapperFactory seam.
// NewProviderWrapper needs the concrete *Client that defaultClientFactory
// produces; in production the client always is one. A non-*Client only occurs
// when a test wires a fake clientFactory without a matching wrapperFactory,
// which is a misconfiguration reported as an error.
func defaultWrapperFactory(client pluginClient, name string, opts ...WrapperOption) (registrableProvider, error) {
	c, ok := client.(*Client)
	if !ok {
		return nil, fmt.Errorf("defaultWrapperFactory requires *Client, got %T", client)
	}
	return NewProviderWrapper(c, name, opts...)
}

/*
invariants
	Plugin identity invariants:
		- the identity of a plugin is defined by its name and catalog and version.
	Version index invariants:
		- For each key, the versions slice is sorted in descending order (highest version first).
		- For each key, the versions slice contains no duplicates.
		- For each key, the versions slice contains only valid semver versions.
	- it can handle version constraints and return the highest version that satisfies the constraint.
	Store invariants:
		- For each key, the store contains a poolEntry for each version in the index.
		- The store contains no poolEntry for any version not present in the index.
		- The key for the store is a combination of the plugin identity and version, ensuring uniqueness.
	Pool invariants:
		- The pool maintains a consistent view of the version index and store, ensuring that for every version in the index, there is a corresponding entry in the store.

*/

type VersionIndex[T comparable] struct {
	entries map[T][]*semver.Version
}

func NewVersionIndex[T comparable]() *VersionIndex[T] {
	return &VersionIndex[T]{
		entries: make(map[T][]*semver.Version),
	}
}

// AddVersion inserts an already-parsed version into the index. Callers holding
// a *semver.Version avoid re-parsing and the operation cannot fail. Inserting a
// duplicate is a no-op; otherwise the version is placed to preserve the
// descending-sort invariant.
func (vi *VersionIndex[T]) AddVersion(key T, sv *semver.Version) {
	entry := vi.entries[key]
	idx, found := search(entry, sv)
	if found {
		return // duplicate, do not insert
	}
	entry = append(entry, nil)
	copy(entry[idx+1:], entry[idx:])
	entry[idx] = sv
	vi.entries[key] = entry
}

// DeleteVersion removes an already-parsed version from the index. Callers
// holding a *semver.Version avoid re-parsing and the operation cannot fail.
// Removing an absent version (or a version under an absent key) is a no-op.
func (vi *VersionIndex[T]) DeleteVersion(key T, sv *semver.Version) {
	entry := vi.entries[key]
	idx := findExact(entry, sv)
	if idx >= 0 {
		entry = append(entry[:idx], entry[idx+1:]...)
		if len(entry) == 0 {
			delete(vi.entries, key)
		} else {
			vi.entries[key] = entry
		}
	}
}

// GetVersion performs an exact, parse-free lookup of an already-parsed version.
// It returns the indexed version equal to sv, or nil if that version (or its
// key) is absent. It never interprets ranges or "latest".
func (vi *VersionIndex[T]) GetVersion(key T, sv *semver.Version) *semver.Version {
	entry := vi.entries[key]
	if idx := findExact(entry, sv); idx >= 0 {
		return entry[idx]
	}
	return nil
}

// GetConstraints returns the highest indexed version satisfying c, or nil if
// none does (or the key is absent). Callers hold a pre-compiled
// *semver.Constraints; because entries are sorted descending, the first match
// is the highest satisfying version.
func (vi *VersionIndex[T]) GetConstraints(key T, c *semver.Constraints) *semver.Version {
	for _, v := range vi.entries[key] {
		if c.Check(v) {
			return v
		}
	}
	return nil
}

// latestVersion is the sentinel version string meaning "resolve to the newest
// available version at fetch time". It is not a valid semver version or
// constraint; the pool (like the fetcher and bundler) treats it, together with
// the empty string, as a wildcard that matches any version.
const latestVersion = "latest"

// isLatestConstraint reports whether s requests the latest available version
// rather than pinning a concrete version or range. Both the empty string and
// "latest" (case-insensitive, ignoring surrounding whitespace) are the "latest"
// sentinel; every other value is a concrete version or a real semver
// constraint. It never allocates a *semver.Constraints, so callers can cheaply
// branch before falling back to parseVersionOrConstraint.
func isLatestConstraint(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.EqualFold(s, latestVersion)
}

// parseVersionOrConstraint classifies a version string as either an exact
// version or a range constraint. Exactly one of the returned pointers is
// non-nil on success:
//   - sv non-nil, c nil    -> an exact X.Y.Z pin (StrictNewVersion succeeded).
//   - c non-nil, sv nil    -> a range/wildcard/operator expression ("^1.2", ">=1.0").
//
// It does not special-case "" or "latest"; those are sentinels the index layer
// handles and are reported here as constraint-parse errors. An unparseable
// string yields an error with both pointers nil.
func parseVersionOrConstraint(s string) (sv *semver.Version, c *semver.Constraints, err error) {
	// StrictNewVersion rejects partials ("1.2") and ranges (">=1.2.3"), so a
	// success unambiguously means an exact version was given.
	if v, verr := semver.StrictNewVersion(s); verr == nil {
		return v, nil, nil
	}
	cc, cerr := semver.NewConstraint(s)
	if cerr != nil {
		return nil, nil, cerr
	}
	return nil, cc, nil
}

// search binary-searches entry (sorted descending, the VersionIndex invariant)
// for sv. It returns the index where sv is or would be inserted to preserve
// order, and whether an equal version was found at that index. Callers that
// insert use idx on the miss path; callers that look up or remove use idx only
// when found is true.
func search(entry []*semver.Version, sv *semver.Version) (idx int, found bool) {
	idx = sort.Search(len(entry), func(i int) bool {
		return !entry[i].GreaterThan(sv)
	})
	return idx, idx < len(entry) && entry[idx].Equal(sv)
}

// findExact returns the index of the version in entry equal to sv, or -1 if
// absent. entry must be sorted in descending order (the VersionIndex
// invariant), enabling a binary search instead of a linear scan.
func findExact(entry []*semver.Version, sv *semver.Version) int {
	if idx, found := search(entry, sv); found {
		return idx
	}
	return -1
}

type PluginIdentity struct {
	Name    string
	Catalog string
}

type VersionedPluginIdentity struct {
	PluginIdentity
	Version string
}

type options struct {
	clock           func() time.Time
	spawnTimeout    time.Duration
	idleTimeout     time.Duration
	maxPlugins      int
	logger          logr.Logger
	newClient       clientFactory
	newWrapper      wrapperFactory
	disableExternal bool
	allowedPlugins  map[string]bool
	clientOpts      []ClientOption // extra options for spawned plugin clients
	sanitizeEnv     bool           // prepend WithSanitizedEnv() on spawn
}

type Option func(*options)

func WithClock(clock func() time.Time) Option {
	return func(o *options) {
		o.clock = clock
	}
}

func WithLogger(logger logr.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}
func WithVersionPoolDisableExternal(disable bool) Option {
	return func(o *options) {
		o.disableExternal = disable
	}
}

func WithVersionPoolAAllowedPlugins(allowed map[string]bool) Option {
	return func(o *options) {
		o.allowedPlugins = allowed
	}
}

// WithVersionPoolSpawnTimeout bounds the entire spawn sequence for a single
// plugin (fetch, process start, handshake, provider registration). Zero
// disables the bound. It mirrors WithSpawnTimeout (a PoolOption) but returns an
// Option for NewVersionPool; the two cannot share a name in this package.
func WithVersionPoolSpawnTimeout(d time.Duration) Option {
	return func(o *options) {
		o.spawnTimeout = d
	}
}

// WithVersionPoolClientOptions sets additional ClientOption values that are
// passed to every plugin client spawned by the pool. Use this to inject
// host-side dependencies such as auth registries (via WithHostDeps) or the gRPC
// max message size. It mirrors WithClientOptions (a PoolOption) but returns an
// Option for NewVersionPool.
func WithVersionPoolClientOptions(opts ...ClientOption) Option {
	return func(o *options) {
		o.clientOpts = append(o.clientOpts, opts...)
	}
}

// WithVersionPoolSanitizeEnv controls whether spawned plugin clients get a
// sanitized environment (true) or inherit the host environment (false). API
// server deployments should use true; MCP interactive sessions may use false so
// plugins can access host credentials (SSH_AUTH_SOCK, tokens, etc.). It mirrors
// WithSanitizeEnv (a PoolOption) but returns an Option for NewVersionPool.
func WithVersionPoolSanitizeEnv(sanitize bool) Option {
	return func(o *options) {
		o.sanitizeEnv = sanitize
	}
}

// WithVersionPoolIdleTimeout sets how long an unused (ready, unreferenced)
// plugin stays alive before the background eviction loop tears it down. Zero
// disables idle eviction and prevents the eviction loop from starting. It
// mirrors WithIdleTimeout (a PoolOption) but returns an Option for
// NewVersionPool.
func WithVersionPoolIdleTimeout(d time.Duration) Option {
	return func(o *options) {
		o.idleTimeout = d
	}
}

// WithVersionPoolMaxPlugins bounds the number of distinct plugin versions the
// pool will hold at once. Attempting to load a new version once the bound is
// reached fails with ErrPoolFull. Zero means unbounded. It mirrors
// WithMaxPlugins (a PoolOption) but returns an Option for NewVersionPool.
func WithVersionPoolMaxPlugins(n int) Option {
	return func(o *options) {
		o.maxPlugins = n
	}
}

/*
Invariants :
- For every key in the index, the store contains a poolEntry for each version in the index.
- The store contains no poolEntry for any version not present in the index.
- The store is the authoritative source for the poolEntry data; the index is a derived view of the versions present in the store.
*/
type VersionPool struct {
	mu       sync.RWMutex
	index    VersionIndex[PluginIdentity]
	store    store.Storer[VersionedPluginIdentity, *poolEntry]
	opts     *options
	fetcher  pluginFetcher
	logger   logr.Logger
	registry providerRegistry
	ctx      context.Context
	cancel   context.CancelFunc
	stop     chan struct{}
	closed   atomic.Bool
	stopOnce sync.Once
	evicted  int // cumulative evictions since pool creation; guarded by mu
}

func NewVersionPool(cx context.Context, fetcher pluginFetcher, registry providerRegistry, opts ...Option) *VersionPool {
	options := &options{
		clock:       func() time.Time { return time.Now() },
		logger:      logr.Discard(),
		newClient:   defaultClientFactory,
		newWrapper:  defaultWrapperFactory,
		sanitizeEnv: true,
	}
	for _, o := range opts {
		o(options)
	}
	poolctx, cancel := context.WithCancel(cx)
	vp := &VersionPool{
		index:    *NewVersionIndex[PluginIdentity](),
		store:    store.New[VersionedPluginIdentity, *poolEntry](),
		opts:     options,
		fetcher:  fetcher,
		logger:   options.logger,
		registry: registry,
		ctx:      poolctx,
		cancel:   cancel,
		stop:     make(chan struct{}),
	}
	if options.idleTimeout > 0 {
		go vp.evictionLoop()
	}
	return vp
}

func (vp *VersionPool) ensureOneByVersion(ctx context.Context, dep solution.PluginDependency, version *semver.Version, acquire bool) (*poolEntry, bool, error) {
	vp.mu.Lock()
	id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}
	if entry := vp.GetByVersion(id, version); entry != nil {
		defer vp.mu.Unlock()
		return vp.waitForEntry(ctx, id, entry, acquire)
	}
	// Cache miss: loading this version adds a new entry, so enforce capacity.
	if err := vp.checkCapacity(dep); err != nil {
		vp.mu.Unlock()
		return nil, false, err
	}
	entry := &poolEntry{
		state:   entryStarting,
		dep:     dep,
		version: version,
		ready:   make(chan struct{}),
	}
	vp.insert(id, version, entry)
	vp.mu.Unlock()
	spawnCtx, cancel := vp.newSpawnContext()
	go vp.spawnWithEntry(spawnCtx, cancel, entry, dep)
	return vp.waitForEntry(ctx, id, entry, acquire)
}

func (vp *VersionPool) ensureOneByConstraint(ctx context.Context, dep solution.PluginDependency, con *semver.Constraints, acquire bool) (*poolEntry, bool, error) {
	id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}

	// Fast path: a version already in the index satisfies the constraint.
	vp.mu.Lock()
	if entry := vp.GetByConstraint(id, con); entry != nil {
		defer vp.mu.Unlock()
		return vp.waitForEntry(ctx, id, entry, acquire)
	}
	// Nothing satisfies the constraint yet, so resolving it will add a new
	// entry. Reject early (before the expensive fetch) if the pool is full. The
	// commit-point check below is authoritative; this only avoids wasted I/O.
	if err := vp.checkCapacity(dep); err != nil {
		vp.mu.Unlock()
		return nil, false, err
	}
	vp.mu.Unlock()

	// Slow path: nothing indexed satisfies the constraint, so the concrete
	// version is unknown until the catalog resolves it. Fetch to discover it.
	// The fetch runs OUTSIDE vp.mu so pool-wide operations are not blocked on
	// network I/O.
	if vp.fetcher == nil {
		return nil, false, fmt.Errorf("plugin fetcher not available")
	}
	results, err := vp.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	if err != nil {
		releaseResults(results)
		return nil, false, fmt.Errorf("fetching plugin %q: %w", dep.Name, err)
	}
	if len(results) == 0 {
		return nil, false, fmt.Errorf("no fetch results for plugin %q", dep.Name)
	}
	result := results[0]

	version, err := semver.NewVersion(result.Version)
	if err != nil {
		releaseResults(results)
		return nil, false, fmt.Errorf("parsing resolved version %q for plugin %q: %w", result.Version, dep.Name, err)
	}

	// Re-acquire the lock and commit. A concurrent caller may have resolved the
	// same concrete version while we fetched, so re-check under the lock and
	// release our redundant fetch pin if so.
	vp.mu.Lock()
	if entry := vp.GetByVersion(id, version); entry != nil {
		vp.mu.Unlock()
		releaseResults(results)
		return vp.waitForEntry(ctx, id, entry, acquire)
	}
	// Definitive capacity check: another caller may have filled the pool while
	// we fetched. Release the redundant pin and reject if so.
	if err := vp.checkCapacity(dep); err != nil {
		vp.mu.Unlock()
		releaseResults(results)
		return nil, false, err
	}
	entry := &poolEntry{
		state:   entryStarting,
		dep:     dep,
		version: version,
		ready:   make(chan struct{}),
	}
	vp.insert(id, version, entry)
	vp.mu.Unlock()

	// Hand the already-fetched result to the spawner, which owns releasing the
	// cache pin once the process has started.
	spawnCtx, cancel := vp.newSpawnContext()
	go vp.spawnFromResult(spawnCtx, cancel, entry, result)
	return vp.waitForEntry(ctx, id, entry, acquire)
}

// releaseResults releases any active cache pins on the given fetch results. It
// is safe to call with nil results or results whose Release is nil (CLI mode).
func releaseResults(results []FetchResult) {
	for i := range results {
		if results[i].Release != nil {
			results[i].Release()
		}
	}
}

// spawnFromResult starts a plugin from an already-fetched binary (constraint
// path, where the fetch already resolved the concrete version). It owns
// releasing the single cache pin, then delegates to startAndRegister.
func (vp *VersionPool) spawnFromResult(ctx context.Context, cancel context.CancelFunc, entry *poolEntry, result FetchResult) {
	defer cancel()
	defer close(entry.ready)
	if result.Release != nil {
		// After exec the OS has the binary mapped, so the on-disk file can be
		defer result.Release()
	}
	vp.logger.V(1).Info("spawning plugin from fetched result",
		"plugin", entry.dep.Name, "version", result.Version)
	vp.startAndRegister(ctx, entry, result)
}

// spawnWithEntry fetches the binary for dep (version path, where the concrete
// version is already known), releases the fetch batch, then delegates to
// startAndRegister.
func (vp *VersionPool) spawnWithEntry(ctx context.Context, cancel context.CancelFunc, entry *poolEntry, dep solution.PluginDependency) {
	defer cancel()
	defer close(entry.ready)

	vp.logger.V(1).Info("spawning plugin", "plugin", dep.Name, "version", entry.version)
	if vp.fetcher == nil {
		err := fmt.Errorf("plugin fetcher not available")
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.Name, "error", err)
		return
	}
	results, err := vp.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	defer releaseResults(results)
	if err != nil {
		err = fmt.Errorf("fetching: %w", err)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.Name, "error", err)
		return
	}
	if len(results) == 0 {
		err = fmt.Errorf("no fetch results returned for plugin %q", dep.Name)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.Name, "error", err)
		return
	}
	vp.startAndRegister(ctx, entry, results[0])
}

// startAndRegister launches result's binary, registers its providers, and marks
// entry ready (or dead on failure). It is the binary-agnostic tail shared by
// both spawn paths; the caller owns fetching the binary and releasing its pin.
func (vp *VersionPool) startAndRegister(ctx context.Context, entry *poolEntry, result FetchResult) {
	clientOpts := buildSpawnClientOpts(vp.opts.sanitizeEnv, vp.opts.clientOpts)
	client, err := vp.opts.newClient(result.Path, clientOpts...)
	if err != nil {
		err = fmt.Errorf("starting process: %w", err)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", entry.dep.Name, "error", err)
		return
	}

	providers, err := client.GetProviders(ctx)
	if err != nil {
		client.Kill()
		err = fmt.Errorf("getting providers: %w", err)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", entry.dep.Name, "error", err)
		return
	}

	registered := vp.registerProviders(ctx, entry, client, providers)

	entry.mu.Lock()
	entry.client = client
	entry.result = result
	entry.registeredProviders = registered
	entry.state = entryReady
	entry.lastUsed = vp.opts.clock()
	entry.mu.Unlock()

	vp.logger.V(0).Info("plugin loaded into pool",
		"plugin", entry.dep.Name, "version", result.Version, "providers", providers)
}

// registerProviders wraps, registers, and configures each provider exposed by a
// freshly started client, returning the names it successfully registered. It
// performs no process I/O, so it is unit-testable through the wrapperFactory and
// providerRegistry seams. A provider is skipped (not registered) when no
// registry is configured, when its name is already taken, or when wrapping,
// registering, or configuring it fails.
func (vp *VersionPool) registerProviders(ctx context.Context, entry *poolEntry, client pluginClient, providers []string) []string {
	var registered []string
	for _, provName := range providers {
		if vp.registry == nil || vp.registry.Has(provName) {
			continue // no registry, or name already taken by another plugin
		}
		wrapper, wErr := vp.opts.newWrapper(client, provName, WithContext(ctx))
		if wErr != nil {
			vp.logger.V(1).Info("failed to create provider wrapper",
				"plugin", entry.dep.Name, "provider", provName, "error", wErr)
			continue
		}
		if rErr := vp.registry.Register(wrapper); rErr != nil {
			vp.logger.V(1).Info("failed to register provider",
				"plugin", entry.dep.Name, "provider", provName, "error", rErr)
			continue
		}
		// Configure so the plugin receives the host service ID and can dial
		// back to the host for auth tokens or other host services.
		if cErr := wrapper.Configure(ctx, ProviderConfig{}); cErr != nil {
			vp.logger.V(1).Info("failed to configure provider wrapper",
				"plugin", entry.dep.Name, "provider", provName, "error", cErr)
			continue
		}
		registered = append(registered, provName)
	}
	return registered
}

func (vp *VersionPool) newSpawnContext() (context.Context, context.CancelFunc) {
	if vp.opts.spawnTimeout > 0 {
		return context.WithTimeout(vp.ctx, vp.opts.spawnTimeout)
	}
	return context.WithCancel(vp.ctx)
}

func (vp *VersionPool) ensureOne(ctx context.Context, dep solution.PluginDependency, acquire bool) (*poolEntry, bool, error) {
	if vp.closed.Load() {
		return nil, false, ErrPoolClosed
	}
	if err := vp.validateEnsure(dep); err != nil {
		return nil, false, err
	}

	v, c, err := parseVersionOrConstraint(dep.Version)
	if err != nil {
		return nil, false, fmt.Errorf("parsing version %q for plugin %q: %w", dep.Version, dep.Name, err)
	}
	if v != nil {
		// Exact pin -> route to the concrete-version path.
		return vp.ensureOneByVersion(ctx, dep, v, acquire)
	}
	// Range/constraint -> resolve the concrete version via fetch.
	return vp.ensureOneByConstraint(ctx, dep, c, acquire)
}

// Ensure loads every provider dependency into the pool, spawning any that are
// not already present, without taking a reference. Non-provider kinds are
// skipped. It returns the first error encountered.
func (vp *VersionPool) Ensure(ctx context.Context, deps []solution.PluginDependency) error {
	for _, dep := range deps {
		if dep.Kind != solution.PluginKindProvider {
			continue
		}
		if _, _, err := vp.ensureOne(ctx, dep, false); err != nil {
			return err
		}
	}
	return nil
}

// EnsureAndAcquire loads every provider dependency and takes a reference on each
// ready entry, preventing idle eviction or teardown for the duration of the
// caller's work. It returns a release function that drops all acquired
// references and must be called when the caller is done (typically via defer).
//
// Acquisition is atomic with readiness validation (under the entry lock), so a
// just-readied plugin cannot be torn down before its reference is recorded. If
// any dependency fails, references already taken in this call are released
// before the error is returned.
//
// Because a constraint dependency resolves its concrete version internally, the
// release closure captures the resolved entry handles directly; callers never
// need to name the version that was selected.
func (vp *VersionPool) EnsureAndAcquire(ctx context.Context, deps []solution.PluginDependency) (release func(), err error) {
	var acquired []*poolEntry
	for _, dep := range deps {
		if dep.Kind != solution.PluginKindProvider {
			continue
		}
		entry, ok, eErr := vp.ensureOne(ctx, dep, true)
		if eErr != nil {
			for _, e := range acquired {
				vp.releaseEntry(e)
			}
			return nil, eErr
		}
		if ok {
			acquired = append(acquired, entry)
		}
	}
	return func() {
		for _, e := range acquired {
			vp.releaseEntry(e)
		}
	}, nil
}

// releaseEntry drops one reference on entry. If this drains the last reference
// on an entry whose teardown was deferred (pendingKill), the client is killed
// now. It operates purely on the entry handle returned by the ensure path, so a
// caller that acquired via a constraint never needs to name the concrete
// version that was resolved. It is the counterpart of the acquire performed in
// waitAndValidate.
func (vp *VersionPool) releaseEntry(entry *poolEntry) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	newCount := atomic.AddInt32(&entry.refCount, -1)
	var client pluginClient
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

func (vp *VersionPool) validateEnsure(dep solution.PluginDependency) error {
	if vp.opts.disableExternal {
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrExternalDisabled)
	}

	// Security: reject if allowlist is configured and plugin is not on it.
	if vp.opts.allowedPlugins != nil && !vp.opts.allowedPlugins[dep.Name] {
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrPluginNotAllowed)
	}
	return nil
}

// checkCapacity reports ErrPoolFull when loading a new plugin version would
// exceed the configured maxPlugins bound. A zero bound means unbounded. The
// store holds one entry per loaded version, so its length is the live count.
// Callers must hold vp.mu.
func (vp *VersionPool) checkCapacity(dep solution.PluginDependency) error {
	if vp.opts.maxPlugins > 0 && vp.store.Len() >= vp.opts.maxPlugins {
		return fmt.Errorf("plugin %q: %w", dep.Name, ErrPoolFull)
	}
	return nil
}

// evictCandidate pairs an entry with the composite key needed to remove it from
// both the index and the store during eviction.
type evictCandidate struct {
	id      PluginIdentity
	version *semver.Version
	entry   *poolEntry
}

// evictionLoop periodically scans for idle or dead entries and removes them. It
// runs until the pool's stop channel is closed or its context is cancelled. It
// is only started when idleTimeout > 0.
func (vp *VersionPool) evictionLoop() {
	ticker := time.NewTicker(vp.opts.idleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-vp.stop:
			return
		case <-vp.ctx.Done():
			return
		case <-ticker.C:
			vp.evict()
		}
	}
}

// evict removes idle and dead entries that have no active references. Idle
// entries are ready, unreferenced, and untouched for longer than idleTimeout;
// dead entries are failed spawns awaiting cleanup. Entries still in use
// (refCount > 0) are left in place; their teardown is handled by the final
// releaseEntry. Candidates are collected under vp.mu (walking the index, the
// authoritative key set) and removed from the index and store, then torn down
// OUTSIDE the lock so the process Kill never blocks pool-wide operations.
func (vp *VersionPool) evict() {
	now := vp.opts.clock()

	vp.mu.Lock()
	var candidates []evictCandidate
	for id, versions := range vp.index.entries {
		for _, sv := range versions {
			entry, ok := vp.store.Get(VersionedPluginIdentity{PluginIdentity: id, Version: sv.String()})
			if !ok {
				continue // invariant guards this; skip defensively
			}
			entry.mu.Lock()
			refs := atomic.LoadInt32(&entry.refCount)
			idle := entry.state == entryReady &&
				refs == 0 &&
				vp.opts.idleTimeout > 0 &&
				now.Sub(entry.lastUsed) > vp.opts.idleTimeout
			dead := entry.state == entryDead && refs == 0
			entry.mu.Unlock()

			if idle || dead {
				candidates = append(candidates, evictCandidate{id: id, version: sv, entry: entry})
			}
		}
	}
	// Remove from the index and store while the lock is held. This runs after
	// the range above completes, so the index map is not mutated mid-iteration.
	for _, c := range candidates {
		vp.delete(c.id, c.version)
		vp.evicted++
	}
	vp.mu.Unlock()

	for _, c := range candidates {
		vp.teardownEntry(c.entry)
		vp.logger.V(1).Info("evicted plugin from pool",
			"plugin", c.id.Name, "version", c.version)
	}
}

func (vp *VersionPool) GetByVersion(id PluginIdentity, v *semver.Version) *poolEntry {
	resolved := vp.index.GetVersion(id, v)
	if resolved == nil {
		return nil // no indexed version satisfies the constraint
	}
	vid := VersionedPluginIdentity{
		PluginIdentity: id,
		Version:        resolved.String(),
	}
	entry, ok := vp.store.Get(vid)
	if !ok {
		return nil
	}
	return entry
}

func (vp *VersionPool) GetByConstraint(id PluginIdentity, con *semver.Constraints) *poolEntry {
	resolved := vp.index.GetConstraints(id, con)
	if resolved == nil {
		return nil // no indexed version satisfies the constraint
	}
	vid := VersionedPluginIdentity{
		PluginIdentity: id,
		Version:        resolved.String(),
	}
	entry, ok := vp.store.Get(vid)
	if !ok {
		return nil
	}
	return entry
}

// insert registers a placeholder entry under a resolved version in both the
// index and the store, keeping the cross-structure invariant (every store key
// has a matching indexed version and vice versa). It is the counterpart of
// delete and takes an already-parsed *semver.Version so it cannot fail. entry
// must carry the same version (entry.version) so later cleanup via delete can
// find it. Callers must hold vp.mu.
func (vp *VersionPool) insert(id PluginIdentity, sv *semver.Version, entry *poolEntry) {
	vp.index.AddVersion(id, sv)
	vp.store.Set(VersionedPluginIdentity{PluginIdentity: id, Version: sv.String()}, entry)
}

// delete removes a resolved version from both the index and the store, keeping
// the cross-structure invariant. It takes an already-parsed *semver.Version so
// it cannot fail: the version's string form is the canonical store key.
func (vp *VersionPool) delete(id PluginIdentity, sv *semver.Version) {
	vid := VersionedPluginIdentity{
		PluginIdentity: id,
		Version:        sv.String(),
	}
	vp.index.DeleteVersion(id, sv)
	vp.store.Delete(vid)
}

func (vp *VersionPool) waitForEntry(ctx context.Context, id PluginIdentity, entry *poolEntry, acquire bool) (*poolEntry, bool, error) {
	acquiredEntry, acquired, err := vp.waitAndValidate(ctx, entry, acquire)
	if err != nil {
		vp.mu.Lock()
		entry.mu.Lock()
		dead := entry.state == entryDead
		refs := atomic.LoadInt32(&entry.refCount)
		entry.mu.Unlock()
		// Only remove entries that genuinely failed and are unreferenced.
		// Referenced dead entries stay in the map so their final Release can
		// finalize the deferred teardown.
		if dead && refs == 0 {
			vp.delete(id, entry.version)
			vp.mu.Unlock()
			vp.teardownEntry(entry)
		} else {
			vp.mu.Unlock()
		}
	}
	return acquiredEntry, acquired, err
}

// waitAndValidate blocks until entry is ready (or ctx is cancelled), then
// validates its health. When acquire is true and the entry is ready, a
// reference is taken under the entry lock (atomic with the readiness check) and
// the entry handle is returned so the caller can later release it directly. On
// the non-acquire or error paths the returned entry is nil.
func (vp *VersionPool) waitAndValidate(ctx context.Context, entry *poolEntry, acquire bool) (*poolEntry, bool, error) {
	select {
	case <-entry.ready:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.state == entryDead {
		return nil, false, fmt.Errorf("plugin %q is dead: %w", entry.dep.Name, entry.err)
	}

	entry.lastUsed = vp.opts.clock()
	if acquire {
		atomic.AddInt32(&entry.refCount, 1)
		return entry, true, nil
	}
	return nil, false, nil
}

func (vp *VersionPool) teardownEntry(entry *poolEntry) {
	entry.mu.Lock()
	registered := entry.registeredProviders
	entry.registeredProviders = nil
	client := entry.client
	if client == nil {
		entry.mu.Unlock()
		vp.unregisterProviders(registered)
		return
	}
	if atomic.LoadInt32(&entry.refCount) > 0 {
		entry.pendingKill = true
		entry.mu.Unlock()
		vp.unregisterProviders(registered)
		return
	}
	entry.client = nil
	entry.mu.Unlock()
	vp.unregisterProviders(registered)
	client.Kill()
}

func (vp *VersionPool) unregisterProviders(providers []string) {
	if vp.registry == nil {
		return
	}
	for _, p := range providers {
		vp.registry.Unregister(p)
	}
}

// Adopt registers an already-running plugin client into the pool so that its
// lifecycle (idle eviction, shutdown) is managed by the pool. This is used
// for official providers pre-loaded at startup.
func (vp *VersionPool) Adopt(
	id PluginIdentity,
	version *semver.Version,
	client *Client,
	dep solution.PluginDependency,
	registeredProviders []string,
) {
	if vp.closed.Load() {
		return
	}
	ready := make(chan struct{})
	close(ready) // already ready — no spawn to wait on
	entry := &poolEntry{
		client:              client,
		state:               entryReady,
		lastUsed:            vp.opts.clock(),
		dep:                 dep,
		version:             version,
		registeredProviders: registeredProviders,
		ready:               ready,
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()

	vp.insert(id, version, entry)
}

// Stats returns a point-in-time snapshot of pool metrics. It walks the index
// (the authoritative key set) and resolves each version through the store,
// classifying every entry by state. Starting entries count as Active (a spawn
// is in progress); ready entries are Active when referenced and Idle otherwise.
func (vp *VersionPool) Stats() PoolStats {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	var stats PoolStats
	stats.Total = vp.store.Len()
	stats.Evicted = vp.evicted

	for id, versions := range vp.index.entries {
		for _, sv := range versions {
			entry, ok := vp.store.Get(VersionedPluginIdentity{PluginIdentity: id, Version: sv.String()})
			if !ok {
				continue // invariant guards this; skip defensively
			}
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
	}
	return stats
}

// Shutdown kills all managed plugin processes and marks the pool closed so no
// further loads succeed. It is idempotent: the stop signal and context cancel
// fire once, and a second call is a no-op teardown over an already-empty pool.
// Called once on server stop.
func (vp *VersionPool) Shutdown() {
	vp.stopOnce.Do(func() {
		close(vp.stop)
		if vp.cancel != nil {
			vp.cancel()
		}
	})

	vp.closed.Store(true)

	// Snapshot every entry, then clear the index and store, so the kills below
	// run without holding vp.mu (client.Kill can block on process teardown).
	vp.mu.Lock()
	var entries []*poolEntry
	for id, versions := range vp.index.entries {
		for _, sv := range versions {
			if entry, ok := vp.store.Get(VersionedPluginIdentity{PluginIdentity: id, Version: sv.String()}); ok {
				entries = append(entries, entry)
			}
		}
	}
	vp.index = *NewVersionIndex[PluginIdentity]()
	vp.store = store.New[VersionedPluginIdentity, *poolEntry]()
	vp.mu.Unlock()

	for _, entry := range entries {
		entry.mu.Lock()
		registered := entry.registeredProviders
		entry.registeredProviders = nil
		client := entry.client
		entry.client = nil
		entry.mu.Unlock()
		// Unregister providers before killing so the shared registry is not
		// left holding dead-backed wrappers if it outlives the pool.
		vp.unregisterProviders(registered)
		if client != nil {
			client.Kill()
		}
	}
}
