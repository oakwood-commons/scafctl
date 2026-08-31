// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNilVersionProvider returns a provider whose descriptor carries a nil
// Version, used to exercise the nil-version guard in RegisterBuiltin.
func newNilVersionProvider(name string) Provider {
	return &mockProvider{
		descriptor: &Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      nil,
			Description:  "Mock provider with nil version",
			Capabilities: []Capability{CapabilityFrom},
		},
	}
}

// withRegVer is a test helper that returns a WithRegistrationVersion option
// for a version string, panicking if the string is not a valid semver.
func withRegVer(v string) VersionedRegistryOptionFunc {
	return WithRegistrationVersion(semver.MustParse(v))
}

func TestVersionedRegistry_Register(t *testing.T) {
	tests := []struct {
		name        string
		provider    Provider
		catalog     string // applied to both Register and Get
		regVersion  string // explicit registration version; empty means omit WithRegistrationVersion
		getVersion  string // version/constraint used to retrieve on success
		wantErr     bool
		errContains string
	}{
		{
			name:       "valid provider",
			provider:   newMockProvider("alpha", "1.0.0"),
			regVersion: "1.0.0",
			getVersion: "1.0.0",
		},
		{
			name:       "valid provider with catalog",
			provider:   newMockProvider("alpha", "1.0.0"),
			catalog:    "my-catalog",
			regVersion: "1.0.0",
			getVersion: "1.0.0",
		},
		{
			name:        "nil provider",
			provider:    nil,
			regVersion:  "1.0.0",
			wantErr:     true,
			errContains: "cannot register nil provider",
		},
		{
			name:        "nil descriptor",
			provider:    &mockProvider{descriptor: nil},
			regVersion:  "1.0.0",
			wantErr:     true,
			errContains: "descriptor cannot be nil",
		},
		{
			name:        "missing registration version",
			provider:    newMockProvider("alpha", "1.0.0"),
			wantErr:     true,
			errContains: "registration version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := NewVersionedRegistry()

			var registerOpts []VersionedRegistryOptionFunc
			if tt.catalog != "" {
				registerOpts = append(registerOpts, WithCatalogName(tt.catalog))
			}
			if tt.regVersion != "" {
				registerOpts = append(registerOpts, withRegVer(tt.regVersion))
			}

			err := vr.Register(tt.provider, registerOpts...)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)

			getOpts := append(registerOpts, WithVersionOrConstraint(tt.getVersion))
			got, ok := vr.Get(tt.provider.Descriptor().Name, getOpts...)
			assert.True(t, ok)
			assert.Same(t, tt.provider, got)
		})
	}
}

// TestVersionedRegistry_Register_MultipleVersions verifies that distinct
// versions of the same provider name coexist and resolve independently.
func TestVersionedRegistry_Register_MultipleVersions(t *testing.T) {
	vr := NewVersionedRegistry()

	v1 := newMockProvider("alpha", "1.0.0")
	v2 := newMockProvider("alpha", "2.0.0")

	require.NoError(t, vr.Register(v1, withRegVer("1.0.0")))
	require.NoError(t, vr.Register(v2, withRegVer("2.0.0")))

	got1, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, v1, got1)

	got2, ok := vr.Get("alpha", WithVersionOrConstraint("2.0.0"))
	require.True(t, ok)
	assert.Same(t, v2, got2)

	// "latest" resolves to the highest version.
	gotLatest, ok := vr.Get("alpha", WithVersionOrConstraint(VersionLatest))
	require.True(t, ok)
	assert.Same(t, v2, gotLatest)
}

