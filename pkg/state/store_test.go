// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_NotFound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.json")

	sd, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersionCurrent, sd.SchemaVersion)
	assert.Empty(t, sd.Values)
}

func TestLoadFromFile_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	sd := NewData()
	sd.Values["mykey"] = &Entry{
		Value:     "hello",
		Type:      "string",
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	sd.Metadata.Solution = "test-sol"

	err := SaveToFile(path, sd)
	require.NoError(t, err)

	loaded, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "test-sol", loaded.Metadata.Solution)
	require.Contains(t, loaded.Values, "mykey")
	assert.Equal(t, "hello", loaded.Values["mykey"].Value)
	assert.Equal(t, "string", loaded.Values["mykey"].Type)
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0o600))

	_, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
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

func TestSaveToFile_EmptyPath(t *testing.T) {
	t.Parallel()
	err := SaveToFile("", NewData())
	require.Error(t, err)
}

func BenchmarkLoadFromFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.json")
	sd := NewData()
	for i := range 100 {
		sd.Values[filepath.Join("key", string(rune('a'+i%26)))] = &Entry{
			Value: "value",
			Type:  "string",
		}
	}
	require.NoError(b, SaveToFile(path, sd))

	b.ResetTimer()
	for range b.N {
		_, _ = LoadFromFile(path)
	}
}

func BenchmarkSaveToFile(b *testing.B) {
	dir := b.TempDir()
	sd := NewData()
	sd.Values["key"] = &Entry{Value: "val", Type: "string"}

	b.ResetTimer()
	for i := range b.N {
		path := filepath.Join(dir, filepath.Base(filepath.Join("bench", string(rune('a'+i%26))+".json")))
		_ = SaveToFile(path, sd)
	}
}
