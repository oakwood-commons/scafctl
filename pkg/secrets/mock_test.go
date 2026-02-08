// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockStore(t *testing.T) {
	t.Run("Get returns stored value", func(t *testing.T) {
		store := NewMockStore()
		store.Data["test"] = []byte("value")

		value, err := store.Get(context.Background(), "test")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), value)
		assert.Equal(t, []string{"test"}, store.GetCalls)
	})

	t.Run("Get returns ErrNotFound for missing key", func(t *testing.T) {
		store := NewMockStore()

		_, err := store.Get(context.Background(), "missing")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("Get returns injected error", func(t *testing.T) {
		store := NewMockStore()
		store.GetErr = errors.New("get failed")

		_, err := store.Get(context.Background(), "test")
		assert.EqualError(t, err, "get failed")
	})

	t.Run("Set stores value", func(t *testing.T) {
		store := NewMockStore()

		err := store.Set(context.Background(), "test", []byte("value"))
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), store.Data["test"])
		assert.Len(t, store.SetCalls, 1)
		assert.Equal(t, "test", store.SetCalls[0].Name)
	})

	t.Run("Set returns injected error", func(t *testing.T) {
		store := NewMockStore()
		store.SetErr = errors.New("set failed")

		err := store.Set(context.Background(), "test", []byte("value"))
		assert.EqualError(t, err, "set failed")
	})

	t.Run("Delete removes value", func(t *testing.T) {
		store := NewMockStore()
		store.Data["test"] = []byte("value")

		err := store.Delete(context.Background(), "test")
		require.NoError(t, err)
		_, exists := store.Data["test"]
		assert.False(t, exists)
		assert.Equal(t, []string{"test"}, store.DeleteCalls)
	})

	t.Run("Delete returns injected error", func(t *testing.T) {
		store := NewMockStore()
		store.DeleteErr = errors.New("delete failed")

		err := store.Delete(context.Background(), "test")
		assert.EqualError(t, err, "delete failed")
	})

	t.Run("List returns all keys", func(t *testing.T) {
		store := NewMockStore()
		store.Data["key1"] = []byte("value1")
		store.Data["key2"] = []byte("value2")

		names, err := store.List(context.Background())
		require.NoError(t, err)
		assert.Len(t, names, 2)
		assert.Contains(t, names, "key1")
		assert.Contains(t, names, "key2")
		assert.Equal(t, 1, store.ListCalls)
	})

	t.Run("List returns injected error", func(t *testing.T) {
		store := NewMockStore()
		store.ListErr = errors.New("list failed")

		_, err := store.List(context.Background())
		assert.EqualError(t, err, "list failed")
	})

	t.Run("Exists returns true for existing key", func(t *testing.T) {
		store := NewMockStore()
		store.Data["test"] = []byte("value")

		exists, err := store.Exists(context.Background(), "test")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, []string{"test"}, store.ExistsCalls)
	})

	t.Run("Exists returns false for missing key", func(t *testing.T) {
		store := NewMockStore()

		exists, err := store.Exists(context.Background(), "missing")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Exists returns injected error", func(t *testing.T) {
		store := NewMockStore()
		store.ExistsErr = errors.New("exists failed")

		_, err := store.Exists(context.Background(), "test")
		assert.EqualError(t, err, "exists failed")
	})

	t.Run("Reset clears all state", func(t *testing.T) {
		store := NewMockStore()
		store.Data["test"] = []byte("value")
		store.GetErr = errors.New("error")
		_, _ = store.Get(context.Background(), "test")

		store.Reset()

		assert.Empty(t, store.Data)
		assert.Nil(t, store.GetErr)
		assert.Nil(t, store.GetCalls)
	})
}

func TestMockKeyring(t *testing.T) {
	t.Run("Get returns stored value", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.Data["service:account"] = "value"

		value, err := kr.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "value", value)
		assert.Len(t, kr.GetCalls, 1)
		assert.Equal(t, "service", kr.GetCalls[0].Service)
		assert.Equal(t, "account", kr.GetCalls[0].Account)
	})

	t.Run("Get returns ErrKeyNotFound for missing key", func(t *testing.T) {
		kr := NewMockKeyring()

		_, err := kr.Get("service", "missing")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("Get returns injected error", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.GetErr = errors.New("get failed")

		_, err := kr.Get("service", "account")
		assert.EqualError(t, err, "get failed")
	})

	t.Run("Set stores value", func(t *testing.T) {
		kr := NewMockKeyring()

		err := kr.Set("service", "account", "value")
		require.NoError(t, err)
		assert.Equal(t, "value", kr.Data["service:account"])
		assert.Len(t, kr.SetCalls, 1)
	})

	t.Run("Set returns injected error", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.SetErr = errors.New("set failed")

		err := kr.Set("service", "account", "value")
		assert.EqualError(t, err, "set failed")
	})

	t.Run("Delete removes value", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.Data["service:account"] = "value"

		err := kr.Delete("service", "account")
		require.NoError(t, err)
		_, exists := kr.Data["service:account"]
		assert.False(t, exists)
		assert.Len(t, kr.DeleteCalls, 1)
	})

	t.Run("Delete returns injected error", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.DeleteErr = errors.New("delete failed")

		err := kr.Delete("service", "account")
		assert.EqualError(t, err, "delete failed")
	})

	t.Run("Reset clears all state", func(t *testing.T) {
		kr := NewMockKeyring()
		kr.Data["key"] = "value"
		kr.GetErr = errors.New("error")
		_, _ = kr.Get("service", "account")

		kr.Reset()

		assert.Empty(t, kr.Data)
		assert.Nil(t, kr.GetErr)
		assert.Nil(t, kr.GetCalls)
	})
}