// TestVersionedRegistry_Register_CatalogIsolation verifies that the same name
// and version in different catalogs are independent entries. The empty catalog
// ("") is a legitimate first-class key (the default when WithCatalogName is not
// supplied), isolated from any named catalog.
func TestVersionedRegistry_Register_CatalogIsolation(t *testing.T) {
	vr := NewVersionedRegistry()

	def := newMockProvider("alpha", "1.0.0") // empty catalog ""
	a := newMockProvider("alpha", "1.0.0")   // catalog "cat-a"
	b := newMockProvider("alpha", "1.0.0")   // catalog "cat-b"

	require.NoError(t, vr.Register(def, withRegVer("1.0.0"))) // no WithCatalogName -> ""
	require.NoError(t, vr.Register(a, WithCatalogName("cat-a"), withRegVer("1.0.0")))
	require.NoError(t, vr.Register(b, WithCatalogName("cat-b"), withRegVer("1.0.0")))

	// The empty catalog resolves to its own entry, not a named one.
	gotDef, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, def, gotDef)

	// Explicitly asking for the empty catalog is equivalent to omitting it.
	gotDefExplicit, ok := vr.Get("alpha", WithCatalogName(""), WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, def, gotDefExplicit)

	gotA, ok := vr.Get("alpha", WithCatalogName("cat-a"), WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, a, gotA)

	gotB, ok := vr.Get("alpha", WithCatalogName("cat-b"), WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, b, gotB)

	// A catalog that was never registered resolves to nothing.
	_, ok = vr.Get("alpha", WithCatalogName("missing"), WithVersionOrConstraint("1.0.0"))
	assert.False(t, ok)
}

// TestVersionedRegistry_Register_DuplicateOverwrites documents the current
// behavior: re-registering the same name+catalog+version replaces the provider
// without error.
func TestVersionedRegistry_Register_DuplicateOverwrites(t *testing.T) {
	vr := NewVersionedRegistry()

	first := newMockProvider("alpha", "1.0.0")
	second := newMockProvider("alpha", "1.0.0")

	require.NoError(t, vr.Register(first, withRegVer("1.0.0")))
	require.NoError(t, vr.Register(second, withRegVer("1.0.0")))

	got, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, second, got, "second registration should overwrite the first")
}

// TestVersionedRegistry_Register_Concurrent ensures Register is safe under
// concurrent use (run with -race).
func TestVersionedRegistry_Register_Concurrent(t *testing.T) {
	vr := NewVersionedRegistry()

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			p := newMockProvider("alpha", "1.0.0")
			assert.NoError(t, vr.Register(p, WithCatalogName("shared"), withRegVer("1.0.0")))
		}()
	}
	wg.Wait()

	_, ok := vr.Get("alpha", WithCatalogName("shared"), WithVersionOrConstraint("1.0.0"))
	assert.True(t, ok)
}

// TestVersionedRegistry_Get exercises the version/constraint resolution paths of
// Get against a registry seeded with several versions of one provider name.
func TestVersionedRegistry_Get(t *testing.T) {
	// Seed versions shared by the table cases.
	seed := []string{"1.0.0", "1.2.0", "1.5.0", "2.0.0", "2.1.0"}

	tests := []struct {
		name        string
		versionOpt  string // value for WithVersionOrConstraint; unset means omit the option
		omitOpt     bool
		wantVersion string // expected resolved version; "" means expect not found
	}{
		{name: "empty resolves latest", versionOpt: "", wantVersion: "2.1.0"},
		{name: "omitted option resolves latest", omitOpt: true, wantVersion: "2.1.0"},
		{name: "literal latest resolves highest", versionOpt: VersionLatest, wantVersion: "2.1.0"},
		{name: "exact version", versionOpt: "1.2.0", wantVersion: "1.2.0"},
		{name: "exact highest", versionOpt: "2.1.0", wantVersion: "2.1.0"},
		{name: "exact absent returns not found", versionOpt: "9.9.9", wantVersion: ""},
		{name: "caret constraint picks highest in major", versionOpt: "^1.0.0", wantVersion: "1.5.0"},
		{name: "tilde constraint picks highest patch", versionOpt: "~1.2.0", wantVersion: "1.2.0"},
		{name: "range constraint picks highest match", versionOpt: ">=1.2.0, <2.0.0", wantVersion: "1.5.0"},
		{name: "wildcard minor picks highest", versionOpt: "2.x", wantVersion: "2.1.0"},
		{name: "constraint with no match returns not found", versionOpt: ">=3.0.0", wantVersion: ""},
		{name: "invalid version string returns not found", versionOpt: "not-a-version", wantVersion: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := NewVersionedRegistry()
			for _, v := range seed {
				require.NoError(t, vr.Register(newMockProvider("alpha", v), withRegVer(v)))
			}

			var opts []VersionedRegistryOptionFunc
			if !tt.omitOpt {
				opts = append(opts, WithVersionOrConstraint(tt.versionOpt))
			}

			got, ok := vr.Get("alpha", opts...)
			if tt.wantVersion == "" {
				assert.False(t, ok)
				assert.Nil(t, got)
				return
			}
			require.True(t, ok)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVersion, got.Descriptor().Version.String())
		})
	}
}

