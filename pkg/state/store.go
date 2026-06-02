// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// LoadFromFile reads and unmarshals a StateData JSON file.
// If the file does not exist, it returns an empty StateData.
// The path is resolved relative to paths.StateDir() unless absolute.
func LoadFromFile(path string) (*Data, error) {
	absPath, err := ResolveStatePath(path)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return NewData(), nil
	}

	data, err := os.ReadFile(absPath) //nolint:gosec // path validated by ResolveStatePath
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var sd Data
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, fmt.Errorf("unmarshal state file: %w", err)
	}

	if sd.SchemaVersion > SchemaVersionCurrent {
		return nil, fmt.Errorf("%w: file version %d is newer than supported version %d; upgrade scafctl",
			ErrUnsupportedSchemaVersion, sd.SchemaVersion, SchemaVersionCurrent)
	}

	if sd.Parameters == nil {
		sd.Parameters = make(map[string]any)
	}
	if sd.Immutables == nil {
		sd.Immutables = make(map[string]*ImmutableEntry)
	}
	if sd.Fingerprints == nil {
		sd.Fingerprints = make(map[string]*FingerprintEntry)
	}
	if sd.Command.Parameters == nil {
		sd.Command.Parameters = make(map[string]string)
	}

	return &sd, nil
}

// SaveToFile marshals and writes StateData to a JSON file using atomic write.
// The path is resolved relative to paths.StateDir() unless absolute.
func SaveToFile(path string, sd *Data) error {
	absPath, err := ResolveStatePath(path)
	if err != nil {
		return err
	}

	jsonBytes, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(jsonBytes); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	renameErr := os.Rename(tmpName, absPath) //nolint:gosec // absPath validated by ResolveStatePath
	if renameErr != nil {
		// Windows does not reliably support renaming over an existing file.
		// Retry after removing the destination when it already exists.
		if _, statErr := os.Stat(absPath); statErr == nil {
			if removeErr := os.Remove(absPath); removeErr == nil {
				renameErr = os.Rename(tmpName, absPath) //nolint:gosec // same rationale as above
			}
		}
		if renameErr != nil {
			return fmt.Errorf("rename temp file: %w", renameErr)
		}
	}

	return nil
}

// ResolveStatePath resolves a state file path. Absolute paths are used as-is.
// Relative paths are resolved against paths.StateDir().
// Path traversal (../) is rejected.
func ResolveStatePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("state path is required")
	}

	// Reject path traversal by checking individual segments
	cleaned := filepath.Clean(path)
	for _, seg := range strings.Split(cleaned, string(filepath.Separator)) {
		if seg == ".." {
			return "", fmt.Errorf("path traversal not allowed: %s", path)
		}
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	return filepath.Join(paths.StateDir(), cleaned), nil
}
