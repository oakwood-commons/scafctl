// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	newCtx := WithRegistry(ctx, registry)
	assert.NotEqual(t, ctx, newCtx)
	assert.Equal(t, registry, RegistryFromContext(newCtx))
}

func TestRegistryFromContext(t *testing.T) {
	assert.Nil(t, RegistryFromContext(context.Background()))
	ctx := WithRegistry(context.Background(), NewRegistry())
	assert.NotNil(t, RegistryFromContext(ctx))
}

func TestMustRegistryFromContext(t *testing.T) {
	assert.Panics(t, func() { MustRegistryFromContext(context.Background()) })

	registry := NewRegistry()
	ctx := WithRegistry(context.Background(), registry)
	assert.Equal(t, registry, MustRegistryFromContext(ctx))
}

func TestGetHandler(t *testing.T) {
	_, err := GetHandler(context.Background(), "entra")
	require.Error(t, err)

	registry := NewRegistry()
	ctx := WithRegistry(context.Background(), registry)
	_, err = GetHandler(ctx, "entra")
	require.Error(t, err)

	handler := NewMockHandler("entra")
	require.NoError(t, registry.Register(handler))
	got, err := GetHandler(ctx, "entra")
	require.NoError(t, err)
	assert.Equal(t, handler, got)
}

func TestHasHandler(t *testing.T) {
	assert.False(t, HasHandler(context.Background(), "entra"))

	registry := NewRegistry()
	ctx := WithRegistry(context.Background(), registry)
	assert.False(t, HasHandler(ctx, "entra"))

	_ = registry.Register(NewMockHandler("entra"))
	assert.True(t, HasHandler(ctx, "entra"))
}

func TestListHandlers(t *testing.T) {
	assert.Nil(t, ListHandlers(context.Background()))

	registry := NewRegistry()
	ctx := WithRegistry(context.Background(), registry)
	assert.Empty(t, ListHandlers(ctx))

	_ = registry.Register(NewMockHandler("entra"))
	assert.Len(t, ListHandlers(ctx), 1)
}