// TestVersionedRegistry_Get_UnknownName verifies that querying an unregistered
// name returns not found for every resolution mode.
func TestVersionedRegistry_Get_UnknownName(t *testing.T) {
	vr := NewVersionedRegistry()
	require.NoError(t, vr.Register(newMockProvider("alpha", "1.0.0"), withRegVer("1.0.0")))

	for _, opt := range []string{"", VersionLatest, "1.0.0", "^1.0.0"} {
		got, ok := vr.Get("missing", WithVersionOrConstraint(opt))
		assert.False(t, ok, "opt %q", opt)
		assert.Nil(t, got, "opt %q", opt)
	}
}

// TestVersionedRegistry_Get_CatalogScoped verifies that Get resolves within the
// requested catalog and does not leak across catalogs.
func TestVersionedRegistry_Get_CatalogScoped(t *testing.T) {
	vr := NewVersionedRegistry()

	catA := newMockProvider("alpha", "1.0.0")
	catB := newMockProvider("alpha", "2.0.0")

	require.NoError(t, vr.Register(catA, WithCatalogName("cat-a"), withRegVer("1.0.0")))
	require.NoError(t, vr.Register(catB, WithCatalogName("cat-b"), withRegVer("2.0.0")))

	// latest within each catalog resolves to that catalog's only version.
	gotA, ok := vr.Get("alpha", WithCatalogName("cat-a"))
	require.True(t, ok)
	assert.Same(t, catA, gotA)

	gotB, ok := vr.Get("alpha", WithCatalogName("cat-b"))
	require.True(t, ok)
	assert.Same(t, catB, gotB)

	// cat-b's 2.0.0 is not visible when scoped to cat-a.
	_, ok = vr.Get("alpha", WithCatalogName("cat-a"), WithVersionOrConstraint("2.0.0"))
	assert.False(t, ok)
}

// TestVersionedRegistry_Unregister_RemovesEntry verifies that Unregister removes
// the provider so it is no longer retrievable.
func TestVersionedRegistry_Unregister_RemovesEntry(t *testing.T) {
	vr := NewVersionedRegistry()
	p := newMockProvider("alpha", "1.0.0")
	require.NoError(t, vr.Register(p, withRegVer("1.0.0")))

	_, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)

	require.NoError(t, vr.Unregister("alpha", semver.MustParse("1.0.0")))

	_, ok = vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	assert.False(t, ok)
	// latest also finds nothing once the only version is gone.
	_, ok = vr.Get("alpha")
	assert.False(t, ok)
}

