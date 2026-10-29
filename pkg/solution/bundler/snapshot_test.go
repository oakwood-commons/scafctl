// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotOf reads files under root into a snapshot, failing the test on error.
func snapshotOf(t testing.TB, root string, files []FileEntry) *FileSnapshot {
	t.Helper()
	snap, err := ReadFileSnapshot(root, files)
	require.NoError(t, err)
	return snap
}

func TestReadFileSnapshot(t *testing.T) {
	t.Parallel()

	digestOf := func(s string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s))) }

	tests := []struct {
		name      string
		disk      map[string]string
		files     []FileEntry
		opts      []SnapshotOption
		wantFiles []SnapshotFile
		wantSize  int64
		wantErr   string
	}{
		{
			name:      "reads files in order",
			disk:      map[string]string{"b.txt": "bb", "dir/a.txt": "a"},
			files:     []FileEntry{{RelPath: "b.txt"}, {RelPath: filepath.Join("dir", "a.txt")}},
			wantFiles: []SnapshotFile{{RelPath: "b.txt", Content: []byte("bb"), Digest: digestOf("bb")}, {RelPath: "dir/a.txt", Content: []byte("a"), Digest: digestOf("a")}},
			wantSize:  3,
		},
		{
			name:      "duplicate paths are read once",
			disk:      map[string]string{"a.txt": "a"},
			files:     []FileEntry{{RelPath: "a.txt"}, {RelPath: "a.txt", Source: ExplicitInclude}},
			wantFiles: []SnapshotFile{{RelPath: "a.txt", Content: []byte("a"), Digest: digestOf("a")}},
			wantSize:  1,
		},
		{
			name:      "in-memory content is not read from disk",
			files:     []FileEntry{{RelPath: ".scafctl/vendor/dep.yaml"}},
			opts:      []SnapshotOption{WithSnapshotContents(map[string][]byte{".scafctl/vendor/dep.yaml": []byte("dep")})},
			wantFiles: []SnapshotFile{{RelPath: ".scafctl/vendor/dep.yaml", Content: []byte("dep"), Digest: digestOf("dep")}},
			wantSize:  3,
		},
		{
			name:      "empty",
			wantFiles: []SnapshotFile{},
		},
		{
			name:    "missing file",
			files:   []FileEntry{{RelPath: "missing.txt"}},
			wantErr: "failed to read missing.txt",
		},
		{
			name:    "exceeds max size",
			disk:    map[string]string{"a.txt": "aaaa", "b.txt": "bbbb"},
			files:   []FileEntry{{RelPath: "a.txt"}, {RelPath: "b.txt"}},
			opts:    []SnapshotOption{WithSnapshotMaxSize(5)},
			wantErr: "bundle exceeds maximum size limit (8 bytes, limit 5 bytes)",
		},
		{
			name:    "read error from read func",
			files:   []FileEntry{{RelPath: "a.txt"}},
			opts:    []SnapshotOption{WithSnapshotReadFileFunc(func(string) ([]byte, error) { return nil, errors.New("boom") })},
			wantErr: "failed to read a.txt: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for rel, content := range tt.disk {
				path := filepath.Join(root, filepath.FromSlash(rel))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}

			snap, err := ReadFileSnapshot(root, tt.files, tt.opts...)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFiles, snap.Files)
			assert.Equal(t, tt.wantSize, snap.TotalSize)
		})
	}
}

func TestReadFileSnapshot_ReadsEachFileOnce(t *testing.T) {
	t.Parallel()

	reads := map[string]int{}
	readFile := func(path string) ([]byte, error) {
		reads[filepath.Base(path)]++
		return []byte(filepath.Base(path)), nil
	}

	snap, err := ReadFileSnapshot("/root", []FileEntry{{RelPath: "a.txt"}, {RelPath: "b.txt"}, {RelPath: "a.txt"}},
		WithSnapshotReadFileFunc(readFile))
	require.NoError(t, err)

	// The fingerprint and both archive formats use the snapshot, not the disk.
	_ = ComputeBuildFingerprint([]byte("sol"), snap, nil, "", nil)
	_, _, err = CreateBundleTar(snap, nil)
	require.NoError(t, err)
	_, err = CreateDeduplicatedBundle(snap, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"a.txt": 1, "b.txt": 1}, reads)
}

func TestFileSnapshot_EntriesNil(t *testing.T) {
	t.Parallel()
	var snap *FileSnapshot
	assert.Nil(t, snap.entries())
}

func BenchmarkReadFileSnapshot(b *testing.B) {
	root := b.TempDir()
	files := make([]FileEntry, 0, 20)
	for i := range 20 {
		name := fmt.Sprintf("f%02d.txt", i)
		require.NoError(b, os.WriteFile(filepath.Join(root, name), make([]byte, 8*1024), 0o600))
		files = append(files, FileEntry{RelPath: name})
	}

	for b.Loop() {
		if _, err := ReadFileSnapshot(root, files); err != nil {
			b.Fatal(err)
		}
	}
}
