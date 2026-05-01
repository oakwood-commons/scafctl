// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"sync"
	"time"
)

// Mock is a deterministic Clock for tests. Time only advances when Add is
// called, making tests that depend on tickers or timers run instantly.
type Mock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*mockTicker
	afters  []*mockAfter
}

// NewMock returns a Mock clock initialized to the Unix epoch.
func NewMock() *Mock {
	return &Mock{now: time.Unix(0, 0)}
}

// Now returns the mock's current time.
func (m *Mock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Add advances the mock clock by d, firing any tickers or After channels
// whose deadlines have been reached.
func (m *Mock) Add(d time.Duration) {
	m.mu.Lock()
	target := m.now.Add(d)
	m.now = target

	// Fire tickers
	for _, t := range m.tickers {
		t.fire(target)
	}

	// Fire afters
	for _, a := range m.afters {
		a.fire(target)
	}
	m.mu.Unlock()
}

// NewTicker creates a mock ticker that fires when the clock is advanced.
func (m *Mock) NewTicker(d time.Duration) Ticker {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := &mockTicker{
		c:        make(chan time.Time, 1),
		interval: d,
		nextFire: m.now.Add(d),
	}
	m.tickers = append(m.tickers, t)
	return t
}

// After returns a channel that receives when the clock advances past d.
func (m *Mock) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	a := &mockAfter{
		c:        make(chan time.Time, 1),
		deadline: m.now.Add(d),
	}
	m.afters = append(m.afters, a)
	return a.c
}

type mockTicker struct {
	mu       sync.Mutex
	c        chan time.Time
	interval time.Duration
	nextFire time.Time
	stopped  bool
}

func (t *mockTicker) C() <-chan time.Time { return t.c }

func (t *mockTicker) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
}

func (t *mockTicker) Reset(d time.Duration) {
	t.mu.Lock()
	t.interval = d
	// Note: nextFire is recalculated on next fire; this just changes the interval.
	t.mu.Unlock()
}

func (t *mockTicker) fire(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}

	for !now.Before(t.nextFire) {
		select {
		case t.c <- now:
		default:
		}
		t.nextFire = t.nextFire.Add(t.interval)
	}
}

type mockAfter struct {
	c        chan time.Time
	deadline time.Time
	fired    bool
}

func (a *mockAfter) fire(now time.Time) {
	if a.fired {
		return
	}
	if !now.Before(a.deadline) {
		a.fired = true
		select {
		case a.c <- now:
		default:
		}
	}
}
