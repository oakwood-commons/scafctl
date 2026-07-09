// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
)

func TestVerifyImmutables(t *testing.T) {
	immutableResolvers := []*resolver.Resolver{
		{Name: "cluster_id", Type: "string", Immutable: true},
	}

	t.Run("nil state data is a no-op", func(t *testing.T) {
		err := VerifyImmutables(nil, resolver.NewContext(), immutableResolvers)
		assert.NoError(t, err)
	})

	t.Run("nil immutables map is a no-op", func(t *testing.T) {
		sd := &Data{}
		err := VerifyImmutables(sd, resolver.NewContext(), immutableResolvers)
		assert.NoError(t, err)
	})

	t.Run("matching value passes", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{
			Immutable: true,
			Value:     "uuid-1234",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		err := VerifyImmutables(sd, rctx, immutableResolvers)
		assert.NoError(t, err)
	})

	t.Run("different value errors", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-CHANGED",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{
			Immutable: true,
			Value:     "uuid-1234",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		err := VerifyImmutables(sd, rctx, immutableResolvers)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrImmutableEntry)
		assert.Contains(t, err.Error(), "cluster_id")
		assert.Contains(t, err.Error(), "state delete")
	})

	t.Run("does not mutate state (no locking of new immutables)", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})

		// No existing immutable entry -- VerifyImmutables must NOT create one.
		sd := NewData()

		err := VerifyImmutables(sd, rctx, immutableResolvers)
		assert.NoError(t, err)
		assert.NotContains(t, sd.Resolvers, "cluster_id", "verify must not lock new immutables")
	})

	t.Run("non-immutable resolver is ignored", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("env", &resolver.ExecutionResult{
			Value:  "prod",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		sd.Resolvers["env"] = &PersistedEntry{
			Immutable: true,
			Value:     "dev",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		resolvers := []*resolver.Resolver{
			{Name: "env", Type: "string", Immutable: false},
		}

		err := VerifyImmutables(sd, rctx, resolvers)
		assert.NoError(t, err, "non-immutable resolvers are never verified")
	})

	t.Run("failed resolver result is skipped", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-CHANGED",
			Status: resolver.ExecutionStatusFailed,
		})

		sd := NewData()
		sd.Resolvers["cluster_id"] = &PersistedEntry{
			Immutable: true,
			Value:     "uuid-1234",
			Type:      "string",
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		err := VerifyImmutables(sd, rctx, immutableResolvers)
		assert.NoError(t, err, "non-successful resolver results must not be verified")
	})
}

func TestPersistResolvers(t *testing.T) {
	t.Run("nil state data is a no-op", func(t *testing.T) {
		err := PersistResolvers(nil, resolver.NewContext(), nil)
		assert.NoError(t, err)
	})

	t.Run("persist-only resolver is recorded and overwritten each run", func(t *testing.T) {
		resolvers := []*resolver.Resolver{
			{Name: "token", Type: "string", Persist: true},
		}

		rctx := resolver.NewContext()
		rctx.SetResult("token", &resolver.ExecutionResult{
			Value:  "first",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		err := PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)

		entry := sd.Resolvers["token"]
		assert.NotNil(t, entry)
		assert.Equal(t, "first", entry.Value)
		assert.False(t, entry.Immutable)
		firstCreated := entry.CreatedAt

		// Second run with a different value overwrites, preserving CreatedAt.
		rctx.SetResult("token", &resolver.ExecutionResult{
			Value:  "second",
			Status: resolver.ExecutionStatusSuccess,
		})
		err = PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)

		entry = sd.Resolvers["token"]
		assert.Equal(t, "second", entry.Value)
		assert.Equal(t, firstCreated, entry.CreatedAt, "persist-only preserves CreatedAt")
	})

	t.Run("immutable resolver locks on first run and verifies on later runs", func(t *testing.T) {
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true},
		}

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		err := PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)
		assert.True(t, sd.Resolvers["cluster_id"].Immutable)
		assert.Equal(t, "uuid-1234", sd.Resolvers["cluster_id"].Value)

		// Same value on a subsequent run passes.
		err = PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)

		// A changed value errors.
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-CHANGED",
			Status: resolver.ExecutionStatusSuccess,
		})
		err = PersistResolvers(sd, rctx, resolvers)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrImmutableEntry)
	})

	t.Run("immutable implies persist even without persist flag", func(t *testing.T) {
		resolvers := []*resolver.Resolver{
			{Name: "cluster_id", Type: "string", Immutable: true, Persist: false},
		}

		rctx := resolver.NewContext()
		rctx.SetResult("cluster_id", &resolver.ExecutionResult{
			Value:  "uuid-1234",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		err := PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)
		assert.Contains(t, sd.Resolvers, "cluster_id")
	})

	t.Run("plain resolver is not recorded", func(t *testing.T) {
		resolvers := []*resolver.Resolver{
			{Name: "env", Type: "string"},
		}

		rctx := resolver.NewContext()
		rctx.SetResult("env", &resolver.ExecutionResult{
			Value:  "prod",
			Status: resolver.ExecutionStatusSuccess,
		})

		sd := NewData()
		err := PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)
		assert.NotContains(t, sd.Resolvers, "env")
	})

	t.Run("skipped or failed results are not recorded", func(t *testing.T) {
		resolvers := []*resolver.Resolver{
			{Name: "token", Type: "string", Persist: true},
		}

		rctx := resolver.NewContext()
		rctx.SetResult("token", &resolver.ExecutionResult{
			Value:  "value",
			Status: resolver.ExecutionStatusFailed,
		})

		sd := NewData()
		err := PersistResolvers(sd, rctx, resolvers)
		assert.NoError(t, err)
		assert.NotContains(t, sd.Resolvers, "token")
	})
}
