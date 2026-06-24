// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCatalogIdentity_RegistryHash(t *testing.T) {
	tests := []struct {
		name     string
		identity CatalogIdentity
	}{
		{
			name:     "OCI registry with repository",
			identity: CatalogIdentity{Canonical: "ghcr.io/acme/plugins", Alias: "production"},
		},
		{
			name:     "OCI registry without repository",
			identity: CatalogIdentity{Canonical: "ghcr.io", Alias: "hub"},
		},
		{
			name:     "filesystem path",
			identity: CatalogIdentity{Canonical: "/home/user/.config/scafctl/catalog", Alias: "local"},
		},
		{
			name:     "deeply nested repository",
			identity: CatalogIdentity{Canonical: "registry.internal.corp/team/division/plugins", Alias: "internal"},
		},
		{
			name:     "no slashes",
			identity: CatalogIdentity{Canonical: "local", Alias: "local"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.RegistryHash()
			// Must be exactly 16 hex chars.
			assert.Len(t, got, 16)
			assert.Regexp(t, `^[0-9a-f]{16}$`, got)
		})
	}
}

func TestCatalogIdentity_RegistryHash_Deterministic(t *testing.T) {
	id := CatalogIdentity{Canonical: "ghcr.io/acme/plugins"}
	first := id.RegistryHash()
	second := id.RegistryHash()
	assert.Equal(t, first, second)
}

func TestCatalogIdentity_RegistryHash_UniquePerCanonical(t *testing.T) {
	id1 := CatalogIdentity{Canonical: "ghcr.io/acme/plugins"}
	id2 := CatalogIdentity{Canonical: "ghcr.io/other/plugins"}
	assert.NotEqual(t, id1.RegistryHash(), id2.RegistryHash())
}

func TestCatalogIdentity_RegistryHash_AliasIgnored(t *testing.T) {
	id1 := CatalogIdentity{Canonical: "ghcr.io/acme/plugins", Alias: "prod"}
	id2 := CatalogIdentity{Canonical: "ghcr.io/acme/plugins", Alias: "staging"}
	assert.Equal(t, id1.RegistryHash(), id2.RegistryHash())
}

func TestCatalogIdentity_RegistryHash_ZeroIdentity(t *testing.T) {
	id := CatalogIdentity{}
	assert.Equal(t, "", id.RegistryHash())
}

func TestCatalogIdentity_String(t *testing.T) {
	tests := []struct {
		name     string
		identity CatalogIdentity
		want     string
	}{
		{
			name:     "alias differs from canonical",
			identity: CatalogIdentity{Canonical: "ghcr.io/acme/plugins", Alias: "production"},
			want:     "ghcr.io/acme/plugins (production)",
		},
		{
			name:     "alias same as canonical",
			identity: CatalogIdentity{Canonical: "local", Alias: "local"},
			want:     "local",
		},
		{
			name:     "empty alias",
			identity: CatalogIdentity{Canonical: "ghcr.io/acme/plugins"},
			want:     "ghcr.io/acme/plugins",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.identity.String())
		})
	}
}

func TestCatalogIdentity_IsZero(t *testing.T) {
	assert.True(t, CatalogIdentity{}.IsZero())
	assert.True(t, CatalogIdentity{Alias: "production"}.IsZero())
	assert.False(t, CatalogIdentity{Canonical: "ghcr.io/acme/plugins"}.IsZero())
}

// mockRemoteCatalog simulates a catalog with Registry/Repository methods.
type mockRemoteCatalog struct {
	name       string
	registry   string
	repository string
}

func (m *mockRemoteCatalog) Name() string       { return m.name }
func (m *mockRemoteCatalog) Registry() string   { return m.registry }
func (m *mockRemoteCatalog) Repository() string { return m.repository }

// mockLocalCatalog simulates a catalog with a Path method.
type mockLocalCatalog struct {
	name string
	path string
}

func (m *mockLocalCatalog) Name() string { return m.name }
func (m *mockLocalCatalog) Path() string { return m.path }

