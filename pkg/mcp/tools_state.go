// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// registerStateTools registers state inspection MCP tools.
func (s *Server) registerStateTools() {
	listTool := mcp.NewTool("state_list",
		mcp.WithDescription(fmt.Sprintf("List all entries in a %s state file. Shows key names, types, values, and timestamps. Use this to inspect persisted resolver values between solution runs.", s.name)),
		mcp.WithTitleAnnotation("List State Entries"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("State file path relative to the XDG state directory (e.g., 'my-app-state.json')"),
		),
	)
	s.addTool(listTool, s.handleStateList)

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
			mcp.Description("State file path relative to the XDG state directory"),
		),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("State entry key to retrieve (typically a resolver name)"),
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
			mcp.Description("State file path relative to the XDG state directory"),
		),
		mcp.WithString("key",
			mcp.Description("State entry key to delete. Omit to clear all entries."),
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
			mcp.Description("State file path relative to the XDG state directory"),
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
			mcp.Enum("string", "int", "float", "bool"),
		),
	)
	s.addTool(setTool, s.handleStateSet)
}

// handleStateList lists all entries in a state file.
func (s *Server) handleStateList(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	if path == "" {
		return newStructuredError(ErrCodeInvalidInput, "path is required",
			WithField("path"),
			WithSuggestion("Provide the state file path (e.g., 'my-app-state.json')"),
		), nil
	}

	sd, err := state.LoadFromFile(path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err),
			WithSuggestion("Check that the path is correct and the file is valid JSON"),
		), nil
	}

	type entryInfo struct {
		Key       string `json:"key"`
		Value     any    `json:"value,omitempty"`
		Type      string `json:"type,omitempty"`
		Section   string `json:"section"`
		CreatedAt string `json:"createdAt,omitempty"`
		Readonly  bool   `json:"readonly,omitempty"`
	}

	totalEntries := len(sd.Parameters) + len(sd.Immutables)

	paramKeys := make([]string, 0, len(sd.Parameters))
	for k := range sd.Parameters {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)

	immKeys := make([]string, 0, len(sd.Immutables))
	for k := range sd.Immutables {
		immKeys = append(immKeys, k)
	}
	sort.Strings(immKeys)

	entries := make([]entryInfo, 0, totalEntries)
	for _, key := range paramKeys {
		entries = append(entries, entryInfo{
			Key:     key,
			Value:   sd.Parameters[key],
			Section: "parameters",
		})
	}
	for _, key := range immKeys {
		entry := sd.Immutables[key]
		info := entryInfo{
			Key:      key,
			Value:    entry.Value,
			Type:     entry.Type,
			Section:  "immutables",
			Readonly: true,
		}
		if !entry.CreatedAt.IsZero() {
			info.CreatedAt = entry.CreatedAt.Format("2006-01-02T15:04:05Z")
		}
		entries = append(entries, info)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"path":     path,
		"count":    len(entries),
		"entries":  entries,
		"metadata": sd.Metadata,
	})
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

	sd, err := state.LoadFromFile(path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err)), nil
	}

	// Check parameters first, then immutables
	if val, ok := sd.Parameters[key]; ok {
		return mcp.NewToolResultJSON(map[string]any{
			"key":     key,
			"value":   val,
			"section": "parameters",
		})
	}

	if entry, ok := sd.Immutables[key]; ok {
		return mcp.NewToolResultJSON(map[string]any{
			"key":     key,
			"value":   entry.Value,
			"type":    entry.Type,
			"section": "immutables",
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

	sd, err := state.LoadFromFile(path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load state: %v", err)), nil
	}

	key := request.GetString("key", "")

	if key != "" {
		// Delete a single key -- check parameters first, then immutables
		if _, ok := sd.Parameters[key]; ok {
			delete(sd.Parameters, key)
			if err := state.SaveToFile(path, sd); err != nil {
				return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
			}
			return mcp.NewToolResultJSON(map[string]any{
				"success": true,
				"message": fmt.Sprintf("deleted parameter %q", key),
			})
		}

		if _, ok := sd.Immutables[key]; ok {
			delete(sd.Immutables, key)
			if err := state.SaveToFile(path, sd); err != nil {
				return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
			}
			return mcp.NewToolResultJSON(map[string]any{
				"success": true,
				"message": fmt.Sprintf("deleted immutable key %q", key),
			})
		}

		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("key %q not found in state", key),
			WithField("key"),
			WithRelatedTools("state_list"),
		), nil
	}

	// Clear all entries
	count := len(sd.Parameters) + len(sd.Immutables) + len(sd.Fingerprints)
	sd.Parameters = make(map[string]any)
	sd.Immutables = make(map[string]*state.ImmutableEntry)
	sd.Fingerprints = make(map[string]*state.FingerprintEntry)
	if err := state.SaveToFile(path, sd); err != nil {
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

	sd, err := state.LoadFromFile(path)
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

	if err := state.SaveToFile(path, sd); err != nil {
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
