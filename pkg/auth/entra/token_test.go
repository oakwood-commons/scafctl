// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"net/http"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ensureOfflineAccess
// ============================================================================

func TestEnsureOfflineAccess(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{
			name:  "appends when missing",
			scope: "https://graph.microsoft.com/.default",
			want:  "https://graph.microsoft.com/.default offline_access",
		},
		{
			name:  "no-op when present",
			scope: "https://graph.microsoft.com/.default offline_access",
			want:  "https://graph.microsoft.com/.default offline_access",
		},
		{
			name:  "no-op when only offline_access",
			scope: "offline_access",
			want:  "offline_access",
		},
		{
			name:  "no-op when present among multiple",
			scope: "openid profile offline_access",
			want:  "openid profile offline_access",
		},
		{
			name:  "appends to multiple scopes",
			scope: "openid profile",
			want:  "openid profile offline_access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ensureOfflineAccess(tt.scope))
		})
	}
}

// ============================================================================
// mintToken claims challenge detection
// ============================================================================

func TestMintToken_ClaimsChallenge(t *testing.T) {
	// Set up a stored refresh token + metadata so mintToken can execute
	store := secrets.NewMockStore()
	require.NoError(t, store.Set(context.Background(), SecretKeyRefreshToken, []byte("refresh-token")))
	require.NoError(t, store.Set(context.Background(), SecretKeyMetadata,
		[]byte(`{"tenantId":"test-tenant","clientId":"test-client","loginFlow":"interactive"}`)))

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "interaction_required",
		"error_description": "AADSTS53003: Access blocked by Conditional Access.",
		"claims":            `eyJhY2Nlc3NfdG9rZW4iOnsiYWNycyI6eyJlc3NlbnRpYWwiOnRydWV9fX0=`,
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.mintToken(context.Background(), "https://graph.microsoft.com/.default")
	require.Error(t, err)

	var claimsErr *auth.ClaimsChallengeError
	require.ErrorAs(t, err, &claimsErr)
	assert.Equal(t, "https://graph.microsoft.com/.default", claimsErr.Scope)
	assert.NotEmpty(t, claimsErr.Claims)
	assert.True(t, auth.IsClaimsChallenge(err))
}

func TestMintToken_ClaimsChallengeWithoutClaimsField(t *testing.T) {
	// AADSTS53003 without a claims field should still trigger ClaimsChallengeError
	store := secrets.NewMockStore()
	require.NoError(t, store.Set(context.Background(), SecretKeyRefreshToken, []byte("refresh-token")))
	require.NoError(t, store.Set(context.Background(), SecretKeyMetadata,
		[]byte(`{"tenantId":"test-tenant","clientId":"test-client","loginFlow":"device_code"}`)))

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "interaction_required",
		"error_description": "AADSTS53003: Access blocked.",
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.mintToken(context.Background(), "some-scope")
	require.Error(t, err)
	assert.True(t, auth.IsClaimsChallenge(err))
}

func TestMintToken_IncludesOfflineAccess(t *testing.T) {
	store := secrets.NewMockStore()
	require.NoError(t, store.Set(context.Background(), SecretKeyRefreshToken, []byte("refresh-token")))
	require.NoError(t, store.Set(context.Background(), SecretKeyMetadata,
		[]byte(`{"tenantId":"test-tenant","clientId":"test-client","loginFlow":"interactive"}`)))

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "new-access-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "new-refresh-token",
		"scope":         "https://graph.microsoft.com/.default",
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.mintToken(context.Background(), "https://graph.microsoft.com/.default")
	require.NoError(t, err)

	// Verify offline_access was appended to the scope
	require.Len(t, mockHTTP.Requests, 1)
	sentScope := mockHTTP.Requests[0].Data.Get("scope")
	assert.Contains(t, sentScope, "offline_access")
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkEnsureOfflineAccess(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ensureOfflineAccess("https://graph.microsoft.com/.default")
	}
}