// TestVersionedRegistry_Unregister_NilVersion verifies the nil-version guard.
func TestVersionedRegistry_Unregister_NilVersion(t *testing.T) {
	vr := NewVersionedRegistry()
	require.NoError(t, vr.Register(newMockProvider("alpha", "1.0.0"), withRegVer("1.0.0")))

	err := vr.Unregister("alpha", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version cannot be nil")

	// The existing entry is untouched.
	_, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	assert.True(t, ok)
}

// TestVersionedRegistry_Unregister_LeavesOtherVersions verifies that removing
// one version does not disturb the others, and that latest re-resolves.
func TestVersionedRegistry_Unregister_LeavesOtherVersions(t *testing.T) {
	vr := NewVersionedRegistry()
	v1 := newMockProvider("alpha", "1.0.0")
	v2 := newMockProvider("alpha", "2.0.0")
	require.NoError(t, vr.Register(v1, withRegVer("1.0.0")))
	require.NoError(t, vr.Register(v2, withRegVer("2.0.0")))

	require.NoError(t, vr.Unregister("alpha", semver.MustParse("2.0.0")))

	// 2.0.0 is gone.
	_, ok := vr.Get("alpha", WithVersionOrConstraint("2.0.0"))
	assert.False(t, ok)

	// 1.0.0 remains and is now the latest.
	got, ok := vr.Get("alpha", WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, v1, got)

	gotLatest, ok := vr.Get("alpha")
	require.True(t, ok)
	assert.Same(t, v1, gotLatest)
}

// TestVersionedRegistry_Unregister_NoOps verifies that unregistering an absent
// version, name, or catalog is a no-op that does not error.
func TestVersionedRegistry_Unregister_NoOps(t *testing.T) {
	vr := NewVersionedRegistry()
	p := newMockProvider("alpha", "1.0.0")
	require.NoError(t, vr.Register(p, WithCatalogName("cat-a"), withRegVer("1.0.0")))

	// Absent version under an existing name/catalog.
	require.NoError(t, vr.Unregister("alpha", semver.MustParse("9.9.9"), WithCatalogName("cat-a")))
	// Absent name.
	require.NoError(t, vr.Unregister("missing", semver.MustParse("1.0.0")))
	// Existing name+version but wrong catalog.
	require.NoError(t, vr.Unregister("alpha", semver.MustParse("1.0.0")))

	// The real entry survives all of the above.
	got, ok := vr.Get("alpha", WithCatalogName("cat-a"), WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, p, got)
}

// TestVersionedRegistry_Unregister_CatalogScoped verifies that Unregister only
// removes the entry in the requested catalog.
func TestVersionedRegistry_Unregister_CatalogScoped(t *testing.T) {
	vr := NewVersionedRegistry()
	a := newMockProvider("alpha", "1.0.0")
	b := newMockProvider("alpha", "1.0.0")
	require.NoError(t, vr.Register(a, WithCatalogName("cat-a"), withRegVer("1.0.0")))
	require.NoError(t, vr.Register(b, WithCatalogName("cat-b"), withRegVer("1.0.0")))

	require.NoError(t, vr.Unregister("alpha", semver.MustParse("1.0.0"), WithCatalogName("cat-a")))

	_, ok := vr.Get("alpha", WithCatalogName("cat-a"), WithVersionOrConstraint("1.0.0"))
	assert.False(t, ok)

	// cat-b is untouched.
	got, ok := vr.Get("alpha", WithCatalogName("cat-b"), WithVersionOrConstraint("1.0.0"))
	require.True(t, ok)
	assert.Same(t, b, got)
}

// TestVersionedRegistry_Unregister_Concurrent ensures Unregister is safe under
// concurrent use alongside Register (run with -race).
func TestVersionedRegistry_Unregister_Concurrent(t *testing.T) {
	vr := NewVersionedRegistry()

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			p := newMockProvider("alpha", "1.0.0")
			require.NoError(t, vr.Register(p, WithCatalogName("shared"), withRegVer("1.0.0")))
			assert.NoError(t, vr.Unregister("alpha", semver.MustParse("1.0.0"), WithCatalogName("shared")))
		}()
	}
	wg.Wait()
}

// TestProviderRegistry_RegisterBuiltin verifies that a built-in registers under
// its bare name and is retrievable via GetBuiltin.
func TestProviderRegistry_RegisterBuiltin(t *testing.T) {
	r := NewCompositeRegistry()
	p := newMockProvider("shell", "1.0.0")

	require.NoError(t, r.RegisterBase(p))

	got, ok := r.GetBase("shell")
	require.True(t, ok)
	assert.Same(t, p, got)
}

