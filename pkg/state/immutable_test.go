// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newImmutableResolverCtx(t *testing.T, value any) *resolver.Context {
	t.Helper()
	rctx := resolver.NewContext()
	rctx.SetResult("cluster_id", &resolver.ExecutionResult{
		Value:  value,
		Status: resolver.ExecutionStatusSuccess,
	})
	return rctx
}

func TestCheckImmutables(t *testing.T) {
	resolvers := []*resolver.Resolver{
		{Name: "cluster_id", Type: "string", Immutable: true},
	}

	t.Run("first run locks the value", func(t *testing.T) {
		sd := NewData()
		rctx := newImmutableResolverCtx(t, "uuid-1234")

		err := CheckImmutables(sd, rctx, resolvers, nil)
		require.NoError(t, err)
		require.Contains(t, sd.Immutables, "cluster_id")
		assert.Equal(t, "uuid-1234", sd.Immutables["cluster_id"].Value)
	})

	t.Run("skip prevents locking a new value on first run", func(t *testing.T) {
		sd := NewData()
		rctx := newImmutableResolverCtx(t, "uuid-1234")

		err := CheckImmutables(sd, rctx, resolvers, map[string]bool{"cluster_id": true})
		require.NoError(t, err)
		assert.NotContains(t, sd.Immutables, "cluster_id")
	})

	t.Run("existing lock with matching value passes", func(t *testing.T) {
		sd := NewData()
		sd.Immutables["cluster_id"] = &ImmutableEntry{Value: "uuid-1234", Type: "string"}
		rctx := newImmutableResolverCtx(t, "uuid-1234")

		err := CheckImmutables(sd, rctx, resolvers, nil)
		require.NoError(t, err)
	})

	t.Run("existing lock with differing value fails", func(t *testing.T) {
		sd := NewData()
		sd.Immutables["cluster_id"] = &ImmutableEntry{Value: "uuid-1234", Type: "string"}
		rctx := newImmutableResolverCtx(t, "uuid-9999")

		err := CheckImmutables(sd, rctx, resolvers, nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrImmutableEntry))
	})

	t.Run("skip still verifies an existing lock and detects drift", func(t *testing.T) {
		sd := NewData()
		sd.Immutables["cluster_id"] = &ImmutableEntry{Value: "uuid-1234", Type: "string"}
		rctx := newImmutableResolverCtx(t, "uuid-9999")

		err := CheckImmutables(sd, rctx, resolvers, map[string]bool{"cluster_id": true})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrImmutableEntry))
	})

	t.Run("nil state data is a no-op", func(t *testing.T) {
		err := CheckImmutables(nil, resolver.NewContext(), resolvers, nil)
		require.NoError(t, err)
	})
}
