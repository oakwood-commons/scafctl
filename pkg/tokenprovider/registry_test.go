package tokenprovider

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSource is a test double implementing TokenProvider.
type mockSource struct {
	name  string
	token Token
	err   error
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) GetToken(_ context.Context, _ RequestOptions) (Token, error) {
	return m.token, m.err
}

func newMockSource(name string) *mockSource {
	return &mockSource{
		name: name,
		token: Token{
			AccessToken: "tok-" + name,
			ExpiresAt:   time.Now().Add(time.Hour),
			TokenType:   "Bearer",
		},
	}
}

// ---------------------------------------------------------------------------
// Registry Tests
// ---------------------------------------------------------------------------

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name    string
		source  TokenProvider
		wantErr string
	}{
		{
			name:   "registers successfully",
			source: newMockSource("entra"),
		},
		{
			name:    "rejects nil source",
			source:  nil,
			wantErr: "cannot register nil source",
		},
		{
			name:    "rejects empty name",
			source:  &mockSource{name: ""},
			wantErr: "source name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			err := reg.Register(tt.source)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockSource("entra")))

	err := reg.Register(newMockSource("entra"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateSource)
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockSource("entra")))

	t.Run("found", func(t *testing.T) {
		src, ok := reg.Get("entra")
		assert.True(t, ok)
		assert.Equal(t, "entra", src.Name())
	})

	t.Run("not found", func(t *testing.T) {
		src, ok := reg.Get("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, src)
	})
}

func TestRegistry_MustGet(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockSource("gcp")))

	t.Run("returns source", func(t *testing.T) {
		src := reg.MustGet("gcp")
		assert.Equal(t, "gcp", src.Name())
	})

	t.Run("panics on missing", func(t *testing.T) {
		assert.Panics(t, func() {
			reg.MustGet("missing")
		})
	})
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockSource("gcp")))
	require.NoError(t, reg.Register(newMockSource("entra")))
	require.NoError(t, reg.Register(newMockSource("github")))

	names := reg.Names()
	assert.Equal(t, []string{"entra", "gcp", "github"}, names)
}

func TestRegistry_Len(t *testing.T) {
	reg := NewRegistry()
	assert.Equal(t, 0, reg.Len())

	require.NoError(t, reg.Register(newMockSource("entra")))
	assert.Equal(t, 1, reg.Len())

	require.NoError(t, reg.Register(newMockSource("gcp")))
	assert.Equal(t, 2, reg.Len())
}

// ---------------------------------------------------------------------------
// Context Tests
// ---------------------------------------------------------------------------

func TestContext_RoundTrip(t *testing.T) {
	reg := NewRegistry()
	ctx := WithRegistry(context.Background(), reg)

	got := RegistryFromContext(ctx)
	assert.Same(t, reg, got)
}

func TestContext_MissingRegistry(t *testing.T) {
	got := RegistryFromContext(context.Background())
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// GetToken Convenience Function Tests
// ---------------------------------------------------------------------------

func TestGetToken_Success(t *testing.T) {
	src := newMockSource("entra")
	reg := NewRegistry()
	require.NoError(t, reg.Register(src))
	ctx := WithRegistry(context.Background(), reg)

	token, err := GetToken(ctx, "entra", RequestOptions{Scope: "https://vault.azure.net/.default"})
	require.NoError(t, err)
	assert.Equal(t, "tok-entra", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestGetToken_NoRegistry(t *testing.T) {
	_, err := GetToken(context.Background(), "entra", RequestOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoRegistry)
}

func TestGetToken_SourceNotFound(t *testing.T) {
	reg := NewRegistry()
	ctx := WithRegistry(context.Background(), reg)

	_, err := GetToken(ctx, "missing", RequestOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSourceNotFound)
}

func TestGetToken_SourceError(t *testing.T) {
	src := &mockSource{
		name: "entra",
		err:  errors.New("token expired"),
	}
	reg := NewRegistry()
	require.NoError(t, reg.Register(src))
	ctx := WithRegistry(context.Background(), reg)

	_, err := GetToken(ctx, "entra", RequestOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}
