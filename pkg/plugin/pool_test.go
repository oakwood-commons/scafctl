// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPool(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard())
	defer p.Shutdown()

	assert.NotNil(t, p)
	assert.False(t, p.closed)
	assert.Empty(t, p.entries)
}

func TestNewPool_Options(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(10*time.Minute),
		WithMaxPlugins(100),
		WithHealthCheckInterval(time.Minute),
	)
	defer p.Shutdown()

	assert.Equal(t, 10*time.Minute, p.opts.idleTimeout)
	assert.Equal(t, 100, p.opts.maxPlugins)
	assert.Equal(t, time.Minute, p.opts.healthInterval)
}

func TestNewPool_WithClientOptions(t *testing.T) {
	reg := provider.NewRegistry()
	opt1 := WithSanitizedEnv()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithClientOptions(opt1),
	)
	defer p.Shutdown()

	assert.Len(t, p.opts.clientOpts, 1)
}

func TestNewPool_WithClientOptions_Multiple(t *testing.T) {
	reg := provider.NewRegistry()
	opt1 := WithSanitizedEnv()
	opt2 := WithSanitizedEnv()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithClientOptions(opt1, opt2),
	)
	defer p.Shutdown()

	assert.Len(t, p.opts.clientOpts, 2)
}

func TestNewPool_WithSanitizeEnv(t *testing.T) {
	reg := provider.NewRegistry()

	t.Run("defaults to true", func(t *testing.T) {
		p := NewPool(nil, reg, logr.Discard())
		defer p.Shutdown()
		assert.True(t, p.SanitizeEnv())
	})

	t.Run("can be disabled", func(t *testing.T) {
		p := NewPool(nil, reg, logr.Discard(), WithSanitizeEnv(false))
		defer p.Shutdown()
		assert.False(t, p.SanitizeEnv())
	})

	t.Run("can be explicitly enabled", func(t *testing.T) {
		p := NewPool(nil, reg, logr.Discard(), WithSanitizeEnv(true))
		defer p.Shutdown()
		assert.True(t, p.SanitizeEnv())
	})
}

func TestPool_Adopt(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "test-plugin", path: "/fake/path"}
	dep := solution.PluginDependency{Name: "test-plugin", Kind: solution.PluginKindProvider}

	p.Adopt("test-plugin", mockClient, dep, nil)

	p.mu.Lock()
	entry, ok := p.entries["test-plugin"]
	p.mu.Unlock()

	require.True(t, ok)
	assert.Equal(t, entryReady, entry.state)
	assert.Equal(t, mockClient, entry.client)
}

func TestPool_Adopt_ClosedPool(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	p.Shutdown()

	mockClient := &Client{name: "test-plugin", path: "/fake/path"}
	dep := solution.PluginDependency{Name: "test-plugin", Kind: solution.PluginKindProvider}

	p.Adopt("test-plugin", mockClient, dep, nil)

	p.mu.Lock()
	_, ok := p.entries["test-plugin"]
	p.mu.Unlock()
	assert.False(t, ok)
}

func TestPool_Ensure_AlreadyRegistered(t *testing.T) {
	reg := provider.NewRegistry()
	reg.MarkKnown("existing-provider")

	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "existing-provider", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	assert.NoError(t, err)

	// No entry created in pool
	p.mu.Lock()
	assert.Empty(t, p.entries)
	p.mu.Unlock()
}

func TestPool_Ensure_ClosedPool(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "some-plugin", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	assert.ErrorIs(t, err, ErrPoolClosed)
}

func TestPool_Ensure_PoolFull(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0), WithMaxPlugins(1))
	defer p.Shutdown()

	// Adopt one plugin to fill the pool
	mockClient := &Client{name: "first", path: "/fake"}
	p.Adopt("first", mockClient, solution.PluginDependency{Name: "first", Kind: solution.PluginKindProvider}, nil)

	// Try to ensure a second plugin — should fail
	deps := []solution.PluginDependency{
		{Name: "second", Kind: solution.PluginKindProvider},
	}
	err := p.Ensure(context.Background(), deps)
	assert.ErrorIs(t, err, ErrPoolFull)
}

