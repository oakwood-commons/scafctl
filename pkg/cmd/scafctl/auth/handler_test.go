// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
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

func TestIsHandlerRegistered_WithOfficialRegistry(t *testing.T) {
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.True(t, isHandlerRegistered(ctx, "github"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestIsHandlerRegistered_BothRegistries(t *testing.T) {
	// Auth registry has "custom", official has "entra"
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	assert.True(t, isHandlerRegistered(ctx, "custom"))
	assert.True(t, isHandlerRegistered(ctx, "entra"))
	assert.False(t, isHandlerRegistered(ctx, "unknown"))
}

func TestListHandlers_WithOfficialRegistry(t *testing.T) {
	// listHandlers should NOT include official-only handlers (not installed).
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	handlers := listHandlers(ctx)
	assert.Nil(t, handlers) // no eager registry → nil
}

func TestListHandlers_OnlyEagerRegistry(t *testing.T) {
	// listHandlers returns only eagerly-registered handlers, ignoring official.
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listHandlers(ctx)
	assert.Equal(t, []string{"custom"}, handlers)
}

func TestListKnownHandlers_WithOfficialRegistry(t *testing.T) {
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	handlers := listKnownHandlers(ctx)
	assert.Contains(t, handlers, "entra")
	assert.Contains(t, handlers, "github")
	assert.Len(t, handlers, 2)
}

func TestListKnownHandlers_MergesBothRegistries(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("custom")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listKnownHandlers(ctx)
	assert.Contains(t, handlers, "custom")
	assert.Contains(t, handlers, "entra")
	assert.Len(t, handlers, 2)
}

func TestListKnownHandlers_DeduplicatesOverlap(t *testing.T) {
	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(auth.NewMockHandler("entra")))
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	})
	ctx := auth.WithRegistry(context.Background(), registry)
	ctx = authofficial.WithRegistry(ctx, official)

	handlers := listKnownHandlers(ctx)
	assert.ElementsMatch(t, []string{"entra", "github"}, handlers)
}

func TestListKnownHandlers_WithTestHandler(t *testing.T) {
	mock := auth.NewMockHandler("test")
	ctx := withTestHandler(context.Background(), mock)

	handlers := listKnownHandlers(ctx)
	assert.Equal(t, []string{"test"}, handlers)
}

func TestListKnownHandlers_NoContext(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, listKnownHandlers(ctx))
}

func TestValidateHandlerName_OfficialHandler(t *testing.T) {
	// No auth registry, but official registry has "entra"
	official := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	})
	ctx := authofficial.WithRegistry(context.Background(), official)

	assert.NoError(t, validateHandlerName(ctx, "entra"))

	err := validateHandlerName(ctx, "unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
	assert.Contains(t, err.Error(), "entra")
}
