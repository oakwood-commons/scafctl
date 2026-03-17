// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ConflictStrategy
// ---------------------------------------------------------------------------

func TestConflictStrategy_IsValid(t *testing.T) {
	tests := []struct {
		strategy ConflictStrategy
		want     bool
	}{
		{ConflictError, true},
		{ConflictOverwrite, true},
		{ConflictSkip, true},
		{ConflictSkipUnchanged, true},
		{ConflictAppend, true},
		{"invalid", false},
		{"", false},
		{"Error", false}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.strategy.IsValid())
		})
	}
}

func TestConflictStrategy_OrDefault(t *testing.T) {
	t.Run("empty returns default", func(t *testing.T) {
		got := ConflictStrategy("").OrDefault()
		assert.Equal(t, ConflictStrategy(settings.DefaultConflictStrategy), got)
	})

	t.Run("explicit value preserved", func(t *testing.T) {
		got := ConflictOverwrite.OrDefault()
		assert.Equal(t, ConflictOverwrite, got)
	})

	t.Run("invalid value still preserved (IsValid is separate)", func(t *testing.T) {
		got := ConflictStrategy("nope").OrDefault()
		assert.Equal(t, ConflictStrategy("nope"), got)
	})
}

// ---------------------------------------------------------------------------
// contentMatchesFile
// ---------------------------------------------------------------------------

func TestContentMatchesFile(t *testing.T) {
	t.Run("matching content", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		data := []byte("hello world\n")
		require.NoError(t, os.WriteFile(p, data, 0o644))

		match, err := contentMatchesFile(p, data)
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("differing content", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("old"), 0o644))

		match, err := contentMatchesFile(p, []byte("new"))
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("empty files match", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte{}, 0o644))

		match, err := contentMatchesFile(p, []byte{})
		require.NoError(t, err)
		assert.True(t, match)
	})
}

func TestContentMatchesFile_NonExistent(t *testing.T) {
	match, err := contentMatchesFile(filepath.Join(t.TempDir(), "nope"), []byte("x"))
	require.NoError(t, err)
	assert.False(t, match)
}

// ---------------------------------------------------------------------------
// backupFile
// ---------------------------------------------------------------------------

func TestBackupFile(t *testing.T) {
	t.Run("no existing backup", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("data"), 0o644))

		bp, err := backupFile(p)
		require.NoError(t, err)
		assert.Equal(t, p+".bak", bp)

		got, err := os.ReadFile(bp)
		require.NoError(t, err)
		assert.Equal(t, "data", string(got))
	})

	t.Run("existing .bak rolls to .bak.1", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("v2"), 0o644))
		require.NoError(t, os.WriteFile(p+".bak", []byte("v1"), 0o644))

		bp, err := backupFile(p)
		require.NoError(t, err)
		assert.Equal(t, p+".bak.1", bp)
	})

	t.Run("multiple existing backups", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("latest"), 0o644))
		require.NoError(t, os.WriteFile(p+".bak", []byte("b0"), 0o644))
		require.NoError(t, os.WriteFile(p+".bak.1", []byte("b1"), 0o644))
		require.NoError(t, os.WriteFile(p+".bak.2", []byte("b2"), 0o644))

		bp, err := backupFile(p)
		require.NoError(t, err)
		assert.Equal(t, p+".bak.3", bp)
	})

	t.Run("cap limit reached", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
		require.NoError(t, os.WriteFile(p+".bak", []byte("b0"), 0o644))
		for i := 1; i < settings.DefaultMaxBackups; i++ {
			require.NoError(t, os.WriteFile(
				fmt.Sprintf("%s.bak.%d", p, i),
				[]byte("b"), 0o644,
			))
		}

		_, err := backupFile(p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup limit reached")
	})
}

func TestBackupFile_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("data"), 0o755))

	bp, err := backupFile(p)
	require.NoError(t, err)

	info, err := os.Stat(bp)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

// ---------------------------------------------------------------------------
// appendToFile
// ---------------------------------------------------------------------------

func TestAppendToFile(t *testing.T) {
	t.Run("raw append to existing", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("line1\n"), 0o644))

		status, err := appendToFile(p, []byte("line2\n"), 0o644, false)
		require.NoError(t, err)
		assert.Equal(t, StatusAppended, status)

		got, _ := os.ReadFile(p)
		assert.Equal(t, "line1\nline2\n", string(got))
	})

	t.Run("raw append creates new file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "new.txt")

		status, err := appendToFile(p, []byte("hello\n"), 0o644, false)
		require.NoError(t, err)
		assert.Equal(t, StatusCreated, status)

		got, _ := os.ReadFile(p)
		assert.Equal(t, "hello\n", string(got))
	})

	t.Run("dedupe with unique lines", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("aaa\n"), 0o644))

		status, err := appendToFile(p, []byte("bbb\n"), 0o644, true)
		require.NoError(t, err)
		assert.Equal(t, StatusAppended, status)

		got, _ := os.ReadFile(p)
		assert.Contains(t, string(got), "bbb")
	})

	t.Run("dedupe with all-duplicate lines", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("aaa\nbbb\n"), 0o644))

		status, err := appendToFile(p, []byte("aaa\nbbb\n"), 0o644, true)
		require.NoError(t, err)
		assert.Equal(t, StatusUnchanged, status)

		got, _ := os.ReadFile(p)
		assert.Equal(t, "aaa\nbbb\n", string(got))
	})

	t.Run("dedupe with mixed lines", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("aaa\nbbb\n"), 0o644))

		status, err := appendToFile(p, []byte("bbb\nccc\n"), 0o644, true)
		require.NoError(t, err)
		assert.Equal(t, StatusAppended, status)

		got, _ := os.ReadFile(p)
		assert.Contains(t, string(got), "ccc")
		// bbb should not be duplicated
		assert.Equal(t, 1, strings.Count(string(got), "bbb"))
	})

	t.Run("dedupe on missing file creates it", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "new.txt")

		status, err := appendToFile(p, []byte("hello\n"), 0o644, true)
		require.NoError(t, err)
		assert.Equal(t, StatusCreated, status)

		got, _ := os.ReadFile(p)
		assert.Equal(t, "hello\n", string(got))
	})
}

