// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Digest:       "sha256:abc",
				ResolvedFrom: "plugins.example.com",
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
