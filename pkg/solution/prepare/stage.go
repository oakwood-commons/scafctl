// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
)

var (
	// ErrInvalidLock is returned by Stage when the solution's lock layer is
	// present but cannot be parsed.
	ErrInvalidLock = errors.New("invalid solution lock")
	// ErrInvalidBundle is returned by Stage when the solution's bundle layer
	// is rejected (corrupt archive, unsafe paths, or over the size limit -- the
	// latter also matches bundler.ErrBundleTooLarge). Host filesystem failures
	// while writing the extracted files (disk full, I/O errors) are returned
	// without it, since the archive itself may be valid.
	ErrInvalidBundle = errors.New("invalid solution bundle")

	errStageNilSolution = errors.New("stage: materialized solution is nil")
)

// StageOptions configures Stage.
type StageOptions struct {
	// Entrypoint is recorded as the host entrypoint (EntrypointAPI, ...) in the
	// per-execution metadata settings delivered to providers.
	Entrypoint string
	// MaxBundleSize caps the bytes extracted from the bundle, nested bundles
	// included. Zero or negative uses bundler.DefaultMaxBundleSize.
	MaxBundleSize int64
}

// Staged is a solution made ready to execute for a single request without
// touching process-global state: the bundle is extracted to a private
// directory, the lock is parsed in memory, and nothing is written next to the
// bundle files. Callers must invoke Cleanup when execution finishes.
type Staged struct {
	// Solution is the loaded solution with bundle plugin defaults merged in.
	Solution *solution.Solution
	// Lock is the parsed lock layer; nil when the solution has none.
	Lock *bundler.LockFile
	// SolutionDir is the directory the bundle was extracted to; empty when the
	// solution has no bundle.
	SolutionDir string

	execSettings map[string]json.RawMessage
	cleanupOnce  sync.Once
}

// Stage prepares ms for execution in a long-lived, concurrent host such as the
// API server. Unlike SolutionFromLoader it never changes the process working
// directory and never writes the solution or lock to disk; bundle-relative
// paths are resolved through the context returned by Staged.Context instead.
// On error nothing is left on disk.
func Stage(ctx context.Context, ms *load.MaterializedSolution, opts StageOptions) (*Staged, error) {
	if ms == nil || ms.Solution == nil {
		return nil, errStageNilSolution
	}

	// Parse the lock before extracting so a bad lock costs no disk work.
	lock, err := ms.LockFile()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidLock, err)
	}

	dir, err := extractBundleLimited(ctx, ms.Bundle, opts.MaxBundleSize, bundler.ExtractBundleTarLimited)
	if err != nil {
		return nil, err
	}

	sol := ms.Solution
	if len(sol.Bundle.Plugins) > 0 {
		bundler.MergePluginDefaults(sol)
	}

	return &Staged{
		Solution:     sol,
		Lock:         lock,
		SolutionDir:  dir,
		execSettings: buildPerSolutionSettings(opts.Entrypoint, sol),
	}, nil
}

// Context returns ctx carrying the solution directory (when a bundle was
// extracted) and the per-execution provider settings.
func (s *Staged) Context(ctx context.Context) context.Context {
	if s.SolutionDir != "" {
		ctx = provider.WithSolutionDirectory(ctx, s.SolutionDir)
	}
	if s.execSettings != nil {
		ctx = provider.WithExecutionSettings(ctx, s.execSettings)
	}
	return ctx
}

// Cleanup removes the extracted bundle directory. It is idempotent and safe on
// a nil receiver.
func (s *Staged) Cleanup() {
	if s == nil || s.SolutionDir == "" {
		return
	}
	s.cleanupOnce.Do(func() { _ = os.RemoveAll(s.SolutionDir) })
}

// bundleExtractor extracts a bundle archive into destDir, writing at most
// maxBytes. bundler.ExtractBundleTarLimited is the production implementation.
type bundleExtractor func(tarData []byte, destDir string, maxBytes int64) (*bundler.BundleManifest, error)

// extractBundleLimited extracts bundle into a new temp directory, capped at
// maxBytes (bundler.DefaultMaxBundleSize when non-positive). It returns "" when
// bundle is empty and removes the directory on error. Archive problems wrap
// ErrInvalidBundle; host filesystem failures (*fs.PathError from creating or
// writing the extracted files) do not.
func extractBundleLimited(ctx context.Context, bundle []byte, maxBytes int64, extract bundleExtractor) (string, error) {
	if len(bundle) == 0 {
		return "", nil
	}
	if maxBytes <= 0 {
		maxBytes = bundler.DefaultMaxBundleSize
	}

	dir, err := os.MkdirTemp("", paths.AppName()+"-bundle-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory for bundle: %w", err)
	}

	manifest, err := extract(bundle, dir, maxBytes)
	if err != nil {
		_ = os.RemoveAll(dir)
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return "", fmt.Errorf("failed to write extracted bundle: %w", err)
		}
		return "", fmt.Errorf("%w: %w", ErrInvalidBundle, err)
	}

	if lgr := logger.FromContext(ctx); lgr != nil {
		lgr.V(1).Info("staged solution bundle", "files", len(manifest.Files), "dir", dir)
	}
	return dir, nil
}
