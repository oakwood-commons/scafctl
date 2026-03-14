// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePollConfig(t *testing.T) {
	t.Run("no poll config", func(t *testing.T) {
		cfg, err := parsePollConfig(map[string]any{})
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("nil poll", func(t *testing.T) {
		cfg, err := parsePollConfig(map[string]any{"poll": nil})
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("missing until", func(t *testing.T) {
		_, err := parsePollConfig(map[string]any{
			"poll": map[string]any{
				"interval": "5s",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "until is required")
	})

	t.Run("valid full config", func(t *testing.T) {
		cfg, err := parsePollConfig(map[string]any{
			"poll": map[string]any{
				"until":       "_.body.status == 'done'",
				"failWhen":    "_.body.status == 'failed'",
				"interval":    "5s",
				"maxAttempts": float64(10),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "_.body.status == 'done'", cfg.Until)
		assert.Equal(t, "_.body.status == 'failed'", cfg.FailWhen)
		assert.Equal(t, 5*time.Second, cfg.Interval)
		assert.Equal(t, 10, cfg.MaxAttempts)
	})

	t.Run("defaults applied", func(t *testing.T) {
		cfg, err := parsePollConfig(map[string]any{
			"poll": map[string]any{
				"until": "_.statusCode == 200",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 10*time.Second, cfg.Interval)
		assert.Equal(t, 30, cfg.MaxAttempts)
	})

	t.Run("numeric interval seconds", func(t *testing.T) {
		cfg, err := parsePollConfig(map[string]any{
			"poll": map[string]any{
				"until":    "true",
				"interval": float64(15),
			},
		})
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, cfg.Interval)
	})

	t.Run("interval too short", func(t *testing.T) {
		_, err := parsePollConfig(map[string]any{
			"poll": map[string]any{
				"until":    "true",
				"interval": "500ms",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 1s")
	})
}

func TestHTTPProvider_Execute_Poll_UntilConditionMet(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if count >= 3 {
			_, _ = w.Write([]byte(`{"status":"succeeded"}`))
		} else {
			_, _ = w.Write([]byte(`{"status":"running"}`))
		}
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":           server.URL,
		"method":        "GET",
		"autoParseJson": true,
		"poll": map[string]any{
			"until":       "_.body.status == 'succeeded'",
			"interval":    "1s",
			"maxAttempts": float64(5),
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	body := data["body"].(map[string]any)
	assert.Equal(t, "succeeded", body["status"])
	assert.GreaterOrEqual(t, int(callCount.Load()), 3)
}

func TestHTTPProvider_Execute_Poll_FailWhen(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if count >= 2 {
			_, _ = w.Write([]byte(`{"status":"failed","error":"deployment crashed"}`))
		} else {
			_, _ = w.Write([]byte(`{"status":"running"}`))
		}
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":           server.URL,
		"method":        "GET",
		"autoParseJson": true,
		"poll": map[string]any{
			"until":       "_.body.status == 'succeeded'",
			"failWhen":    "_.body.status == 'failed'",
			"interval":    "1s",
			"maxAttempts": float64(10),
		},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failWhen condition met")
}

func TestHTTPProvider_Execute_Poll_MaxAttemptsExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":           server.URL,
		"method":        "GET",
		"autoParseJson": true,
		"poll": map[string]any{
			"until":       "_.body.status == 'succeeded'",
			"interval":    "1s",
			"maxAttempts": float64(2),
		},
	}

	output, err := p.Execute(ctx, inputs)
	// Max attempts exhausted returns the last output, not an error
	require.NoError(t, err)
	require.NotNil(t, output)
	data := output.Data.(map[string]any)
	body := data["body"].(map[string]any)
	assert.Equal(t, "running", body["status"])
}

func TestHTTPProvider_Execute_Poll_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
	defer cancel()

	inputs := map[string]any{
		"url":           server.URL,
		"method":        "GET",
		"autoParseJson": true,
		"poll": map[string]any{
			"until":       "_.body.status == 'succeeded'",
			"interval":    "5s",
			"maxAttempts": float64(100),
		},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestHTTPProvider_Execute_Poll_WithStringBody(t *testing.T) {
	// Test polling with autoParseJson=false (body is a string)
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := callCount.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if count >= 2 {
			_, _ = fmt.Fprint(w, "READY")
		} else {
			_, _ = fmt.Fprint(w, "PENDING")
		}
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"poll": map[string]any{
			"until":       "_.body == 'READY'",
			"interval":    "1s",
			"maxAttempts": float64(5),
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	data := output.Data.(map[string]any)
	assert.Equal(t, "READY", data["body"])
}
