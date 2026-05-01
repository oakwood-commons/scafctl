// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package official

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_Contains10Providers(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 10, r.Len())
}

func TestNewRegistry_AllNamesPresent(t *testing.T) {
	r := NewRegistry()

	expected := []string{
		"directory", "env", "exec", "git", "github", "hcl",
		"identity", "metadata", "secret", "sleep",
	}
	assert.Equal(t, expected, r.Names())
}

func TestRegistry_Get(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantFound bool
		wantName  string
	}{
		{name: "known provider", query: "exec", wantFound: true, wantName: "exec"},
		{name: "another known provider", query: "github", wantFound: true, wantName: "github"},
		{name: "unknown provider", query: "nonexistent", wantFound: false},
		{name: "empty string", query: "", wantFound: false},
		{name: "builtin provider not in list", query: "cel", wantFound: false},
		{name: "builtin data-primitive not in list", query: "static", wantFound: false},
	}

	r := NewRegistry()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := r.Get(tt.query)
			assert.Equal(t, tt.wantFound, ok)
			if tt.wantFound {
				assert.Equal(t, tt.wantName, p.Name)
				assert.NotEmpty(t, p.CatalogRef)
				assert.NotEmpty(t, p.DefaultVersion)
			}
		})
	}
}

func TestRegistry_Has(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "known", query: "exec", want: true},
		{name: "unknown", query: "terraform", want: false},
		{name: "builtin not extracted", query: "file", want: false},
	}

	r := NewRegistry()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, r.Has(tt.query))
		})
	}
}

func TestRegistry_Names_Sorted(t *testing.T) {
	r := NewRegistry()
	names := r.Names()

	require.NotEmpty(t, names)
	for i := 1; i < len(names); i++ {
		assert.Less(t, names[i-1], names[i], "Names() must return sorted results")
	}
}

func TestNewRegistryFrom_CustomProviders(t *testing.T) {
	custom := []Provider{
		{Name: "aws", CatalogRef: "aws", DefaultVersion: ">=1.0.0"},
		{Name: "vault", CatalogRef: "vault", DefaultVersion: ">=2.0.0"},
	}

	r := NewRegistryFrom(custom)
	assert.Equal(t, 2, r.Len())
	assert.True(t, r.Has("aws"))
	assert.True(t, r.Has("vault"))
	assert.False(t, r.Has("static"))
}

func TestNewRegistryFrom_Empty(t *testing.T) {
	r := NewRegistryFrom(nil)
	assert.Equal(t, 0, r.Len())
	assert.Empty(t, r.Names())
	assert.False(t, r.Has("static"))
}

func TestNewRegistryFrom_Deduplicates(t *testing.T) {
	dupes := []Provider{
		{Name: "foo", CatalogRef: "foo-v1", DefaultVersion: ">=1.0.0"},
		{Name: "foo", CatalogRef: "foo-v2", DefaultVersion: ">=2.0.0"},
	}

	r := NewRegistryFrom(dupes)
	assert.Equal(t, 1, r.Len())

	p, ok := r.Get("foo")
	require.True(t, ok)
	assert.Equal(t, "foo-v2", p.CatalogRef, "last entry wins on duplicate name")
}

func TestDefaultProviders_ReturnsCopy(t *testing.T) {
	a := DefaultProviders()
	b := DefaultProviders()

	require.Equal(t, len(a), len(b))

	// Mutating the returned slice must not affect subsequent calls.
	a[0].Name = "mutated"
	assert.NotEqual(t, a[0].Name, b[0].Name)
}

func TestDefaultProviders_Count(t *testing.T) {
	assert.Equal(t, 10, len(DefaultProviders()))
}

func TestDefaultProviders_ExtendPattern(t *testing.T) {
	extended := append(DefaultProviders(),
		Provider{Name: "aws", CatalogRef: "aws", DefaultVersion: ">=1.0.0"},
	)

	r := NewRegistryFrom(extended)
	assert.Equal(t, 11, r.Len())
	assert.True(t, r.Has("exec"))
	assert.True(t, r.Has("aws"))
}

func TestProvider_ToPluginDependency(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     solution.PluginDependency
	}{
		{
			name:     "standard provider",
			provider: Provider{Name: "static", CatalogRef: "static", DefaultVersion: "latest"},
			want: solution.PluginDependency{
				Name:    "static",
				Kind:    solution.PluginKindProvider,
				Version: "latest",
			},
		},
		{
			name:     "custom catalog ref",
			provider: Provider{Name: "aws", CatalogRef: "aws-provider", DefaultVersion: ">=1.0.0"},
			want: solution.PluginDependency{
				Name:    "aws-provider",
				Kind:    solution.PluginKindProvider,
				Version: ">=1.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.provider.ToPluginDependency()
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.Kind, got.Kind)
			assert.Equal(t, tt.want.Version, got.Version)
			assert.Nil(t, got.Defaults, "auto-resolved providers have no defaults")
		})
	}
}

func TestProvider_ToPluginDependency_AllDefaults(t *testing.T) {
	for _, p := range DefaultProviders() {
		t.Run(p.Name, func(t *testing.T) {
			dep := p.ToPluginDependency()
			assert.Equal(t, p.CatalogRef, dep.Name)
			assert.Equal(t, solution.PluginKindProvider, dep.Kind)
			assert.Equal(t, p.DefaultVersion, dep.Version)
		})
	}
}

func TestRegistry_Len(t *testing.T) {
	tests := []struct {
		name      string
		providers []Provider
		want      int
	}{
		{name: "default", providers: DefaultProviders(), want: 10},
		{name: "empty", providers: nil, want: 0},
		{name: "custom", providers: []Provider{{Name: "a"}, {Name: "b"}}, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistryFrom(tt.providers)
			assert.Equal(t, tt.want, r.Len())
		})
	}
}

func BenchmarkRegistry_Get(b *testing.B) {
	r := NewRegistry()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		r.Get("exec")
	}
}

func BenchmarkRegistry_Has(b *testing.B) {
	r := NewRegistry()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		r.Has("nonexistent")
	}
}

func BenchmarkRegistry_Names(b *testing.B) {
	r := NewRegistry()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		r.Names()
	}
}

// ── Context helpers ──────────────────────────────────────────────────────────

func TestWithRegistry_RoundTrip(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	ctx := WithRegistry(t.Context(), reg)
	got := RegistryFromContext(ctx)

	require.NotNil(t, got)
	assert.Equal(t, reg, got)
}

func TestRegistryFromContext_NilWhenMissing(t *testing.T) {
	t.Parallel()

	got := RegistryFromContext(t.Context())
	assert.Nil(t, got)
}

func TestWithRegistry_CustomRegistry(t *testing.T) {
	t.Parallel()

	custom := NewRegistryFrom([]Provider{
		{Name: "mycli-extra", CatalogRef: "mycli-extra", DefaultVersion: ">=1.0.0"},
	})
	ctx := WithRegistry(t.Context(), custom)
	got := RegistryFromContext(ctx)

	require.NotNil(t, got)
	assert.Equal(t, 1, got.Len())
	assert.True(t, got.Has("mycli-extra"))
}
