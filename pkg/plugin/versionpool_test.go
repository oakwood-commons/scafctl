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

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versions extracts the string form of the indexed versions for a key, in
// stored (descending) order, for concise assertions.
func versions[T comparable](t *testing.T, vi *VersionIndex[T], key T) []string {
	t.Helper()
	entry := vi.entries[key]
	out := make([]string, len(entry))
	for i, v := range entry {
		out[i] = v.String()
	}
	return out
}

// assertDescendingNoDup verifies the core invariant: each key's slice is sorted
// strictly descending with no duplicates.
func assertDescendingNoDup[T comparable](t *testing.T, vi *VersionIndex[T], key T) {
	t.Helper()
	entry := vi.entries[key]
	for i := 1; i < len(entry); i++ {
		assert.Truef(t, entry[i-1].GreaterThan(entry[i]),
			"not strictly descending at %d: %s !> %s", i, entry[i-1], entry[i])
	}
}

// mustConstraintT compiles a semver constraint or fails the test.
func mustConstraintT(t *testing.T, s string) *semver.Constraints {
	t.Helper()
	c, err := semver.NewConstraint(s)
	require.NoError(t, err)
	return c
}

func TestVersionIndex_DeleteVersion(t *testing.T) {
	t.Parallel()

	t.Run("removes an already-parsed version", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		for _, v := range []string{"1.0.0", "2.0.0", "3.0.0"} {
			vi.AddVersion("plugin", semver.MustParse(v))
		}
		vi.DeleteVersion("plugin", semver.MustParse("2.0.0"))
		assert.Equal(t, []string{"3.0.0", "1.0.0"}, versions(t, vi, "plugin"))
		assertDescendingNoDup(t, vi, "plugin")
	})

	t.Run("removing the last version deletes the key", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		vi.AddVersion("plugin", semver.MustParse("1.0.0"))
		vi.DeleteVersion("plugin", semver.MustParse("1.0.0"))
		_, ok := vi.entries["plugin"]
		assert.False(t, ok)
	})

	t.Run("absent version and absent key are no-ops", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		vi.AddVersion("plugin", semver.MustParse("1.0.0"))
		vi.DeleteVersion("plugin", semver.MustParse("9.9.9"))
		vi.DeleteVersion("missing", semver.MustParse("1.0.0"))
		assert.Equal(t, []string{"1.0.0"}, versions(t, vi, "plugin"))
	})
}

func TestVersionIndex_AddVersion(t *testing.T) {
	t.Parallel()

	t.Run("maintains descending order", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		for _, v := range []string{"1.0.0", "2.0.0", "1.5.0", "0.9.0", "2.1.0"} {
			vi.AddVersion("plugin", semver.MustParse(v))
		}
		assert.Equal(t, []string{"2.1.0", "2.0.0", "1.5.0", "1.0.0", "0.9.0"},
			versions(t, vi, "plugin"))
		assertDescendingNoDup(t, vi, "plugin")
	})

	t.Run("ignores duplicates", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		vi.AddVersion("plugin", semver.MustParse("1.2.3"))
		vi.AddVersion("plugin", semver.MustParse("1.2.3"))
		assert.Equal(t, []string{"1.2.3"}, versions(t, vi, "plugin"))
	})

	t.Run("keys are isolated", func(t *testing.T) {
		t.Parallel()
		vi := NewVersionIndex[string]()
		vi.AddVersion("a", semver.MustParse("1.0.0"))
		vi.AddVersion("b", semver.MustParse("2.0.0"))
		assert.Equal(t, []string{"1.0.0"}, versions(t, vi, "a"))
		assert.Equal(t, []string{"2.0.0"}, versions(t, vi, "b"))
	})
}

func TestVersionIndex_GetVersion(t *testing.T) {
	t.Parallel()

	vi := NewVersionIndex[string]()
	for _, v := range []string{"1.0.0", "1.2.3", "2.0.0"} {
		vi.AddVersion("plugin", semver.MustParse(v))
	}

	t.Run("returns the exact indexed version", func(t *testing.T) {
		t.Parallel()
		got := vi.GetVersion("plugin", semver.MustParse("1.2.3"))
		require.NotNil(t, got)
		assert.Equal(t, "1.2.3", got.String())
	})

	t.Run("absent version returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetVersion("plugin", semver.MustParse("1.2.4")))
	})

	t.Run("absent key returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetVersion("missing", semver.MustParse("1.0.0")))
	})

	t.Run("does not interpret ranges", func(t *testing.T) {
		t.Parallel()
		// GetVersion is exact-only: a prerelease equal-but-not-identical must
		// only match its exact form.
		exact := NewVersionIndex[string]()
		exact.AddVersion("plugin", semver.MustParse("1.2.3-rc1"))
		assert.Nil(t, exact.GetVersion("plugin", semver.MustParse("1.2.3")))
		got := exact.GetVersion("plugin", semver.MustParse("1.2.3-rc1"))
		require.NotNil(t, got)
		assert.Equal(t, "1.2.3-rc1", got.String())
	})
}

func TestVersionIndex_GetConstraints(t *testing.T) {
	t.Parallel()

	vi := NewVersionIndex[string]()
	for _, v := range []string{"1.0.0", "1.2.0", "1.5.0", "2.0.0", "2.1.0"} {
		vi.AddVersion("plugin", semver.MustParse(v))
	}

	mustConstraint := func(t *testing.T, s string) *semver.Constraints {
		t.Helper()
		c, err := semver.NewConstraint(s)
		require.NoError(t, err)
		return c
	}

	tests := []struct {
		name       string
		constraint string
		want       string // "" means nil expected
	}{
		{name: "caret picks highest in major", constraint: "^1.0.0", want: "1.5.0"},
		{name: "range picks highest match", constraint: ">=1.2.0, <2.0.0", want: "1.5.0"},
		{name: "tilde picks highest patch", constraint: "~1.2.0", want: "1.2.0"},
		{name: "wildcard minor picks highest", constraint: "2.x", want: "2.1.0"},
		{name: "no match returns nil", constraint: ">=3.0.0", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vi.GetConstraints("plugin", mustConstraint(t, tt.constraint))
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.String())
		})
	}

	t.Run("absent key returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, vi.GetConstraints("missing", mustConstraint(t, "^1.0.0")))
	})
}

func TestIsLatestConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "empty string is latest", input: "", want: true},
		{name: "whitespace-only is latest", input: "   ", want: true},
		{name: "exact lowercase latest", input: "latest", want: true},
		{name: "mixed case latest", input: "Latest", want: true},
		{name: "uppercase latest", input: "LATEST", want: true},
		{name: "latest with surrounding whitespace", input: "  latest  ", want: true},
		{name: "exact version is not latest", input: "1.2.3", want: false},
		{name: "caret constraint is not latest", input: "^1.2.0", want: false},
		{name: "range constraint is not latest", input: ">=1.0.0, <2.0.0", want: false},
		{name: "wildcard is not latest", input: "*", want: false},
		{name: "substring is not latest", input: "latest-1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isLatestConstraint(tt.input))
		})
	}
}

