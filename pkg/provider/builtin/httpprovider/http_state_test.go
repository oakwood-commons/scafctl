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
	"time"

	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
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
	sd.Parameters["greeting"] = "hello"
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
	assert.Equal(t, "hello", loaded.Parameters["greeting"])
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
	// A 404 reports absence so the loader starts from fresh state.
	assert.Equal(t, false, data[state.OutputKeyFound])
	assert.NotNil(t, data[state.OutputKeyData])
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
	assert.Contains(t, err.Error(), "read state schema version")
}

func TestHTTPProvider_StateLoad_IncompatibleSchemaVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Below the minimum floor AND a field whose type no longer matches the
		// current struct -- the version guard must win over the type error.
		_, _ = w.Write([]byte(`{"schemaVersion":1,"metadata":{"solution":{"nested":"object"}}}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"url":       server.URL,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, state.ErrIncompatibleSchemaVersion)
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
	sd.Parameters["key"] = "value"

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
	assert.Equal(t, "value", saved.Parameters["key"])
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
	sd.Parameters["env"] = "production"
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
	assert.Equal(t, "production", loaded.Parameters["env"])

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

func TestHTTPProvider_StateAuth_ForwardsHostnameAndRefreshes(t *testing.T) {
	t.Parallel()

	sd := state.NewData()
	sd.Parameters["cluster"] = "alpha"
	body, err := json.Marshal(sd)
	require.NoError(t, err)

	attempts := 0
	var lastAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		lastAuth = r.Header.Get("Authorization")
		if attempts == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	handler := newClusterAwareAuthHandler("cluster-oidc", map[string]*auth.Token{
		"https://api.cluster-a.example.com:6443": {
			AccessToken: "state-cluster-a-token",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))

	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: ptrs.BoolPtr(true)},
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"cluster-oidc": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{
							"cluster-a": "https://api.cluster-a.example.com:6443",
						},
					},
				},
			},
		},
	}
	ctx := auth.WithRegistry(config.WithConfig(context.Background(), cfg), reg)
	p := NewHTTPProvider()

	res, err := p.Execute(ctx, map[string]any{
		"operation":    "state_load",
		"url":          server.URL + "/state",
		"authProvider": "cluster-oidc",
		"hostname":     "cluster-a",
	})
	require.NoError(t, err)
	assert.True(t, res.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, "Bearer state-cluster-a-token", lastAuth)
	assert.Equal(t, 2, attempts)

	calls := handler.recordedCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", calls[0].Hostname)
	assert.Equal(t, sdkauth.ServerContextDelegated, calls[0].ServerContext)
	assert.False(t, calls[0].ForceRefresh)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", calls[1].Hostname)
	assert.True(t, calls[1].ForceRefresh)
}

func TestHTTPProvider_StateAuth_HeaderPassThrough(t *testing.T) {
	t.Parallel()
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx := middleware.WithContextTokens(testContext(t), map[string]string{"Github": "passthrough-state-token"})
	p := NewHTTPProvider()

	res, err := p.Execute(ctx, map[string]any{
		"operation":    "state_delete",
		"url":          server.URL + "/state",
		"authProvider": "github",
	})
	require.NoError(t, err)
	assert.True(t, res.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, "Bearer passthrough-state-token", receivedAuth)

	// Setting hostname without authProvider fails closed on state operations too.
	_, err = p.Execute(testContext(t), map[string]any{
		"operation": "state_load",
		"url":       server.URL + "/state",
		"hostname":  "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname requires authProvider to be set")
}

func TestHTTPProvider_StateSave_401RefreshReplaysBodyAndPreservesHostname(t *testing.T) {
	t.Parallel()

	attempts := 0
	var lastAuth string
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		if attempts == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lastAuth = r.Header.Get("Authorization")
		lastBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Handler intentionally omits TokenType ("") to verify Bearer default on refresh too.
	handler := newClusterAwareAuthHandler("cluster-save", map[string]*auth.Token{
		server.URL: {
			AccessToken: "save-cluster-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(handler))
	ctx := testContextWithCLIAuth(t, reg)

	sd := state.NewData()
	sd.Parameters["region"] = "us-east-1"

	p := NewHTTPProvider()
	res, err := p.Execute(ctx, map[string]any{
		"operation":    "state_save",
		"url":          server.URL + "/state",
		"authProvider": "cluster-save",
		"data":         sd,
		"headers": map[string]any{
			"authorization": "Bearer stale-header",
		},
	})
	require.NoError(t, err)
	assert.True(t, res.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, 2, attempts)
	assert.Equal(t, "Bearer save-cluster-token", lastAuth)

	var decoded state.Data
	require.NoError(t, json.Unmarshal(lastBody, &decoded))
	assert.Equal(t, "us-east-1", decoded.Parameters["region"])

	calls := handler.recordedCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, server.URL, calls[0].Hostname)
	assert.False(t, calls[0].ForceRefresh)
	assert.Equal(t, server.URL, calls[1].Hostname)
	assert.True(t, calls[1].ForceRefresh)
}

func TestHTTPProvider_StateOperations_ProfileQualifiedAuthProvider(t *testing.T) {
	t.Parallel()

	sd := state.NewData()
	sd.Parameters["env"] = "prod"
	encodedState, err := json.Marshal(sd)
	require.NoError(t, err)

	loadAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer profiled-state-token", r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			loadAttempts++
			if loadAttempts == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(encodedState)
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	clusterHandler := newClusterAwareAuthHandler("openshift", map[string]*auth.Token{
		"https://api.cluster-a.example.com:6443": {
			AccessToken: "profiled-state-token",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
	githubHandler := auth.NewMockHandler("github")
	githubHandler.SetToken(&auth.Token{
		AccessToken: "profiled-state-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(clusterHandler))
	require.NoError(t, reg.Register(githubHandler))

	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: ptrs.BoolPtr(true)},
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{
							"cluster-a": "https://api.cluster-a.example.com:6443",
						},
					},
				},
			},
		},
	}
	ctx := auth.WithRegistry(config.WithConfig(context.Background(), cfg), reg)
	p := NewHTTPProvider()

	// 1. state_load with "openshift@work" + alias + 401 refresh preserves profile="work" and Hostname.
	loadOut, err := p.Execute(ctx, map[string]any{
		"operation":    "state_load",
		"url":          server.URL + "/state",
		"authProvider": "openshift@work",
		"hostname":     "cluster-a",
	})
	require.NoError(t, err)
	assert.True(t, loadOut.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, []string{"work", "work"}, clusterHandler.recordedProfiles())
	calls := clusterHandler.recordedCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", calls[0].Hostname)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", calls[1].Hostname)

	// 2. state_save with "github@work" propagates profile="work".
	saveOut, err := p.Execute(ctx, map[string]any{
		"operation":    "state_save",
		"url":          server.URL + "/state",
		"authProvider": "github@work",
		"data":         sd,
	})
	require.NoError(t, err)
	assert.True(t, saveOut.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, "work", githubHandler.LastContextProfile)

	// 3. state_delete with "github@built-in" normalizes to the unnamed built-in profile.
	delOut, err := p.Execute(auth.WithProfile(ctx, "ambient"), map[string]any{
		"operation":    "state_delete",
		"url":          server.URL + "/state",
		"authProvider": "github@built-in",
	})
	require.NoError(t, err)
	assert.True(t, delOut.Data.(map[string]any)["success"].(bool))
	assert.Equal(t, "", githubHandler.LastContextProfile)
}

func BenchmarkHTTPProvider_StateLoad(b *testing.B) {
	sd := state.NewData()
	sd.Parameters["x"] = "y"
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
	sd.Parameters["x"] = "y"

	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, map[string]any{
			"operation": "state_save",
			"url":       server.URL,
			"data":      sd,
		})
	}
}
