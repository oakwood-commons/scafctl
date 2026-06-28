// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	"github.com/stretchr/testify/assert"
)

func TestClaims_IsEmpty(t *testing.T) {
	tests := []struct {
		name   string
		claims *Claims
		want   bool
	}{
		{name: "nil claims", claims: nil, want: true},
		{name: "empty claims", claims: &Claims{}, want: true},
		{name: "only issuer", claims: &Claims{Issuer: "https://login.microsoftonline.com/"}, want: true},
		{name: "with subject", claims: &Claims{Subject: "user-123"}, want: false},
		{name: "with email", claims: &Claims{Email: "user@example.com"}, want: false},
		{name: "with name", claims: &Claims{Name: "Test User"}, want: false},
		{name: "with username", claims: &Claims{Username: "testuser"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.claims.IsEmpty())
		})
	}
}

func TestClaims_DisplayIdentity(t *testing.T) {
	tests := []struct {
		name   string
		claims *Claims
		want   string
	}{
		{name: "nil claims", claims: nil, want: ""},
		{name: "empty claims", claims: &Claims{}, want: ""},
		{name: "only subject", claims: &Claims{Subject: "user-123"}, want: "user-123"},
		{name: "name and subject", claims: &Claims{Name: "Test User", Subject: "user-123"}, want: "Test User"},
		{name: "username preferred over name", claims: &Claims{Username: "testuser", Name: "Test User"}, want: "testuser"},
		{name: "email preferred over all", claims: &Claims{Email: "user@example.com", Username: "testuser", Name: "Test User"}, want: "user@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.claims.DisplayIdentity())
		})
	}
}

func TestHandlerMetadata_IsSDKAlias(t *testing.T) {
	// Compile-time proof that HandlerMetadata and the SDK type are mutually
	// assignable without conversion (i.e. a true type alias). Passing a value
	// of one type where the other is expected only compiles for an alias.
	wantSDK := func(sdkauth.HandlerMetadata) {}
	wantCore := func(HandlerMetadata) {}
	wantSDK(HandlerMetadata{})          // core value satisfies the SDK type
	wantCore(sdkauth.HandlerMetadata{}) // SDK value satisfies the core type

	// Runtime sanity: fields set through the alias are carried across.
	meta := HandlerMetadata{SessionID: "session-123", ClientID: "client-abc"}
	wantSDK(meta)
	assert.Equal(t, "session-123", meta.SessionID)
	assert.Equal(t, "client-abc", meta.ClientID)
}

func TestHandlerMetadata_MetaHelpers(t *testing.T) {
	// The alias must expose the SDK helper methods (SetMeta/MetaString).
	meta := &HandlerMetadata{}
	assert.Equal(t, "", meta.MetaString("missing"))

	meta.SetMeta("server", "https://api.example.com")
	assert.Equal(t, "https://api.example.com", meta.MetaString("server"))

	// Non-string values resolve to the empty string.
	meta.SetMeta("count", 42)
	assert.Equal(t, "", meta.MetaString("count"))
}
