// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/git"
)

func TestResolveGitMetadata(t *testing.T) {
	t.Parallel()

	statusErr := errors.New("git not found")
	tests := []struct {
		name       string
		enabled    bool
		status     *git.RepoStatus
		statusErr  error
		want       GitMetadata
		wantCalled bool
		wantErr    error
	}{
		{name: "disabled skips git", status: &git.RepoStatus{IsRepo: true, Commit: "abc1234"}},
		{name: "not a repository", enabled: true, status: &git.RepoStatus{}, wantCalled: true},
		{name: "nil status", enabled: true, wantCalled: true},
		{
			name: "clean repository", enabled: true, status: &git.RepoStatus{IsRepo: true, Commit: "abc1234"},
			want: GitMetadata{Commit: "abc1234"}, wantCalled: true,
		},
		{
			name: "dirty repository", enabled: true, status: &git.RepoStatus{IsRepo: true, IsDirty: true, Commit: "abc1234"},
			want: GitMetadata{Commit: "abc1234", Dirty: true}, wantCalled: true,
		},
		{name: "status error", enabled: true, statusErr: statusErr, wantCalled: true, wantErr: statusErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &SolutionSource{BundleRoot: t.TempDir()}
			var gotDir string
			called := false
			status := func(_ context.Context, dir string) (*git.RepoStatus, error) {
				called, gotDir = true, dir
				return tt.status, tt.statusErr
			}

			got, err := ResolveGitMetadata(t.Context(), status, src, tt.enabled)

			assert.Equal(t, tt.wantCalled, called, "status called")
			if called {
				assert.Equal(t, src.BundleRoot, gotDir, "status reads the bundle root")
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.Commit != "", got.Recorded())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestResolveGitMetadata_NotARepository(t *testing.T) {
	t.Parallel()

	got, err := ResolveGitMetadata(t.Context(), git.GetRepositoryStatus, &SolutionSource{BundleRoot: t.TempDir()}, true)
	require.NoError(t, err)
	assert.False(t, got.Recorded())
}