func TestVersionPool_GetByVersion(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}

	// seed adds a version to both the index and the store, keeping the
	// VersionPool invariant that every indexed version has a store entry.
	seed := func(t *testing.T, vp *VersionPool, version string, entry *poolEntry) {
		t.Helper()
		vp.index.AddVersion(id, semver.MustParse(version))
		vp.store.Set(VersionedPluginIdentity{PluginIdentity: id, Version: version}, entry)
	}

	t.Run("resolves the exact version, not the highest", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		v100 := &poolEntry{}
		v200 := &poolEntry{}
		seed(t, vp, "1.0.0", v100)
		seed(t, vp, "2.0.0", v200)

		got := vp.GetByVersion(id, semver.MustParse("1.0.0"))
		assert.Same(t, v100, got, "must resolve the exact version requested")
	})

	t.Run("absent version returns nil", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		seed(t, vp, "1.0.0", &poolEntry{})

		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("9.9.9")))
	})

	t.Run("does not interpret ranges", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		seed(t, vp, "1.2.3-rc1", &poolEntry{})

		// GetByVersion is exact-only: 1.2.3 must not match an indexed 1.2.3-rc1.
		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.2.3")))
	})

	t.Run("missing store entry for an indexed version returns nil", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		// Index the version but omit the store entry (invariant violation);
		// GetByVersion must fail closed rather than panic or return a wrong entry.
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))

		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")))
	})

	t.Run("identities are isolated by name and catalog", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		want := &poolEntry{}
		seed(t, vp, "1.0.0", want)

		other := PluginIdentity{Name: "plugin", Catalog: "other"}
		assert.Nil(t, vp.GetByVersion(other, semver.MustParse("1.0.0")), "a different catalog must not resolve")
		assert.Same(t, want, vp.GetByVersion(id, semver.MustParse("1.0.0")))
	})
}

func TestVersionPool_GetByConstraint(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}

	seed := func(t *testing.T, vp *VersionPool, version string, entry *poolEntry) {
		t.Helper()
		vp.index.AddVersion(id, semver.MustParse(version))
		vp.store.Set(VersionedPluginIdentity{PluginIdentity: id, Version: version}, entry)
	}

	mustConstraint := func(t *testing.T, s string) *semver.Constraints {
		t.Helper()
		c, err := semver.NewConstraint(s)
		require.NoError(t, err)
		return c
	}

	t.Run("resolves the highest matching version", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		v120 := &poolEntry{}
		v150 := &poolEntry{}
		v200 := &poolEntry{}
		seed(t, vp, "1.2.0", v120)
		seed(t, vp, "1.5.0", v150)
		seed(t, vp, "2.0.0", v200)

		got := vp.GetByConstraint(id, mustConstraint(t, ">=1.0.0, <2.0.0"))
		assert.Same(t, v150, got, "must resolve the highest version within the range")
	})

	t.Run("no matching version returns nil", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		seed(t, vp, "1.0.0", &poolEntry{})

		assert.Nil(t, vp.GetByConstraint(id, mustConstraint(t, ">=2.0.0")))
	})

	t.Run("missing store entry for the resolved version returns nil", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		// Index a version the constraint matches but omit the store entry;
		// GetByConstraint must fail closed.
		vp.index.AddVersion(id, semver.MustParse("1.5.0"))

		assert.Nil(t, vp.GetByConstraint(id, mustConstraint(t, "^1.0.0")))
	})

	t.Run("identities are isolated by name and catalog", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		want := &poolEntry{}
		seed(t, vp, "1.5.0", want)

		other := PluginIdentity{Name: "plugin", Catalog: "other"}
		assert.Nil(t, vp.GetByConstraint(other, mustConstraint(t, "^1.0.0")), "a different catalog must not resolve")
		assert.Same(t, want, vp.GetByConstraint(id, mustConstraint(t, "^1.0.0")))
	})
}

func TestVersionPool_insert(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}

	t.Run("registers in both index and store", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		entry := &poolEntry{}
		sv := semver.MustParse("1.2.3")

		vp.insert(id, sv, entry)

		// Index side: the version is now resolvable by exact and by constraint.
		assert.NotNil(t, vp.index.GetVersion(id, sv), "version must be indexed")
		// Store side: the entry is retrievable through both lookup orchestrators.
		assert.Same(t, entry, vp.GetByVersion(id, sv))
		assert.Same(t, entry, vp.GetByConstraint(id, mustConstraintT(t, "^1.0.0")))
	})

	t.Run("keeps the index and store consistent for delete", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		entry := &poolEntry{}
		sv := semver.MustParse("1.2.3")

		vp.insert(id, sv, entry)
		vp.delete(id, sv)

		// delete is the inverse of insert: both structures are cleared.
		assert.Nil(t, vp.index.GetVersion(id, sv), "version must be removed from the index")
		assert.Nil(t, vp.GetByVersion(id, sv), "entry must be removed from the store")
	})

	t.Run("second insert of same version overwrites the store entry", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		first := &poolEntry{}
		second := &poolEntry{}
		sv := semver.MustParse("1.2.3")

		vp.insert(id, sv, first)
		vp.insert(id, sv, second)

		// AddVersion dedupes in the index; Set overwrites in the store, so the
		// latest entry wins and no duplicate index entry is created.
		assert.Same(t, second, vp.GetByVersion(id, sv))
	})
}

func TestVersionPool_waitForEntry(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}

	// readyEntry builds a poolEntry whose ready channel is already closed and
	// whose dep/version record the given version, so waitAndValidate proceeds
	// without blocking and waitForEntry can resolve entry.version on cleanup.
	readyEntry := func(state entryState, version string) *poolEntry {
		ready := make(chan struct{})
		close(ready)
		return &poolEntry{
			state:   state,
			ready:   ready,
			version: semver.MustParse(version),
			dep:     solution.PluginDependency{Name: id.Name, Catalog: id.Catalog, Version: version},
		}
	}

	t.Run("ready entry without acquire succeeds", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		entry := readyEntry(entryReady, "1.0.0")

		_, acquired, err := vp.waitForEntry(context.Background(), id, entry, false)
		require.NoError(t, err)
		assert.False(t, acquired)
		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
	})

	t.Run("ready entry with acquire increments refCount", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		entry := readyEntry(entryReady, "1.0.0")

		_, acquired, err := vp.waitForEntry(context.Background(), id, entry, true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
	})

	t.Run("cancelled context returns error and leaves entry indexed", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		// ready is never closed, so waitAndValidate blocks until ctx is done.
		entry := &poolEntry{
			state: entryStarting,
			ready: make(chan struct{}),
			dep:   solution.PluginDependency{Name: id.Name, Catalog: id.Catalog, Version: "1.0.0"},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, acquired, err := vp.waitForEntry(ctx, id, entry, true)
		assert.ErrorIs(t, err, context.Canceled)
		assert.False(t, acquired)
		// A context cancellation is not an entry failure: the version stays in
		// the index so the background spawn can still complete.
		_, ok := vp.index.entries[id]
		assert.True(t, ok, "context cancel must not remove the index entry")
	})

	t.Run("dead unreferenced entry is removed from index and store", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		entry := readyEntry(entryDead, "1.0.0")
		entry.err = errors.New("spawn failed")
		// Seed the store so we can assert the composed deletion removes it too.
		vp.store.Set(VersionedPluginIdentity{PluginIdentity: id, Version: "1.0.0"}, entry)

		_, acquired, err := vp.waitForEntry(context.Background(), id, entry, true)
		assert.Error(t, err)
		assert.False(t, acquired)
		_, ok := vp.index.entries[id]
		assert.False(t, ok, "dead unreferenced entry must be removed from the index")
		_, ok = vp.store.Get(VersionedPluginIdentity{PluginIdentity: id, Version: "1.0.0"})
		assert.False(t, ok, "dead unreferenced entry must be removed from the store")
	})

	t.Run("dead referenced entry stays in index and store", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		entry := readyEntry(entryDead, "1.0.0")
		entry.err = errors.New("spawn failed")
		vp.store.Set(VersionedPluginIdentity{PluginIdentity: id, Version: "1.0.0"}, entry)
		// Simulate an in-flight reference: teardown must be deferred, so the
		// index and store entries must be retained for the final release.
		atomic.StoreInt32(&entry.refCount, 1)

		_, acquired, err := vp.waitForEntry(context.Background(), id, entry, false)
		assert.Error(t, err)
		assert.False(t, acquired)
		_, ok := vp.index.entries[id]
		assert.True(t, ok, "referenced dead entry must remain in the index")
		_, ok = vp.store.Get(VersionedPluginIdentity{PluginIdentity: id, Version: "1.0.0"})
		assert.True(t, ok, "referenced dead entry must remain in the store")
	})
}

