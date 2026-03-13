// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath resolves a filesystem path based on the current execution context.
//
// When all of the following are true, the path is resolved against the output directory:
//   - The path is relative
//   - The execution mode is CapabilityAction
//   - An output directory is set in the context
//
// Otherwise, the path is resolved against the context working directory (set via
// WithWorkingDirectory), falling back to the process CWD when no context directory
// is set.
//
// When resolving against an output directory, the result is validated to ensure it
// does not escape the output directory via parent traversal (e.g., "../../../etc/passwd").
func ResolvePath(ctx context.Context, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	mode, modeOK := ExecutionModeFromContext(ctx)
	if modeOK && mode == CapabilityAction {
		if outputDir, dirOK := OutputDirectoryFromContext(ctx); dirOK && outputDir != "" {
			resolved := filepath.Clean(filepath.Join(outputDir, path))
			if err := validatePathContainment(outputDir, resolved); err != nil {
				return "", fmt.Errorf("path %q resolves outside output directory %q: %w", path, outputDir, err)
			}
			return resolved, nil
		}
	}

	return AbsFromContext(ctx, path)
}

// GetWorkingDirectory returns the effective working directory from the context.
// It checks the context for a logical working directory first (set via
// WithWorkingDirectory), then falls back to the process CWD via os.Getwd().
func GetWorkingDirectory(ctx context.Context) (string, error) {
	if cwd, ok := WorkingDirectoryFromContext(ctx); ok && cwd != "" {
		return cwd, nil
	}
	return os.Getwd()
}

// ValidateDirectory resolves a directory path to an absolute path and validates
// that it exists and is a directory. Returns the resolved absolute path or an error.
func ValidateDirectory(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving path %q: %w", dir, err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return "", fmt.Errorf("directory %q does not exist: %w", absDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", absDir)
	}
	return absDir, nil
}

// AbsFromContext resolves a relative path to an absolute path using the context
// working directory. If no working directory is set in the context, it falls back
// to filepath.Abs (which uses os.Getwd()).
//
// Note: this function does NOT perform path containment/traversal validation.
// If the caller needs to restrict resolved paths within a specific directory,
// use ResolvePath (which validates containment for output directories) or
// perform additional checks after calling this function.
func AbsFromContext(ctx context.Context, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if cwd, ok := WorkingDirectoryFromContext(ctx); ok && cwd != "" {
		return filepath.Join(cwd, path), nil
	}
	return filepath.Abs(path)
}

// validatePathContainment verifies that resolved is inside or equal to baseDir.
// Both paths must already be cleaned/absolute. Symlinks in the resolved path are
// evaluated to prevent escaping the base directory via symlink indirection.
func validatePathContainment(baseDir, resolved string) error {
	// Resolve symlinks on the base directory so comparisons are consistent.
	realBase, err := evalSymlinksExisting(baseDir)
	if err != nil {
		realBase = baseDir
	}

	// Resolve symlinks on as much of the resolved path as exists.
	realResolved, err := evalSymlinksExisting(resolved)
	if err != nil {
		return fmt.Errorf("evaluating symlinks: %w", err)
	}

	rel, err := filepath.Rel(realBase, realResolved)
	if err != nil {
		return fmt.Errorf("cannot compute relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolved path escapes base directory")
	}
	return nil
}

// evalSymlinksExisting resolves symlinks for the longest existing prefix of path,
// then appends the remaining (non-existent) suffix. This handles paths where
// intermediate directories exist as symlinks but the leaf does not yet exist.
func evalSymlinksExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	// Walk up until we find an existing ancestor.
	parent := filepath.Dir(path)
	if parent == path {
		// Root; nothing to resolve.
		return path, nil
	}
	resolvedParent, err := evalSymlinksExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}
