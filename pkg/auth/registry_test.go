// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	assert.True(t, errors.Is(err, ErrNilHandler))

	err = r.Register(&MockHandler{NameValue: ""})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmptyHandlerName))

	err = r.Register(NewMockHandler("entra"))
	require.NoError(t, err)
	assert.True(t, r.Has("entra"))
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(NewMockHandler("entra")))
	err := r.Register(NewMockHandler("entra"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerAlreadyRegistered))
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

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	h1 := NewMockHandler("handler1")
	h2 := NewMockHandler("handler2")
	_ = r.Register(h1)
	_ = r.Register(h2)

	all := r.All()
	assert.Len(t, all, 2)
	assert.Contains(t, all, "handler1")
	assert.Contains(t, all, "handler2")
}

func TestRegistry_All_Empty(t *testing.T) {
	r := NewRegistry()
	all := r.All()
	assert.Empty(t, all)
}

func TestRegistry_FallbackResolver_ResolvesMissing(t *testing.T) {
	resolved := NewMockHandler("github")
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		if name == "github" {
			return resolved, nil
		}
		return nil, fmt.Errorf("unknown handler: %s", name)
	}))

	// First Get triggers fallback.
	handler, err := r.Get("github")
	require.NoError(t, err)
	assert.Equal(t, resolved, handler)

	// Second Get returns from cache (no fallback).
	handler, err = r.Get("github")
	require.NoError(t, err)
	assert.Equal(t, resolved, handler)
}

func TestRegistry_FallbackResolver_NotFoundReturnsError(t *testing.T) {
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		return nil, fmt.Errorf("not available: %s", name)
	}))

	_, err := r.Get("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback resolver")
}

func TestRegistry_FallbackResolver_NilHandlerReturnsNotFound(t *testing.T) {
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, _ string) (Handler, error) {
		return nil, nil
	}))

	_, err := r.Get("nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerNotFound))
}

func TestRegistry_FallbackResolver_NotCalledForRegistered(t *testing.T) {
	called := false
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, _ string) (Handler, error) {
		called = true
		return nil, nil
	}))

	// Pre-register a handler.
	require.NoError(t, r.Register(NewMockHandler("entra")))

	// Get the registered handler — fallback must NOT be invoked.
	handler, err := r.Get("entra")
	require.NoError(t, err)
	assert.Equal(t, "entra", handler.Name())
	assert.False(t, called)
}

func TestRegistry_FallbackResolver_NoFallbackReturnsNotFound(t *testing.T) {
	r := NewRegistry() // no fallback
	_, err := r.Get("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerNotFound))
}

func TestRegistry_SetFallbackResolver(t *testing.T) {
	r := NewRegistry()

	// Initially no fallback — Get returns not found.
	_, err := r.Get("lazy")
	require.Error(t, err)

	// Set fallback after construction.
	r.SetFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		return NewMockHandler(name), nil
	})

	handler, err := r.Get("lazy")
	require.NoError(t, err)
	assert.Equal(t, "lazy", handler.Name())
}

func TestRegistry_FallbackResolver_ConcurrentResolution(t *testing.T) {
	var callCount atomic.Int32

	// Use a barrier so all goroutines call Get at roughly the same instant,
	// and a small sleep inside the fallback so the resolve window overlaps.
	// Without this, an instantaneous fallback completes (including the
	// sync.Map cleanup) before the next goroutine starts, defeating dedup.
	var ready sync.WaitGroup
	ready.Add(10)

	r := NewRegistry(WithFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		callCount.Add(1)
		time.Sleep(10 * time.Millisecond) // keep the resolve window open
		return NewMockHandler(name), nil
	}))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			ready.Wait() // all goroutines release together
			handler, err := r.Get("concurrent")
			assert.NoError(t, err)
			assert.Equal(t, "concurrent", handler.Name())
		}()
	}
	wg.Wait()

	// With the barrier + sleep the dedup channel coordinates all waiters
	// behind a single in-flight resolve. Allow a small margin for edge
	// cases where a goroutine sneaks past between Delete and the next
	// LoadOrStore, but the count must be far below 10.
	assert.LessOrEqual(t, callCount.Load(), int32(3), "fallback should be called far fewer than 10 times")
}

func TestRegistry_GetRegistered_DoesNotTriggerFallback(t *testing.T) {
	called := false
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, _ string) (Handler, error) {
		called = true
		return NewMockHandler("should-not-be-called"), nil
	}))

	handler, exists := r.GetRegistered("missing")
	assert.False(t, exists)
	assert.Nil(t, handler)
	assert.False(t, called)
}

func TestRegistry_FallbackResolver_ContextCancelledDuringWait(t *testing.T) {
	// One goroutine starts resolving; a second goroutine waits on the channel
	// but its context is cancelled before the first goroutine finishes.
	resolving := make(chan struct{})
	r := NewRegistry(WithFallbackResolver(func(ctx context.Context, name string) (Handler, error) {
		// Signal that we are inside the resolver, then block until test releases.
		close(resolving)
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	ctx2, cancel2 := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	// Goroutine 1: starts the resolution (will block inside fallback).
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = r.resolveWithFallbackContext(ctx1, "blocked", r.fallback)
	}()

	// Wait for goroutine 1 to enter the fallback.
	<-resolving

	// Goroutine 2: tries the same name, waits on the channel.
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := r.resolveWithFallbackContext(ctx2, "blocked", r.fallback)
		errCh <- err
	}()

	// Cancel goroutine 2's context — it should return promptly.
	cancel2()

	err := <-errCh
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Release goroutine 1 so the test can clean up.
	cancel1()
	wg.Wait()
}

func TestRegistry_FallbackResolver_AlreadyRegisteredByFallback(t *testing.T) {
	// The fallback itself registers the handler before returning it.
	// resolveWithFallbackContext should not overwrite the entry.
	r := NewRegistry()
	r.SetFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		h := NewMockHandler(name)
		_ = r.Register(h)
		return h, nil
	})

	handler, err := r.Get("pre-registered")
	require.NoError(t, err)
	assert.Equal(t, "pre-registered", handler.Name())
	assert.Equal(t, 1, r.Count())
}

func TestRegistry_GetContext_PropagatesCancellation(t *testing.T) {
	// GetContext should pass the caller's context into the fallback resolver,
	// allowing cancellation to propagate.
	r := NewRegistry(WithFallbackResolver(func(ctx context.Context, _ string) (Handler, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := r.GetContext(ctx, "cancelled")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRegistry_GetContext_ReturnsRegistered(t *testing.T) {
	// GetContext should return an already-registered handler without
	// invoking the fallback.
	called := false
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, _ string) (Handler, error) {
		called = true
		return nil, fmt.Errorf("should not be called")
	}))
	require.NoError(t, r.Register(NewMockHandler("existing")))

	handler, err := r.GetContext(context.Background(), "existing")
	require.NoError(t, err)
	assert.Equal(t, "existing", handler.Name())
	assert.False(t, called)
}

func TestRegistry_GetContext_InvokesFallback(t *testing.T) {
	resolved := NewMockHandler("lazy-ctx")
	r := NewRegistry(WithFallbackResolver(func(_ context.Context, name string) (Handler, error) {
		if name == "lazy-ctx" {
			return resolved, nil
		}
		return nil, fmt.Errorf("unknown: %s", name)
	}))

	handler, err := r.GetContext(context.Background(), "lazy-ctx")
	require.NoError(t, err)
	assert.Equal(t, resolved, handler)

	// Second call returns from cache.
	handler, err = r.GetContext(context.Background(), "lazy-ctx")
	require.NoError(t, err)
	assert.Equal(t, resolved, handler)
}
