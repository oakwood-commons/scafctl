// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockHTTPClient_PostForm(t *testing.T) {
	mock := NewMockHTTPClient()

	expected := map[string]string{"access_token": "test-token"}
	mock.AddResponse(200, expected)

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://oauth2.googleapis.com/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"test-code"},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"test-client-id"},
		"code_verifier": {"test-verifier"},
	})
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "test-token", result["access_token"])

	// Verify request was recorded
	require.Len(t, mock.Requests, 1)
	assert.Equal(t, "https://oauth2.googleapis.com/token", mock.Requests[0].Endpoint)
	assert.Equal(t, "POST", mock.Requests[0].Method)
}

func TestMockHTTPClient_Get(t *testing.T) {
	mock := NewMockHTTPClient()

	expected := map[string]string{"email": "user@example.com"}
	mock.AddResponse(200, expected)

	ctx := context.Background()
	headers := map[string]string{
		"Authorization": "Bearer test-token",
	}
	resp, err := mock.Get(ctx, "https://www.googleapis.com/oauth2/v3/userinfo", headers)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", result["email"])

	// Verify request was recorded
	require.Len(t, mock.Requests, 1)
	assert.Equal(t, "GET", mock.Requests[0].Method)
	assert.Equal(t, "Bearer test-token", mock.Requests[0].Headers["Authorization"])
}

func TestMockHTTPClient_Error(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddError(fmt.Errorf("network error"))

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://example.com", nil) //nolint:bodyclose // resp is nil on error
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "network error")
}

func TestMockHTTPClient_NoResponses(t *testing.T) {
	mock := NewMockHTTPClient()

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://example.com", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 500 when no responses configured
	assert.Equal(t, 500, resp.StatusCode)
}

func TestMockHTTPClient_Reset(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(200, "ok")

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://example.com", nil)
	require.NoError(t, err)
	resp.Body.Close()

	require.Len(t, mock.Requests, 1)

	// Clear requests manually (no Reset method)
	mock.Requests = nil
	assert.Empty(t, mock.Requests)
}

func TestDefaultHTTPClient(t *testing.T) {
	client := NewDefaultHTTPClient()
	require.NotNil(t, client)
}

func TestMockHTTPClient_Do(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(200, map[string]string{"status": "ok"})

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/api", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := mock.Do(ctx, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]string
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])

	// Verify request was recorded
	require.Len(t, mock.Requests, 1)
	assert.Equal(t, "POST", mock.Requests[0].Method)
}

func TestMockHTTPClient_MultipleResponses(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(200, map[string]string{"call": "first"})
	mock.AddResponse(201, map[string]string{"call": "second"})

	ctx := context.Background()

	resp1, err := mock.PostForm(ctx, "https://example.com/1", nil)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, 200, resp1.StatusCode)

	resp2, err := mock.PostForm(ctx, "https://example.com/2", nil)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, 201, resp2.StatusCode)
}