func TestVersionPool_delete(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	vid := func(version string) VersionedPluginIdentity {
		return VersionedPluginIdentity{PluginIdentity: id, Version: version}
	}

	t.Run("removes the version from both index and store", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		vp.index.AddVersion(id, semver.MustParse("2.0.0"))
		vp.store.Set(vid("1.0.0"), &poolEntry{})
		vp.store.Set(vid("2.0.0"), &poolEntry{})

		vp.delete(id, semver.MustParse("1.0.0"))

		// The deleted version is gone from both structures.
		_, ok := vp.store.Get(vid("1.0.0"))
		assert.False(t, ok, "store must no longer hold the deleted version")
		assert.Nil(t, vp.index.GetVersion(id, semver.MustParse("1.0.0")),
			"index must no longer hold the deleted version")

		// The sibling version is untouched, preserving the invariant.
		_, ok = vp.store.Get(vid("2.0.0"))
		assert.True(t, ok, "sibling version must remain in the store")
		got := vp.index.GetVersion(id, semver.MustParse("2.0.0"))
		require.NotNil(t, got)
		assert.Equal(t, "2.0.0", got.String())
	})

	t.Run("deleting the last version removes the index key", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		vp.store.Set(vid("1.0.0"), &poolEntry{})

		vp.delete(id, semver.MustParse("1.0.0"))

		_, ok := vp.index.entries[id]
		assert.False(t, ok, "index key must be removed when no versions remain")
		_, ok = vp.store.Get(vid("1.0.0"))
		assert.False(t, ok)
	})

	t.Run("deleting an absent version is a no-op", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.index.AddVersion(id, semver.MustParse("1.0.0"))
		vp.store.Set(vid("1.0.0"), &poolEntry{})

		vp.delete(id, semver.MustParse("9.9.9"))

		_, ok := vp.store.Get(vid("1.0.0"))
		assert.True(t, ok, "existing version must remain after deleting an absent one")
	})
}

// clockAt returns an Option fixing the pool clock to a constant instant, so
// lastUsed updates are deterministic in tests.
func clockAt(ts time.Time) Option {
	return WithClock(func() time.Time { return ts })
}

func TestVersionPool_waitAndValidate_TouchesLastUsed(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	vp := NewVersionPool(context.Background(), nil, nil, clockAt(fixed))

	ready := make(chan struct{})
	close(ready)
	entry := &poolEntry{
		state: entryReady,
		ready: ready,
		dep:   solution.PluginDependency{Name: "plugin", Version: "1.0.0"},
	}

	entry, acquired, err := vp.waitAndValidate(context.Background(), entry, true)
	require.NoError(t, err)
	assert.True(t, acquired)
	require.NotNil(t, entry, "an acquired entry handle must be returned")
	assert.Equal(t, fixed, entry.lastUsed, "acquire must touch lastUsed via the pool clock")
	assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
}

// fakePluginClient is a pluginClient that records Kill calls, so teardown tests
// can assert whether (and how often) the subprocess was terminated without
// launching a real process.
type fakePluginClient struct {
	mu        sync.Mutex
	killed    int
	providers []string
	provErr   error
}

func (f *fakePluginClient) GetProviders(context.Context) ([]string, error) {
	return f.providers, f.provErr
}

func (f *fakePluginClient) Kill() {
	f.mu.Lock()
	f.killed++
	f.mu.Unlock()
}

func (f *fakePluginClient) killCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.killed
}

// fakeProviderRegistry is a providerRegistry that records Unregister (and
// Register) calls, letting teardown tests assert which provider names were
// retracted without touching the global registry.
type fakeProviderRegistry struct {
	mu           sync.Mutex
	has          map[string]bool
	registerErr  error
	registered   []provider.Provider
	unregistered []string
}

func (f *fakeProviderRegistry) Register(p provider.Provider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, p)
	return f.registerErr
}

func (f *fakeProviderRegistry) Has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.has[name]
}

func (f *fakeProviderRegistry) Unregister(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unregistered = append(f.unregistered, name)
	return true
}

func (f *fakeProviderRegistry) unregisteredNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.unregistered))
	copy(out, f.unregistered)
	return out
}

func TestVersionPool_teardownEntry(t *testing.T) {
	t.Parallel()

	t.Run("nil client unregisters providers and clears the slice", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		vp := NewVersionPool(context.Background(), nil, reg)
		entry := &poolEntry{
			registeredProviders: []string{"exec", "git"},
		}

		vp.teardownEntry(entry)

		assert.Nil(t, entry.registeredProviders, "registered providers must be cleared")
		assert.Equal(t, []string{"exec", "git"}, reg.unregisteredNames(),
			"a nil client must still retract the entry's providers")
		assert.False(t, entry.pendingKill)
	})

	t.Run("referenced client defers the kill and marks pendingKill", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		vp := NewVersionPool(context.Background(), nil, reg)
		fc := &fakePluginClient{}
		entry := &poolEntry{
			client:              fc,
			registeredProviders: []string{"exec"},
		}
		atomic.StoreInt32(&entry.refCount, 1)

		vp.teardownEntry(entry)

		assert.True(t, entry.pendingKill, "kill must be deferred while referenced")
		assert.Same(t, fc, entry.client, "client must be retained until the last release")
		assert.Equal(t, 0, fc.killCount(), "a referenced client must not be killed")
		assert.Nil(t, entry.registeredProviders)
		assert.Equal(t, []string{"exec"}, reg.unregisteredNames(),
			"providers are retracted immediately even when the kill is deferred")
	})

	t.Run("unreferenced client is killed and cleared", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		vp := NewVersionPool(context.Background(), nil, reg)
		fc := &fakePluginClient{}
		entry := &poolEntry{
			client:              fc,
			registeredProviders: []string{"exec", "git"},
		}

		vp.teardownEntry(entry)

		assert.Nil(t, entry.client, "unreferenced client must be cleared")
		assert.Equal(t, 1, fc.killCount(), "unreferenced client must be killed exactly once")
		assert.False(t, entry.pendingKill)
		assert.Nil(t, entry.registeredProviders)
		assert.Equal(t, []string{"exec", "git"}, reg.unregisteredNames())
	})

	t.Run("nil registry does not panic and still kills the client", func(t *testing.T) {
		t.Parallel()
		// A nil registry exercises the unregisterProviders early return.
		vp := NewVersionPool(context.Background(), nil, nil)
		fc := &fakePluginClient{}
		entry := &poolEntry{
			client:              fc,
			registeredProviders: []string{"exec"},
		}

		assert.NotPanics(t, func() { vp.teardownEntry(entry) })
		assert.Nil(t, entry.client)
		assert.Equal(t, 1, fc.killCount())
		assert.Nil(t, entry.registeredProviders)
	})
}

// fakeRegistrableProvider is a registrableProvider (provider.Provider +
// Configure) whose Configure result is programmable and whose call count is
// recorded, so registration tests can drive the configure-error branch and
// assert Configure was invoked. Descriptor/Execute are unused by the pool's
// registration path (the fakeProviderRegistry only records), so they are stubs.
type fakeRegistrableProvider struct {
	name       string
	configErr  error
	configured int32
}

func (f *fakeRegistrableProvider) Descriptor() *provider.Descriptor { return nil }
func (f *fakeRegistrableProvider) Execute(context.Context, any) (*provider.Output, error) {
	return nil, nil
}

func (f *fakeRegistrableProvider) Configure(context.Context, ProviderConfig) error {
	atomic.AddInt32(&f.configured, 1)
	return f.configErr
}

