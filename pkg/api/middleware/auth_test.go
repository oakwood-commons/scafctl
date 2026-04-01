// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAzureOIDCAuth_MissingParams(t *testing.T) {
	tests := []struct {
		name     string
		tenant   string
		clientID string
	}{
		{"empty tenant", "", "client-id"},
		{"empty clientID", "tenant-id", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := NewAzureOIDCAuth(tt.tenant, tt.clientID, logr.Discard())
			require.Error(t, err)
			assert.Nil(t, mw)
		})
	}
}

func TestNewAzureOIDCAuth_ValidParams(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)
	assert.NotNil(t, mw)
}

func TestAzureOIDCAuth_MissingAuthHeader(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing Authorization header")
}

func TestAzureOIDCAuth_InvalidAuthHeaderFormat(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid Authorization header format")
}

func TestAzureOIDCAuth_InvalidToken(t *testing.T) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(t, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestClaimsFromContext_Nil(t *testing.T) {
	claims := ClaimsFromContext(context.Background())
	assert.Nil(t, claims)
}

func TestClaimsFromContext_Present(t *testing.T) {
	expected := &AuthClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Email:    "test@example.com",
		TenantID: "tenant-abc",
	}
	ctx := context.WithValue(context.Background(), claimsContextKey, expected)
	claims := ClaimsFromContext(ctx)
	require.NotNil(t, claims)
	assert.Equal(t, "user-123", claims.Subject)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "tenant-abc", claims.TenantID)
}

func BenchmarkAzureOIDCAuth_MissingHeader(b *testing.B) {
	mw, err := NewAzureOIDCAuth("tenant-id", "client-id", logr.Discard())
	require.NoError(b, err)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
