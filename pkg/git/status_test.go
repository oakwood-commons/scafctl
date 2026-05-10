// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initTestRepo creates a temporary git repo with one commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, string(out))
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	// Create initial commit.
	f := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(f, []byte("hello"), 0o644))
	run("add", ".")
	run("commit", "-m", "initial")

	return dir
}

func TestGetRepositoryStatus_CleanRepo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	status, err := GetRepositoryStatus(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, status.IsRepo)
	assert.False(t, status.IsDirty)
	assert.NotEmpty(t, status.Commit)
}

func TestGetRepositoryStatus_DirtyRepo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Make the tree dirty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("dirty"), 0o644))

	status, err := GetRepositoryStatus(t.Context(), dir)
	require.NoError(t, err)
	assert.True(t, status.IsRepo)
	assert.True(t, status.IsDirty)
	assert.NotEmpty(t, status.Commit)
}

func TestGetRepositoryStatus_NotARepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	status, err := GetRepositoryStatus(t.Context(), dir)
	require.NoError(t, err)
	assert.False(t, status.IsRepo)
	assert.False(t, status.IsDirty)
	assert.Empty(t, status.Commit)
}

func TestGetRepositoryStatus_CanceledContext(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := GetRepositoryStatus(ctx, dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

func TestGetRepositoryStatus_DirNotExist(t *testing.T) {
	t.Parallel()

	_, err := GetRepositoryStatus(t.Context(), "/nonexistent/path/that/does/not/exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking directory")
}
