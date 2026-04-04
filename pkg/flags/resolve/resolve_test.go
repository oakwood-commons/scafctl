// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/flags/resolve"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveValue_JSON(t *testing.T) {
	ctx := context.Background()

	t.Run("json object", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", `json://{"key":"value","count":42}`)
		require.NoError(t, err)

		m, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", m["key"])
		assert.Equal(t, float64(42), m["count"])
	})

	t.Run("json array", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", `json://[1,2,3]`)
		require.NoError(t, err)

		slice, ok := result.([]any)
		require.True(t, ok)
		assert.Len(t, slice, 3)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := resolve.ResolveValue(ctx, "test", `json://{invalid}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid JSON")
	})
}

func TestResolveValue_YAML(t *testing.T) {
	ctx := context.Background()

	t.Run("yaml object", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", `yaml://key: value`)
		require.NoError(t, err)

		m, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", m["key"])
	})

	t.Run("invalid yaml", func(t *testing.T) {
		_, err := resolve.ResolveValue(ctx, "test", "yaml://key: value\n\t\tbad: tabs")
		require.Error(t, err)
	})
}

func TestResolveValue_Base64(t *testing.T) {
	ctx := context.Background()

	t.Run("valid base64", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", `base64://SGVsbG8sIFdvcmxkIQ==`)
		require.NoError(t, err)

		bytes, ok := result.([]byte)
		require.True(t, ok)
		assert.Equal(t, []byte("Hello, World!"), bytes)
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, err := resolve.ResolveValue(ctx, "test", `base64://not@valid#base64$`)
		require.Error(t, err)
	})
}

func TestResolveValue_File(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("test file content")
	err := os.WriteFile(tmpFile, testContent, 0o600)
	require.NoError(t, err)

	t.Run("valid file", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", "file://"+tmpFile)
		require.NoError(t, err)

		bytes, ok := result.([]byte)
		require.True(t, ok)
		assert.Equal(t, testContent, bytes)
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := resolve.ResolveValue(ctx, "test", "file:///nonexistent/file.txt")
		require.Error(t, err)
	})
}

func TestResolveValue_HTTP(t *testing.T) {
	ctx := context.Background()

	testContent := []byte("server response content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testContent)
	}))
	defer server.Close()

	t.Run("valid http", func(t *testing.T) {
		result, err := resolve.ResolveValue(ctx, "test", server.URL)
		require.NoError(t, err)

		bytes, ok := result.([]byte)
		require.True(t, ok)
		assert.Equal(t, testContent, bytes)
	})
}

func TestResolveValue_HTTP_UserAgent(t *testing.T) {
	t.Run("default User-Agent", func(t *testing.T) {
		var gotUA string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()

		ctx := context.Background()
		_, err := resolve.ResolveValue(ctx, "test", srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "scafctl-flags-resolver/1.0", gotUA)
	})

	t.Run("custom BinaryName in context", func(t *testing.T) {
		var gotUA string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()

		run := &settings.Run{BinaryName: "mycli"}
		ctx := settings.IntoContext(context.Background(), run)
		_, err := resolve.ResolveValue(ctx, "test", srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "mycli-flags-resolver/1.0", gotUA)
	})
}

func TestResolveValue_NoScheme(t *testing.T) {
	ctx := context.Background()

	result, err := resolve.ResolveValue(ctx, "test", "plain value")
	require.NoError(t, err)

	str, ok := result.(string)
	require.True(t, ok)
	assert.Equal(t, "plain value", str)
}

func TestResolveAll(t *testing.T) {
	ctx := context.Background()

	input := map[string][]string{
		"config": {`json://{"key":"value"}`},
		"env":    {"production"},
	}

	resolved, err := resolve.ResolveAll(ctx, input)
	require.NoError(t, err)

	config := resolved["config"][0].(map[string]any)
	assert.Equal(t, "value", config["key"])

	env := resolved["env"][0].(string)
	assert.Equal(t, "production", env)
}

func TestHelperFunctions(t *testing.T) {
	data := map[string][]any{
		"single": {"value1"},
		"multi":  {"value1", "value2", "value3"},
	}

	t.Run("GetFirst", func(t *testing.T) {
		assert.Equal(t, "value1", resolve.GetFirst(data, "single"))
		assert.Equal(t, "value1", resolve.GetFirst(data, "multi"))
		assert.Nil(t, resolve.GetFirst(data, "nonexistent"))
	})

	t.Run("GetAll", func(t *testing.T) {
		assert.Equal(t, []any{"value1"}, resolve.GetAll(data, "single"))
		assert.Equal(t, []any{"value1", "value2", "value3"}, resolve.GetAll(data, "multi"))
		assert.Equal(t, []any{}, resolve.GetAll(data, "nonexistent"))
	})

	t.Run("Has", func(t *testing.T) {
		assert.True(t, resolve.Has(data, "single"))
		assert.True(t, resolve.Has(data, "multi"))
		assert.False(t, resolve.Has(data, "nonexistent"))
	})
}