func TestPool_Ensure_NilFetcher(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "needs-fetch", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetcher not available")
}

func TestPool_Ensure_SkipsNonProvider(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "auth-thing", Kind: solution.PluginKindAuthHandler},
	}

	err := p.Ensure(context.Background(), deps)
	assert.NoError(t, err)

	p.mu.Lock()
	assert.Empty(t, p.entries)
	p.mu.Unlock()
}

func TestPool_Ensure_ConcurrentSamePlugin(t *testing.T) {
	reg := provider.NewRegistry()
	// Pre-adopt a plugin so Ensure is a no-op wait
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "shared", path: "/fake"}
	dep := solution.PluginDependency{Name: "shared", Kind: solution.PluginKindProvider}
	p.Adopt("shared", mockClient, dep, nil)

	// Multiple goroutines Ensure the same plugin — all should succeed
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = p.Ensure(context.Background(), []solution.PluginDependency{dep})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d", i)
	}
}

func TestPool_Ensure_ContextCancelled(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Create an entry stuck in entryStarting state
	ready := make(chan struct{}) // never closed
	entry := &poolEntry{
		state: entryStarting,
		dep:   solution.PluginDependency{Name: "stuck", Kind: solution.PluginKindProvider},
		ready: ready,
	}
	p.mu.Lock()
	p.entries["stuck"] = entry
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	err := p.Ensure(ctx, []solution.PluginDependency{
		{Name: "stuck", Kind: solution.PluginKindProvider},
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPool_AcquireRelease(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "acq", path: "/fake"}
	dep := solution.PluginDependency{Name: "acq", Kind: solution.PluginKindProvider}
	p.Adopt("acq", mockClient, dep, nil)

	ok := p.Acquire("acq")
	assert.True(t, ok)

	p.mu.Lock()
	entry := p.entries["acq"]
	p.mu.Unlock()
	assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))

	p.Release("acq")
	assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
}

func TestPool_Acquire_NotFound(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	ok := p.Acquire("nonexistent")
	assert.False(t, ok)
}

func TestPool_Acquire_Dead(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	ready := make(chan struct{})
	close(ready)
	entry := &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "dead", Kind: solution.PluginKindProvider},
		ready: ready,
	}
	p.mu.Lock()
	p.entries["dead"] = entry
	p.mu.Unlock()

	ok := p.Acquire("dead")
	assert.False(t, ok)
}

func TestPool_Release_NotFound(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Should not panic
	p.Release("nonexistent")
}

func TestPool_markDead_AlreadyDead(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	entry := &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "already-dead", Kind: solution.PluginKindProvider},
		ready: make(chan struct{}),
	}
	close(entry.ready)

	// Should be a no-op — state remains dead, no panic.
	p.markDead("already-dead", entry, errors.New("test"))
	assert.Equal(t, entryDead, entry.state)
}

func TestPool_markDead_ReadyNoClient(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	entry := &poolEntry{
		state:  entryReady,
		client: nil,
		dep:    solution.PluginDependency{Name: "no-client", Kind: solution.PluginKindProvider},
		ready:  make(chan struct{}),
	}
	close(entry.ready)

	reason := errors.New("connection lost")
	p.markDead("no-client", entry, reason)

	entry.mu.Lock()
	defer entry.mu.Unlock()
	assert.Equal(t, entryDead, entry.state)
	assert.Equal(t, reason, entry.err)
}

func TestPool_markDead_ReadyWithClient(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Use a client with nil pluginClient and nil plugin — Kill is a no-op
	client := &Client{name: "kill-me", path: "/fake"}
	entry := &poolEntry{
		state:  entryReady,
		client: client,
		dep:    solution.PluginDependency{Name: "kill-me", Kind: solution.PluginKindProvider},
		ready:  make(chan struct{}),
	}
	close(entry.ready)

	reason := errors.New("health check failed")
	p.markDead("kill-me", entry, reason)

	entry.mu.Lock()
	defer entry.mu.Unlock()
	assert.Equal(t, entryDead, entry.state)
	assert.Equal(t, reason, entry.err)
}

