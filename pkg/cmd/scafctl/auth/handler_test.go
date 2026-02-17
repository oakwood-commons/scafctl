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

func TestIsHandlerRegistered_WithRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestIsHandlerRegistered_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	// Test-injected handler matches any name
	assert.True(t, isHandlerRegistered(ctx, "test"))
	assert.True(t, isHandlerRegistered(ctx, "anything"))
}

func TestIsHandlerRegistered_NoContext(t *testing.T) {
	ctx := context.Background()
	assert.False(t, isHandlerRegistered(ctx, "entra"))
}

func TestListHandlers_WithRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mockEntra := auth.NewMockHandler("entra")
	mockGH := auth.NewMockHandler("github")
	require.NoError(t, registry.Register(mockEntra))
	require.NoError(t, registry.Register(mockGH))
	ctx := auth.WithRegistry(context.Background(), registry)

	handlers := listHandlers(ctx)
	assert.Contains(t, handlers, "entra")
	assert.Contains(t, handlers, "github")
	assert.Len(t, handlers, 2)
}

func TestListHandlers_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handlers := listHandlers(ctx)
	assert.Equal(t, []string{"test"}, handlers)
}

func TestValidateHandlerName(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	assert.NoError(t, validateHandlerName(ctx, "entra"))

	err := validateHandlerName(ctx, "unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
	assert.Contains(t, err.Error(), "entra")
}

func TestGetHandler_FromRegistry(t *testing.T) {
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("entra")
	require.NoError(t, registry.Register(mock))
	ctx := auth.WithRegistry(context.Background(), registry)

	handler, err := getHandler(ctx, "entra")
	require.NoError(t, err)
	assert.Equal(t, "entra", handler.Name())

	_, err = getHandler(ctx, "unknown")
	assert.Error(t, err)
}

func TestGetHandler_FromTestContext(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handler, err := getHandler(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, "test", handler.Name())
}

func TestGetHandler_NoContext(t *testing.T) {
	ctx := context.Background()
	_, err := getHandler(ctx, "entra")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no auth registry in context")
}
