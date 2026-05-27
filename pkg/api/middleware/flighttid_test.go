// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlightID_AssignsUniqueIDs(t *testing.T) {
	t.Parallel()

	var id1, id2 uint64
	handler := FlightID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id1 = FlightIDFromContext(r.Context())
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	handler2 := FlightID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id2 = FlightIDFromContext(r.Context())
	}))
	handler2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	assert.NotZero(t, id1)
	assert.NotZero(t, id2)
	assert.NotEqual(t, id1, id2, "each request must get a unique ID")
}

func TestFlightID_Monotonic(t *testing.T) {
	t.Parallel()

	ids := make([]uint64, 100)

	for i := range ids {
		var id uint64
		h := FlightID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			id = FlightIDFromContext(r.Context())
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		ids[i] = id
	}

	for i := 1; i < len(ids); i++ {
		assert.Greater(t, ids[i], ids[i-1], "IDs must be strictly increasing")
	}
}

func TestFlightID_ConcurrentUniqueness(t *testing.T) {
	t.Parallel()

	const goroutines = 50
	ids := make([]uint64, goroutines)
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h := FlightID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				ids[idx] = FlightIDFromContext(r.Context())
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]bool, goroutines)
	for _, id := range ids {
		assert.NotZero(t, id)
		assert.False(t, seen[id], "duplicate ID detected: %d", id)
		seen[id] = true
	}
}

func TestFlightIDFromContext_NoID_ReturnsZero(t *testing.T) {
	t.Parallel()

	id := FlightIDFromContext(context.Background())
	assert.Equal(t, uint64(0), id)
}

func TestFlightIDExtractor_ReturnsFromContext(t *testing.T) {
	t.Parallel()

	var captured uint64
	extractor := FlightIDExtractor()

	h := FlightID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = extractor(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	require.NotZero(t, captured)
}
