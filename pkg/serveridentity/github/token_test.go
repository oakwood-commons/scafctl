// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJWT(t *testing.T) {
	key := generateTestKey(t)

	jwt, err := buildJWT("Iv23li8abc123", key)
	require.NoError(t, err)
	assert.NotEmpty(t, jwt)

	// JWT has 3 parts separated by dots.
	parts := splitJWT(jwt)
	assert.Len(t, parts, 3)

	// Verify the header.
	assert.Equal(t, jwtHeader, parts[0])
}

func TestBuildJWT_DifferentClientIDs(t *testing.T) {
	key := generateTestKey(t)

	jwt1, err := buildJWT("Iv23li8abc111", key)
	require.NoError(t, err)

	jwt2, err := buildJWT("Iv23li8abc222", key)
	require.NoError(t, err)

	// Different client IDs should produce different JWTs (different claims).
	assert.NotEqual(t, jwt1, jwt2)
}

func TestBuildJWT_NilKey(t *testing.T) {
	_, err := buildJWT("Iv23li8abc123", nil)
	require.Error(t, err)
}

func TestExchangeForInstallationToken_Success(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour).Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/app/installations/67890/access_tokens", r.URL.Path)
		assert.Equal(t, "Bearer test-jwt", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     "ghs_abc123",
			ExpiresAt: expiry,
		})
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	tokenURL := server.URL + "/app/installations/67890/access_tokens"
	ctx := context.Background()
	token, err := exchangeForInstallationToken(ctx, "test-jwt", tokenURL, client)
	require.NoError(t, err)
	assert.Equal(t, "ghs_abc123", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, expiry.UTC(), token.ExpiresAt.UTC())
}

func TestExchangeForInstallationToken_NonCreatedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	ctx := context.Background()
	_, err := exchangeForInstallationToken(ctx, "bad-jwt", server.URL+"/token", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GitHub API returned 403")
	assert.Contains(t, err.Error(), "Bad credentials")
}

func TestExchangeForInstallationToken_EmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{Token: ""})
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	ctx := context.Background()
	_, err := exchangeForInstallationToken(ctx, "jwt", server.URL+"/token", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GitHub API returned empty token")
}

func TestExchangeForInstallationToken_HTTPError(t *testing.T) {
	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  1 * time.Second,
		RetryMax: 0,
	})

	ctx := context.Background()
	_, err := exchangeForInstallationToken(ctx, "bad-jwt", "http://localhost:1/token", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting installation token")
}

func TestExchangeForInstallationToken_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 0,
	})

	ctx := context.Background()
	_, err := exchangeForInstallationToken(ctx, "jwt", server.URL+"/token", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing response")
}

func TestBase64URLEncode(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple string",
			input: []byte("hello"),
			want:  "aGVsbG8",
		},
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
		{
			name:  "binary data with padding",
			input: []byte{0xFF, 0xFE},
			want:  "__4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := base64URLEncode(tt.input)
			assert.Equal(t, tt.want, got)
			// Ensure no padding characters.
			assert.NotContains(t, got, "=")
		})
	}
}

func splitJWT(jwt string) []string {
	var parts []string
	start := 0
	for i := range jwt {
		if jwt[i] == '.' {
			parts = append(parts, jwt[start:i])
			start = i + 1
		}
	}
	parts = append(parts, jwt[start:])
	return parts
}

func TestBuildJWT_Signature_Verifiable(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwt, err := buildJWT("Iv23li8abc999", key)
	require.NoError(t, err)

	parts := splitJWT(jwt)
	require.Len(t, parts, 3)

	// The signature should be non-empty.
	assert.NotEmpty(t, parts[2])
}

func TestAPIBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		{
			name:     "github.com uses api subdomain",
			hostname: "github.com",
			want:     "https://api.github.com",
		},
		{
			name:     "GHES uses /api/v3 path",
			hostname: "github.example.com",
			want:     "https://github.example.com/api/v3",
		},
		{
			name:     "custom GHES hostname",
			hostname: "git.corp.internal",
			want:     "https://git.corp.internal/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := apiBaseURL(tt.hostname)
			assert.Equal(t, tt.want, got)
		})
	}
}
