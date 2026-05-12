// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

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
		Value     any    `json:"value"`
		Type      string `json:"type,omitempty"`
		UpdatedAt string `json:"updatedAt,omitempty"`
		Immutable bool   `json:"immutable,omitempty"`
	}

	keys := make([]string, 0, len(sd.Values))
	for k := range sd.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	entries := make([]entryInfo, 0, len(sd.Values))
	for _, key := range keys {
		entry := sd.Values[key]
		info := entryInfo{
			Key:       key,
			Value:     entry.Value,
			Type:      entry.Type,
			Immutable: entry.Immutable,
		}
		if !entry.UpdatedAt.IsZero() {
			info.UpdatedAt = entry.UpdatedAt.Format("2006-01-02T15:04:05Z")
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

	entry, ok := sd.Values[key]
	if !ok {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("key %q not found in state", key),
			WithField("key"),
			WithSuggestion("Use state_list to see available keys"),
			WithRelatedTools("state_list"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"key":   key,
		"entry": entry,
	})
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
		// Delete a single key
		if _, ok := sd.Values[key]; !ok {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("key %q not found in state", key),
				WithField("key"),
				WithRelatedTools("state_list"),
			), nil
		}

		delete(sd.Values, key)
		if err := state.SaveToFile(path, sd); err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
		}

		return mcp.NewToolResultJSON(map[string]any{
			"success": true,
			"message": fmt.Sprintf("deleted key %q", key),
		})
	}

	// Clear all entries
	count := len(sd.Values)
	sd.Values = make(map[string]*state.Entry)
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

	// Check immutability
	if existing, ok := sd.Values[key]; ok && existing.Immutable {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("key %q is immutable and cannot be modified", key),
			WithField("key"),
			WithSuggestion("Use state_delete to remove the key first, then set it again"),
			WithRelatedTools("state_delete"),
		), nil
	}

	typ := request.GetString("type", "string")
	sd.Values[key] = &state.Entry{
		Value:     coerceStateValue(value, typ),
		Type:      typ,
		UpdatedAt: time.Now().UTC(),
	}

	if err := state.SaveToFile(path, sd); err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to save state: %v", err)), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"success": true,
		"message": fmt.Sprintf("set key %q", key),
	})
}

// coerceStateValue converts a string value to the appropriate Go type.
func coerceStateValue(raw, typ string) any {
	switch typ {
	case "int":
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return v
		}
	case "float":
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v
		}
	case "bool":
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}
	return raw
}
