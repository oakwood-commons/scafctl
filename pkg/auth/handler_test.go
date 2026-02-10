// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestToken_IsValidFor(t *testing.T) {
	tests := []struct {
		name     string
		token    *Token
		duration time.Duration
		want     bool
	}{
		{name: "nil token", token: nil, duration: time.Minute, want: false},
		{name: "empty access token", token: &Token{ExpiresAt: time.Now().Add(time.Hour)}, duration: time.Minute, want: false},
		{name: "zero expiry", token: &Token{AccessToken: "token"}, duration: time.Minute, want: false},
		{name: "token valid for duration", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}, duration: 30 * time.Minute, want: true},
		{name: "token expires before duration", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(5 * time.Minute)}, duration: 10 * time.Minute, want: false},
		{name: "expired token", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(-time.Hour)}, duration: 0, want: false},
		{name: "zero duration on valid token", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}, duration: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.token.IsValidFor(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToken_IsExpired(t *testing.T) {
	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{name: "nil token", token: nil, want: true},
		{name: "empty token", token: &Token{}, want: true},
		{name: "expired token", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(-time.Hour)}, want: true},
		{name: "valid token", token: &Token{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.token.IsExpired()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToken_TimeUntilExpiry(t *testing.T) {
	t.Run("nil token", func(t *testing.T) {
		var token *Token
		assert.Equal(t, time.Duration(0), token.TimeUntilExpiry())
	})

	t.Run("zero expiry", func(t *testing.T) {
		token := &Token{AccessToken: "token"}
		assert.Equal(t, time.Duration(0), token.TimeUntilExpiry())
	})

	t.Run("expired token", func(t *testing.T) {
		token := &Token{AccessToken: "token", ExpiresAt: time.Now().Add(-time.Hour)}
		assert.Equal(t, time.Duration(0), token.TimeUntilExpiry())
	})

	t.Run("valid token", func(t *testing.T) {
		token := &Token{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}
		got := token.TimeUntilExpiry()
		assert.InDelta(t, time.Hour.Seconds(), got.Seconds(), 1)
	})
}
