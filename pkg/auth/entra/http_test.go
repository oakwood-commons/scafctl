// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPClient_PostForm(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		err := r.ParseForm()
		require.NoError(t, err)
		assert.Equal(t, "test-value", r.Form.Get("test-key"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	client := NewDefaultHTTPClient()
	ctx := context.Background()

	data := map[string][]string{
		"test-key": {"test-value"},
	}

	resp, err := client.PostForm(ctx, server.URL, data)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDefaultHTTPClient_PostForm_Cancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewDefaultHTTPClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resp, err := client.PostForm(ctx, server.URL, nil)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
}

func TestMockHTTPClient_QueuedResponses(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, map[string]string{"status": "ok"})
	mock.AddResponse(http.StatusBadRequest, map[string]string{"error": "bad"})

	ctx := context.Background()

	// First call
	resp1, err := mock.PostForm(ctx, "http://example.com/first", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	// Second call
	resp2, err := mock.PostForm(ctx, "http://example.com/second", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
	resp2.Body.Close()

	// Check recorded requests
	requests := mock.GetRequests()
	assert.Len(t, requests, 2)
	assert.Equal(t, "http://example.com/first", requests[0].Endpoint)
	assert.Equal(t, "http://example.com/second", requests[1].Endpoint)
}

func TestMockHTTPClient_Error(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddError(assert.AnError)

	ctx := context.Background()

	resp, err := mock.PostForm(ctx, "http://example.com", nil)
	if resp != nil {
		resp.Body.Close()
	}
	require.ErrorIs(t, err, assert.AnError)
}

func TestMockHTTPClient_Reset(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, map[string]string{"status": "ok"})

	ctx := context.Background()

	resp, err := mock.PostForm(ctx, "http://example.com", nil)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Len(t, mock.GetRequests(), 1)

	mock.Reset()
	assert.Empty(t, mock.GetRequests())
	assert.Empty(t, mock.Responses)
}

func TestMockHTTPClient_NoResponseConfigured(t *testing.T) {
	mock := NewMockHTTPClient()
	ctx := context.Background()

	// Should return 500 when no responses configured
	resp, err := mock.PostForm(ctx, "http://example.com", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	resp.Body.Close()
}

func TestMockHTTPClient_ContextCancelled(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := mock.PostForm(ctx, "http://example.com", nil)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
}