// TestVersionPool_startAndRegister drives the provider registration loop
// through its real caller, startAndRegister, so the client-start and
// GetProviders branches are covered alongside every registerProviders branch
// (nil registry, name taken, wrap/register/configure failure, and success).
func TestVersionPool_startAndRegister(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "plugin", Catalog: "official"}
	result := FetchResult{Path: "/fake/plugin", Version: "1.2.3"}

	// clientFor injects a newClient seam that yields fc (or err) without
	// launching a real subprocess.
	clientFor := func(fc pluginClient, err error) clientFactory {
		return func(string, ...ClientOption) (pluginClient, error) { return fc, err }
	}

	// wrapperFor builds a newWrapper seam that returns a distinct fake provider
	// per name (recording them by name) and can be told to fail for specific
	// names. It also counts how many times it was invoked.
	wrapperFor := func(wraps map[string]*fakeRegistrableProvider, failNames map[string]bool, calls *int32) wrapperFactory {
		return func(_ pluginClient, name string, _ ...WrapperOption) (registrableProvider, error) {
			atomic.AddInt32(calls, 1)
			if failNames[name] {
				return nil, errors.New("wrap failed for " + name)
			}
			w := &fakeRegistrableProvider{name: name}
			wraps[name] = w
			return w, nil
		}
	}

	t.Run("client start failure marks the entry dead", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		vp.opts.newClient = clientFor(nil, errors.New("boom"))
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, entryDead, entry.state, "a start failure must kill the entry")
		assert.ErrorContains(t, entry.err, "starting process")
		assert.Nil(t, entry.client)
	})

	t.Run("GetProviders failure kills the client and marks the entry dead", func(t *testing.T) {
		t.Parallel()
		fc := &fakePluginClient{provErr: errors.New("handshake failed")}
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		vp.opts.newClient = clientFor(fc, nil)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, entryDead, entry.state)
		assert.ErrorContains(t, entry.err, "getting providers")
		assert.Equal(t, 1, fc.killCount(), "a client that cannot enumerate providers must be killed")
		assert.Nil(t, entry.client)
	})

	t.Run("nil registry skips everything without wrapping", func(t *testing.T) {
		t.Parallel()
		var calls int32
		fc := &fakePluginClient{providers: []string{"exec", "git"}}
		vp := NewVersionPool(context.Background(), nil, nil)
		vp.opts.newClient = clientFor(fc, nil)
		vp.opts.newWrapper = wrapperFor(map[string]*fakeRegistrableProvider{}, nil, &calls)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, entryReady, entry.state, "the plugin still loads even with no registry")
		assert.Empty(t, entry.registeredProviders, "a nil registry registers nothing")
		assert.Equal(t, int32(0), calls, "a nil registry must short-circuit before wrapping")
	})

	t.Run("already-registered names are skipped", func(t *testing.T) {
		t.Parallel()
		var calls int32
		reg := &fakeProviderRegistry{has: map[string]bool{"exec": true}}
		fc := &fakePluginClient{providers: []string{"exec", "git"}}
		vp := NewVersionPool(context.Background(), nil, reg)
		vp.opts.newClient = clientFor(fc, nil)
		wraps := map[string]*fakeRegistrableProvider{}
		vp.opts.newWrapper = wrapperFor(wraps, nil, &calls)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, []string{"git"}, entry.registeredProviders, "the taken name must be skipped, the free one registered")
		assert.Equal(t, int32(1), calls, "the taken name must not be wrapped")
		assert.Equal(t, int32(1), atomic.LoadInt32(&wraps["git"].configured), "the registered provider must be configured")
	})

	t.Run("wrapper factory error skips the provider", func(t *testing.T) {
		t.Parallel()
		var calls int32
		reg := &fakeProviderRegistry{}
		fc := &fakePluginClient{providers: []string{"exec", "git"}}
		vp := NewVersionPool(context.Background(), nil, reg)
		vp.opts.newClient = clientFor(fc, nil)
		wraps := map[string]*fakeRegistrableProvider{}
		vp.opts.newWrapper = wrapperFor(wraps, map[string]bool{"exec": true}, &calls)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, []string{"git"}, entry.registeredProviders, "the wrap failure is skipped, the other registered")
		assert.Len(t, reg.registered, 1, "only the successfully wrapped provider is registered")
	})

	t.Run("register error skips the provider", func(t *testing.T) {
		t.Parallel()
		var calls int32
		reg := &fakeProviderRegistry{registerErr: errors.New("dup")}
		fc := &fakePluginClient{providers: []string{"exec"}}
		vp := NewVersionPool(context.Background(), nil, reg)
		vp.opts.newClient = clientFor(fc, nil)
		wraps := map[string]*fakeRegistrableProvider{}
		vp.opts.newWrapper = wrapperFor(wraps, nil, &calls)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Empty(t, entry.registeredProviders, "a registration error yields no registered names")
		assert.Equal(t, int32(0), atomic.LoadInt32(&wraps["exec"].configured),
			"Configure must not run when Register fails")
	})

	t.Run("configure error skips the provider", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		fc := &fakePluginClient{providers: []string{"exec"}}
		vp := NewVersionPool(context.Background(), nil, reg)
		vp.opts.newClient = clientFor(fc, nil)
		vp.opts.newWrapper = func(_ pluginClient, name string, _ ...WrapperOption) (registrableProvider, error) {
			return &fakeRegistrableProvider{name: name, configErr: errors.New("bad config")}, nil
		}
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Empty(t, entry.registeredProviders, "a configure error yields no registered names")
		assert.Len(t, reg.registered, 1, "the provider was registered before Configure failed")
	})

	t.Run("all providers register successfully and the entry becomes ready", func(t *testing.T) {
		t.Parallel()
		var calls int32
		fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		reg := &fakeProviderRegistry{}
		fc := &fakePluginClient{providers: []string{"exec", "git", "directory"}}
		vp := NewVersionPool(context.Background(), nil, reg, clockAt(fixed))
		vp.opts.newClient = clientFor(fc, nil)
		wraps := map[string]*fakeRegistrableProvider{}
		vp.opts.newWrapper = wrapperFor(wraps, nil, &calls)
		entry := &poolEntry{dep: dep}

		vp.startAndRegister(context.Background(), entry, result)

		assert.Equal(t, entryReady, entry.state)
		assert.Same(t, fc, entry.client, "the started client is retained on the entry")
		assert.Equal(t, result, entry.result, "the fetch result is recorded on the entry")
		assert.Equal(t, fixed, entry.lastUsed, "lastUsed is stamped from the pool clock")
		assert.Equal(t, []string{"exec", "git", "directory"}, entry.registeredProviders, "all names register in input order")
		assert.Len(t, reg.registered, 3)
		for _, name := range []string{"exec", "git", "directory"} {
			assert.Equal(t, int32(1), atomic.LoadInt32(&wraps[name].configured), name+" must be configured once")
		}
	})
}

// fakePluginFetcher is a pluginFetcher returning canned results, so the fetch
// branches of ensureOneByConstraint are exercised without a real catalog or
// network. hook, when set, runs at the start of FetchPlugins to simulate a
// concurrent caller resolving the same concrete version mid-fetch.
type fakePluginFetcher struct {
	results []FetchResult
	err     error
	calls   int32
	hook    func()
}

