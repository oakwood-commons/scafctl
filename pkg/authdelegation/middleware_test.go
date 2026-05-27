// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddleware_InjectsRegistry(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	reg.Register("entra", &mockDelegator{name: "entra"})

	var got *DelegatorRegistry
	handler := Middleware(reg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = RegistryFromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.NotNil(t, got)
	assert.Same(t, reg, got)
}

func TestMiddleware_NilRegistry_Passthrough(t *testing.T) {
	t.Parallel()

	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		got := RegistryFromContext(r.Context())
		assert.Nil(t, got)
	})

	handler := Middleware(nil)(inner)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.True(t, called)
}