func TestPoolEntry_failWith(t *testing.T) {
	entry := &poolEntry{
		state: entryStarting,
		ready: make(chan struct{}),
	}

	testErr := errors.New("spawn failed")
	entry.failWith(testErr)

	entry.mu.Lock()
	defer entry.mu.Unlock()
	assert.Equal(t, entryDead, entry.state)
	assert.Equal(t, testErr, entry.err)
}

func TestPool_Ping_NotFound(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	ok := p.Ping(context.Background(), "nonexistent")
	assert.False(t, ok)
}

func TestPool_Ping_Dead(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	ready := make(chan struct{})
	close(ready)
	entry := &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "dead-ping", Kind: solution.PluginKindProvider},
		ready: ready,
	}
	p.mu.Lock()
	p.entries["dead-ping"] = entry
	p.mu.Unlock()

	ok := p.Ping(context.Background(), "dead-ping")
	assert.False(t, ok)
}

func TestPool_Stats(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Empty pool
	stats := p.Stats()
	assert.Equal(t, 0, stats.Total)

	// Add an idle entry
	ready1 := make(chan struct{})
	close(ready1)
	p.mu.Lock()
	p.entries["idle1"] = &poolEntry{
		state:  entryReady,
		client: &Client{name: "idle1", path: "/fake"},
		dep:    solution.PluginDependency{Name: "idle1", Kind: solution.PluginKindProvider},
		ready:  ready1,
	}
	p.mu.Unlock()

	// Add an active entry
	ready2 := make(chan struct{})
	close(ready2)
	p.mu.Lock()
	p.entries["active1"] = &poolEntry{
		state:    entryReady,
		client:   &Client{name: "active1", path: "/fake"},
		dep:      solution.PluginDependency{Name: "active1", Kind: solution.PluginKindProvider},
		ready:    ready2,
		refCount: 2,
	}
	p.mu.Unlock()

	// Add a dead entry
	ready3 := make(chan struct{})
	close(ready3)
	p.mu.Lock()
	p.entries["dead1"] = &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "dead1", Kind: solution.PluginKindProvider},
		ready: ready3,
	}
	p.mu.Unlock()

	stats = p.Stats()
	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 1, stats.Idle)
	assert.Equal(t, 1, stats.Active)
	assert.Equal(t, 1, stats.Dead)
	assert.Equal(t, 0, stats.Evicted)
}

func TestPool_Evict_IdleEntries(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(5*time.Minute),
		withClock(clock),
	)
	defer p.Shutdown()

	// Add an idle entry with old lastUsed
	ready := make(chan struct{})
	close(ready)
	p.mu.Lock()
	p.entries["old"] = &poolEntry{
		state:    entryReady,
		client:   &Client{name: "old", path: "/fake"},
		dep:      solution.PluginDependency{Name: "old", Kind: solution.PluginKindProvider},
		ready:    ready,
		lastUsed: now.Add(-10 * time.Minute), // 10 min idle
	}
	p.mu.Unlock()

	// Run eviction
	p.evict()

	p.mu.Lock()
	_, exists := p.entries["old"]
	evicted := p.evicted
	p.mu.Unlock()
	assert.False(t, exists)
	assert.Equal(t, 1, evicted)
}

func TestPool_Evict_SkipsActiveEntries(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(5*time.Minute),
		withClock(clock),
	)
	defer p.Shutdown()

	// Add entry with refCount > 0 even though lastUsed is old
	ready := make(chan struct{})
	close(ready)
	p.mu.Lock()
	p.entries["active"] = &poolEntry{
		state:    entryReady,
		client:   &Client{name: "active", path: "/fake"},
		dep:      solution.PluginDependency{Name: "active", Kind: solution.PluginKindProvider},
		ready:    ready,
		lastUsed: now.Add(-10 * time.Minute),
		refCount: 1, // in use
	}
	p.mu.Unlock()

	p.evict()

	p.mu.Lock()
	_, exists := p.entries["active"]
	p.mu.Unlock()
	assert.True(t, exists, "active entry should not be evicted")
}

