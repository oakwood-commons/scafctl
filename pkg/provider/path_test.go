// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePath(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name         string
		path         string
		mode         Capability
		modeSet      bool
		outputDir    string
		outputDirSet bool
		expected     string
	}{
		{
			name:     "absolute path, no mode, no output dir",
			path:     "/usr/local/bin/tool",
			expected: "/usr/local/bin/tool",
		},
		{
			name:         "absolute path, action mode, with output dir",
			path:         "/usr/local/bin/tool",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/usr/local/bin/tool",
		},
		{
			name:     "relative path, no mode, no output dir",
			path:     "subdir/file.txt",
			expected: filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:     "relative path, from mode, no output dir",
			path:     "subdir/file.txt",
			mode:     CapabilityFrom,
			modeSet:  true,
			expected: filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "relative path, action mode, no output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDirSet: false,
			expected:     filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "relative path, action mode, empty output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "",
			outputDirSet: true,
			expected:     filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "relative path, action mode, with output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/tmp/output/subdir/file.txt",
		},
		{
			name:         "relative path, from mode, with output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityFrom,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "relative path, transform mode, with output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityTransform,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "relative path, validation mode, with output dir",
			path:         "subdir/file.txt",
			mode:         CapabilityValidation,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     filepath.Join(cwd, "subdir/file.txt"),
		},
		{
			name:         "dot path, action mode, with output dir",
			path:         ".",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/tmp/output",
		},
		{
			name:         "dot-slash prefix, action mode, with output dir",
			path:         "./config/settings.yaml",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/tmp/output/config/settings.yaml",
		},
		{
			name:         "path with parent traversal staying within output dir, action mode",
			path:         "subdir/../file.txt",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/tmp/output/file.txt",
		},
		{
			name:         "dotdot-prefixed directory name is not traversal, action mode",
			path:         "..foo/bar",
			mode:         CapabilityAction,
			modeSet:      true,
			outputDir:    "/tmp/output",
			outputDirSet: true,
			expected:     "/tmp/output/..foo/bar",
		},
		{
			name:     "absolute path with unnecessary dots cleaned",
			path:     "/usr/local/../bin/tool",
			expected: "/usr/bin/tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.modeSet {
				ctx = WithExecutionMode(ctx, tt.mode)
			}
			if tt.outputDirSet {
				ctx = WithOutputDirectory(ctx, tt.outputDir)
			}

			result, err := ResolvePath(ctx, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolvePath_TraversalAttack(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		outputDir string
	}{
		{
			name:      "parent traversal escapes output directory",
			path:      "../outside.txt",
			outputDir: "/tmp/output",
		},
		{
			name:      "deep parent traversal escapes output directory",
			path:      "../../../etc/passwd",
			outputDir: "/tmp/output",
		},
		{
			name:      "traversal via subdirectory escape",
			path:      "subdir/../../outside.txt",
			outputDir: "/tmp/output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithExecutionMode(ctx, CapabilityAction)
			ctx = WithOutputDirectory(ctx, tt.outputDir)

			_, err := ResolvePath(ctx, tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "resolves outside output directory")
		})
	}
}

func TestValidatePathContainment_SymlinkEscape(t *testing.T) {
	// Create a temp dir structure: baseDir/link -> /tmp (or another outside dir)
	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	link := filepath.Join(baseDir, "link")
	err := os.Symlink(outsideDir, link)
	require.NoError(t, err)

	// A path through the symlink should be caught even though it's lexically inside baseDir.
	resolved := filepath.Join(baseDir, "link", "secret.txt")
	err = validatePathContainment(baseDir, resolved)
	assert.Error(t, err, "symlink escape should be detected")
	assert.Contains(t, err.Error(), "escapes base directory")
}

func TestValidatePathContainment_SymlinkWithinBaseDir(t *testing.T) {
	// Symlink pointing inside the base dir should be allowed.
	baseDir := t.TempDir()
	subDir := filepath.Join(baseDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	link := filepath.Join(baseDir, "link")
	err := os.Symlink(subDir, link)
	require.NoError(t, err)

	resolved := filepath.Join(baseDir, "link", "file.txt")
	err = validatePathContainment(baseDir, resolved)
	assert.NoError(t, err, "symlink within base dir should be allowed")
}

// Benchmarks

func BenchmarkResolvePath_AbsolutePath(b *testing.B) {
	ctx := context.Background()
	ctx = WithExecutionMode(ctx, CapabilityAction)
	ctx = WithOutputDirectory(ctx, "/tmp/output")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "/usr/local/bin/tool")
	}
}

func BenchmarkResolvePath_RelativeNoOutputDir(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "subdir/file.txt")
	}
}

func BenchmarkResolvePath_RelativeActionWithOutputDir(b *testing.B) {
	ctx := context.Background()
	ctx = WithExecutionMode(ctx, CapabilityAction)
	ctx = WithOutputDirectory(ctx, "/tmp/output")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "subdir/file.txt")
	}
}

func BenchmarkResolvePath_RelativeFromModeWithOutputDir(b *testing.B) {
	ctx := context.Background()
	ctx = WithExecutionMode(ctx, CapabilityFrom)
	ctx = WithOutputDirectory(ctx, "/tmp/output")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "subdir/file.txt")
	}
}

