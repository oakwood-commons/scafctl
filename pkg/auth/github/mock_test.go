// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockHTTPClient_PostForm(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, map[string]string{"status": "ok"})

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://github.com/login/device/code", nil)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	reqs := mock.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Equal(t, "https://github.com/login/device/code", reqs[0].Endpoint)
}

func TestMockHTTPClient_Get(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, map[string]string{"login": "octocat"})

	ctx := context.Background()
	headers := map[string]string{"Authorization": "Bearer test"}
	resp, err := mock.Get(ctx, "https://api.github.com/user", headers)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	reqs := mock.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "GET", reqs[0].Method)
	assert.Equal(t, "Bearer test", reqs[0].Headers["Authorization"])
}

func TestMockHTTPClient_AddError(t *testing.T) {
	mock := NewMockHTTPClient()
	expectedErr := errors.New("network error")
	mock.AddError(expectedErr)

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://example.com", nil)
	if resp != nil {
		defer resp.Body.Close()
	}

	assert.ErrorIs(t, err, expectedErr)
}

func TestMockHTTPClient_MultipleResponses(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, map[string]string{"step": "1"})
	mock.AddResponse(http.StatusOK, map[string]string{"step": "2"})
	mock.AddResponse(http.StatusOK, map[string]string{"step": "3"})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		resp, err := mock.PostForm(ctx, "https://example.com", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}

	assert.Len(t, mock.GetRequests(), 3)
}

func TestMockHTTPClient_NoResponseConfigured(t *testing.T) {
	mock := NewMockHTTPClient()

	ctx := context.Background()
	resp, err := mock.PostForm(ctx, "https://example.com", nil)

	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMockHTTPClient_Reset(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, nil)
	ctx := context.Background()
	resp, _ := mock.PostForm(ctx, "https://example.com", nil)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Len(t, mock.GetRequests(), 1)

	mock.Reset()
	assert.Empty(t, mock.GetRequests())
}

func TestMockHTTPClient_CancelledContext(t *testing.T) {
	mock := NewMockHTTPClient()
	mock.AddResponse(http.StatusOK, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := mock.PostForm(ctx, "https://example.com", nil)
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, err)

	resp, err = mock.Get(ctx, "https://example.com", nil)
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, err)
}
