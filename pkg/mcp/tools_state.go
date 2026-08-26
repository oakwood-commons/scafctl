// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// registerStateTools registers state inspection MCP tools.
func (s *Server) registerStateTools() {
	listTool := mcp.NewTool("state_list",
		mcp.WithDescription(fmt.Sprintf("List the persisted resolver values in a %s state file. Returns the resolvers map (keyed by resolver name, each with value, type, immutable flag, and timestamps) -- the entries the state provider can read back on later runs. Use state_show to inspect the full state document.", s.name)),
		mcp.WithTitleAnnotation("List Persisted Resolvers"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path (relative to working directory or absolute)"),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(listTool, s.handleStateList)

	showTool := mcp.NewTool("state_show",
		mcp.WithDescription(fmt.Sprintf("Show the full contents of a %s state file. Returns the faithful on-disk state document (schemaVersion, metadata, command, parameters, resolvers, fingerprints). Use state_list for just the persisted resolver values.", s.name)),
		mcp.WithTitleAnnotation("Show Full State"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path (relative to working directory or absolute)"),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(showTool, s.handleStateShow)

	getTool := mcp.NewTool("state_get",
		mcp.WithDescription(fmt.Sprintf("Get a single entry from a %s state file by key. Returns the value, type, and metadata for the specified key.", s.name)),
		mcp.WithTitleAnnotation("Get State Entry"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path (relative to working directory or absolute)"),
		),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("State entry key to retrieve (typically a resolver name)"),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(getTool, s.handleStateGet)

	deleteTool := mcp.NewTool("state_delete",
		mcp.WithDescription(fmt.Sprintf("Delete a single entry from a %s state file by key, or clear all entries. This modifies the state file on disk.", s.name)),
		mcp.WithTitleAnnotation("Delete State Entry"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path (relative to working directory or absolute)"),
		),
		mcp.WithString("key",
			mcp.Description("State entry key to delete. Omit to clear all entries."),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(deleteTool, s.handleStateDelete)

	setTool := mcp.NewTool("state_set",
		mcp.WithDescription(fmt.Sprintf("Set or update a single entry in a %s state file. This modifies the state file on disk. Immutable entries cannot be overwritten.", s.name)),
		mcp.WithTitleAnnotation("Set State Entry"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path (relative to working directory or absolute)"),
		),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("State entry key to set"),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("Value to store"),
		),
		mcp.WithString("type",
			mcp.Description("Value type for coercion: string (default), int, float, bool"),
			enumOpt("string", "int", "float", "bool"),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescDefault),
		),
	)
	s.addTool(setTool, s.handleStateSet)
}

// stateBaseDir returns the base directory for resolving relative state paths.
// If cwd is provided, it is used directly; otherwise os.Getwd() is called.
func stateBaseDir(cwd string) (string, error) {
	if cwd != "" {
		return cwd, nil
	}
	return os.Getwd()
}

// handleStateList returns the persisted resolver values from a state file.
func (s *Server) handleStateList(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
			WithSuggestion("Provide the state file path (e.g., 'my-app-state.json')"),
		), nil
	}

	baseDir, err := stateBaseDir(request.GetString("cwd", ""))
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("cannot determine working directory: %v", err)), nil
	}

	sd, err := state.LoadFromFile(path, baseDir)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err),
			WithSuggestion("Check that the path is correct and the file is valid JSON"),
		), nil
	}

	// Return just the persisted resolvers -- the entries the state provider can
	// read back on later runs. Use state_show for the full document. Mirrors
	// `scafctl state list -o json`.
	return mcp.NewToolResultJSON(sd.Resolvers)
}

// handleStateShow returns the full on-disk state document.
func (s *Server) handleStateShow(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
			WithSuggestion("Provide the state file path (e.g., 'my-app-state.json')"),
		), nil
	}

	baseDir, err := stateBaseDir(request.GetString("cwd", ""))
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("cannot determine working directory: %v", err)), nil
	}

	sd, err := state.LoadFromFile(path, baseDir)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err),
			WithSuggestion("Check that the path is correct and the file is valid JSON"),
		), nil
	}

	// Return the faithful on-disk state document so callers always see the true
	// schema. This is schema-driven: new fields in the state format surface
	// automatically without changes here, mirroring `scafctl state show -o json`.
	return mcp.NewToolResultJSON(sd)
}

