// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sourced_plugin(name, kind, registry string) solution.PluginDependency {
	return solution.PluginDependency{
		Name: name,
		Kind: solution.PluginKind(kind),
		Source: &solution.PluginSource{
			Registry: registry,
		},
	}
}

func TestLockFile_FindDependency(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{Ref: "deploy-to-k8s@2.0.0", Digest: "sha256:abc123", ResolvedFrom: "default", VendoredAt: ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml"},
			{Ref: "setup-env@1.0.0", Digest: "sha256:def456", ResolvedFrom: "default", VendoredAt: ".scafctl/vendor/setup-env@1.0.0.yaml"},
		},
	}

	t.Run("found", func(t *testing.T) {
		dep := lf.FindDependency("deploy-to-k8s@2.0.0")
		require.NotNil(t, dep)
		assert.Equal(t, "sha256:abc123", dep.Digest)
		assert.Equal(t, "default", dep.ResolvedFrom)
	})

	t.Run("not found", func(t *testing.T) {
		dep := lf.FindDependency("nonexistent@1.0.0")
		assert.Nil(t, dep)
	})

	t.Run("nil lock file", func(t *testing.T) {
		var nilLf *LockFile
		dep := nilLf.FindDependency("anything")
		assert.Nil(t, dep)
	})
}

func TestLockFile_FindPlugin(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{Name: "azure-provider", Kind: "provider", Version: "1.0.0", Digest: "sha256:aaa"},
			{Name: "entra-auth", Kind: "auth-handler", Version: "2.0.0", Digest: "sha256:bbb"},
		},
	}

	t.Run("found by name and kind", func(t *testing.T) {
		p := lf.FindPlugin("azure-provider", "provider")
		require.NotNil(t, p)
		assert.Equal(t, "1.0.0", p.Version)
	})

	t.Run("wrong kind", func(t *testing.T) {
		p := lf.FindPlugin("azure-provider", "auth-handler")
		assert.Nil(t, p)
	})

	t.Run("not found", func(t *testing.T) {
		p := lf.FindPlugin("nonexistent", "provider")
		assert.Nil(t, p)
	})

	t.Run("nil lock file", func(t *testing.T) {
		var nilLf *LockFile
		p := nilLf.FindPlugin("anything", "provider")
		assert.Nil(t, p)
	})
}

func TestLockFile_FindPluginByIdentity(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			// Same name/kind, two distinct canonical origins -- must NOT collapse.
			{Name: "github", Kind: "provider", Version: "1.5.0", ResolvedCanonical: "ghcr.io/orgA"},
			{Name: "github", Kind: "provider", Version: "2.3.0", ResolvedCanonical: "ghcr.io/orgB"},
			// Short-name/local plugin with no portable origin.
			{Name: "echo", Kind: "provider", Version: "1.0.0"},
		},
	}

	t.Run("scopes match to a single canonical origin", func(t *testing.T) {
		got := lf.FindPluginByIdentity("ghcr.io/orgB", "github", "provider")
		require.NotNil(t, got)
		assert.Equal(t, "2.3.0", got.Version)
	})

	t.Run("distinct origin resolves to its own entry", func(t *testing.T) {
		got := lf.FindPluginByIdentity("ghcr.io/orgA", "github", "provider")
		require.NotNil(t, got)
		assert.Equal(t, "1.5.0", got.Version)
	})

	t.Run("empty canonical matches only entries with no origin", func(t *testing.T) {
		got := lf.FindPluginByIdentity("", "echo", "provider")
		require.NotNil(t, got)
		assert.Equal(t, "1.0.0", got.Version)
	})

	t.Run("empty canonical does not match origin-bearing entries", func(t *testing.T) {
		assert.Nil(t, lf.FindPluginByIdentity("", "github", "provider"))
	})

	t.Run("kind mismatch does not match", func(t *testing.T) {
		assert.Nil(t, lf.FindPluginByIdentity("ghcr.io/orgA", "github", "auth-handler"))
	})

	t.Run("unknown canonical returns no match", func(t *testing.T) {
		assert.Nil(t, lf.FindPluginByIdentity("ghcr.io/orgC", "github", "provider"))
	})

	t.Run("nil lock file", func(t *testing.T) {
		var nilLf *LockFile
		assert.Nil(t, nilLf.FindPluginByIdentity("ghcr.io/orgA", "github", "provider"))
	})
}

