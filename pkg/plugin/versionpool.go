package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/store"
	"github.com/oakwood-commons/scafctl/pkg/versionindex"
)

var ErrUnresolvedDependency = errors.New("dependency has no catalog; must be resolved to a concrete catalog before reaching the pool")

type (
	pluginIdentity        = identity.PluginIdentity
	pluginVersionIdentity = identity.VersionedPluginIdentity
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
	ResolvePlugins(ctx context.Context, plugins []solution.PluginDependency) ([]catalog.ArtifactInfo, error)
}

// registrableProvider is a provider that can be registered in a provider
// registry and configured with host-side settings. *ProviderWrapper satisfies
// it.
type registrableProvider interface {
	provider.Provider
	Configure(ctx context.Context, cfg ProviderConfig) error
}

// providerRegistry is the subset of *provider.ProviderRegistry the pool uses to
// publish and retract external plugin providers. The pool only ever touches the
// external tier: built-ins are filtered out upstream (Fetcher.FetchPlugins) and
// never enter the pool. Every provider the pool registers is keyed by its full
// {catalog, name, version} identity, so the seam is identity-aware. Depending on
// the interface (rather than the concrete registry) lets tests assert
// registration without the global registry's side effects. *provider.ProviderRegistry
// satisfies this interface directly.
type providerRegistry interface {
	RegisterExternal(p provider.Provider, opts ...provider.VersionedRegistryOptionFunc) error
	HasExternal(name string, opts ...provider.VersionedRegistryOptionFunc) bool
	UnregisterExternal(name string, v *semver.Version, opts ...provider.VersionedRegistryOptionFunc) error
}

// The production external tier of *provider.ProviderRegistry satisfies the seam
// directly — no adapter required.
var _ providerRegistry = (*provider.CompositeRegistry)(nil)

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
	Version index invariants (see pkg/plugin/versionindex):
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
// It does not special-case "" or "latest"; callers must route those via
// isLatestConstraint before reaching here, since both are reported as
// constraint-parse errors. An unparseable string yields an error with both
// pointers nil.
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

