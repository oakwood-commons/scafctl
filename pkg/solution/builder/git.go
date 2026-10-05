// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/git"
)

// RepoStatusFunc reports the git status of dir. git.GetRepositoryStatus
// satisfies it.
type RepoStatusFunc func(ctx context.Context, dir string) (*git.RepoStatus, error)

// GitMetadata is the build provenance recorded on a packaged solution.
// The zero value means nothing is recorded: git metadata was disabled, the
// bundle root is not in a git repository, or git status could not be read.
type GitMetadata struct {
	// Commit is the short HEAD commit SHA.
	Commit string
	// Dirty is true when the working tree had uncommitted changes.
	Dirty bool
}

// Recorded reports whether there is git metadata to record.
func (g GitMetadata) Recorded() bool {
	return g.Commit != ""
}

// ResolveGitMetadata reads the git status of src.BundleRoot. When enabled is
// false (--no-git-metadata) it returns the zero GitMetadata without calling
// status.
//
// Git metadata is best effort: a non-nil error means the status could not be
// read; callers log it and continue with the zero GitMetadata it returns.
func ResolveGitMetadata(ctx context.Context, status RepoStatusFunc, src *SolutionSource, enabled bool) (GitMetadata, error) {
	if !enabled {
		return GitMetadata{}, nil
	}
	repo, err := status(ctx, src.BundleRoot)
	if err != nil {
		return GitMetadata{}, fmt.Errorf("reading git status of %s: %w", src.BundleRoot, err)
	}
	if repo == nil || !repo.IsRepo {
		return GitMetadata{}, nil
	}
	return GitMetadata{Commit: repo.Commit, Dirty: repo.IsDirty}, nil
}