func TestPool_Evict_DeadEntries(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(5*time.Minute),
	)
	defer p.Shutdown()

	ready := make(chan struct{})
	close(ready)
	p.mu.Lock()
	p.entries["dead"] = &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "dead", Kind: solution.PluginKindProvider},
		ready: ready,
	}
	p.mu.Unlock()

	p.evict()

	p.mu.Lock()
	_, exists := p.entries["dead"]
	evicted := p.evicted
	p.mu.Unlock()
	assert.False(t, exists)
	assert.Equal(t, 1, evicted)
}

func TestPool_Shutdown(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))

	mockClient := &Client{name: "shutdown-test", path: "/fake"}
	dep := solution.PluginDependency{Name: "shutdown-test", Kind: solution.PluginKindProvider}
	p.Adopt("shutdown-test", mockClient, dep, nil)

	p.Shutdown()

	assert.True(t, p.closed)
	p.mu.Lock()
	assert.Empty(t, p.entries)
	p.mu.Unlock()
}

func TestPool_Shutdown_Idempotent(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))

	// Should not panic when called multiple times
	p.Shutdown()
	p.Shutdown()
	p.Shutdown()
}

func BenchmarkPool_Ensure_HotPath(b *testing.B) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "bench", path: "/fake"}
	dep := solution.PluginDependency{Name: "bench", Kind: solution.PluginKindProvider}
	p.Adopt("bench", mockClient, dep, nil)

	ctx := context.Background()
	deps := []solution.PluginDependency{dep}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = p.Ensure(ctx, deps)
	}
}

func BenchmarkPool_Stats(b *testing.B) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Populate with 20 entries
	for i := range 20 {
		name := "plugin-" + string(rune('a'+i))
		ready := make(chan struct{})
		close(ready)
		p.mu.Lock()
		p.entries[name] = &poolEntry{
			state:  entryReady,
			client: &Client{name: name, path: "/fake"},
			dep:    solution.PluginDependency{Name: name, Kind: solution.PluginKindProvider},
			ready:  ready,
		}
		p.mu.Unlock()
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = p.Stats()
	}
}

func BenchmarkPool_AcquireRelease(b *testing.B) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "bench-ar", path: "/fake"}
	dep := solution.PluginDependency{Name: "bench-ar", Kind: solution.PluginKindProvider}
	p.Adopt("bench-ar", mockClient, dep, nil)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		p.Acquire("bench-ar")
		p.Release("bench-ar")
	}
}

// --- Security mitigation tests ---

func TestPool_Ensure_ExternalDisabled(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithDisableExternal(true),
	)
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "malicious-plugin", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	assert.ErrorIs(t, err, ErrExternalDisabled)
}

func TestPool_Ensure_ExternalDisabled_AdoptedBypasses(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithDisableExternal(true),
	)
	defer p.Shutdown()

	// Pre-adopted plugins should still work
	mockClient := &Client{name: "official", path: "/fake"}
	dep := solution.PluginDependency{Name: "official", Kind: solution.PluginKindProvider}
	p.Adopt("official", mockClient, dep, nil)

	err := p.Ensure(context.Background(), []solution.PluginDependency{dep})
	assert.NoError(t, err)
}

func TestPool_Ensure_AllowedPlugins_Rejected(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithAllowedPlugins([]string{"safe-plugin", "another-safe"}),
	)
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "not-on-list", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	assert.ErrorIs(t, err, ErrPluginNotAllowed)
}

func TestPool_Ensure_AllowedPlugins_Permitted(t *testing.T) {
	reg := provider.NewRegistry()
	// Use nil fetcher — plugin is allowed but will fail at fetch stage
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithAllowedPlugins([]string{"safe-plugin"}),
	)
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "safe-plugin", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	// Fails at fetcher (nil), not at allowlist — proving it passed the check
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrPluginNotAllowed)
	assert.Contains(t, err.Error(), "fetcher not available")
}

func TestPool_Ensure_AllowedPlugins_Empty_AllowsAll(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithAllowedPlugins([]string{}), // empty = no restriction
	)
	defer p.Shutdown()

	deps := []solution.PluginDependency{
		{Name: "any-plugin", Kind: solution.PluginKindProvider},
	}

	err := p.Ensure(context.Background(), deps)
	// Should pass allowlist, fail at fetcher
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrPluginNotAllowed)
}