func TestLockFile_FindPluginByDep(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			// Unsourced (local-catalog) entry: Source is nil.
			{Name: "echo", Kind: "provider", Version: "1.0.0"},
			// Sourced entries sharing a leaf name across distinct registries --
			// must be disambiguated by Source.Registry, not collapsed.
			{Name: "github", Kind: "provider", Version: "1.5.0", Source: &LockPluginSource{Registry: "ghcr.io/orgA"}},
			{Name: "github", Kind: "provider", Version: "2.3.0", Source: &LockPluginSource{Registry: "ghcr.io/orgB"}},
			// Sourced entry recorded under its RESOLVED remote leaf name.
			{Name: "scafctl-exec-provider", Kind: "provider", Version: "3.0.0", Source: &LockPluginSource{Registry: "ghcr.io/orgA"}},
		},
	}

	tests := []struct {
		name        string
		lf          *LockFile
		dep         solution.PluginDependency
		wantVersion string // empty means no match is expected
	}{
		{
			name:        "unsourced dep matches the unsourced entry",
			lf:          lf,
			dep:         solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider},
			wantVersion: "1.0.0",
		},
		{
			name: "unsourced dep does not match a sourced entry of the same name",
			lf:   lf,
			dep:  solution.PluginDependency{Name: "github", Kind: solution.PluginKindProvider},
		},
		{
			name:        "sourced dep matches on registry, leaf, and kind",
			lf:          lf,
			dep:         sourced_plugin("github", "provider", "ghcr.io/orgB"),
			wantVersion: "2.3.0",
		},
		{
			name:        "sourced dep disambiguates a shared leaf by registry",
			lf:          lf,
			dep:         sourced_plugin("github", "provider", "ghcr.io/orgA"),
			wantVersion: "1.5.0",
		},
		{
			// Local handle "exec" aliases the remote artifact
			// "scafctl-exec-provider"; the lock is keyed by the resolved leaf, so
			// the registry travels in Source, not embedded in the Name.
			name:        "sourced dep matches on aliased remote leaf name",
			lf:          lf,
			dep:         sourced_plugin("scafctl-exec-provider", "provider", "ghcr.io/orgA"),
			wantVersion: "3.0.0",
		},
		{
			name: "sourced dep does not match an unsourced entry",
			lf:   lf,
			dep:  sourced_plugin("echo", "provider", "ghcr.io/orgA"),
		},
		{
			name: "registry mismatch returns no match",
			lf:   lf,
			dep:  sourced_plugin("github", "provider", "ghcr.io/orgC"),
		},
		{
			name: "kind mismatch returns no match",
			lf:   lf,
			dep:  sourced_plugin("github", "auth-handler", "ghcr.io/orgA"),
		},
		{
			name: "nil lock file",
			lf:   nil,
			dep:  solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.lf.FindPluginByDep(tt.dep)
			if tt.wantVersion == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVersion, got.Version)
		})
	}
}

