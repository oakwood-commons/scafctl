// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReal_NewTicker(t *testing.T) {
	c := clock.Real{}
	ticker := c.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	select {
	case <-ticker.C():
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("ticker did not fire")
	}
}

func TestReal_After(t *testing.T) {
	c := clock.Real{}
	ch := c.After(10 * time.Millisecond)

	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("After channel did not fire")
	}
}

func TestMock_NewTicker(t *testing.T) {
	m := clock.NewMock()
	ticker := m.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Should not fire immediately
	select {
	case <-ticker.C():
		t.Fatal("ticker fired before time advanced")
	default:
	}

	// Advance past interval
	m.Add(5 * time.Second)

	select {
	case <-ticker.C():
		// success
	default:
		t.Fatal("ticker did not fire after advancing")
	}
}

func TestMock_NewTicker_MultipleFires(t *testing.T) {
	m := clock.NewMock()
	ticker := m.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Advance 3 seconds — should fire multiple times
	m.Add(3 * time.Second)

	count := 0
	for i := 0; i < 3; i++ {
		select {
		case <-ticker.C():
			count++
		default:
		}
	}
	// Channel buffer is 1, so we get at least 1 fire; remaining are dropped.
	assert.GreaterOrEqual(t, count, 1)
}

func TestMock_NewTicker_Stop(t *testing.T) {
	m := clock.NewMock()
	ticker := m.NewTicker(1 * time.Second)
	ticker.Stop()

	m.Add(5 * time.Second)

	select {
	case <-ticker.C():
		t.Fatal("stopped ticker should not fire")
	default:
		// success
	}
}

func TestMock_NewTicker_Reset(t *testing.T) {
	m := clock.NewMock()
	ticker := m.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Reset to shorter interval
	ticker.Reset(1 * time.Second)

	// The next fire point is still at +10s from creation, but interval changed.
	// After it fires once, subsequent fires will be at the new interval.
	m.Add(10 * time.Second)

	select {
	case <-ticker.C():
		// success — first fire at original schedule
	default:
		t.Fatal("ticker should fire at original next-fire point")
	}
}

func TestMock_After(t *testing.T) {
	m := clock.NewMock()
	ch := m.After(3 * time.Second)

	// Should not fire yet
	select {
	case <-ch:
		t.Fatal("After fired before time advanced")
	default:
	}

	// Advance past deadline
	m.Add(3 * time.Second)

	select {
	case <-ch:
		// success
	default:
		t.Fatal("After did not fire after advancing")
	}
}

func TestMock_After_OnlyFiresOnce(t *testing.T) {
	m := clock.NewMock()
	ch := m.After(1 * time.Second)

	m.Add(1 * time.Second)
	<-ch

	m.Add(1 * time.Second)

	select {
	case <-ch:
		t.Fatal("After should only fire once")
	default:
		// success
	}
}

func TestReal_ImplementsClock(t *testing.T) {
	var _ clock.Clock = clock.Real{}
	require.NotNil(t, clock.Real{})
}

func TestMock_ImplementsClock(t *testing.T) {
	var _ clock.Clock = clock.NewMock()
	require.NotNil(t, clock.NewMock())
}

func BenchmarkMock_Add(b *testing.B) {
	b.ReportAllocs()
	m := clock.NewMock()
	_ = m.NewTicker(1 * time.Second)

	for b.Loop() {
		m.Add(1 * time.Second)
	}
}

func BenchmarkReal_NewTicker(b *testing.B) {
	b.ReportAllocs()
	c := clock.Real{}

	for b.Loop() {
		t := c.NewTicker(1 * time.Second)
		t.Stop()
	}
}
