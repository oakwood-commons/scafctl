// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
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