func TestPool_Ensure_AllowedPlugins_AdoptedBypasses(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithAllowedPlugins([]string{"only-this"}),
	)
	defer p.Shutdown()

	// Adopt a plugin NOT on the allowlist — should still work via Adopt
	mockClient := &Client{name: "not-on-list", path: "/fake"}
	dep := solution.PluginDependency{Name: "not-on-list", Kind: solution.PluginKindProvider}
	p.Adopt("not-on-list", mockClient, dep, nil)

	err := p.Ensure(context.Background(), []solution.PluginDependency{dep})
	assert.NoError(t, err)
}

func TestWithAllowedPlugins_Nil(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithAllowedPlugins(nil),
	)
	defer p.Shutdown()

	assert.Nil(t, p.opts.allowedPlugins)
}

func TestWithDisableExternal(t *testing.T) {
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(),
		WithIdleTimeout(0),
		WithDisableExternal(true),
	)
	defer p.Shutdown()

	assert.True(t, p.opts.disableExternal)
}

func TestPoolErrorHTTPStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"not allowed", fmt.Errorf("plugin %q: %w", "foo", ErrPluginNotAllowed), 403},
		{"external disabled", fmt.Errorf("plugin %q: %w", "bar", ErrExternalDisabled), 403},
		{"pool full", fmt.Errorf("plugin %q: %w", "baz", ErrPoolFull), 503},
		{"pool closed", ErrPoolClosed, 503},
		{"context canceled", fmt.Errorf("fetching: %w", context.Canceled), 499},
		{"deadline exceeded", fmt.Errorf("fetching: %w", context.DeadlineExceeded), 504},
		{"generic error", errors.New("some rpc failure"), 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, PoolErrorHTTPStatus(tt.err))
		})
	}
}

func TestPool_EnsureAndAcquire_Adopted(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	mockClient := &Client{name: "adopted-plugin", path: "/fake"}
	dep := solution.PluginDependency{Name: "adopted-plugin", Kind: solution.PluginKindProvider}
	p.Adopt("adopted-plugin", mockClient, dep, nil)

	release, err := p.EnsureAndAcquire(context.Background(), []solution.PluginDependency{dep})
	require.NoError(t, err)
	require.NotNil(t, release)

	// refCount should be incremented
	p.mu.Lock()
	entry := p.entries["adopted-plugin"]
	p.mu.Unlock()
	assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))

	// After release, refCount goes back to 0
	release()
	assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
}

func TestPool_EnsureAndAcquire_Error(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// No fetcher — Ensure will fail
	deps := []solution.PluginDependency{
		{Name: "unfetchable", Kind: solution.PluginKindProvider},
	}

	release, err := p.EnsureAndAcquire(context.Background(), deps)
	assert.Error(t, err)
	assert.Nil(t, release)
}

func TestPool_EnsureOne_RemovesDeadEntry(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry()
	p := NewPool(nil, reg, logr.Discard(), WithIdleTimeout(0))
	defer p.Shutdown()

	// Inject a dead entry directly
	ready := make(chan struct{})
	close(ready)
	entry := &poolEntry{
		state: entryDead,
		dep:   solution.PluginDependency{Name: "dead-plugin", Kind: solution.PluginKindProvider},
		err:   errors.New("process crashed"),
		ready: ready,
	}
	p.mu.Lock()
	p.entries["dead-plugin"] = entry
	p.mu.Unlock()

	// Ensure should fail (entry is dead) but also remove the dead entry
	err := p.Ensure(context.Background(), []solution.PluginDependency{
		{Name: "dead-plugin", Kind: solution.PluginKindProvider},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dead")

	// The dead entry should have been removed from the map
	p.mu.Lock()
	_, exists := p.entries["dead-plugin"]
	p.mu.Unlock()
	assert.False(t, exists, "dead entry should be removed so plugin can recover")
}

func TestPoolEntry_RegisteredProviders(t *testing.T) {
	t.Parallel()
	// Verify the registeredProviders field is correctly stored
	entry := &poolEntry{
		state:               entryReady,
		registeredProviders: []string{"exec", "git", "directory"},
	}
	assert.Equal(t, []string{"exec", "git", "directory"}, entry.registeredProviders)
}
