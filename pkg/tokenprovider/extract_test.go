package tokenprovider

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/stretchr/testify/assert"
)

// contextWithTokens builds a context containing passthrough tokens by running
// an HTTP request through the TokenPassthrough middleware.
func contextWithTokens(headers map[string]string) context.Context {
	providers := make([]string, 0, len(headers))
	for k := range headers {
		providers = append(providers, k)
	}

	var captured context.Context
	mw := middleware.TokenPassthrough(providers)
	handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = r.Context()
	}))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	for k, v := range headers {
		req.Header.Set(middleware.TokenHeaderPrefix+k, v)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return captured
}

func TestExtractPassthroughTokenFromContext(t *testing.T) {
	t.Run("returns token when present", func(t *testing.T) {
		ctx := contextWithTokens(map[string]string{"Entra": "abc123"})

		token, found := ExtractPassthroughTokenFromContext(ctx, "entra")

		assert.True(t, found)
		assert.Equal(t, "abc123", token.AccessToken)
		assert.Equal(t, "Bearer", token.TokenType)
	})

	t.Run("returns false for missing provider", func(t *testing.T) {
		ctx := contextWithTokens(map[string]string{"Github": "ghp_xxx"})

		token, found := ExtractPassthroughTokenFromContext(ctx, "entra")

		assert.False(t, found)
		assert.Zero(t, token)
	})

	t.Run("returns false for empty context", func(t *testing.T) {
		token, found := ExtractPassthroughTokenFromContext(context.Background(), "entra")

		assert.False(t, found)
		assert.Zero(t, token)
	})
}
