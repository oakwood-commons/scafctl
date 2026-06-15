// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refs

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDirectory(t *testing.T) {
	t.Parallel()

	t.Run("scans go-template files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tpl"), []byte("{{ ._.appName }} {{ ._.config.host }}"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "other.tpl"), []byte("{{ ._.port }}"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("not a template"), 0o644))

		result, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.NoError(t, err)

		assert.Equal(t, 3, result.Count)
		assert.ElementsMatch(t, []string{"appName", "config", "port"}, result.References)
		assert.Equal(t, 2, result.FilesCount)
		assert.Empty(t, result.Warnings)
	})

	t.Run("scans CEL files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "expr.tpl"), []byte(`_.appName + ":" + _.port`), 0o644))

		result, err := ScanDirectory(context.Background(), dir, nil, "cel")
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"appName", "port"}, result.References)
		assert.Equal(t, 1, result.FilesCount)
	})

	t.Run("custom glob patterns", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.yaml"), []byte("{{ ._.appName }}"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.tpl"), []byte("{{ ._.skipped }}"), 0o644))

		result, err := ScanDirectory(context.Background(), dir, []string{"*.yaml"}, "go-template")
		require.NoError(t, err)

		assert.Equal(t, []string{"appName"}, result.References)
		assert.Equal(t, 1, result.FilesCount)
	})

	t.Run("empty directory returns zero results", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		result, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.NoError(t, err)

		assert.Equal(t, 0, result.Count)
		assert.Empty(t, result.Files)
	})

	t.Run("nonexistent directory returns error", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
		_, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("file path returns error", func(t *testing.T) {
		t.Parallel()

		f := filepath.Join(t.TempDir(), "file.txt")
		require.NoError(t, os.WriteFile(f, []byte("hello"), 0o644))

		_, err := ScanDirectory(context.Background(), f, nil, "go-template")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("invalid glob pattern returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := ScanDirectory(context.Background(), dir, []string{"[a-"}, "go-template")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid glob pattern")
	})

	t.Run("unsupported expression type returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := ScanDirectory(context.Background(), dir, nil, "jinja2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported expression type")
	})

	t.Run("unreadable file surfaces warning", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("file permission test not reliable on Windows")
		}
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "good.tpl"), []byte("{{ ._.appName }}"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.tpl"), []byte("{{ ._.secret }}"), 0o000))

		result, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.NoError(t, err)

		// Verify good file was still processed
		assert.Contains(t, result.References, "appName")

		// Check if the file was actually unreadable
		badPath := filepath.Join(dir, "bad.tpl")
		if _, readErr := os.ReadFile(badPath); readErr != nil {
			// File is truly unreadable, warnings must be present
			require.NotEmpty(t, result.Warnings, "expected warnings for unreadable file")
		} else {
			t.Skip("platform did not restrict file access (likely running as root)")
		}
	})

	t.Run("parse errors surface as warnings", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// Write an invalid go template
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tpl"), []byte("{{ ._.appName }"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "good.tpl"), []byte("{{ ._.port }}"), 0o644))

		result, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.NoError(t, err)

		assert.Contains(t, result.References, "port")
		assert.NotEmpty(t, result.Warnings)
	})

	t.Run("recursive subdirectory scanning", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		sub := filepath.Join(dir, "subdir")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "root.tpl"), []byte("{{ ._.rootRef }}"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "child.tpl"), []byte("{{ ._.childRef }}"), 0o644))

		result, err := ScanDirectory(context.Background(), dir, nil, "go-template")
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"rootRef", "childRef"}, result.References)
		assert.Equal(t, 2, result.FilesCount)
	})
}

func TestParseGlobs(t *testing.T) {
	t.Parallel()

	t.Run("empty returns defaults", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, DefaultTemplateGlobs, ParseGlobs(""))
	})

	t.Run("single pattern", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"*.yaml"}, ParseGlobs("*.yaml"))
	})

	t.Run("multiple patterns trimmed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"*.tpl", "*.yaml"}, ParseGlobs("*.tpl, *.yaml"))
	})
}

func TestBuildDetails(t *testing.T) {
	t.Parallel()

	t.Run("groups fields by resolver", func(t *testing.T) {
		t.Parallel()
		paths := []string{"config.host", "config.port", "appName"}
		details := BuildDetails(paths)

		assert.Len(t, details, 2)
		assert.Equal(t, Detail{Resolver: "appName", Fields: []string{}}, details[0])
		assert.Equal(t, Detail{Resolver: "config", Fields: []string{"host", "port"}}, details[1])
	})

	t.Run("deduplicates fields", func(t *testing.T) {
		t.Parallel()
		paths := []string{"config.host", "config.host"}
		details := BuildDetails(paths)

		assert.Len(t, details, 1)
		assert.Equal(t, []string{"host"}, details[0].Fields)
	})
}