// TestProviderRegistry_RegisterBuiltin_DuplicateRejected verifies that a second
// built-in of the same name is rejected rather than silently overwritten.
func TestProviderRegistry_RegisterBuiltin_DuplicateRejected(t *testing.T) {
	r := NewCompositeRegistry()
	first := newMockProvider("shell", "1.0.0")
	second := newMockProvider("shell", "2.0.0")

	require.NoError(t, r.RegisterBase(first))

	err := r.RegisterBase(second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// The original registration is untouched.
	got, ok := r.GetBase("shell")
	require.True(t, ok)
	assert.Same(t, first, got)
}

// TestProviderRegistry_RegisterBuiltin_InvalidDescriptor verifies that the
// built-in tier enforces descriptor validation (delegated to Registry).
func TestProviderRegistry_RegisterBuiltin_InvalidDescriptor(t *testing.T) {
	r := NewCompositeRegistry()

	tests := []struct {
		name        string
		provider    Provider
		errContains string
	}{
		{
			name:        "nil provider",
			provider:    nil,
			errContains: "cannot register nil provider",
		},
		{
			name:        "nil descriptor",
			provider:    &mockProvider{descriptor: nil},
			errContains: "descriptor cannot be nil",
		},
		{
			name:        "nil version",
			provider:    newNilVersionProvider("shell"),
			errContains: "version cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.RegisterBase(tt.provider)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestProviderRegistry_GetBuiltin_Unknown verifies that an unregistered built-in
// name resolves to not found.
func TestProviderRegistry_GetBuiltin_Unknown(t *testing.T) {
	r := NewCompositeRegistry()
	require.NoError(t, r.RegisterBase(newMockProvider("shell", "1.0.0")))

	got, ok := r.GetBase("missing")
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestProviderRegistry_External delegates registration, lookup, and removal to
// the versioned tier keyed by {catalog, name, version}.
func TestProviderRegistry_External(t *testing.T) {
	r := NewCompositeRegistry()
	p := newMockProvider("http", "1.2.0")

	require.NoError(t, r.RegisterExternal(p, WithCatalogName("cat-a"), withRegVer("1.2.0")))

	got, ok := r.GetExternal("http",
		WithCatalogName("cat-a"),
		WithVersionOrConstraint("1.2.0"),
	)
	require.True(t, ok)
	assert.Same(t, p, got)

	require.NoError(t, r.UnregisterExternal("http", semver.MustParse("1.2.0"), WithCatalogName("cat-a")))

	_, ok = r.GetExternal("http", WithCatalogName("cat-a"), WithVersionOrConstraint("1.2.0"))
	assert.False(t, ok)
}

// TestProviderRegistry_External_ConstraintResolution verifies that the external
// tier resolves version constraints to the highest satisfying version.
func TestProviderRegistry_External_ConstraintResolution(t *testing.T) {
	r := NewCompositeRegistry()
	for _, v := range []string{"1.0.0", "1.5.0", "2.0.0"} {
		require.NoError(t, r.RegisterExternal(newMockProvider("http", v), WithCatalogName("cat-a"), withRegVer(v)))
	}

	got, ok := r.GetExternal("http",
		WithCatalogName("cat-a"),
		WithVersionOrConstraint("^1.0.0"),
	)
	require.True(t, ok)
	assert.Equal(t, "1.5.0", got.Descriptor().Version.String())
}

// TestProviderRegistry_TierIsolation verifies that the built-in and external
// tiers are independent: the same name in one tier does not shadow or collide
// with the other.
func TestProviderRegistry_TierIsolation(t *testing.T) {
	r := NewCompositeRegistry()

	builtin := newMockProvider("json", "1.0.0")
	external := newMockProvider("json", "2.0.0")

	require.NoError(t, r.RegisterBase(builtin))
	require.NoError(t, r.RegisterExternal(external, WithCatalogName("cat-a"), withRegVer("2.0.0")))

	// Built-in tier resolves by name only.
	gotBuiltin, ok := r.GetBase("json")
	require.True(t, ok)
	assert.Same(t, builtin, gotBuiltin)

	// External tier resolves by catalog + version, independently.
	gotExternal, ok := r.GetExternal("json",
		WithCatalogName("cat-a"),
		WithVersionOrConstraint("2.0.0"),
	)
	require.True(t, ok)
	assert.Same(t, external, gotExternal)

	// The external name is not visible in the built-in tier's catalog space and
	// vice versa: GetBuiltin never consults the external tier.
	_, ok = r.GetExternal("json", WithVersionOrConstraint("1.0.0"))
	assert.False(t, ok, "built-in must not leak into the external (empty-catalog) tier")
}

// TestProviderRegistry_ExternalDoesNotAffectBuiltin verifies that churning the
// external tier (register/unregister) never disturbs a registered built-in.
func TestProviderRegistry_ExternalDoesNotAffectBuiltin(t *testing.T) {
	r := NewCompositeRegistry()
	builtin := newMockProvider("shell", "1.0.0")
	require.NoError(t, r.RegisterBase(builtin))

	external := newMockProvider("shell", "9.9.9")
	require.NoError(t, r.RegisterExternal(external, WithCatalogName("cat-a"), withRegVer("9.9.9")))
	require.NoError(t, r.UnregisterExternal("shell", semver.MustParse("9.9.9"), WithCatalogName("cat-a")))

	// The built-in survives external churn untouched.
	got, ok := r.GetBase("shell")
	require.True(t, ok)
	assert.Same(t, builtin, got)
}

// TestVersionedRegistry_Has verifies that Has mirrors Get: it reports presence
// only when a concrete version resolves for the requested catalog/constraint.
func TestVersionedRegistry_Has(t *testing.T) {
	vr := NewVersionedRegistry()
	for _, v := range []string{"1.0.0", "1.5.0", "2.0.0"} {
		require.NoError(t, vr.Register(newMockProvider("alpha", v), WithCatalogName("cat-a"), withRegVer(v)))
	}

	tests := []struct {
		name    string
		catalog string
		version string
		want    bool
	}{
		{name: "latest resolves", catalog: "cat-a", version: "", want: true},
		{name: "exact present", catalog: "cat-a", version: "1.5.0", want: true},
		{name: "exact absent", catalog: "cat-a", version: "9.9.9", want: false},
		{name: "constraint satisfied", catalog: "cat-a", version: "^1.0.0", want: true},
		{name: "constraint unsatisfied", catalog: "cat-a", version: ">=3.0.0", want: false},
		{name: "wrong catalog", catalog: "cat-b", version: "1.5.0", want: false},
		{name: "unparseable constraint", catalog: "cat-a", version: "not-a-version", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vr.Has("alpha",
				WithCatalogName(tt.catalog),
				WithVersionOrConstraint(tt.version),
			)
			assert.Equal(t, tt.want, got)
		})
	}

	// Unknown name is never present.
	assert.False(t, vr.Has("missing", WithCatalogName("cat-a")))
}

// TestProviderRegistry_HasExternal verifies that HasExternal delegates to the
// external tier and is catalog/version aware.
func TestProviderRegistry_HasExternal(t *testing.T) {
	r := NewCompositeRegistry()
	require.NoError(t, r.RegisterExternal(newMockProvider("http", "1.2.0"), WithCatalogName("cat-a"), withRegVer("1.2.0")))

	assert.True(t, r.HasExternal("http",
		WithCatalogName("cat-a"),
		WithVersionOrConstraint("1.2.0"),
	))
	assert.True(t, r.HasExternal("http",
		WithCatalogName("cat-a"),
		WithVersionOrConstraint("^1.0.0"),
	))
	// Wrong catalog, wrong version, and unknown name are all absent.
	assert.False(t, r.HasExternal("http", WithCatalogName("cat-b"), WithVersionOrConstraint("1.2.0")))
	assert.False(t, r.HasExternal("http", WithCatalogName("cat-a"), WithVersionOrConstraint("2.0.0")))
	assert.False(t, r.HasExternal("missing", WithCatalogName("cat-a")))

	// A built-in of the same name is not visible through the external door.
	require.NoError(t, r.RegisterBase(newMockProvider("http", "9.9.9")))
	assert.False(t, r.HasExternal("http"),
		"built-in must not satisfy an external presence check")
}