func (f *fakePluginFetcher) FetchPlugins(context.Context, []solution.PluginDependency, []bundler.LockPlugin) ([]FetchResult, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.hook != nil {
		f.hook()
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func (f *fakePluginFetcher) callCount() int32 { return atomic.LoadInt32(&f.calls) }

// seedReadyEntry inserts a ready poolEntry at version into both the index and
// the store, mirroring a plugin that is already loaded. It is lock-free (it does
// not take vp.mu), so callers must either run before any concurrent use or hold
// vp.mu themselves.
func seedReadyEntry(vp *VersionPool, id PluginIdentity, version string, dep solution.PluginDependency) *poolEntry {
	ready := make(chan struct{})
	close(ready)
	entry := &poolEntry{
		state:   entryReady,
		ready:   ready,
		version: semver.MustParse(version),
		dep:     dep,
	}
	vp.insert(id, semver.MustParse(version), entry)
	return entry
}

func TestVersionPool_ensureOneByVersion(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "1.2.3", Kind: solution.PluginKindProvider}
	id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}
	v := semver.MustParse("1.2.3")

	t.Run("cache hit acquires the existing entry without fetching", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		entry := seedReadyEntry(vp, id, "1.2.3", dep)

		_, acquired, err := vp.ensureOneByVersion(context.Background(), dep, v, true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
		assert.Equal(t, int32(0), ff.callCount(), "a cache hit must not fetch")
	})

	t.Run("cache hit without acquire does not increment refCount", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})
		entry := seedReadyEntry(vp, id, "1.2.3", dep)

		_, acquired, err := vp.ensureOneByVersion(context.Background(), dep, v, false)
		require.NoError(t, err)
		assert.False(t, acquired)
		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
	})

	t.Run("cache miss spawns, indexes, and acquires", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{results: []FetchResult{{Version: "1.2.3", Path: "/fake/plugin"}}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		vp.opts.newClient = func(string, ...ClientOption) (pluginClient, error) {
			return &fakePluginClient{}, nil
		}

		_, acquired, err := vp.ensureOneByVersion(context.Background(), dep, v, true)
		require.NoError(t, err)
		assert.True(t, acquired)

		got := vp.GetByVersion(id, v)
		require.NotNil(t, got, "the spawned entry must be indexed")
		got.mu.Lock()
		state := got.state
		got.mu.Unlock()
		assert.Equal(t, entryReady, state)
		assert.Equal(t, int32(1), ff.callCount())
	})

	t.Run("cache miss with a failed spawn returns an error and cleans up", func(t *testing.T) {
		t.Parallel()
		// A nil fetcher makes spawnWithEntry fail the entry deterministically,
		// so waitForEntry sees a dead, unreferenced entry and removes it.
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOneByVersion(context.Background(), dep, v, true)
		require.Error(t, err)
		assert.False(t, acquired)
		assert.Nil(t, vp.GetByVersion(id, v), "a failed, unreferenced entry must be cleaned up")
	})
}

func TestVersionPool_ensureOneByConstraint(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "^1.0.0", Kind: solution.PluginKindProvider}
	id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}

	t.Run("fast path resolves an indexed version without fetching", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		entry := seedReadyEntry(vp, id, "1.2.0", dep)

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
		assert.Equal(t, int32(0), ff.callCount(), "a fast-path hit must not fetch")
	})

	t.Run("no fetcher returns an error", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		assert.False(t, acquired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "fetcher not available")
	})

	t.Run("fetch error is wrapped", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{err: errors.New("boom")}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		assert.False(t, acquired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "fetching plugin")
	})

	t.Run("empty fetch results return an error", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{results: []FetchResult{}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		assert.False(t, acquired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "no fetch results")
	})

	t.Run("unparseable resolved version errors and releases the pin", func(t *testing.T) {
		t.Parallel()
		var released int32
		ff := &fakePluginFetcher{results: []FetchResult{
			{Version: "not-a-version", Release: func() { atomic.AddInt32(&released, 1) }},
		}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		assert.False(t, acquired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "parsing resolved version")
		assert.Equal(t, int32(1), atomic.LoadInt32(&released), "the redundant fetch pin must be released")
	})

	t.Run("concurrent resolution of the same version reuses the entry", func(t *testing.T) {
		t.Parallel()
		var released, spawned int32
		ff := &fakePluginFetcher{results: []FetchResult{
			{Version: "1.5.0", Release: func() { atomic.AddInt32(&released, 1) }},
		}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		vp.opts.newClient = func(string, ...ClientOption) (pluginClient, error) {
			atomic.AddInt32(&spawned, 1)
			return &fakePluginClient{}, nil
		}
		// hook simulates a concurrent caller that resolves 1.5.0 mid-fetch, so
		// the post-fetch re-check under the lock finds the entry already present.
		var winner *poolEntry
		ff.hook = func() {
			vp.mu.Lock()
			winner = seedReadyEntry(vp, id, "1.5.0", dep)
			vp.mu.Unlock()
		}

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Same(t, winner, vp.GetByVersion(id, semver.MustParse("1.5.0")),
			"the pre-existing entry must win the race")
		assert.Equal(t, int32(1), atomic.LoadInt32(&released), "the redundant fetch pin must be released")
		assert.Equal(t, int32(0), atomic.LoadInt32(&spawned), "the loser must not spawn a second process")
	})

	t.Run("resolves via fetch then spawns and acquires", func(t *testing.T) {
		t.Parallel()
		var released int32
		ff := &fakePluginFetcher{results: []FetchResult{
			{Version: "1.5.0", Path: "/fake/plugin", Release: func() { atomic.AddInt32(&released, 1) }},
		}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		vp.opts.newClient = func(string, ...ClientOption) (pluginClient, error) {
			return &fakePluginClient{}, nil
		}

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), dep, mustConstraintT(t, "^1.0.0"), true)
		require.NoError(t, err)
		assert.True(t, acquired)
		require.NotNil(t, vp.GetByVersion(id, semver.MustParse("1.5.0")), "the resolved version must be indexed")
		assert.Equal(t, int32(1), atomic.LoadInt32(&released), "spawnFromResult must release the fetch pin")
	})
}

func TestVersionPool_ensureOne(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	newDep := func(version string) solution.PluginDependency {
		return solution.PluginDependency{Name: "plugin", Catalog: "official", Version: version, Kind: solution.PluginKindProvider}
	}

	t.Run("closed pool is rejected before any work", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		vp.closed.Store(true)

		_, acquired, err := vp.ensureOne(context.Background(), newDep("1.0.0"), true)
		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrPoolClosed)
		assert.Equal(t, int32(0), ff.callCount(), "a closed pool must not fetch")
	})

	t.Run("disabled external plugins are rejected", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{},
			WithVersionPoolDisableExternal(true))

		_, acquired, err := vp.ensureOne(context.Background(), newDep("1.0.0"), true)
		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrExternalDisabled)
	})

	t.Run("plugin not in allowlist is rejected", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{},
			WithVersionPoolAAllowedPlugins(map[string]bool{"other": true}))

		_, acquired, err := vp.ensureOne(context.Background(), newDep("1.0.0"), true)
		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrPluginNotAllowed)
	})

	t.Run("unparseable version string errors", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})

		_, acquired, err := vp.ensureOne(context.Background(), newDep("not@a@version"), true)
		assert.False(t, acquired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "parsing version")
	})

	t.Run("exact pin dispatches to the version path", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		dep := newDep("1.2.3")
		entry := seedReadyEntry(vp, id, "1.2.3", dep)

		_, acquired, err := vp.ensureOne(context.Background(), dep, true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
		assert.Equal(t, int32(0), ff.callCount(), "an exact pin resolved from the index must not fetch")
	})

	t.Run("constraint dispatches to the constraint path", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		dep := newDep("^1.0.0")
		entry := seedReadyEntry(vp, id, "1.4.0", dep)

		_, acquired, err := vp.ensureOne(context.Background(), dep, true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
		assert.Equal(t, int32(0), ff.callCount(), "a constraint satisfied by the index must not fetch")
	})
}

