// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_NotFound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.json")

	sd, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersionCurrent, sd.SchemaVersion)
	assert.Empty(t, sd.Parameters)
}

func TestLoadFromFile_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	sd := NewData()
	sd.Parameters["env"] = "prod"
	sd.Metadata.Solution = "test-sol"

	err := SaveToFile(path, sd)
	require.NoError(t, err)

	loaded, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "test-sol", loaded.Metadata.Solution)
	require.Contains(t, loaded.Parameters, "env")
	assert.Equal(t, "prod", loaded.Parameters["env"])
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0o600))

	_, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestLoadFromFile_NilMapsNormalized(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "minimal.json")
	// State file with null maps and no command.parameters
	content := `{"schemaVersion":1,"metadata":{},"command":{},"parameters":null,"immutables":null,"fingerprints":null}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	loaded, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.NotNil(t, loaded.Parameters)
	assert.NotNil(t, loaded.Immutables)
	assert.NotNil(t, loaded.Fingerprints)
	assert.NotNil(t, loaded.Command.Parameters)
	// Should be safe to assign into
	loaded.Parameters["key"] = "test"
	loaded.Command.Parameters["p"] = "v"
}

func TestSaveToFile_CreatesDirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "subdir", "nested", "state.json")

	err := SaveToFile(path, NewData())
	require.NoError(t, err)

	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

func TestResolveStatePath_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := ResolveStatePath("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestResolveStatePath_Traversal(t *testing.T) {
	t.Parallel()
	_, err := ResolveStatePath("../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "traversal")
}

func TestResolveStatePath_Absolute(t *testing.T) {
	t.Parallel()
	abs := "/tmp/test-state.json"
	result, err := ResolveStatePath(abs)
	require.NoError(t, err)
	assert.Equal(t, abs, result)
}

func TestLoadFromFile_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := LoadFromFile("")
	require.Error(t, err)
}

func TestLoadFromFile_UnsupportedSchemaVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "future.json")
	err := os.WriteFile(path, []byte(`{"schemaVersion":999,"metadata":{},"parameters":{}}`), 0o600)
	require.NoError(t, err)

	_, err = LoadFromFile(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedSchemaVersion)
	assert.Contains(t, err.Error(), "999")
}

func TestSaveToFile_EmptyPath(t *testing.T) {
	t.Parallel()
	err := SaveToFile("", NewData())
	require.Error(t, err)
}

func BenchmarkLoadFromFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.json")
	sd := NewData()
	for i := range 100 {
		sd.Parameters[filepath.Join("key", string(rune('a'+i%26)))] = "value"
	}
	require.NoError(b, SaveToFile(path, sd))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = LoadFromFile(path)
	}
}

func BenchmarkSaveToFile(b *testing.B) {
	dir := b.TempDir()
	sd := NewData()
	sd.Parameters["key"] = "val"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		path := filepath.Join(dir, "bench.json")
		_ = SaveToFile(path, sd)
	}
}
