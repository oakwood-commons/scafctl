// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPProvider_StateCapability(t *testing.T) {
	p := NewHTTPProvider()
	desc := p.Descriptor()
	assert.Contains(t, desc.Capabilities, provider.CapabilityState)
	assert.Contains(t, desc.OutputSchemas, provider.CapabilityState)
}

func TestHTTPProvider_StateLoad_Success(t *testing.T) {
	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"greeting": {Value: "hello"},
	}
	body, err := json.Marshal(sd)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	loaded, ok := data["data"].(*state.Data)
	require.True(t, ok)
	assert.Equal(t, "hello", loaded.Values["greeting"].Value)
}

func TestHTTPProvider_StateLoad_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.NotNil(t, data["data"])
}

func TestHTTPProvider_StateLoad_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestHTTPProvider_StateLoad_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestHTTPProvider_StateSave_Success(t *testing.T) {
	var receivedBody []byte
	var receivedMethod string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"key": {Value: "value"},
	}

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_save",
		"url":       server.URL,
		"data":      sd,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	assert.Equal(t, http.MethodPut, receivedMethod)
	assert.Equal(t, "application/json", receivedContentType)

	var saved state.Data
	require.NoError(t, json.Unmarshal(receivedBody, &saved))
	assert.Equal(t, "value", saved.Values["key"].Value)
}

func TestHTTPProvider_StateSave_MissingData(t *testing.T) {
	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_save",
		"url":       "https://example.com/state",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data is required")
}

func TestHTTPProvider_StateSave_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_save",
		"url":       server.URL,
		"data":      state.NewData(),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 403")
}

func TestHTTPProvider_StateDelete_Success(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_delete",
		"url":       server.URL,
	})
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, http.MethodDelete, receivedMethod)
}

func TestHTTPProvider_StateDelete_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_delete",
		"url":       server.URL,
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
}

func TestHTTPProvider_StateDelete_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_delete",
		"url":       server.URL,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestHTTPProvider_StateMissingURL(t *testing.T) {
	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
}

func TestHTTPProvider_StateDryRun(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		hasData   bool
	}{
		{"load", "state_load", true},
		{"save", "state_save", false},
		{"delete", "state_delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewHTTPProvider()
			ctx := provider.WithDryRun(testContext(t), true)

			result, err := p.Execute(ctx, map[string]any{
				"operation": tt.operation,
				"url":       "https://example.com/state",
				"data":      state.NewData(),
			})
			require.NoError(t, err)
			data := result.Data.(map[string]any)
			assert.True(t, data["success"].(bool))
			if tt.hasData {
				assert.NotNil(t, data["data"])
			}
		})
	}
}

func TestHTTPProvider_StateWithHeaders(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("X-Custom-Token")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
		"headers": map[string]any{
			"X-Custom-Token": "my-secret",
		},
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, "my-secret", receivedAuth)
}

func TestHTTPProvider_StateUnsupportedOperation(t *testing.T) {
	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.dispatchStateOperation(ctx, "state_unknown", map[string]any{
		"url": "https://example.com/state",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported state operation")
}

func TestHTTPProvider_StateRoundTrip(t *testing.T) {
	// In-memory state store
	var stored []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored)
		case http.MethodPut:
			var err error
			stored, err = io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			stored = nil
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	// Load -- empty
	loadResult, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.NoError(t, err)
	assert.True(t, loadResult.Data.(map[string]any)["success"].(bool))

	// Save
	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"env": {Value: "production"},
	}
	saveResult, err := p.Execute(ctx, map[string]any{
		"operation": "state_save",
		"url":       server.URL,
		"data":      sd,
	})
	require.NoError(t, err)
	assert.True(t, saveResult.Data.(map[string]any)["success"].(bool))

	// Load again -- should have data
	loadResult2, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.NoError(t, err)
	loaded := loadResult2.Data.(map[string]any)["data"].(*state.Data)
	assert.Equal(t, "production", loaded.Values["env"].Value)

	// Delete
	delResult, err := p.Execute(ctx, map[string]any{
		"operation": "state_delete",
		"url":       server.URL,
	})
	require.NoError(t, err)
	assert.True(t, delResult.Data.(map[string]any)["success"].(bool))

	// Load after delete -- empty again
	loadResult3, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.NoError(t, err)
	assert.NotNil(t, loadResult3.Data.(map[string]any)["data"])
}

func TestHTTPProvider_StateWhatIf(t *testing.T) {
	p := NewHTTPProvider()
	desc := p.Descriptor()

	tests := []struct {
		operation string
		contains  string
	}{
		{"state_load", "Would load state from"},
		{"state_save", "Would save state to"},
		{"state_delete", "Would delete state at"},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			msg, err := desc.WhatIf(context.Background(), map[string]any{
				"operation": tt.operation,
				"url":       "https://api.example.com/state",
			})
			require.NoError(t, err)
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func BenchmarkHTTPProvider_StateLoad(b *testing.B) {
	sd := state.NewData()
	sd.Values = map[string]*state.Entry{"x": {Value: "y"}}
	body, _ := json.Marshal(sd)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(b)

	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, map[string]any{
			"operation": "state_load",
			"url":       server.URL,
		})
	}
}

func BenchmarkHTTPProvider_StateSave(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(b)
	sd := state.NewData()
	sd.Values = map[string]*state.Entry{"x": {Value: "y"}}

	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, map[string]any{
			"operation": "state_save",
			"url":       server.URL,
			"data":      sd,
		})
	}
}