func TestAppendToFile_CRLFHandling(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("aaa\r\nbbb\r\n"), 0o644))

	status, err := appendToFile(p, []byte("bbb\nccc\n"), 0o644, true)
	require.NoError(t, err)
	assert.Equal(t, StatusAppended, status)

	got, _ := os.ReadFile(p)
	assert.Contains(t, string(got), "ccc")
	// bbb should not be duplicated (CRLF in existing matched LF in new)
	assert.Equal(t, 1, strings.Count(string(got), "bbb"))
}

func TestAppendToFile_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("no newline at end"), 0o644))

	status, err := appendToFile(p, []byte("appended"), 0o644, false)
	require.NoError(t, err)
	assert.Equal(t, StatusAppended, status)

	got, _ := os.ReadFile(p)
	// Should have a newline separator inserted
	assert.Equal(t, "no newline at end\nappended", string(got))
}

func TestAppendToFile_EmptyContent(t *testing.T) {
	t.Run("empty content on existing file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(t, os.WriteFile(p, []byte("existing"), 0o644))

		status, err := appendToFile(p, []byte{}, 0o644, false)
		require.NoError(t, err)
		assert.Equal(t, StatusUnchanged, status)

		got, _ := os.ReadFile(p)
		assert.Equal(t, "existing", string(got))
	})

	t.Run("empty content on missing file does not create", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "nope.txt")

		status, err := appendToFile(p, []byte{}, 0o644, false)
		require.NoError(t, err)
		assert.Equal(t, StatusUnchanged, status)

		_, err = os.Stat(p)
		assert.True(t, os.IsNotExist(err))
	})
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkContentMatchesFile(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			dir := b.TempDir()
			p := filepath.Join(dir, "f.bin")
			data := make([]byte, sz.size)
			for i := range data {
				data[i] = byte(i % 256)
			}
			require.NoError(b, os.WriteFile(p, data, 0o644))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = contentMatchesFile(p, data)
			}
		})
	}
}

func BenchmarkAppendToFile(b *testing.B) {
	b.Run("raw append", func(b *testing.B) {
		dir := b.TempDir()
		p := filepath.Join(dir, "f.txt")
		require.NoError(b, os.WriteFile(p, []byte("initial\n"), 0o644))
		content := []byte("appended line\n")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Re-create the base file each iteration to keep append consistent.
			_ = os.WriteFile(p, []byte("initial\n"), 0o644)
			_, _ = appendToFile(p, content, 0o644, false)
		}
	})

	b.Run("dedupe 100 lines", func(b *testing.B) {
		dir := b.TempDir()
		p := filepath.Join(dir, "f.txt")

		var existing strings.Builder
		for i := 0; i < 100; i++ {
			existing.WriteString("line-" + itoa(i) + "\n")
		}
		base := existing.String()

		// New content: 50 existing + 50 new
		var newContent strings.Builder
		for i := 50; i < 150; i++ {
			newContent.WriteString("line-" + itoa(i) + "\n")
		}
		nc := []byte(newContent.String())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = os.WriteFile(p, []byte(base), 0o644)
			_, _ = appendToFile(p, nc, 0o644, true)
		}
	})
}

// itoa is a small helper to avoid importing strconv in tests.
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func BenchmarkComputePlannedStatus(b *testing.B) {
	dir := b.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	require.NoError(b, os.WriteFile(existing, []byte("content"), 0o644))
	nonExistent := filepath.Join(dir, "nonexistent.txt")

	b.Run("new file", func(b *testing.B) {
		content := []byte("hello")
		for i := 0; i < b.N; i++ {
			computePlannedStatus(nonExistent, content, ConflictSkipUnchanged, false)
		}
	})

	b.Run("skip-unchanged same content", func(b *testing.B) {
		content := []byte("content")
		for i := 0; i < b.N; i++ {
			computePlannedStatus(existing, content, ConflictSkipUnchanged, false)
		}
	})

	b.Run("skip-unchanged different content", func(b *testing.B) {
		content := []byte("different")
		for i := 0; i < b.N; i++ {
			computePlannedStatus(existing, content, ConflictSkipUnchanged, false)
		}
	})

	b.Run("overwrite", func(b *testing.B) {
		content := []byte("new content")
		for i := 0; i < b.N; i++ {
			computePlannedStatus(existing, content, ConflictOverwrite, false)
		}
	})
}
