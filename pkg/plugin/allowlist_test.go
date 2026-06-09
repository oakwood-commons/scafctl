// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowedPlugin_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   AllowedPlugin
		wantErr string
	}{
		{name: "valid official/exec", input: "official/exec"},
		{name: "valid internal/my-plugin", input: "internal/my-plugin"},
		{name: "valid with numbers", input: "catalog1/provider2"},
		{name: "empty", input: "", wantErr: "must not be empty"},
		{name: "no slash", input: "exec", wantErr: "must be in catalog/plugin format"},
		{name: "uppercase", input: "Official/Exec", wantErr: "does not match required pattern"},
		{name: "spaces", input: "official/ exec", wantErr: "does not match required pattern"},
		{name: "trailing slash", input: "official/", wantErr: "does not match required pattern"},
		{name: "leading slash", input: "/exec", wantErr: "does not match required pattern"},
		{name: "double slash", input: "official//exec", wantErr: "does not match required pattern"},
		{name: "underscores", input: "my_catalog/my_plugin", wantErr: "does not match required pattern"},
		{name: "single char segments", input: "a/b", wantErr: "does not match required pattern"},
		{name: "starts with hyphen", input: "-catalog/exec", wantErr: "does not match required pattern"},
		{name: "ends with hyphen", input: "catalog-/exec", wantErr: "does not match required pattern"},
		{name: "wildcard official/*", input: "official/*"},
		{name: "wildcard internal/*", input: "internal/*"},
		{name: "bare wildcard", input: "*/exec", wantErr: "does not match required pattern"},
		{name: "double wildcard", input: "*/*", wantErr: "does not match required pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAllowedPlugin_Catalog(t *testing.T) {
	tests := []struct {
		input AllowedPlugin
		want  string
	}{
		{"official/exec", "official"},
		{"internal/my-plugin", "internal"},
		{"no-slash", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.input.Catalog())
	}
}

func TestAllowedPlugin_Plugin(t *testing.T) {
	tests := []struct {
		input AllowedPlugin
		want  string
	}{
		{"official/exec", "exec"},
		{"internal/my-plugin", "my-plugin"},
		{"no-slash", ""},
		{"official/*", "*"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.input.Plugin())
	}
}

func TestAllowedPlugin_IsWildcard(t *testing.T) {
	assert.True(t, AllowedPlugin("official/*").IsWildcard())
	assert.True(t, AllowedPlugin("internal/*").IsWildcard())
	assert.False(t, AllowedPlugin("official/exec").IsWildcard())
	assert.False(t, AllowedPlugin("official/git").IsWildcard())
}

func TestParseAllowedPlugins(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []AllowedPlugin
		wantErr string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty input",
			input: []string{},
			want:  nil,
		},
		{
			name:  "valid entries",
			input: []string{"official/exec", "official/git", "internal/custom"},
			want:  []AllowedPlugin{"official/exec", "official/git", "internal/custom"},
		},
		{
			name:    "invalid entry fails early",
			input:   []string{"official/exec", "bad"},
			wantErr: "must be in catalog/plugin format",
		},
		{
			name:  "wildcard entry",
			input: []string{"official/*"},
			want:  []AllowedPlugin{"official/*"},
		},
		{
			name:  "mixed wildcard and explicit",
			input: []string{"official/*", "internal/custom"},
			want:  []AllowedPlugin{"official/*", "internal/custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAllowedPlugins(tt.input)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGroupByCatalog(t *testing.T) {
	tests := []struct {
		name  string
		input []AllowedPlugin
		want  map[string]catalog.PluginPolicy
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "single catalog explicit",
			input: []AllowedPlugin{"official/exec", "official/git"},
			want: map[string]catalog.PluginPolicy{
				"official": {Plugins: []string{"exec", "git"}},
			},
		},
		{
			name:  "multiple catalogs explicit",
			input: []AllowedPlugin{"official/exec", "internal/custom", "official/git"},
			want: map[string]catalog.PluginPolicy{
				"official": {Plugins: []string{"exec", "git"}},
				"internal": {Plugins: []string{"custom"}},
			},
		},
		{
			name:  "wildcard catalog",
			input: []AllowedPlugin{"official/*"},
			want: map[string]catalog.PluginPolicy{
				"official": {AllowAll: true},
			},
		},
		{
			name:  "wildcard overrides explicit same catalog",
			input: []AllowedPlugin{"official/exec", "official/*"},
			want: map[string]catalog.PluginPolicy{
				"official": {AllowAll: true},
			},
		},
		{
			name:  "explicit after wildcard ignored",
			input: []AllowedPlugin{"official/*", "official/exec"},
			want: map[string]catalog.PluginPolicy{
				"official": {AllowAll: true},
			},
		},
		{
			name:  "mixed wildcard and explicit different catalogs",
			input: []AllowedPlugin{"official/*", "internal/custom"},
			want: map[string]catalog.PluginPolicy{
				"official": {AllowAll: true},
				"internal": {Plugins: []string{"custom"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GroupByCatalog(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBareNames(t *testing.T) {
	tests := []struct {
		name  string
		input []AllowedPlugin
		want  []string
	}{
		{name: "nil", input: nil, want: nil},
		{
			name:  "extracts plugin names",
			input: []AllowedPlugin{"official/exec", "internal/custom"},
			want:  []string{"exec", "custom"},
		},
		{
			name:  "skips wildcards",
			input: []AllowedPlugin{"official/*", "internal/custom"},
			want:  []string{"custom"},
		},
		{
			name:  "all wildcards returns nil",
			input: []AllowedPlugin{"official/*", "internal/*"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BareNames(tt.input))
		})
	}
}

func TestCatalogNames(t *testing.T) {
	tests := []struct {
		name  string
		input []AllowedPlugin
		want  []string
	}{
		{name: "nil", input: nil, want: nil},
		{
			name:  "deduplicates catalogs",
			input: []AllowedPlugin{"official/exec", "official/git", "internal/custom"},
			want:  []string{"official", "internal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CatalogNames(tt.input))
		})
	}
}

func TestPolicyPlugins(t *testing.T) {
	policies := map[string]catalog.PluginPolicy{
		"official": {AllowAll: true},
		"internal": {Plugins: []string{"custom", "git"}},
	}

	// Wildcard catalog returns nil (unrestricted)
	assert.Nil(t, PolicyPlugins(policies, "official"))

	// Explicit catalog returns plugin list
	assert.Equal(t, []string{"custom", "git"}, PolicyPlugins(policies, "internal"))

	// Absent catalog returns nil
	assert.Nil(t, PolicyPlugins(policies, "unknown"))

	// Nil map returns nil
	assert.Nil(t, PolicyPlugins(nil, "official"))
}
