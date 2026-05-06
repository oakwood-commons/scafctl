// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package official

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_Contains3Handlers(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 3, r.Len())
}

func TestNewRegistry_AllNamesPresent(t *testing.T) {
	r := NewRegistry()

	expected := []string{"entra", "gcp", "github"}
	assert.Equal(t, expected, r.Names())
}

func TestRegistry_Get(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantFound bool
		wantName  string
	}{
		{name: "known handler github", query: "github", wantFound: true, wantName: "github"},
		{name: "known handler entra", query: "entra", wantFound: true, wantName: "entra"},
		{name: "known handler gcp", query: "gcp", wantFound: true, wantName: "gcp"},
		{name: "unknown handler", query: "nonexistent", wantFound: false},
		{name: "empty string", query: "", wantFound: false},
		{name: "builtin handler not extracted", query: "oauth2", wantFound: false},
	}

	r := NewRegistry()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ok := r.Get(tt.query)
			assert.Equal(t, tt.wantFound, ok)
			if tt.wantFound {
				assert.Equal(t, tt.wantName, h.Name)
				assert.NotEmpty(t, h.CatalogRef)
				assert.NotEmpty(t, h.DefaultVersion)
				assert.NotEmpty(t, h.MinSDKVersion)
				assert.NotEmpty(t, h.TrustedVerificationDomains)
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
		{name: "known github", query: "github", want: true},
		{name: "known entra", query: "entra", want: true},
		{name: "known gcp", query: "gcp", want: true},
		{name: "unknown", query: "aws", want: false},
		{name: "builtin not extracted", query: "oauth2", want: false},
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

func TestNewRegistryFrom_CustomHandlers(t *testing.T) {
	custom := []AuthHandler{
		{Name: "okta", CatalogRef: "okta", DefaultVersion: ">=1.0.0", MinSDKVersion: "0.2.0"},
		{Name: "auth0", CatalogRef: "auth0", DefaultVersion: ">=2.0.0", MinSDKVersion: "0.2.0"},
	}

	r := NewRegistryFrom(custom)
	assert.Equal(t, 2, r.Len())
	assert.True(t, r.Has("okta"))
	assert.True(t, r.Has("auth0"))
	assert.False(t, r.Has("github"))
}

func TestNewRegistryFrom_Empty(t *testing.T) {
	r := NewRegistryFrom(nil)
	assert.Equal(t, 0, r.Len())
	assert.Empty(t, r.Names())
	assert.False(t, r.Has("github"))
}

func TestNewRegistryFrom_Deduplicates(t *testing.T) {
	dupes := []AuthHandler{
		{Name: "foo", CatalogRef: "foo-v1", DefaultVersion: ">=1.0.0"},
		{Name: "foo", CatalogRef: "foo-v2", DefaultVersion: ">=2.0.0"},
	}

	r := NewRegistryFrom(dupes)
	assert.Equal(t, 1, r.Len())

	h, ok := r.Get("foo")
	require.True(t, ok)
	assert.Equal(t, "foo-v2", h.CatalogRef, "last entry wins on duplicate name")
}

func TestDefaultAuthHandlers_ReturnsCopy(t *testing.T) {
	a := DefaultAuthHandlers()
	b := DefaultAuthHandlers()

	require.Equal(t, len(a), len(b))

	// Mutating the returned slice must not affect subsequent calls.
	a[0].Name = "mutated"
	assert.NotEqual(t, a[0].Name, b[0].Name)
}

func TestDefaultAuthHandlers_Count(t *testing.T) {
	assert.Equal(t, 3, len(DefaultAuthHandlers()))
}

func TestDefaultAuthHandlers_ExtendPattern(t *testing.T) {
	extended := append(DefaultAuthHandlers(),
		AuthHandler{Name: "okta", CatalogRef: "okta", DefaultVersion: ">=1.0.0", MinSDKVersion: "0.2.0"},
	)

	r := NewRegistryFrom(extended)
	assert.Equal(t, 4, r.Len())
	assert.True(t, r.Has("github"))
	assert.True(t, r.Has("okta"))
}

func TestAuthHandler_ToPluginDependency(t *testing.T) {
	tests := []struct {
		name    string
		handler AuthHandler
		want    solution.PluginDependency
	}{
		{
			name:    "standard handler",
			handler: AuthHandler{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
			want: solution.PluginDependency{
				Name:    "github",
				Kind:    solution.PluginKindAuthHandler,
				Version: "latest",
			},
		},
		{
			name:    "custom catalog ref",
			handler: AuthHandler{Name: "okta", CatalogRef: "okta-handler", DefaultVersion: ">=1.0.0"},
			want: solution.PluginDependency{
				Name:    "okta-handler",
				Kind:    solution.PluginKindAuthHandler,
				Version: ">=1.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.handler.ToPluginDependency()
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.Kind, got.Kind)
			assert.Equal(t, tt.want.Version, got.Version)
			assert.Nil(t, got.Defaults, "auto-resolved handlers have no defaults")
		})
	}
}

func TestAuthHandler_ToPluginDependency_AllDefaults(t *testing.T) {
	for _, h := range DefaultAuthHandlers() {
		t.Run(h.Name, func(t *testing.T) {
			dep := h.ToPluginDependency()
			assert.Equal(t, h.CatalogRef, dep.Name)
			assert.Equal(t, solution.PluginKindAuthHandler, dep.Kind)
			assert.Equal(t, h.DefaultVersion, dep.Version)
		})
	}
}

func TestRegistry_Len(t *testing.T) {
	tests := []struct {
		name     string
		handlers []AuthHandler
		want     int
	}{
		{name: "default", handlers: defaultAuthHandlers, want: 3},
		{name: "empty", handlers: nil, want: 0},
		{name: "one", handlers: []AuthHandler{{Name: "x"}}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistryFrom(tt.handlers)
			assert.Equal(t, tt.want, r.Len())
		})
	}
}

func TestRegistry_MustGet_Success(t *testing.T) {
	r := NewRegistry()
	h := r.MustGet("github")
	assert.Equal(t, "github", h.Name)
}

func TestRegistry_MustGet_Panics(t *testing.T) {
	r := NewRegistry()
	assert.Panics(t, func() {
		r.MustGet("nonexistent")
	})
}

func TestWithRegistry_ContextRoundTrip(t *testing.T) {
	r := NewRegistry()
	ctx := WithRegistry(context.Background(), r)

	got := RegistryFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, 3, got.Len())
}

func TestRegistryFromContext_NilWhenNotSet(t *testing.T) {
	got := RegistryFromContext(context.Background())
	assert.Nil(t, got)
}

func TestDefaultAuthHandlers_TrustedDomains(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		handler string
		domains []string
	}{
		{handler: "entra", domains: []string{"login.microsoftonline.com", "login.microsoft.com"}},
		{handler: "gcp", domains: []string{"accounts.google.com"}},
		{handler: "github", domains: []string{"github.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			h, ok := r.Get(tt.handler)
			require.True(t, ok)
			assert.Equal(t, tt.domains, h.TrustedVerificationDomains)
		})
	}
}
