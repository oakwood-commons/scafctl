// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

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
