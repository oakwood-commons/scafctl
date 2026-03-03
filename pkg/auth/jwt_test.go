// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildJWT constructs a minimal unsigned JWT from a claims payload for testing.
func buildJWT(t *testing.T, payload any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + encodedBody + ".sig"
}

func TestParseJWTClaims_FullIDToken(t *testing.T) {
	jwt := buildJWT(t, map[string]any{
		"iss":                "https://login.microsoftonline.com/tenant-123/v2.0",
		"sub":                "user-sub-abc",
		"aud":                "app-client-id",
		"tid":                "tenant-123",
		"oid":                "obj-456",
		"email":              "user@example.com",
		"preferred_username": "user@example.com",
		"name":               "Test User",
		"iat":                1700000000,
		"exp":                1700003600,
	})

	claims, err := ParseJWTClaims(jwt)
	require.NoError(t, err)

	assert.Equal(t, "https://login.microsoftonline.com/tenant-123/v2.0", claims.Issuer)
	assert.Equal(t, "user-sub-abc", claims.Subject)
	assert.Equal(t, "app-client-id", claims.ClientID)
	assert.Equal(t, "tenant-123", claims.TenantID)
	assert.Equal(t, "obj-456", claims.ObjectID)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, "user@example.com", claims.Username)
	assert.False(t, claims.IssuedAt.IsZero())
	assert.False(t, claims.ExpiresAt.IsZero())
}

func TestParseJWTClaims_AccessTokenV1Claims(t *testing.T) {
	// Entra v1 access tokens use appid for client ID and upn for username
	jwt := buildJWT(t, map[string]any{
		"iss":         "https://sts.windows.net/tenant-123/",
		"sub":         "v1-subject",
		"tid":         "tenant-123",
		"oid":         "obj-789",
		"appid":       "v1-app-id",
		"upn":         "upnuser@example.com",
		"unique_name": "unique@example.com",
		"iat":         1700000000,
		"exp":         1700003600,
	})

	claims, err := ParseJWTClaims(jwt)
	require.NoError(t, err)

	// upn should populate both email and username since email & preferred_username are empty
	assert.Equal(t, "upnuser@example.com", claims.Email)
	assert.Equal(t, "upnuser@example.com", claims.Username)
	// No aud, so clientID falls through to appid
	assert.Equal(t, "v1-app-id", claims.ClientID)
}

func TestParseJWTClaims_AccessTokenV2Claims(t *testing.T) {
	// Entra v2 access tokens use azp for authorized party
	jwt := buildJWT(t, map[string]any{
		"iss": "https://login.microsoftonline.com/tenant-123/v2.0",
		"sub": "v2-subject",
		"azp": "v2-azp-client",
		"tid": "tenant-123",
		"oid": "obj-012",
		"iat": 1700000000,
		"exp": 1700003600,
	})

	claims, err := ParseJWTClaims(jwt)
	require.NoError(t, err)

	// No aud, so clientID should fall through to azp
	assert.Equal(t, "v2-azp-client", claims.ClientID)
	assert.Equal(t, "v2-subject", claims.Subject)
}

func TestParseJWTClaims_MinimalClaims(t *testing.T) {
	jwt := buildJWT(t, map[string]any{
		"sub": "minimal-subject",
	})

	claims, err := ParseJWTClaims(jwt)
	require.NoError(t, err)

	assert.Equal(t, "minimal-subject", claims.Subject)
	assert.Empty(t, claims.Email)
	assert.Empty(t, claims.Name)
	assert.Empty(t, claims.TenantID)
	assert.True(t, claims.IssuedAt.IsZero())
	assert.True(t, claims.ExpiresAt.IsZero())
}

func TestParseJWTClaims_EmailFallback(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		wantEmail string
		wantUser  string
	}{
		{
			name:      "email present",
			payload:   map[string]any{"email": "direct@test.com", "preferred_username": "pref@test.com"},
			wantEmail: "direct@test.com",
			wantUser:  "pref@test.com",
		},
		{
			name:      "preferred_username fallback",
			payload:   map[string]any{"preferred_username": "pref@test.com"},
			wantEmail: "pref@test.com",
			wantUser:  "pref@test.com",
		},
		{
			name:      "upn fallback",
			payload:   map[string]any{"upn": "upn@test.com"},
			wantEmail: "upn@test.com",
			wantUser:  "upn@test.com",
		},
		{
			name:      "unique_name fallback",
			payload:   map[string]any{"unique_name": "unique@test.com"},
			wantEmail: "unique@test.com",
			wantUser:  "unique@test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwt := buildJWT(t, tt.payload)
			claims, err := ParseJWTClaims(jwt)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEmail, claims.Email)
			assert.Equal(t, tt.wantUser, claims.Username)
		})
	}
}

func TestParseJWTClaims_ClientIDPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantID  string
	}{
		{
			name:    "aud takes precedence",
			payload: map[string]any{"aud": "aud-val", "azp": "azp-val", "appid": "appid-val"},
			wantID:  "aud-val",
		},
		{
			name:    "azp fallback",
			payload: map[string]any{"azp": "azp-val", "appid": "appid-val"},
			wantID:  "azp-val",
		},
		{
			name:    "appid fallback",
			payload: map[string]any{"appid": "appid-val"},
			wantID:  "appid-val",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwt := buildJWT(t, tt.payload)
			claims, err := ParseJWTClaims(jwt)
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, claims.ClientID)
		})
	}
}

func TestParseJWTClaims_NotAJWT(t *testing.T) {
	_, err := ParseJWTClaims("not-a-jwt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOpaqueToken))
}

func TestParseJWTClaims_InvalidBase64(t *testing.T) {
	_, err := ParseJWTClaims("header.!!!invalid-base64!!!.sig")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOpaqueToken))
}

func TestParseJWTClaims_InvalidJSON(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	_, err := ParseJWTClaims("header." + payload + ".sig")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOpaqueToken))
}

func TestParseJWTClaims_EmptyPayload(t *testing.T) {
	jwt := buildJWT(t, map[string]any{})
	claims, err := ParseJWTClaims(jwt)
	require.NoError(t, err)
	assert.Empty(t, claims.Subject)
	assert.Empty(t, claims.Email)
}