func BenchmarkResolvePath_TraversalRejection(b *testing.B) {
	ctx := context.Background()
	ctx = WithExecutionMode(ctx, CapabilityAction)
	ctx = WithOutputDirectory(ctx, "/tmp/output")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "../../../etc/passwd")
	}
}

// ── AbsFromContext tests ──

func TestAbsFromContext_NoContextCWD(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	result, err := AbsFromContext(ctx, "relative/path.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "relative/path.txt"), result)
}

func TestAbsFromContext_WithContextCWD(t *testing.T) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/custom/cwd")

	result, err := AbsFromContext(ctx, "relative/path.txt")
	require.NoError(t, err)
	assert.Equal(t, "/custom/cwd/relative/path.txt", result)
}

func TestAbsFromContext_AbsolutePathIgnoresContextCWD(t *testing.T) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/custom/cwd")

	result, err := AbsFromContext(ctx, "/absolute/path.txt")
	require.NoError(t, err)
	assert.Equal(t, "/absolute/path.txt", result)
}

func TestAbsFromContext_EmptyContextCWDFallsBack(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "")

	result, err := AbsFromContext(ctx, "relative/path.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "relative/path.txt"), result)
}

func TestAbsFromContext_SolutionDirectoryFallback(t *testing.T) {
	ctx := context.Background()
	ctx = WithSolutionDirectory(ctx, "/solution/dir")

	result, err := AbsFromContext(ctx, "relative/path.txt")
	require.NoError(t, err)
	assert.Equal(t, "/solution/dir/relative/path.txt", result)
}

func TestAbsFromContext_WorkingDirTakesPrecedenceOverSolutionDir(t *testing.T) {
	ctx := context.Background()
	ctx = WithSolutionDirectory(ctx, "/solution/dir")
	ctx = WithWorkingDirectory(ctx, "/working/dir")

	result, err := AbsFromContext(ctx, "relative/path.txt")
	require.NoError(t, err)
	assert.Equal(t, "/working/dir/relative/path.txt", result)
}

// ── GetWorkingDirectory tests ──

func TestGetWorkingDirectory_NoContext(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	ctx := context.Background()
	result, err := GetWorkingDirectory(ctx)
	require.NoError(t, err)
	assert.Equal(t, cwd, result)
}

func TestGetWorkingDirectory_WithContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/custom/cwd")

	result, err := GetWorkingDirectory(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/custom/cwd", result)
}

// ── ResolvePath with context CWD tests ──

func TestResolvePath_WithContextCWD(t *testing.T) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/custom/cwd")

	result, err := ResolvePath(ctx, "subdir/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/custom/cwd/subdir/file.txt", result)
}

func TestResolvePath_ActionModeOutputDirTakesPrecedenceOverContextCWD(t *testing.T) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/custom/cwd")
	ctx = WithExecutionMode(ctx, CapabilityAction)
	ctx = WithOutputDirectory(ctx, "/output/dir")

	result, err := ResolvePath(ctx, "subdir/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/output/dir/subdir/file.txt", result)
}

func TestResolvePath_ActionModeContextCWDUsedWithoutOutputDir(t *testing.T) {
	// Simulates a catalog solution run without --output-dir: action mode is set,
	// context working directory is the caller's CWD, but no output directory.
	// ResolvePath should resolve against the context CWD (not os.Getwd).
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/caller/project")
	ctx = WithExecutionMode(ctx, CapabilityAction)

	result, err := ResolvePath(ctx, "output/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/caller/project/output/file.txt", result)
}

func BenchmarkResolvePath_ActionModeContextCWDNoOutputDir(b *testing.B) {
	ctx := context.Background()
	ctx = WithWorkingDirectory(ctx, "/caller/project")
	ctx = WithExecutionMode(ctx, CapabilityAction)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolvePath(ctx, "output/file.txt")
	}
}

// ── WorkingDirectory context round-trip tests ──

func TestWorkingDirectoryFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	dir, ok := WorkingDirectoryFromContext(ctx)
	assert.False(t, ok)
	assert.Empty(t, dir)
}

func TestWorkingDirectoryFromContext_Set(t *testing.T) {
	ctx := WithWorkingDirectory(context.Background(), "/my/dir")
	dir, ok := WorkingDirectoryFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "/my/dir", dir)
}

// ── Benchmarks for context CWD ──

func BenchmarkAbsFromContext_WithCWD(b *testing.B) {
	ctx := WithWorkingDirectory(context.Background(), "/custom/cwd")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = AbsFromContext(ctx, "relative/path.txt")
	}
}

func BenchmarkAbsFromContext_NoCWD(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = AbsFromContext(ctx, "relative/path.txt")
	}
}

func BenchmarkGetWorkingDirectory_WithCWD(b *testing.B) {
	ctx := WithWorkingDirectory(context.Background(), "/custom/cwd")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetWorkingDirectory(ctx)
	}
}

// ── ValidateDirectory tests ──

func TestValidateDirectory_Valid(t *testing.T) {
	dir := t.TempDir()
	result, err := ValidateDirectory(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, result)
}

func TestValidateDirectory_NonExistent(t *testing.T) {
	_, err := ValidateDirectory("/nonexistent-dir-12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestValidateDirectory_FileNotDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	_, err := ValidateDirectory(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestValidateDirectory_RelativePath(t *testing.T) {
	// A relative path should be resolved to absolute
	result, err := ValidateDirectory(".")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

func BenchmarkValidateDirectory(b *testing.B) {
	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ValidateDirectory(dir)
	}
}