type options struct {
	clock           func() time.Time
	spawnTimeout    time.Duration
	idleTimeout     time.Duration
	maxPlugins      int
	logger          logr.Logger
	newClient       clientFactory
	newWrapper      wrapperFactory
	disableExternal bool
	allowedPlugins  map[string]catalog.PluginPolicy
	clientOpts      []ClientOption // extra options for spawned plugin clients
	sanitizeEnv     bool           // prepend WithSanitizedEnv() on spawn
	baseConfig      ProviderConfig // host-static config delivered to each pooled provider at load
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

// WithVersionPoolAllowedPlugins sets a catalog-aware per-catalog plugin
// allowlist enforced as a fast-reject before fetch/spawn. The map keys are
// catalog names (matched case-insensitively) and the values describe what each
// catalog may serve. A nil/empty map leaves the gate open (every plugin allowed
// from every catalog); when set, a catalog absent from the map is deny-all, a
// catalog with AllowAll is unrestricted, and otherwise the plugin name must
// appear in the catalog's explicit list — the same semantics enforced by the
// catalog chain and the fetcher's catalog-index gate (catalog.CheckPluginPolicy).
func WithVersionPoolAllowedPlugins(allowed map[string]catalog.PluginPolicy) Option {
	return func(o *options) {
		if len(allowed) == 0 {
			o.allowedPlugins = nil
			return
		}
		// Store lowercased keys so CheckPluginPolicy's case-insensitive lookup
		// matches regardless of how callers cased their catalog names.
		normalized := make(map[string]catalog.PluginPolicy, len(allowed))
		for name, policy := range allowed {
			normalized[strings.ToLower(name)] = policy
		}
		o.allowedPlugins = normalized
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

// WithVersionPoolBaseProviderConfig sets the host-static ProviderConfig
// delivered to every pooled provider via the one-time Configure call at load.
// Use it to carry host runtime metadata (build info, entrypoint, command, args)
// and the binary name into long-lived pool-mode hosts so plugins that read
// ProviderConfig.Settings behave the same as under one-shot per-call hosts.
//
// The config must contain only host-static data (constant for the pool's
// lifetime). Per-solution values vary per request and are delivered on the
// per-execution path instead, never re-configured on the shared wrapper.
//
// The cfg.Settings map is cloned so the long-lived pool never shares the
// caller's map reference. It mirrors WithBaseProviderConfig (a PoolOption) but
// returns an Option for NewVersionPool.
func WithVersionPoolBaseProviderConfig(cfg ProviderConfig) Option {
	return func(o *options) {
		o.baseConfig = cloneProviderConfig(cfg)
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
	index    versionindex.Index[pluginIdentity]
	store    store.Storer[pluginVersionIdentity, *poolEntry]
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
	poolctx, cancel := context.WithCancel(cx) //nolint:gosec // G118: cancel is stored in the struct and called in Shutdown()
	vp := &VersionPool{
		index:    *versionindex.New[pluginIdentity](),
		store:    store.New[pluginVersionIdentity, *poolEntry](),
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
	id := identity.DependencyToPluginIdentity(dep)
	if entry := vp.GetByVersion(id, version); entry != nil {
		vp.mu.Unlock()
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
		id:      id,
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
	id := identity.DependencyToPluginIdentity(dep)

	// Fast path: a version already in the index satisfies the constraint.
	vp.mu.Lock()
	if entry := vp.GetByConstraint(id, con); entry != nil {
		vp.mu.Unlock()
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
	// version is unknown until the catalog resolves it. Fetch to discover it,
	// then commit an entry for whatever concrete version came back.
	return vp.fetchResolveCommit(ctx, dep, id, acquire)
}

// ensureOneByLatest handles the "latest" sentinel (and the empty version
// string). Unlike a concrete version or a range, "latest" cannot be matched
// against the index without first learning which concrete version it currently
// points at, so this path ALWAYS resolves it via the fetcher (which caches the
// resolution) and only then decides whether the pool already has that version.
//
// The flow is: resolve -> reuse the pooled entry for the resolved version if we
// already have it (cheap, no download) -> otherwise fetch the binary and spawn.
// checkCapacity is only consulted on the add-a-new-entry branch: an already
// pooled version is net-zero, so a full pool must still be able to reuse it.
func (vp *VersionPool) ensureOneByLatest(ctx context.Context, dep solution.PluginDependency, acquire bool) (*poolEntry, bool, error) {
	id := identity.DependencyToPluginIdentity(dep)
	if vp.fetcher == nil {
		return nil, false, fmt.Errorf("plugin fetcher not available")
	}

	// Always resolve the sentinel to a concrete version. The fetcher caches this
	// resolution, and scoping it to dep's catalog keeps it consistent with the
	// fetch below (a chain fallback would risk resolving against a different
	// catalog than the one we later download from).
	infos, err := vp.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{dep})
	if err != nil {
		return nil, false, fmt.Errorf("resolving latest for plugin %q: %w", dep.DisplayName(), err)
	}
	if len(infos) == 0 {
		return nil, false, fmt.Errorf("resolving latest for plugin %q: no results returned", dep.DisplayName())
	}
	info := infos[0]
	version := info.Reference.Version
	if version == nil {
		return nil, false, fmt.Errorf("resolving latest for plugin %q: resolved reference has no version", dep.DisplayName())
	}

	// Fast path: we already hold the resolved version. Reuse it without
	// downloading, even when the pool is at capacity (reuse is net-zero).
	vp.mu.Lock()
	if entry := vp.GetByVersion(id, version); entry != nil {
		vp.mu.Unlock()
		return vp.waitForEntry(ctx, id, entry, acquire)
	}
	// Miss: committing the resolved version adds a new entry, so enforce
	// capacity early (before the download). fetchResolveCommit re-checks
	// authoritatively under the lock.
	if err := vp.checkCapacity(dep); err != nil {
		vp.mu.Unlock()
		return nil, false, err
	}
	vp.mu.Unlock()

	// Not pooled: fetch the binary and spawn. fetchResolveCommit keys the entry
	// off the version the fetch resolves, so a between-calls upstream move is
	// committed consistently (and its own re-check still catches a concurrent
	// load of the same version).
	return vp.fetchResolveCommit(ctx, dep, id, acquire)
}

// fetchResolveCommit is the shared slow path for the constraint and latest
// routes: it fetches dep's binary (which resolves the concrete version), then
// commits a new pool entry for that version and spawns it. The fetch runs
// OUTSIDE vp.mu so pool-wide operations are not blocked on network I/O. A
// concurrent caller may have resolved the same concrete version while we
// fetched, so it re-checks under the lock and releases the redundant fetch pin
// if the version is already pooled.
func (vp *VersionPool) fetchResolveCommit(ctx context.Context, dep solution.PluginDependency, id pluginIdentity, acquire bool) (*poolEntry, bool, error) {
	if vp.fetcher == nil {
		return nil, false, fmt.Errorf("plugin fetcher not available")
	}
	results, err := vp.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	if err != nil {
		releaseResults(results)
		return nil, false, fmt.Errorf("fetching plugin %q: %w", dep.DisplayName(), err)
	}
	if len(results) == 0 {
		return nil, false, fmt.Errorf("no fetch results for plugin %q", dep.DisplayName())
	}
	result := results[0]

	version, err := semver.NewVersion(result.Version)
	if err != nil {
		releaseResults(results)
		return nil, false, fmt.Errorf("parsing resolved version %q for plugin %q: %w", result.Version, dep.DisplayName(), err)
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
		id:      id,
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
		"plugin", entry.id.Name(), "version", result.Version)
	vp.startAndRegister(ctx, entry, result)
}

// spawnWithEntry fetches the binary for dep (version path, where the concrete
// version is already known), releases the fetch batch, then delegates to
// startAndRegister.
func (vp *VersionPool) spawnWithEntry(ctx context.Context, cancel context.CancelFunc, entry *poolEntry, dep solution.PluginDependency) {
	defer cancel()
	defer close(entry.ready)

	vp.logger.V(1).Info("spawning plugin", "plugin", dep.DisplayName(), "version", entry.version)
	if vp.fetcher == nil {
		err := fmt.Errorf("plugin fetcher not available")
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.DisplayName(), "error", err)
		return
	}
	results, err := vp.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	defer releaseResults(results)
	if err != nil {
		err = fmt.Errorf("fetching: %w", err)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.DisplayName(), "error", err)
		return
	}
	if len(results) == 0 {
		err = fmt.Errorf("no fetch results returned for plugin %q", dep.DisplayName())
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", dep.DisplayName(), "error", err)
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
		vp.logger.V(1).Info("plugin spawn failed", "plugin", entry.id.Name(), "error", err)
		return
	}

	providers, err := client.GetProviders(ctx)
	if err != nil {
		client.Kill()
		err = fmt.Errorf("getting providers: %w", err)
		entry.failWith(err)
		vp.logger.V(1).Info("plugin spawn failed", "plugin", entry.id.Name(), "error", err)
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
		"plugin", entry.id.Name(), "version", result.Version, "providers", providers)
}

// registerProviders wraps, registers, and configures each provider exposed by a
// freshly started client, returning the names it successfully registered. It
// performs no process I/O, so it is unit-testable through the wrapperFactory and
// providerRegistry seams. A provider is skipped (not registered) when no
// registry is configured, when the same {catalog, name, version} identity is
// already registered, or when wrapping, registering, or configuring it fails.
// The entry's catalog and resolved version stamp every registration so the
// provider is keyed by its full external identity.
func (vp *VersionPool) registerProviders(ctx context.Context, entry *poolEntry, client pluginClient, providers []string) []string {
	idOpts := []provider.VersionedRegistryOptionFunc{
		provider.WithCatalogName(entry.id.Catalog()),
		provider.WithRegistrationVersion(entry.version),
	}
	var registered []string
	for _, provName := range providers {
		if vp.registry == nil || vp.registry.HasExternal(provName, idOpts...) {
			continue // no registry, or this exact identity is already registered
		}
		wrapper, wErr := vp.opts.newWrapper(client, provName, WithContext(ctx))
		if wErr != nil {
			vp.logger.V(1).Info("failed to create provider wrapper",
				"plugin", entry.id.Name(), "provider", provName, "error", wErr)
			continue
		}
		if rErr := vp.registry.RegisterExternal(wrapper, idOpts...); rErr != nil {
			vp.logger.V(1).Info("failed to register provider",
				"plugin", entry.id.Name(), "provider", provName, "error", rErr)
			continue
		}
		// Configure so the plugin receives the host service ID and can dial
		// back to the host for auth tokens or other host services. The base
		// config also carries host-static runtime metadata (build info,
		// entrypoint, command, args) so pooled plugins that read
		// ProviderConfig.Settings match per-call hosts; it is empty by default.
		// It was cloned once at option-set time, so it is safe to share.
		if cErr := wrapper.Configure(ctx, vp.opts.baseConfig); cErr != nil {
			vp.logger.V(1).Info("failed to configure provider wrapper",
				"plugin", entry.id.Name(), "provider", provName, "error", cErr)
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

	// "latest" (and the empty string) is not a valid version or constraint, so
	// it must be routed before parseVersionOrConstraint, which would reject it.
	if isLatestConstraint(dep.Version) {
		return vp.ensureOneByLatest(ctx, dep, acquire)
	}

	v, c, err := parseVersionOrConstraint(dep.Version)
	if err != nil {
		return nil, false, fmt.Errorf("parsing version %q for plugin %q: %w", dep.Version, dep.DisplayName(), err)
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
//
// Precondition: every provider dependency must already be resolved to a
// concrete catalog (dep.Catalog != ""). Callers bind short names via ResolveDeps
// first; an unresolved dependency fails with ErrUnresolvedDependency.
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
//
// Precondition: every provider dependency must already be resolved to a
// concrete catalog (dep.Catalog != ""). Callers bind short names via ResolveDeps
// first; an unresolved dependency fails with ErrUnresolvedDependency.
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

func (vp *VersionPool) validateEnsure(dep pluginArtifact) error {
	// Precondition: every dependency reaching the pool must already name a
	// catalog. Short-name (unqualified) deps are bound to a concrete catalog
	// so an empty catalog here is a caller contract violation.
	if dep.CatalogName() == "" {
		return fmt.Errorf("plugin %q: %w", dep.ArtifactName(), ErrUnresolvedDependency)
	}
	if vp.opts.disableExternal {
		return fmt.Errorf("plugin %q: %w", dep.ArtifactName(), ErrExternalDisabled)
	}

	// Security: reject if a catalog-aware allowlist is configured and this
	// plugin is not permitted from its (already-resolved) catalog.
	if err := catalog.CheckPluginPolicy(vp.opts.allowedPlugins, dep.CatalogName(), dep.ArtifactName()); err != nil {
		return fmt.Errorf("plugin %q: %w: %w", dep.ArtifactName(), ErrPluginNotAllowed, err)
	}
	return nil
}

// checkCapacity reports ErrPoolFull when loading a new plugin version would
// exceed the configured maxPlugins bound. A zero bound means unbounded. The
// store holds one entry per loaded version, so its length is the live count.
// Callers must hold vp.mu.
func (vp *VersionPool) checkCapacity(dep solution.PluginDependency) error {
	if vp.opts.maxPlugins > 0 && vp.store.Len() >= vp.opts.maxPlugins {
		return fmt.Errorf("plugin %q: %w", dep.DisplayName(), ErrPoolFull)
	}
	return nil
}

// evictCandidate pairs an entry with the composite key needed to remove it from
// both the index and the store during eviction.
type evictCandidate struct {
	id      pluginIdentity
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
	for id, versions := range vp.index.All() {
		for _, sv := range versions {
			entry, ok := vp.store.Get(identity.IdentityWithVersion(id, sv.String()))
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
			"plugin", c.id.Name(), "version", c.version)
	}
}

func (vp *VersionPool) GetByVersion(id pluginIdentity, v *semver.Version) *poolEntry {
	resolved := vp.index.GetVersion(id, v)
	if resolved == nil {
		return nil // no indexed version satisfies the constraint
	}
	vid := identity.IdentityWithVersion(id, v.String())
	entry, ok := vp.store.Get(vid)
	if !ok {
		return nil
	}
	return entry
}

func (vp *VersionPool) GetByConstraint(id pluginIdentity, con *semver.Constraints) *poolEntry {
	resolved := vp.index.GetConstraints(id, con)
	if resolved == nil {
		return nil // no indexed version satisfies the constraint
	}
	vid := identity.IdentityWithVersion(id, resolved.String())
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
func (vp *VersionPool) insert(id pluginIdentity, sv *semver.Version, entry *poolEntry) {
	vp.index.AddVersion(id, sv)
	vp.store.Set(identity.IdentityWithVersion(id, sv.String()), entry)
}

// delete removes a resolved version from both the index and the store, keeping
// the cross-structure invariant. It takes an already-parsed *semver.Version so
// it cannot fail: the version's string form is the canonical store key.
func (vp *VersionPool) delete(id pluginIdentity, sv *semver.Version) {
	vid := identity.IdentityWithVersion(id, sv.String())
	vp.index.DeleteVersion(id, sv)
	vp.store.Delete(vid)
}

func (vp *VersionPool) waitForEntry(ctx context.Context, id pluginIdentity, entry *poolEntry, acquire bool) (*poolEntry, bool, error) {
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
		return nil, false, fmt.Errorf("plugin %q is dead: %w", entry.id.Name(), entry.err)
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
		vp.unregisterProviders(entry, registered)
		return
	}
	if atomic.LoadInt32(&entry.refCount) > 0 {
		entry.pendingKill = true
		entry.mu.Unlock()
		vp.unregisterProviders(entry, registered)
		return
	}
	entry.client = nil
	entry.mu.Unlock()
	vp.unregisterProviders(entry, registered)
	client.Kill()
}

// unregisterProviders retracts the given provider names from the shared
// registry using the entry's full external identity (catalog + resolved
// version), so only the exact {catalog, name, version} this entry registered is
// removed — never a same-named provider owned by another plugin or catalog.
func (vp *VersionPool) unregisterProviders(entry *poolEntry, providers []string) {
	if vp.registry == nil {
		return
	}
	for _, p := range providers {
		_ = vp.registry.UnregisterExternal(p, entry.version,
			provider.WithCatalogName(entry.id.Catalog()))
	}
}

// Adopt registers an already-running plugin client into the pool so that its
// lifecycle (idle eviction, shutdown) is managed by the pool. This is used
// for official providers pre-loaded at startup.
func (vp *VersionPool) Adopt(
	id pluginIdentity,
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
		state:               entryReady,
		lastUsed:            vp.opts.clock(),
		dep:                 dep,
		id:                  id,
		version:             version,
		registeredProviders: registeredProviders,
		ready:               ready,
		client:              client,
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

	for id, versions := range vp.index.All() {
		for _, sv := range versions {
			entry, ok := vp.store.Get(identity.IdentityWithVersion(id, sv.String()))
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

// SanitizeEnv reports whether this pool sanitizes the environment for spawned
// plugin clients. See [WithVersionPoolSanitizeEnv].
func (vp *VersionPool) SanitizeEnv() bool {
	return vp.opts.sanitizeEnv
}

// ClientOptsLen returns the number of extra client options configured on the
// pool. It is primarily useful for testing that WithVersionPoolClientOptions
// was wired correctly.
func (vp *VersionPool) ClientOptsLen() int {
	return len(vp.opts.clientOpts)
}

// BaseProviderConfig returns the host-static ProviderConfig delivered to each
// pooled provider at load time. See [WithVersionPoolBaseProviderConfig]. It is
// primarily useful for testing that the base config was wired correctly. The
// returned Settings map is a deep clone (map and value bytes), so callers cannot
// mutate the pool's stored config.
func (vp *VersionPool) BaseProviderConfig() ProviderConfig {
	return cloneProviderConfig(vp.opts.baseConfig)
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
	for id, versions := range vp.index.All() {
		for _, sv := range versions {
			if entry, ok := vp.store.Get(identity.IdentityWithVersion(id, sv.String())); ok {
				entries = append(entries, entry)
			}
		}
	}
	vp.index = *versionindex.New[pluginIdentity]()
	vp.store = store.New[pluginVersionIdentity, *poolEntry]()
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
		vp.unregisterProviders(entry, registered)
		if client != nil {
			client.Kill()
		}
	}
}
