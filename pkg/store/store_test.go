// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	assert.NotNil(t, s)
	assert.Equal(t, 0, s.Len())
}

func TestStore_SetGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		value   int
		lookup  string
		wantVal int
		wantOK  bool
	}{
		{name: "present key", key: "a", value: 1, lookup: "a", wantVal: 1, wantOK: true},
		{name: "absent key", key: "a", value: 1, lookup: "b", wantVal: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := New[string, int]()
			s.Set(tt.key, tt.value)

			got, ok := s.Get(tt.lookup)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantVal, got)
		})
	}
}

func TestStore_GetEmpty(t *testing.T) {
	t.Parallel()

	s := New[string, string]()
	got, ok := s.Get("missing")
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestStore_SetOverwrite(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	s.Set("k", 1)
	s.Set("k", 2)

	got, ok := s.Get("k")
	assert.True(t, ok)
	assert.Equal(t, 2, got)
	assert.Equal(t, 1, s.Len(), "overwrite must not change length")
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	s.Set("a", 1)
	s.Set("b", 2)

	s.Delete("a")

	_, ok := s.Get("a")
	assert.False(t, ok, "deleted key must be absent")
	assert.Equal(t, 1, s.Len())

	got, ok := s.Get("b")
	assert.True(t, ok)
	assert.Equal(t, 2, got)
}

func TestStore_DeleteAbsentIsNoOp(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	s.Set("a", 1)

	s.Delete("missing")

	assert.Equal(t, 1, s.Len())
	got, ok := s.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, got)
}

func TestStore_Len(t *testing.T) {
	t.Parallel()

	s := New[int, string]()
	assert.Equal(t, 0, s.Len())

	s.Set(1, "one")
	assert.Equal(t, 1, s.Len())

	s.Set(2, "two")
	assert.Equal(t, 2, s.Len())

	s.Delete(1)
	assert.Equal(t, 1, s.Len())

	s.Delete(2)
	assert.Equal(t, 0, s.Len())
}

// TestStore_StructValue verifies the store works with struct value types.
func TestStore_StructValue(t *testing.T) {
	t.Parallel()

	type entry struct {
		name    string
		version string
	}

	s := New[int, entry]()
	s.Set(1, entry{name: "plugin", version: "1.2.3"})

	got, ok := s.Get(1)
	assert.True(t, ok)
	assert.Equal(t, entry{name: "plugin", version: "1.2.3"}, got)
}

// TestStore_StructKey verifies the store works with comparable struct keys.
func TestStore_StructKey(t *testing.T) {
	t.Parallel()

	type key struct {
		name    string
		version string
	}

	s := New[key, int]()
	k := key{name: "plugin", version: "1.2.3"}
	s.Set(k, 42)

	got, ok := s.Get(k)
	assert.True(t, ok)
	assert.Equal(t, 42, got)

	_, ok = s.Get(key{name: "plugin", version: "9.9.9"})
	assert.False(t, ok)
}

func TestStore_Delete_FiresOnEvictWithRemovedValue(t *testing.T) {
	t.Parallel()

	var evicted []int
	s := New[string, int]()
	s.OnEvict = func(value int) { evicted = append(evicted, value) }

	s.Set("a", 1)
	s.Set("b", 2)

	s.Delete("a")

	assert.Equal(t, []int{1}, evicted, "OnEvict must receive the removed value")
	assert.Equal(t, 1, s.Len())
}

func TestStore_Delete_AbsentKeyDoesNotFireOnEvict(t *testing.T) {
	t.Parallel()

	called := false
	s := New[string, int]()
	s.OnEvict = func(int) { called = true }

	s.Set("a", 1)
	s.Delete("missing")

	assert.False(t, called, "OnEvict must not fire when the key is absent")
	assert.Equal(t, 1, s.Len())
}

func TestStore_Set_OverwriteDoesNotFireOnEvict(t *testing.T) {
	t.Parallel()

	called := false
	s := New[string, int]()
	s.OnEvict = func(int) { called = true }

	s.Set("a", 1)
	s.Set("a", 2)

	assert.False(t, called, "overwriting a key must not fire OnEvict")

	got, ok := s.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 2, got)
}

func TestStore_Delete_NilOnEvictIsNoOp(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	s.Set("a", 1)

	assert.NotPanics(t, func() { s.Delete("a") },
		"Delete must not panic when OnEvict is nil")
	assert.Equal(t, 0, s.Len())
}

func TestStore_Delete_FiresAfterRemoval(t *testing.T) {
	t.Parallel()

	s := New[string, int]()
	var lenAtCallback int
	s.OnEvict = func(int) { lenAtCallback = s.Len() }

	s.Set("a", 1)
	s.Set("b", 2)
	s.Delete("a")

	assert.Equal(t, 1, lenAtCallback,
		"OnEvict must fire after the entry is removed from the map")
}
