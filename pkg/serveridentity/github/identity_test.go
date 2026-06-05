package github

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0
import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHub_Name(t *testing.T) {
	key := generateTestKey(t)
	g, err := NewGitHubAppIdentity("Iv23li8abc123", 67890, key, "github.com")
	require.NoError(t, err)
	assert.Equal(t, "github", g.Name())
}

func TestNewGitHubIdentity_Validation(t *testing.T) {
	key := generateTestKey(t)

	tests := []struct {
		name           string
		clientID       string
		installationID int64
		key            *rsa.PrivateKey
		hostname       string
		wantErr        string
	}{
		{
			name:           "valid with explicit hostname",
			clientID:       "Iv23li8abc123",
			installationID: 2,
			key:            key,
			hostname:       "github.example.com",
		},
		{
			name:           "valid with empty hostname defaults",
			clientID:       "Iv23li8abc123",
			installationID: 2,
			key:            key,
			hostname:       "",
		},
		{
			name:           "missing clientID",
			clientID:       "",
			installationID: 2,
			key:            key,
			hostname:       "",
			wantErr:        "clientID is required",
		},
		{
			name:           "missing installationID",
			clientID:       "Iv23li8abc123",
			installationID: 0,
			key:            key,
			hostname:       "",
			wantErr:        "installationID is required",
		},
		{
			name:           "missing private key",
			clientID:       "Iv23li8abc123",
			installationID: 2,
			key:            nil,
			hostname:       "",
			wantErr:        "private key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewGitHubAppIdentity(tt.clientID, tt.installationID, tt.key, tt.hostname)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, g)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, g)
			}
		})
	}
}

func TestNewGitHubIdentity_DefaultHostname(t *testing.T) {
	key := generateTestKey(t)
	g, err := NewGitHubAppIdentity("Iv23li8abc123", 2, key, "")
	require.NoError(t, err)
	assert.Equal(t, "github.com", g.Hostname)
}

func TestGitHub_GetToken_Integration(t *testing.T) {
	key := generateTestKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/app/installations/67890/access_tokens", r.URL.Path)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer ")
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		w.WriteHeader(http.StatusCreated)
		resp := installationTokenResponse{
			Token:     "ghs_test_token_abc123",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	tokenURL := server.URL + "/app/installations/67890/access_tokens"
	g, err := NewGitHubAppIdentity("Iv23li8abc123", 67890, key, "", WithHTTPClient(client), WithTokenURL(tokenURL))
	require.NoError(t, err)

	ctx := context.Background()
	token, err := g.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.ServerCaller})
	require.NoError(t, err)
	assert.Equal(t, "ghs_test_token_abc123", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestGitHub_GetToken_UnsupportedCaller(t *testing.T) {
	key := generateTestKey(t)
	g, err := NewGitHubAppIdentity("Iv23li8abc123", 67890, key, "")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = g.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.RequesterCaller})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported caller")
}

func TestGitHub_GetToken_WithoutManager(t *testing.T) {
	key := generateTestKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     "ghs_no_manager",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	g, err := NewGitHubAppIdentity("Iv23li8abc123", 67890, key, "",
		WithHTTPClient(client),
		WithTokenURL(server.URL+"/app/installations/67890/access_tokens"),
	)
	require.NoError(t, err)
	assert.Nil(t, g.manager)

	ctx := context.Background()
	token, err := g.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.ServerCaller})
	require.NoError(t, err)
	assert.Equal(t, "ghs_no_manager", token.AccessToken)
}

func TestGitHub_GetToken_WithManager_CachesResult(t *testing.T) {
	t.Parallel()

	key := generateTestKey(t)
	var hitCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     "ghs_cached",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	tokenCache := serveridentity.NewTokenCache[string, tokenprovider.Token](context.Background(), 100, 0, 5*time.Minute)
	mgr := manager.NewManager(
		manager.WithStore("test", tokenCache),
	)

	g, err := NewGitHubAppIdentity("Iv23li8abc123", 67890, key, "",
		WithHTTPClient(client),
		WithTokenURL(server.URL+"/app/installations/67890/access_tokens"),
		WithManager(mgr),
	)
	require.NoError(t, err)
	require.NotNil(t, g.manager)

	ctx := context.Background()

	tok1, err := g.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.ServerCaller})
	require.NoError(t, err)
	assert.Equal(t, "ghs_cached", tok1.AccessToken)

	tok2, err := g.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.ServerCaller})
	require.NoError(t, err)
	assert.Equal(t, "ghs_cached", tok2.AccessToken)

	assert.Equal(t, int32(1), hitCount.Load(), "second call should be served from cache")
}

func TestGitHub_Options(t *testing.T) {
	key := generateTestKey(t)
	customClient := httpc.NewClient(&httpc.ClientConfig{Timeout: 1 * time.Second})

	g, err := NewGitHubAppIdentity("Iv23li8abc123", 2, key, "", WithHTTPClient(customClient))
	require.NoError(t, err)
	assert.Equal(t, customClient, g.httpClient)
}

func TestGitHub_InterfaceCompliance(t *testing.T) {
	// Compile-time check is in identity.go, this is a runtime assertion.
	var ts tokenprovider.TokenProvider
	key := generateTestKey(t)
	g, err := NewGitHubAppIdentity("Iv23li8abc123", 2, key, "")
	require.NoError(t, err)
	ts = g
	assert.Equal(t, "github", ts.Name())
}

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}