func TestVersionPool_releaseEntry(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "plugin", Catalog: "official"}

	t.Run("drops one reference without killing while others remain", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		fc := &fakePluginClient{}
		entry := &poolEntry{client: fc}
		atomic.StoreInt32(&entry.refCount, 2)

		vp.releaseEntry(entry)

		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount), "one reference must remain")
		assert.Same(t, fc, entry.client, "a still-referenced client must not be cleared")
		assert.Equal(t, 0, fc.killCount(), "no pendingKill means no kill")
	})

	t.Run("last release without pendingKill leaves the client intact", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		fc := &fakePluginClient{}
		entry := &poolEntry{client: fc}
		atomic.StoreInt32(&entry.refCount, 1)

		vp.releaseEntry(entry)

		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
		assert.Same(t, fc, entry.client, "without pendingKill the entry keeps its client for reuse")
		assert.Equal(t, 0, fc.killCount())
	})

	t.Run("last release with pendingKill kills and clears the client", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		fc := &fakePluginClient{}
		entry := &poolEntry{client: fc, pendingKill: true}
		atomic.StoreInt32(&entry.refCount, 1)

		vp.releaseEntry(entry)

		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount))
		assert.Nil(t, entry.client, "a deferred kill must clear the client")
		assert.False(t, entry.pendingKill, "pendingKill must be consumed")
		assert.Equal(t, 1, fc.killCount(), "the deferred client must be killed exactly once")
	})

	t.Run("release symmetrically undoes an acquire", func(t *testing.T) {
		t.Parallel()
		id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})
		entry := seedReadyEntry(vp, id, "1.2.3", dep)

		acquiredEntry, acquired, err := vp.ensureOneByVersion(context.Background(), dep,
			semver.MustParse("1.2.3"), true)
		require.NoError(t, err)
		require.True(t, acquired)
		require.Same(t, entry, acquiredEntry)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))

		vp.releaseEntry(acquiredEntry)
		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount), "release must return to zero references")
	})
}

func TestVersionPool_Ensure(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	provDep := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "1.2.3", Kind: solution.PluginKindProvider}

	t.Run("loads provider deps without taking references", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})
		entry := seedReadyEntry(vp, id, "1.2.3", provDep)

		err := vp.Ensure(context.Background(), []solution.PluginDependency{provDep})

		require.NoError(t, err)
		assert.Equal(t, int32(0), atomic.LoadInt32(&entry.refCount), "Ensure must not acquire")
	})

	t.Run("skips non-provider kinds", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		other := solution.PluginDependency{Name: "other", Catalog: "official", Version: "1.0.0"}

		err := vp.Ensure(context.Background(), []solution.PluginDependency{other})

		require.NoError(t, err)
		assert.Equal(t, int32(0), ff.callCount(), "a non-provider dep must be skipped before any fetch")
	})

	t.Run("returns the first ensure error", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{},
			WithVersionPoolDisableExternal(true))

		err := vp.Ensure(context.Background(), []solution.PluginDependency{provDep})

		require.ErrorIs(t, err, ErrExternalDisabled)
	})
}

func TestVersionPool_EnsureAndAcquire(t *testing.T) {
	t.Parallel()

	idA := PluginIdentity{Name: "a", Catalog: "official"}
	idB := PluginIdentity{Name: "b", Catalog: "official"}
	depA := solution.PluginDependency{Name: "a", Catalog: "official", Version: "1.0.0", Kind: solution.PluginKindProvider}
	depB := solution.PluginDependency{Name: "b", Catalog: "official", Version: "2.0.0", Kind: solution.PluginKindProvider}

	t.Run("acquires every provider and release drops them all", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})
		entryA := seedReadyEntry(vp, idA, "1.0.0", depA)
		entryB := seedReadyEntry(vp, idB, "2.0.0", depB)

		release, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{depA, depB})

		require.NoError(t, err)
		require.NotNil(t, release)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entryA.refCount))
		assert.Equal(t, int32(1), atomic.LoadInt32(&entryB.refCount))

		release()
		assert.Equal(t, int32(0), atomic.LoadInt32(&entryA.refCount), "release must drop a's reference")
		assert.Equal(t, int32(0), atomic.LoadInt32(&entryB.refCount), "release must drop b's reference")
	})

	t.Run("a failure releases references already acquired", func(t *testing.T) {
		t.Parallel()
		// a is ready and gets acquired; b is disabled-by-kind? No -- use a
		// missing fetcher for b so its ensure fails after a is acquired.
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		entryA := seedReadyEntry(vp, idA, "1.0.0", depA)
		// depB is not seeded and the pool has no fetcher, so ensureOne(b) fails.

		release, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{depA, depB})

		require.Error(t, err)
		assert.Nil(t, release, "no release is returned on failure")
		assert.Equal(t, int32(0), atomic.LoadInt32(&entryA.refCount),
			"the reference taken on a must be released when b fails")
	})

	t.Run("skips non-provider kinds", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		other := solution.PluginDependency{Name: "other", Catalog: "official", Version: "1.0.0"}

		release, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{other})

		require.NoError(t, err)
		require.NotNil(t, release)
		assert.Equal(t, int32(0), ff.callCount(), "a non-provider dep must be skipped before any fetch")
		assert.NotPanics(t, release, "releasing an empty acquire set is a no-op")
	})
}

func TestVersionPool_Adopt(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	dep := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "1.2.3", Kind: solution.PluginKindProvider}

	t.Run("indexes a ready entry resolvable by its exact version", func(t *testing.T) {
		t.Parallel()
		fixed := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{}, clockAt(fixed))
		client := &Client{name: "plugin", path: "/fake/plugin"}
		v := semver.MustParse("1.2.3")

		vp.Adopt(id, v, client, dep, []string{"exec", "git"})

		entry := vp.GetByVersion(id, v)
		require.NotNil(t, entry, "an adopted version must be indexed and stored")
		assert.Equal(t, entryReady, entry.state, "an adopted entry is immediately ready")
		assert.Same(t, client, entry.client, "the adopted client is retained on the entry")
		assert.Same(t, v, entry.version, "the resolved version is recorded on the entry")
		assert.Equal(t, dep, entry.dep)
		assert.Equal(t, []string{"exec", "git"}, entry.registeredProviders)
		assert.Equal(t, fixed, entry.lastUsed, "lastUsed is stamped from the pool clock")

		// The ready channel must already be closed so waiters never block.
		select {
		case <-entry.ready:
		default:
			t.Fatal("an adopted entry's ready channel must be closed")
		}
	})

	t.Run("closed pool does not index the plugin", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		vp.closed.Store(true)
		v := semver.MustParse("1.2.3")

		vp.Adopt(id, v, &Client{name: "plugin", path: "/fake/plugin"}, dep, nil)

		assert.Nil(t, vp.GetByVersion(id, v), "a closed pool must not adopt")
		_, ok := vp.index.entries[id]
		assert.False(t, ok, "a closed pool must leave the index untouched")
	})

	t.Run("an adopted plugin is a cache hit that never fetches", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{})
		v := semver.MustParse("1.2.3")
		vp.Adopt(id, v, &Client{name: "plugin", path: "/fake/plugin"}, dep, nil)

		entry, acquired, err := vp.ensureOneByVersion(context.Background(), dep, v, true)
		require.NoError(t, err)
		assert.True(t, acquired)
		require.NotNil(t, entry)
		assert.Equal(t, int32(1), atomic.LoadInt32(&entry.refCount))
		assert.Equal(t, int32(0), ff.callCount(), "an adopted plugin must satisfy ensure without fetching")
	})

	t.Run("multiple versions of one identity coexist", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		c100 := &Client{name: "plugin", path: "/fake/v1"}
		c200 := &Client{name: "plugin", path: "/fake/v2"}
		depV1 := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "1.0.0", Kind: solution.PluginKindProvider}
		depV2 := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "2.0.0", Kind: solution.PluginKindProvider}

		vp.Adopt(id, semver.MustParse("1.0.0"), c100, depV1, nil)
		vp.Adopt(id, semver.MustParse("2.0.0"), c200, depV2, nil)

		assert.Same(t, c100, vp.GetByVersion(id, semver.MustParse("1.0.0")).client)
		assert.Same(t, c200, vp.GetByVersion(id, semver.MustParse("2.0.0")).client)
		// A range constraint resolves to the highest adopted version.
		assert.Same(t, c200, vp.GetByConstraint(id, mustConstraintT(t, ">=1.0.0")).client,
			"the highest adopted version must satisfy an open range constraint")
	})

	t.Run("identities are isolated by catalog", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		v := semver.MustParse("1.2.3")
		vp.Adopt(id, v, &Client{name: "plugin", path: "/fake/plugin"}, dep, nil)

		other := PluginIdentity{Name: "plugin", Catalog: "other"}
		assert.Nil(t, vp.GetByVersion(other, v), "a different catalog must not resolve an adopted entry")
	})
}

