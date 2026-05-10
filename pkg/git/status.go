// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package git provides utilities for inspecting Git repository state.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RepoStatus describes the state of a Git repository at a given path.
type RepoStatus struct {
	// IsRepo indicates whether the path is inside a git repository.
	IsRepo bool

	// IsDirty indicates whether the working tree has uncommitted changes.
	IsDirty bool

	// Commit is the short SHA of the HEAD commit (empty if not a repo).
	Commit string
}

// GetRepositoryStatus inspects the git repository at dir and returns its status.
// Returns a non-repo RepoStatus (IsRepo=false) if dir is not inside a git repository.
// Returns an error only for unexpected failures (not for "not a repo").
func GetRepositoryStatus(ctx context.Context, dir string) (*RepoStatus, error) {
	// Verify dir exists before shelling out to git.
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, fmt.Errorf("checking directory %s: %w", dir, statErr)
	}

	// Check if inside a git repo.
	revParseCmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := revParseCmd.Output()
	if err != nil {
		// Distinguish "not a git repo" from unexpected failures.
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("git not found on PATH: %w", err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git check canceled: %w", ctx.Err())
		}
		// Non-zero exit from git rev-parse means not inside a work tree.
		return &RepoStatus{IsRepo: false}, nil //nolint:nilerr // non-zero exit means not a repo
	}
	if strings.TrimSpace(string(out)) != "true" {
		return &RepoStatus{IsRepo: false}, nil
	}

	// Get HEAD commit short SHA.
	commitCmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", "HEAD")
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting HEAD commit: %w", err)
	}
	commit := strings.TrimSpace(string(commitOut))

	// Check for uncommitted changes.
	statusCmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("checking git status: %w", err)
	}
	isDirty := len(strings.TrimSpace(string(statusOut))) > 0

	return &RepoStatus{
		IsRepo:  true,
		IsDirty: isDirty,
		Commit:  commit,
	}, nil
}