func TestLockFile_FindPluginByVersionConstraint(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			// Unsourced entry with explicit constraint.
			{Name: "echo", Kind: "provider", Version: "1.0.0", Constraint: "^1.0.0"},
			// Unsourced entry with empty constraint.
			{Name: "local-auth", Kind: "auth-handler", Version: "0.9.0"},
			// Sourced entries sharing leaf+kind but differing by registry.
			{Name: "github", Kind: "provider", Version: "1.5.0", Constraint: "^1.5.0", Source: &LockPluginSource{Registry: "ghcr.io/orgA"}},
			{Name: "github", Kind: "provider", Version: "2.3.0", Constraint: "^2.0.0", Source: &LockPluginSource{Registry: "ghcr.io/orgB"}},
		},
	}

	tests := []struct {
		name        string
		lf          *LockFile
		dep         pluginDepWithConstraint
		wantVersion string // empty means no match is expected
	}{
		{
			name:        "unsourced dep matches by leaf kind and exact constraint",
			lf:          lf,
			dep:         pluginDepWithConstraint{dep: solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}, constraint: "^1.0.0"},
			wantVersion: "1.0.0",
		},
		{
			name: "unsourced dep does not match when constraint differs",
			lf:   lf,
			dep:  pluginDepWithConstraint{dep: solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}, constraint: "^2.0.0"},
		},
		{
			name:        "unsourced dep with empty constraint matches empty lock constraint",
			lf:          lf,
			dep:         pluginDepWithConstraint{dep: solution.PluginDependency{Name: "local-auth", Kind: solution.PluginKindAuthHandler}, constraint: ""},
			wantVersion: "0.9.0",
		},
		{
			// The registry travels in Source, not embedded in the Name.
			name:        "sourced dep matches by registry leaf kind and exact constraint",
			lf:          lf,
			dep:         pluginDepWithConstraint{dep: sourced_plugin("github", "provider", "ghcr.io/orgB"), constraint: "^2.0.0"},
			wantVersion: "2.3.0",
		},
		{
			name: "sourced dep does not match same leaf from another registry",
			lf:   lf,
			dep:  pluginDepWithConstraint{dep: sourced_plugin("github", "provider", "ghcr.io/orgA"), constraint: "^2.0.0"},
		},
		{
			name: "nil lock file",
			lf:   nil,
			dep:  pluginDepWithConstraint{dep: solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}, constraint: "^1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findPluginByVersionConstraint(tt.lf, tt.dep)
			if tt.wantVersion == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVersion, got.Version)
		})
	}
}

type pluginDepWithConstraint struct {
	dep        solution.PluginDependency
	constraint string
}

func (d pluginDepWithConstraint) HasRegistry() bool {
	return d.dep.HasRegistry()
}

func (d pluginDepWithConstraint) Registry() string {
	return d.dep.Registry()
}

func (d pluginDepWithConstraint) ArtifactName() string {
	return d.dep.ArtifactName()
}

func (d pluginDepWithConstraint) PluginKind() solution.PluginKind {
	return d.dep.PluginKind()
}

func (d pluginDepWithConstraint) VersionConstraint() string {
	return d.constraint
}

func TestLoadLockFile_NonExistent(t *testing.T) {
	lf, err := LoadLockFile(filepath.Join(t.TempDir(), "nonexistent.lock"))
	assert.NoError(t, err)
	assert.Nil(t, lf)
}

func TestWriteAndLoadLockFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.lock")

	original := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:          "deploy-to-k8s@2.0.0",
				Digest:       "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				ResolvedFrom: "company-catalog",
				VendoredAt:   ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml",
			},
		},
		Plugins: []LockPlugin{
			{
				Name:              "azure-provider",
				Kind:              "provider",
				Version:           "1.2.3",
				Constraint:        "^1.0.0",
				Digest:            "sha256:abc",
				ResolvedFrom:      "plugins.example.com",
				ResolvedCanonical: "ghcr.io/org/plugins",
			},
		},
	}

	// Write
	err := WriteLockFile(path, original)
	require.NoError(t, err)

	// Verify the file has the header comment
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# This file is auto-generated")

	// Load back
	loaded, err := LoadLockFile(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, 1, loaded.Version)
	require.Len(t, loaded.Dependencies, 1)
	assert.Equal(t, "deploy-to-k8s@2.0.0", loaded.Dependencies[0].Ref)
	assert.Equal(t, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", loaded.Dependencies[0].Digest)
	assert.Equal(t, "company-catalog", loaded.Dependencies[0].ResolvedFrom)
	assert.Equal(t, ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml", loaded.Dependencies[0].VendoredAt)

	require.Len(t, loaded.Plugins, 1)
	assert.Equal(t, "azure-provider", loaded.Plugins[0].Name)
	assert.Equal(t, "provider", loaded.Plugins[0].Kind)
	assert.Equal(t, "1.2.3", loaded.Plugins[0].Version)
	assert.Equal(t, "^1.0.0", loaded.Plugins[0].Constraint)
	assert.Equal(t, "ghcr.io/org/plugins", loaded.Plugins[0].ResolvedCanonical)
}

func TestLoadLockFile_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.lock")

	err := os.WriteFile(path, []byte("version: 99\n"), 0o644)
	require.NoError(t, err)

	lf, err := LoadLockFile(path)
	assert.Error(t, err)
	assert.Nil(t, lf)
	assert.Contains(t, err.Error(), "unsupported lock file version")
}

func TestLoadLockFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.lock")

	err := os.WriteFile(path, []byte("{{invalid yaml"), 0o644)
	require.NoError(t, err)

	lf, err := LoadLockFile(path)
	assert.Error(t, err)
	assert.Nil(t, lf)
}

func TestWriteLockFile_NilLockFile(t *testing.T) {
	err := WriteLockFile(filepath.Join(t.TempDir(), "solution.lock"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "lock file is nil")
}

func TestWriteLockFile_SetsVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.lock")

	lf := &LockFile{
		Version: 0, // not set
		Dependencies: []LockDependency{
			{Ref: "test@1.0.0", Digest: "sha256:test"},
		},
	}

	err := WriteLockFile(path, lf)
	require.NoError(t, err)

	loaded, err := LoadLockFile(path)
	require.NoError(t, err)
	assert.Equal(t, LockFileVersion, loaded.Version)
}

func TestRefName(t *testing.T) {
	tests := []struct {
		ref, expected string
	}{
		{"deploy-to-k8s@2.0.0", "deploy-to-k8s"},
		{"deploy-to-k8s@^1.5.0", "deploy-to-k8s"},
		{"deploy-to-k8s", "deploy-to-k8s"},
		{"a@@2.0.0", "a@"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.expected, refName(tt.ref))
		})
	}
}

func TestFindDependency_NameFallback(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{Ref: "deploy-to-k8s@^1.5.0", ResolvedVersion: "1.5.2", Constraint: "^1.5.0", Digest: "sha256:abc"},
		},
	}

	// Exact match
	dep := lf.FindDependency("deploy-to-k8s@^1.5.0")
	require.NotNil(t, dep)
	assert.Equal(t, "1.5.2", dep.ResolvedVersion)

	// Name-based fallback: different constraint string, same artifact name
	dep = lf.FindDependency("deploy-to-k8s@^1.6.0")
	require.NotNil(t, dep)
	assert.Equal(t, "1.5.2", dep.ResolvedVersion)

	// Name-based fallback: exact version ref
	dep = lf.FindDependency("deploy-to-k8s@2.0.0")
	require.NotNil(t, dep)
	assert.Equal(t, "1.5.2", dep.ResolvedVersion)
}

func TestFindDependencyByName(t *testing.T) {
	lf := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{Ref: "deploy-to-k8s@^1.5.0", ResolvedVersion: "1.5.2", Digest: "sha256:abc"},
			{Ref: "setup-env@1.0.0", Digest: "sha256:def"},
		},
	}

	dep := lf.FindDependencyByName("deploy-to-k8s")
	require.NotNil(t, dep)
	assert.Equal(t, "1.5.2", dep.ResolvedVersion)

	dep = lf.FindDependencyByName("setup-env")
	require.NotNil(t, dep)
	assert.Equal(t, "sha256:def", dep.Digest)

	dep = lf.FindDependencyByName("nonexistent")
	assert.Nil(t, dep)

	var nilLf *LockFile
	dep = nilLf.FindDependencyByName("anything")
	assert.Nil(t, dep)
}

func TestWriteAndLoadLockFile_WithConstraintFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.lock")

	original := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:             "deploy-to-k8s@^1.5.0",
				ResolvedVersion: "1.5.2",
				Constraint:      "^1.5.0",
				Digest:          "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				ResolvedFrom:    "company-catalog",
				VendoredAt:      ".scafctl/vendor/deploy-to-k8s@1.5.2.yaml",
			},
		},
	}

	err := WriteLockFile(path, original)
	require.NoError(t, err)

	loaded, err := LoadLockFile(path)
	require.NoError(t, err)
	require.Len(t, loaded.Dependencies, 1)
	assert.Equal(t, "deploy-to-k8s@^1.5.0", loaded.Dependencies[0].Ref)
	assert.Equal(t, "1.5.2", loaded.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "^1.5.0", loaded.Dependencies[0].Constraint)
}