func TestVersionPool_ClientOptions(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "plugin", Catalog: "official", Version: "1.2.3", Kind: solution.PluginKindProvider}
	id := PluginIdentity{Name: dep.Name, Catalog: dep.Catalog}
	v := semver.MustParse("1.2.3")

	// applyClientOpts folds a slice of ClientOption into a clientOptions struct
	// so tests can assert the resolved flags the spawner would pass to NewClient.
	applyClientOpts := func(opts []ClientOption) clientOptions {
		var co clientOptions
		for _, o := range opts {
			o(&co)
		}
		return co
	}

	// spawnDep drives ensureOneByVersion to spawn a plugin, capturing the exact
	// ClientOption slice handed to the newClient seam.
	spawnDep := func(t *testing.T, vp *VersionPool) []ClientOption {
		t.Helper()
		var captured []ClientOption
		vp.opts.newClient = func(_ string, opts ...ClientOption) (pluginClient, error) {
			captured = opts
			return &fakePluginClient{}, nil
		}
		vp.fetcher = &fakePluginFetcher{results: []FetchResult{{Version: "1.2.3", Path: "/fake/plugin"}}}
		_, _, err := vp.ensureOneByVersion(context.Background(), dep, v, false)
		require.NoError(t, err)
		require.NotNil(t, vp.GetByVersion(id, v), "the spawned entry must be indexed")
		return captured
	}

	t.Run("WithVersionPoolClientOptions accumulates opts", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolClientOptions(WithGRPCMaxMessageSize(1024)),
			WithVersionPoolClientOptions(WithDebugLogging()))
		assert.Len(t, vp.opts.clientOpts, 2, "options from multiple calls must accumulate")
	})

	t.Run("sanitizeEnv defaults to true", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		assert.True(t, vp.opts.sanitizeEnv, "the pool must sanitize the environment by default")
	})

	t.Run("WithVersionPoolSanitizeEnv can disable sanitization", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolSanitizeEnv(false))
		assert.False(t, vp.opts.sanitizeEnv)
	})

	t.Run("spawn forwards sanitize plus extra opts to the client", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolClientOptions(WithGRPCMaxMessageSize(4096)))

		captured := spawnDep(t, vp)

		co := applyClientOpts(captured)
		assert.True(t, co.sanitizeEnv, "the default sanitized env must reach the client")
		assert.Equal(t, 4096, co.grpcMaxMessageSize, "extra client options must reach the client")
	})

	t.Run("spawn omits sanitize when disabled", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolSanitizeEnv(false),
			WithVersionPoolClientOptions(WithDebugLogging()))

		captured := spawnDep(t, vp)

		co := applyClientOpts(captured)
		assert.False(t, co.sanitizeEnv, "a disabled sanitize must not reach the client")
		assert.True(t, co.debugLog, "extra client options must still reach the client")
	})
}

func TestVersionPool_Capacity(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	newDep := func(name, version string) solution.PluginDependency {
		return solution.PluginDependency{Name: name, Catalog: "official", Version: version, Kind: solution.PluginKindProvider}
	}

	t.Run("a full pool rejects a new version with ErrPoolFull", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{},
			WithVersionPoolMaxPlugins(1))
		// Fill the single slot with an adopted plugin.
		vp.Adopt(id, semver.MustParse("1.0.0"), &Client{name: "plugin", path: "/fake"},
			newDep("plugin", "1.0.0"), nil)

		_, acquired, err := vp.ensureOneByVersion(context.Background(), newDep("plugin", "2.0.0"),
			semver.MustParse("2.0.0"), true)

		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrPoolFull)
		assert.Equal(t, int32(0), ff.callCount(), "a full pool must reject before fetching")
	})

	t.Run("a cache hit is never rejected by capacity", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{},
			WithVersionPoolMaxPlugins(1))
		dep := newDep("plugin", "1.0.0")
		entry := seedReadyEntry(vp, id, "1.0.0", dep)

		// The pool is at capacity, but the requested version is already loaded,
		// so the existing entry must be returned rather than rejected.
		got, acquired, err := vp.ensureOneByVersion(context.Background(), dep, semver.MustParse("1.0.0"), true)
		require.NoError(t, err)
		assert.True(t, acquired)
		assert.Same(t, entry, got)
	})

	t.Run("the constraint path rejects before fetching when full", func(t *testing.T) {
		t.Parallel()
		ff := &fakePluginFetcher{results: []FetchResult{{Version: "3.0.0", Path: "/fake"}}}
		vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{},
			WithVersionPoolMaxPlugins(1))
		vp.Adopt(id, semver.MustParse("1.0.0"), &Client{name: "plugin", path: "/fake"},
			newDep("plugin", "1.0.0"), nil)

		_, acquired, err := vp.ensureOneByConstraint(context.Background(), newDep("plugin", "^3.0.0"),
			mustConstraintT(t, "^3.0.0"), true)

		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrPoolFull)
		assert.Equal(t, int32(0), ff.callCount(), "a full pool must reject the constraint path before fetching")
	})

	t.Run("zero maxPlugins means unbounded", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})
		for i := 0; i < 5; i++ {
			v := semver.MustParse(fmt.Sprintf("%d.0.0", i+1))
			vp.Adopt(id, v, &Client{name: "plugin", path: "/fake"},
				newDep("plugin", v.String()), nil)
		}
		assert.NoError(t, vp.checkCapacity(newDep("plugin", "9.0.0")),
			"an unbounded pool must never report ErrPoolFull")
	})
}

func TestVersionPool_evict(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	dep := func(version string) solution.PluginDependency {
		return solution.PluginDependency{Name: "plugin", Catalog: "official", Version: version, Kind: solution.PluginKindProvider}
	}
	fixed := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)

	// seedEntry inserts a fully-formed entry at version in both index and store.
	seedEntry := func(vp *VersionPool, version string, state entryState, lastUsed time.Time, client pluginClient) *poolEntry {
		ready := make(chan struct{})
		close(ready)
		entry := &poolEntry{
			state:    state,
			ready:    ready,
			version:  semver.MustParse(version),
			dep:      dep(version),
			lastUsed: lastUsed,
			client:   client,
		}
		vp.insert(id, semver.MustParse(version), entry)
		return entry
	}

	t.Run("removes an idle unreferenced entry and kills its client", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		vp := NewVersionPool(context.Background(), nil, reg,
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		fc := &fakePluginClient{}
		// lastUsed is two minutes in the past, exceeding the one-minute timeout.
		seedEntry(vp, "1.0.0", entryReady, fixed.Add(-2*time.Minute), fc)

		vp.evict()

		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")), "an idle entry must be removed")
		assert.Equal(t, 1, fc.killCount(), "an evicted entry's client must be killed")
		assert.Equal(t, 1, vp.evicted, "the eviction counter must increment")
	})

	t.Run("keeps a recently used entry", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		fc := &fakePluginClient{}
		// lastUsed is only 30s in the past, within the one-minute timeout.
		seedEntry(vp, "1.0.0", entryReady, fixed.Add(-30*time.Second), fc)

		vp.evict()

		assert.NotNil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")), "a fresh entry must survive")
		assert.Equal(t, 0, fc.killCount())
		assert.Equal(t, 0, vp.evicted)
	})

	t.Run("keeps an idle but referenced entry", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		fc := &fakePluginClient{}
		entry := seedEntry(vp, "1.0.0", entryReady, fixed.Add(-2*time.Minute), fc)
		atomic.StoreInt32(&entry.refCount, 1)

		vp.evict()

		assert.NotNil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")),
			"a referenced entry must not be evicted even when idle")
		assert.Equal(t, 0, fc.killCount(), "a referenced client must not be killed")
	})

	t.Run("removes a dead unreferenced entry", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		fc := &fakePluginClient{}
		// A dead entry is evicted regardless of lastUsed.
		seedEntry(vp, "1.0.0", entryDead, fixed, fc)

		vp.evict()

		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")), "a dead entry must be removed")
		assert.Equal(t, 1, fc.killCount())
	})

	t.Run("evicts only the stale versions of one identity", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		stale := &fakePluginClient{}
		fresh := &fakePluginClient{}
		seedEntry(vp, "1.0.0", entryReady, fixed.Add(-2*time.Minute), stale)
		seedEntry(vp, "2.0.0", entryReady, fixed, fresh)

		vp.evict()

		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")), "the stale version must be evicted")
		assert.NotNil(t, vp.GetByVersion(id, semver.MustParse("2.0.0")), "the fresh version must remain")
		assert.Equal(t, 1, stale.killCount())
		assert.Equal(t, 0, fresh.killCount())
	})

	t.Run("no idle timeout means no idle eviction", func(t *testing.T) {
		t.Parallel()
		// idleTimeout == 0 disables idle eviction; only dead entries are removed.
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{}, clockAt(fixed))
		fc := &fakePluginClient{}
		seedEntry(vp, "1.0.0", entryReady, fixed.Add(-24*time.Hour), fc)

		vp.evict()

		assert.NotNil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")),
			"with idleTimeout==0 a ready entry must never be evicted for age")
		assert.Equal(t, 0, fc.killCount())
	})
}