// handleStateGet retrieves a single state entry by key.
func (s *Server) handleStateGet(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
		), nil
	}

	key := request.GetString("key", "")
	if key == "" {
		return newStructuredError(ErrCodeInvalidInput, "key is required",
			WithField("key"),
			WithSuggestion("Use state_list to see available keys"),
			WithRelatedTools("state_list"),
		), nil
	}

	baseDir, err := stateBaseDir(request.GetString("cwd", ""))
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("cannot determine working directory: %v", err)), nil
	}

	sd, err := state.LoadFromFile(path, baseDir)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err)), nil
	}

	// Check parameters first, then persisted resolvers
	if val, ok := sd.Parameters[key]; ok {
		return mcp.NewToolResultJSON(map[string]any{
			"key":     key,
			"value":   val,
			"section": "parameters",
		})
	}

	if entry, ok := sd.Resolvers[key]; ok {
		section := "persisted"
		if entry.Immutable {
			section = "immutable"
		}
		return mcp.NewToolResultJSON(map[string]any{
			"key":       key,
			"value":     entry.Value,
			"type":      entry.Type,
			"section":   section,
			"immutable": entry.Immutable,
		})
	}

	return newStructuredError(ErrCodeNotFound, fmt.Sprintf("key %q not found in state", key),
		WithField("key"),
		WithSuggestion("Use state_list to see available keys"),
		WithRelatedTools("state_list"),
	), nil
}

// handleStateDelete deletes a single key or clears all entries from a state file.
func (s *Server) handleStateDelete(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
		), nil
	}

	baseDir, err := stateBaseDir(request.GetString("cwd", ""))
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("cannot determine working directory: %v", err)), nil
	}

	sd, err := state.LoadFromFile(path, baseDir)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err)), nil
	}

	key := request.GetString("key", "")

	if key != "" {
		// Delete a single key -- check both maps (mirrors CLI behavior)
		_, inParams := sd.Parameters[key]
		resEntry, inResolvers := sd.Resolvers[key]
		isImmutable := inResolvers && resEntry.Immutable

		if !inParams && !inResolvers {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("key %q not found in state", key),
				WithField("key"),
				WithRelatedTools("state_list"),
			), nil
		}

		if inParams {
			delete(sd.Parameters, key)
		}
		if inResolvers {
			delete(sd.Resolvers, key)
		}

		if err := state.SaveToFile(path, baseDir, sd); err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
		}

		var msg string
		switch {
		case inParams && isImmutable:
			msg = fmt.Sprintf("deleted parameter and immutable key %q", key)
		case inParams && inResolvers:
			msg = fmt.Sprintf("deleted parameter and persisted key %q", key)
		case isImmutable:
			msg = fmt.Sprintf("deleted immutable key %q", key)
		case inResolvers:
			msg = fmt.Sprintf("deleted persisted key %q", key)
		default:
			msg = fmt.Sprintf("deleted parameter %q", key)
		}

		return mcp.NewToolResultJSON(map[string]any{
			"success": true,
			"message": msg,
		})
	}

	// Clear all entries
	count := len(sd.Parameters) + len(sd.Resolvers) + len(sd.Fingerprints)
	sd.Parameters = make(map[string]any)
	sd.Resolvers = make(map[string]*state.PersistedEntry)
	sd.Fingerprints = make(map[string]*state.FingerprintEntry)
	if err := state.SaveToFile(path, baseDir, sd); err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"success": true,
		"message": fmt.Sprintf("cleared %d entries", count),
	})
}

// handleStateSet sets or updates a single state entry.
func (s *Server) handleStateSet(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
			WithSuggestion("Provide the state file path (e.g., 'my-app-state.json')"),
		), nil
	}

	key := request.GetString("key", "")
	if key == "" {
		return newStructuredError(ErrCodeInvalidInput, "key is required",
			WithField("key"),
		), nil
	}

	value := request.GetString("value", "")

	baseDir, err := stateBaseDir(request.GetString("cwd", ""))
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("cannot determine working directory: %v", err)), nil
	}

	sd, err := state.LoadFromFile(path, baseDir)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err),
			WithSuggestion("Check that the path is correct and the file is valid JSON"),
		), nil
	}

	typ := request.GetString("type", "string")
	coerced, coerceErr := coerceStateValue(value, typ)
	if coerceErr != nil {
		return newStructuredError(ErrCodeInvalidInput, coerceErr.Error(),
			WithField("type"),
			WithSuggestion(fmt.Sprintf("Provide a valid %s value", typ)),
		), nil
	}

	// Default to parameters section
	sd.Parameters[key] = coerced

	if err := state.SaveToFile(path, baseDir, sd); err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"success": true,
		"message": fmt.Sprintf("set key %q", key),
	})
}

// coerceStateValue converts a string value to the appropriate Go type.
// Returns an error if the value cannot be parsed as the requested type.
func coerceStateValue(raw, typ string) (any, error) {
	switch typ {
	case "int":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %w", raw, err)
		}
		return v, nil
	case "float":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as float: %w", raw, err)
		}
		return v, nil
	case "bool":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as bool: %w", raw, err)
		}
		return v, nil
	}
	return raw, nil
}