func TestMarshalAndParseLockJSON_RoundTrip(t *testing.T) {
	original := &LockFile{
		Version: 1,
		Dependencies: []LockDependency{
			{
				Ref:          "deploy-to-k8s@2.0.0",
				Digest:       "sha256:abc",
				ResolvedFrom: "company-catalog",
				VendoredAt:   ".scafctl/vendor/deploy-to-k8s@2.0.0.yaml",
			},
		},
		Plugins: []LockPlugin{
			{
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Digest:       "sha256:def",
				ResolvedFrom: "plugins.example.com",
				Signature: &LockPluginSignature{
					Issuer:   "https://token.actions.githubusercontent.com",
					Identity: "https://github.com/org/repo/.github/workflows/release.yaml@refs/tags/v1.0.0",
					SignedAt: "2026-01-15T10:30:00Z",
				},
			},
		},
	}

	data, err := MarshalLockJSON(original)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version":1`)

	loaded, err := ParseLockJSON(data)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, LockFileVersion, loaded.Version)
	require.Len(t, loaded.Dependencies, 1)
	assert.Equal(t, "deploy-to-k8s@2.0.0", loaded.Dependencies[0].Ref)
	require.Len(t, loaded.Plugins, 1)
	assert.Equal(t, "azure-provider", loaded.Plugins[0].Name)
	require.NotNil(t, loaded.Plugins[0].Signature)
	assert.Equal(t, "2026-01-15T10:30:00Z", loaded.Plugins[0].Signature.SignedAt)
}

func TestMarshalLockJSON_ResolvedCanonical(t *testing.T) {
	withCanonical := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:              "azure-provider",
				Kind:              "provider",
				Version:           "1.2.3",
				Constraint:        "^1.0.0",
				Digest:            "sha256:def",
				ResolvedFrom:      "prod-alias",
				ResolvedCanonical: "ghcr.io/org/plugins",
			},
		},
	}

	data, err := MarshalLockJSON(withCanonical)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"resolvedCanonical":"ghcr.io/org/plugins"`)

	loaded, err := ParseLockJSON(data)
	require.NoError(t, err)
	require.Len(t, loaded.Plugins, 1)
	assert.Equal(t, "prod-alias", loaded.Plugins[0].ResolvedFrom)
	assert.Equal(t, "ghcr.io/org/plugins", loaded.Plugins[0].ResolvedCanonical)
	assert.Equal(t, "^1.0.0", loaded.Plugins[0].Constraint)

	// omitempty: an empty canonical must not appear in the serialized output.
	withoutCanonical := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:         "local-provider",
				Kind:         "provider",
				Version:      "1.0.0",
				Digest:       "sha256:abc",
				ResolvedFrom: "local",
			},
		},
	}
	bareData, err := MarshalLockJSON(withoutCanonical)
	require.NoError(t, err)
	assert.NotContains(t, string(bareData), "resolvedCanonical")
}

func TestMarshalLockJSON_StampsVersion(t *testing.T) {
	lf := &LockFile{
		Version:      0, // not set
		Dependencies: []LockDependency{{Ref: "test@1.0.0", Digest: "sha256:test"}},
	}

	data, err := MarshalLockJSON(lf)
	require.NoError(t, err)
	assert.Equal(t, LockFileVersion, lf.Version)

	loaded, err := ParseLockJSON(data)
	require.NoError(t, err)
	assert.Equal(t, LockFileVersion, loaded.Version)
}

func TestMarshalLockJSON_NilLockFile(t *testing.T) {
	data, err := MarshalLockJSON(nil)
	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "lock file is nil")
}

func TestParseLockJSON_InvalidVersion(t *testing.T) {
	lf, err := ParseLockJSON([]byte(`{"version":99}`))
	assert.Error(t, err)
	assert.Nil(t, lf)
	assert.Contains(t, err.Error(), "unsupported lock file version")
}

func TestParseLockJSON_InvalidJSON(t *testing.T) {
	lf, err := ParseLockJSON([]byte("{{not json"))
	assert.Error(t, err)
	assert.Nil(t, lf)
	assert.Contains(t, err.Error(), "parsing lock file")
}
