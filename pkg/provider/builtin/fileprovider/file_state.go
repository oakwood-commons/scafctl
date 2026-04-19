// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// executeStateLoad loads state from a JSON file in the XDG state directory.
func (p *FileProvider) executeStateLoad(absPath string) (*provider.Output, error) {
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		// First run -- return empty state
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	}

	data, err := os.ReadFile(absPath) //nolint:gosec // path is validated by caller
	if err != nil {
		return nil, fmt.Errorf("state load: %w", err)
	}

	var stateData state.Data
	if err := json.Unmarshal(data, &stateData); err != nil {
		return nil, fmt.Errorf("state load: unmarshal: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"data":    &stateData,
		},
	}, nil
}

// executeStateSave persists state as a JSON file, using atomic write (temp + rename).
func (p *FileProvider) executeStateSave(absPath string, inputs map[string]any) (*provider.Output, error) {
	rawData, ok := inputs["data"]
	if !ok {
		return nil, fmt.Errorf("state save: data is required")
	}

	jsonBytes, err := json.MarshalIndent(rawData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("state save: marshal: %w", err)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("state save: create directory: %w", err)
	}

	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("state save: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(jsonBytes); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("state save: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("state save: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, absPath); err != nil { //nolint:gosec // absPath validated by resolveStatePath
		return nil, fmt.Errorf("state save: rename: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDelete removes a state file.
func (p *FileProvider) executeStateDelete(absPath string) (*provider.Output, error) {
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	}

	if err := os.Remove(absPath); err != nil {
		return nil, fmt.Errorf("state delete: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDryRun handles dry-run mode for state operations.
func (p *FileProvider) executeStateDryRun(operation string) (*provider.Output, error) {
	switch operation {
	case "state_load":
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	case "state_save", "state_delete":
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	default:
		return nil, fmt.Errorf("unknown state operation: %s", operation)
	}
}

// dispatchStateOperation handles the state capability branch in Execute.
func (p *FileProvider) dispatchStateOperation(ctx context.Context, operation string, inputs map[string]any) (*provider.Output, error) {
	statePath, _ := inputs["path"].(string)
	if statePath == "" {
		return nil, fmt.Errorf("%s: path is required for state operations", ProviderName)
	}

	absPath, err := state.ResolveStatePath(statePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	if provider.DryRunFromContext(ctx) {
		return p.executeStateDryRun(operation)
	}

	switch operation {
	case "state_load":
		return p.executeStateLoad(absPath)
	case "state_save":
		return p.executeStateSave(absPath, inputs)
	case "state_delete":
		return p.executeStateDelete(absPath)
	default:
		return nil, fmt.Errorf("%s: unsupported state operation: %s", ProviderName, operation)
	}
}
