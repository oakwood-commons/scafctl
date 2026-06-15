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

	t.Run("rejects multiple inputs", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"text": "{{ ._.appName }}",
			"file": "/some/file.tpl",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := extractText(t, result)
		assert.Contains(t, text, "only one of")
	})

	t.Run("rejects text and directory together", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"text":      "{{ ._.appName }}",
			"directory": t.TempDir(),
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

	t.Run("scans directory recursively", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Create a temp directory with template files
		tmpDir := t.TempDir()
		err = os.WriteFile(filepath.Join(tmpDir, "main.tpl"), []byte("{{ ._.appName }} by {{ ._.author }}"), 0o644)
		require.NoError(t, err)

		subDir := filepath.Join(tmpDir, "partials")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		err = os.WriteFile(filepath.Join(subDir, "header.tpl"), []byte("{{ ._.projectTitle }}"), 0o644)
		require.NoError(t, err)

		// Non-matching file should be ignored
		err = os.WriteFile(filepath.Join(tmpDir, "readme.md"), []byte("{{ ._.ignored }}"), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "extract_resolver_refs"
		request.Params.Arguments = map[string]any{
			"directory": tmpDir,
			"type":      "go-template",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.Equal(t, "directory", data["source"])
		refs := data["references"].([]any)
		assert.Contains(t, refs, "appName")
		assert.Contains(t, refs, "author")
		assert.Contains(t, refs, "projectTitle")
		assert.NotContains(t, refs, "ignored")
		assert.Equal(t, float64(3), data["count"])
		assert.Equal(t, float64(2), data["filesCount"])
	})

	t.Run("scans directory with custom glob", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		err = os.WriteFile(filepath.Join(tmpDir, "config.yaml.tpl"), []byte("host: {{ ._.hostname }}"), 0o644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "main.go.tmpl"), []byte("package {{ ._.packageName }}"), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "extract_resolver_refs"
		request.Params.Arguments = map[string]any{
			"directory": tmpDir,
			"type":      "go-template",
			"glob":      "*.yaml.tpl",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		refs := data["references"].([]any)
		assert.Contains(t, refs, "hostname")
		assert.NotContains(t, refs, "packageName")
	})

	t.Run("directory not found returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"directory": filepath.Join(t.TempDir(), "nonexistent", "subdir"),
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("empty directory returns zero results", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"directory": t.TempDir(),
			"type":      "go-template",
		}

		// Empty directory returns empty results, not an error
		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		assert.Equal(t, float64(0), data["count"])
	})

	t.Run("directory rejects unsupported type", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"directory": t.TempDir(),
			"type":      "invalid-type",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("directory rejects invalid glob pattern", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"directory": t.TempDir(),
			"type":      "go-template",
			"glob":      "[a-",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := extractText(t, result)
		assert.Contains(t, text, "invalid glob pattern")
	})

	t.Run("directory surfaces unreadable file warnings", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		// Create a valid template file
		err = os.WriteFile(filepath.Join(tmpDir, "good.tpl"), []byte("{{ ._.appName }}"), 0o644)
		require.NoError(t, err)

		// Create a file that's not readable (to trigger read warnings)
		badDir := filepath.Join(tmpDir, "restricted")
		require.NoError(t, os.MkdirAll(badDir, 0o755))
		err = os.WriteFile(filepath.Join(badDir, "secret.tpl"), []byte("{{ ._.secret }}"), 0o000)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"directory": tmpDir,
			"type":      "go-template",
		}

		result, err := srv.handleExtractResolverRefs(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		// The good file should still be processed
		refs := data["references"].([]any)
		assert.Contains(t, refs, "appName")

		// Warnings must be present for the unreadable file.
		// On some platforms (e.g. running as root), file permissions may not
		// produce the expected access denial -- skip assertion in that case.
		warnings, ok := data["warnings"]
		if !ok || len(warnings.([]any)) == 0 {
			// Check if we can actually read the restricted file (e.g. running as root)
			restrictedPath := filepath.Join(badDir, "secret.tpl")
			if _, readErr := os.ReadFile(restrictedPath); readErr != nil {
				t.Fatal("expected warnings for unreadable file, but none were returned")
			}
			t.Skip("platform/permissions did not produce unreadable file (likely running as root)")
		}
		assert.NotEmpty(t, warnings)
	})
}
