// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreAndLoadMetadata(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "user@example.com",
			Name:    "Test User",
			Subject: "12345",
			Issuer:  "https://accounts.google.com",
		},
		Flow:                      auth.FlowInteractive,
		ClientID:                  "test-client-id",
		Project:                   "my-project",
		ImpersonateServiceAccount: "",
		Scopes:                    []string{"openid", "email"},
		RefreshTokenExpiresAt:     time.Now().Add(7 * 24 * time.Hour).Truncate(time.Millisecond),
	}

	// Store metadata
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Load metadata
	loaded, err := handler.loadMetadata(ctx)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "user@example.com", loaded.Claims.Email)
	assert.Equal(t, "Test User", loaded.Claims.Name)
	assert.Equal(t, auth.FlowInteractive, loaded.Flow)
	assert.Equal(t, "test-client-id", loaded.ClientID)
	assert.Equal(t, []string{"openid", "email"}, loaded.Scopes)
}

func TestLoadMetadata_NotFound(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	_, err = handler.loadMetadata(ctx)
	require.Error(t, err)
}

func TestLoadMetadata_CorruptedData(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// Store corrupted data
	err = store.Set(ctx, SecretKeyMetadata, []byte("not-valid-json"))
	require.NoError(t, err)

	_, err = handler.loadMetadata(ctx)
	require.Error(t, err)
}

func TestStoreAndLoadRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// Store refresh token
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("my-refresh-token"))
	require.NoError(t, err)

	// Load refresh token
	token, err := handler.loadRefreshToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "my-refresh-token", token)
}

func TestLoadRefreshToken_NotFound(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	_, err = handler.loadRefreshToken(ctx)
	require.Error(t, err)
}

func TestExtractClaimsFromIDToken(t *testing.T) {
	// The extractClaimsFromIDToken function is tested indirectly via roundtrip.
	// This verifies the JWT parsing utility.
	tests := []struct {
		name    string
		parts   int
		wantErr bool
	}{
		{
			name:    "empty token",
			parts:   0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractClaimsFromIDToken("")
			if tt.wantErr {
				require.Error(t, err)
			}
		})
	}
}

func makeTestJWT(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return "eyJhbGciOiJSUzI1NiJ9." + encodedPayload + ".signature"
}

func TestExtractClaimsFromIDToken_ValidJWT(t *testing.T) {
	idToken := makeTestJWT(t, map[string]interface{}{
		"iss":   "https://accounts.google.com",
		"sub":   "123456",
		"email": "alice@example.com",
		"name":  "Alice Smith",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	claims, err := extractClaimsFromIDToken(idToken)
	require.NoError(t, err)
	assert.Equal(t, "https://accounts.google.com", claims.Issuer)
	assert.Equal(t, "123456", claims.Subject)
	assert.Equal(t, "alice@example.com", claims.Email)
	assert.Equal(t, "Alice Smith", claims.Name)
	assert.Equal(t, "alice", claims.Username)
}

func TestExtractClaimsFromIDToken_InvalidBase64(t *testing.T) {
	_, err := extractClaimsFromIDToken("header.!!!invalid!!!.sig")
	require.Error(t, err)
}

func TestExtractClaimsFromIDToken_InvalidJSON(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	_, err := extractClaimsFromIDToken("h." + payload + ".s")
	require.Error(t, err)
}

func TestExtractClaims_NoIDToken(t *testing.T) {
	resp := &TokenResponse{AccessToken: "access-tok"}
	claims, err := extractClaims(resp)
	require.NoError(t, err)
	assert.Equal(t, "https://accounts.google.com", claims.Issuer)
}

func TestExtractClaims_WithIDToken(t *testing.T) {
	idToken := makeTestJWT(t, map[string]interface{}{
		"iss":   "https://accounts.google.com",
		"sub":   "999",
		"email": "bob@example.com",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	resp := &TokenResponse{IDToken: idToken}
	claims, err := extractClaims(resp)
	require.NoError(t, err)
	assert.Equal(t, "bob@example.com", claims.Email)
}

func TestStoreCredentials_WithRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	tokenResp := &TokenResponse{
		AccessToken:  "access-tok",
		RefreshToken: "refresh-tok",
	}

	err = handler.storeCredentials(ctx, tokenResp, auth.FlowInteractive, []string{"openid"}, "")
	require.NoError(t, err)

	// Verify refresh token stored
	rt, err := handler.loadRefreshToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "refresh-tok", rt)

	// Verify metadata stored
	meta, err := handler.loadMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, auth.FlowInteractive, meta.Flow)
	assert.NotEmpty(t, meta.SessionID)
}

func TestStoreCredentials_NoRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	tokenResp := &TokenResponse{AccessToken: "access-tok"}

	err = handler.storeCredentials(ctx, tokenResp, auth.FlowGcloudADC, []string{"openid"}, "existing-session")
	require.NoError(t, err)

	meta, err := handler.loadMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, "existing-session", meta.SessionID)
}

func TestStoreMetadataOnly(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	claims := &auth.Claims{
		Issuer:  "https://accounts.google.com",
		Subject: "svc-acc",
		Email:   "svc@project.iam.gserviceaccount.com",
	}

	err = handler.storeMetadataOnly(ctx, claims, auth.FlowGcloudADC, []string{"cloud-platform"})
	require.NoError(t, err)

	meta, err := handler.loadMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, auth.FlowGcloudADC, meta.Flow)
	assert.NotEmpty(t, meta.SessionID)
	assert.Equal(t, "svc@project.iam.gserviceaccount.com", meta.Claims.Email)
}
