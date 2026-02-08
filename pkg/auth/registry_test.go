// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	assert.NotNil(t, r)
	assert.Equal(t, 0, r.Count())
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	err := r.Register(nil)
	require.Error(t, err)

	err = r.Register(&MockHandler{NameValue: ""})
	require.Error(t, err)

	err = r.Register(NewMockHandler("entra"))
	require.NoError(t, err)
	assert.True(t, r.Has("entra"))
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(NewMockHandler("entra")))
	err := r.Register(NewMockHandler("entra"))
	require.Error(t, err)
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(NewMockHandler("entra")))
	require.NoError(t, r.Unregister("entra"))
	assert.False(t, r.Has("entra"))
	require.Error(t, r.Unregister("entra"))
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("entra")
	require.Error(t, err)

	handler := NewMockHandler("entra")
	require.NoError(t, r.Register(handler))
	got, err := r.Get("entra")
	require.NoError(t, err)
	assert.Equal(t, handler, got)
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	assert.Empty(t, r.List())

	require.NoError(t, r.Register(NewMockHandler("entra")))
	require.NoError(t, r.Register(NewMockHandler("github")))
	require.NoError(t, r.Register(NewMockHandler("aws")))

	list := r.List()
	assert.Len(t, list, 3)
	assert.Equal(t, []string{"aws", "entra", "github"}, list)
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = r.Register(NewMockHandler("handler-" + string(rune('a'+i%26))))
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
			_ = r.Count()
		}()
	}

	wg.Wait()
}
