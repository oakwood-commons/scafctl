// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := context.Background()

	// Without injection, returns nil
	h := handlerFromContext(ctx)
	assert.Nil(t, h)

	// With injection, returns the handler
	ctx = withTestHandler(ctx, mock)
	h = handlerFromContext(ctx)
	require.NotNil(t, h)
	assert.Equal(t, "test", h.Name())
}

func TestIsSupportedHandler(t *testing.T) {
	tests := []struct {
		name      string
		handler   string
		supported bool
	}{
		{"entra is supported", "entra", true},
		{"unknown is not supported", "unknown", false},
		{"empty is not supported", "", false},
		{"azure is not supported", "azure", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.supported, IsSupportedHandler(tt.handler))
		})
	}
}

func TestSupportedHandlers(t *testing.T) {
	handlers := SupportedHandlers()
	assert.Contains(t, handlers, "entra")
	assert.Len(t, handlers, 1)
}