func TestVersionPool_evictionLoop(t *testing.T) {
	t.Parallel()

	t.Run("exits when the context is cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		// idleTimeout==0 keeps NewVersionPool from auto-starting a loop, so this
		// test owns the single loop it starts.
		vp := NewVersionPool(ctx, nil, &fakeProviderRegistry{})
		vp.opts.idleTimeout = time.Hour // valid ticker interval for the manual loop

		done := make(chan struct{})
		go func() { vp.evictionLoop(); close(done) }()

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("evictionLoop did not exit on context cancel")
		}
	})

	t.Run("exits when the stop channel is closed", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		vp.opts.idleTimeout = time.Hour

		done := make(chan struct{})
		go func() { vp.evictionLoop(); close(done) }()

		close(vp.stop)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("evictionLoop did not exit when stop was closed")
		}
	})
}

func TestVersionPool_Stats(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	dep := func(version string) solution.PluginDependency {
		return solution.PluginDependency{Name: "plugin", Catalog: "official", Version: version, Kind: solution.PluginKindProvider}
	}

	// seed inserts an entry in the given state at version. When refs > 0 the
	// entry is marked as referenced so ready entries classify as Active.
	seed := func(vp *VersionPool, version string, state entryState, refs int32) *poolEntry {
		ready := make(chan struct{})
		close(ready)
		entry := &poolEntry{
			state:   state,
			ready:   ready,
			version: semver.MustParse(version),
			dep:     dep(version),
		}
		atomic.StoreInt32(&entry.refCount, refs)
		vp.insert(id, semver.MustParse(version), entry)
		return entry
	}

	t.Run("an empty pool reports zeroes", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		assert.Equal(t, PoolStats{}, vp.Stats())
	})

	t.Run("classifies every entry state", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		seed(vp, "1.0.0", entryStarting, 0) // Active (spawn in progress)
		seed(vp, "2.0.0", entryReady, 1)    // Active (referenced)
		seed(vp, "3.0.0", entryReady, 0)    // Idle (unreferenced)
		seed(vp, "4.0.0", entryDead, 0)     // Dead

		stats := vp.Stats()

		assert.Equal(t, 2, stats.Active, "starting and referenced-ready entries are active")
		assert.Equal(t, 1, stats.Idle, "an unreferenced ready entry is idle")
		assert.Equal(t, 1, stats.Dead)
		assert.Equal(t, 4, stats.Total)
		assert.Equal(t, 0, stats.Evicted)
	})

	t.Run("reflects the cumulative eviction counter", func(t *testing.T) {
		t.Parallel()
		fixed := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
			WithVersionPoolIdleTimeout(time.Minute), clockAt(fixed))
		// An idle-beyond-timeout entry that evict will reap.
		ready := make(chan struct{})
		close(ready)
		vp.insert(id, semver.MustParse("1.0.0"), &poolEntry{
			state:    entryReady,
			ready:    ready,
			version:  semver.MustParse("1.0.0"),
			dep:      dep("1.0.0"),
			lastUsed: fixed.Add(-2 * time.Minute),
		})

		vp.evict()

		stats := vp.Stats()
		assert.Equal(t, 0, stats.Total, "the evicted entry must be gone")
		assert.Equal(t, 1, stats.Evicted, "Stats must surface the eviction count")
	})
}

func TestVersionPool_Shutdown(t *testing.T) {
	t.Parallel()

	id := PluginIdentity{Name: "plugin", Catalog: "official"}
	dep := func(version string) solution.PluginDependency {
		return solution.PluginDependency{Name: "plugin", Catalog: "official", Version: version, Kind: solution.PluginKindProvider}
	}

	seedWithClient := func(vp *VersionPool, version string, client pluginClient, providers []string) *poolEntry {
		ready := make(chan struct{})
		close(ready)
		entry := &poolEntry{
			state:               entryReady,
			ready:               ready,
			version:             semver.MustParse(version),
			dep:                 dep(version),
			client:              client,
			registeredProviders: providers,
		}
		vp.insert(id, semver.MustParse(version), entry)
		return entry
	}

	t.Run("kills every client and unregisters their providers", func(t *testing.T) {
		t.Parallel()
		reg := &fakeProviderRegistry{}
		vp := NewVersionPool(context.Background(), nil, reg)
		fc1 := &fakePluginClient{}
		fc2 := &fakePluginClient{}
		seedWithClient(vp, "1.0.0", fc1, []string{"exec"})
		seedWithClient(vp, "2.0.0", fc2, []string{"git"})

		vp.Shutdown()

		assert.Equal(t, 1, fc1.killCount(), "each client must be killed once")
		assert.Equal(t, 1, fc2.killCount())
		assert.ElementsMatch(t, []string{"exec", "git"}, reg.unregisteredNames(),
			"all registered providers must be unregistered")
	})

	t.Run("marks the pool closed and clears the store", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		seedWithClient(vp, "1.0.0", &fakePluginClient{}, nil)

		vp.Shutdown()

		assert.True(t, vp.closed.Load(), "Shutdown must mark the pool closed")
		assert.Equal(t, 0, vp.store.Len(), "the store must be cleared")
		assert.Nil(t, vp.GetByVersion(id, semver.MustParse("1.0.0")), "no entry must remain resolvable")
	})

	t.Run("a closed pool rejects further loads", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), &fakePluginFetcher{}, &fakeProviderRegistry{})

		vp.Shutdown()

		_, acquired, err := vp.ensureOne(context.Background(), dep("1.0.0"), true)
		assert.False(t, acquired)
		require.ErrorIs(t, err, ErrPoolClosed)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})
		fc := &fakePluginClient{}
		seedWithClient(vp, "1.0.0", fc, nil)

		assert.NotPanics(t, func() {
			vp.Shutdown()
			vp.Shutdown()
		}, "a second Shutdown must not panic (stop is closed once)")
		assert.Equal(t, 1, fc.killCount(), "the client must be killed exactly once")
	})

	t.Run("cancels the pool context", func(t *testing.T) {
		t.Parallel()
		vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{})

		vp.Shutdown()

		select {
		case <-vp.ctx.Done():
		default:
			t.Fatal("Shutdown must cancel the pool context")
		}
	})
}
