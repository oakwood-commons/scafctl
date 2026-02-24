// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleExtractResolverRefs(t *testing.T) {
	t.Run("extracts refs from go template", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "extract_resolver_refs"
		request.Params.Arguments = map[string]any{
			"text": "Hello {{ ._.config.host }}:{{ ._.config.port }} from {{ ._.environment.name }}",
			"type": "go-template",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.Equal(t, "inline", data["source"])
		assert.Equal(t, "go-template", data["sourceType"])

		refs, ok := data["references"].([]any)
		require.True(t, ok)
		assert.GreaterOrEqual(t, len(refs), 2)
	})

	t.Run("extracts refs from cel expression", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "extract_resolver_refs"
		request.Params.Arguments = map[string]any{
			"text": "_.config.host + ':' + string(_.config.port)",
			"type": "cel",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.Equal(t, "cel", data["sourceType"])
		refs := data["references"].([]any)
		assert.Contains(t, refs, "config")
	})

	t.Run("reads from file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Create a temp file with a Go template
		tmpDir := t.TempDir()
		tmplPath := filepath.Join(tmpDir, "test.tmpl")
		err = os.WriteFile(tmplPath, []byte("{{ ._.myresolver.value }}"), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "extract_resolver_refs"
		request.Params.Arguments = map[string]any{
			"file": tmplPath,
			"type": "go-template",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.Equal(t, "file", data["source"])
		refs := data["references"].([]any)
		assert.Contains(t, refs, "myresolver")
	})

	t.Run("requires text or file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("rejects unsupported type", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"text": "something",
			"type": "python",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("no refs returns empty list", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"text": "Hello {{ .name }}",
			"type": "go-template",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		count := data["count"].(float64)
		assert.Equal(t, float64(0), count)
	})
}