// mockPlainCatalog has only Name — fallback case.
type mockPlainCatalog struct {
	name string
}

func (m *mockPlainCatalog) Name() string { return m.name }

func TestIdentityFromCatalog(t *testing.T) {
	tests := []struct {
		name string
		cat  interface{ Name() string }
		want CatalogIdentity
	}{
		{
			name: "OCI remote catalog",
			cat:  &mockRemoteCatalog{name: "production", registry: "ghcr.io", repository: "acme/plugins"},
			want: NewCatalogIdentity("ghcr.io/acme/plugins", "production"),
		},
		{
			name: "OCI remote catalog without repository",
			cat:  &mockRemoteCatalog{name: "hub", registry: "ghcr.io", repository: ""},
			want: NewCatalogIdentity("ghcr.io", "hub"),
		},
		{
			name: "local filesystem catalog",
			cat:  &mockLocalCatalog{name: "local", path: "/home/user/.config/scafctl/catalog"},
			want: NewCatalogIdentity("/home/user/.config/scafctl/catalog", "local"),
		},
		{
			name: "plain catalog (fallback)",
			cat:  &mockPlainCatalog{name: "chain"},
			want: NewCatalogIdentity("chain", "chain"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IdentityFromCatalog(tt.cat)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAllowedCanonicals(t *testing.T) {
	catalogs := []interface{ Name() string }{
		&mockRemoteCatalog{name: "production", registry: "ghcr.io", repository: "acme/plugins"},
		&mockRemoteCatalog{name: "internal", registry: "registry.corp", repository: "team/plugins"},
		&mockLocalCatalog{name: "local", path: "/home/user/.config/scafctl/catalog"},
	}

	tests := []struct {
		name    string
		allowed []string
		want    map[string]bool
	}{
		{
			name:    "nil allowed returns nil",
			allowed: nil,
			want:    nil,
		},
		{
			name:    "empty allowed returns nil",
			allowed: []string{},
			want:    nil,
		},
		{
			name:    "resolves aliases to canonicals",
			allowed: []string{"production", "internal"},
			want: map[string]bool{
				"ghcr.io/acme/plugins":       true,
				"registry.corp/team/plugins": true,
			},
		},
		{
			name:    "case insensitive alias matching",
			allowed: []string{"Production", "INTERNAL"},
			want: map[string]bool{
				"ghcr.io/acme/plugins":       true,
				"registry.corp/team/plugins": true,
			},
		},
		{
			name:    "unknown alias treated as literal canonical",
			allowed: []string{"ghcr.io/other/repo"},
			want: map[string]bool{
				"ghcr.io/other/repo": true,
			},
		},
		{
			name:    "mix of aliases and literal canonicals",
			allowed: []string{"production", "ghcr.io/other/repo"},
			want: map[string]bool{
				"ghcr.io/acme/plugins": true,
				"ghcr.io/other/repo":   true,
			},
		},
		{
			name:    "local catalog resolved by path",
			allowed: []string{"local"},
			want: map[string]bool{
				"/home/user/.config/scafctl/catalog": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAllowedCanonicals(tt.allowed, catalogs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkCatalogIdentity_RegistryHash(b *testing.B) {
	id := CatalogIdentity{Canonical: "ghcr.io/acme/plugins", Alias: "production"}
	b.ReportAllocs()
	for b.Loop() {
		_ = id.RegistryHash()
	}
}

func BenchmarkResolveAllowedCanonicals(b *testing.B) {
	catalogs := []interface{ Name() string }{
		&mockRemoteCatalog{name: "production", registry: "ghcr.io", repository: "acme/plugins"},
		&mockRemoteCatalog{name: "internal", registry: "registry.corp", repository: "team/plugins"},
		&mockRemoteCatalog{name: "staging", registry: "staging.corp", repository: "team/plugins"},
	}
	allowed := []string{"production", "internal", "staging"}

	b.ReportAllocs()
	for b.Loop() {
		_ = ResolveAllowedCanonicals(allowed, catalogs)
	}
}
