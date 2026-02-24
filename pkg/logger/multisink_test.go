// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package logger

import (
	"errors"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spySink is a logr.LogSink that records every call made to it, allowing tests
// to assert fan-out behaviour without relying on real output sinks.
type spySink struct {
	mu sync.Mutex

	enabledLevel int // calls to Enabled(level) return level <= enabledLevel

	initCalls  []logr.RuntimeInfo
	infoCalls  []spyInfoCall
	errorCalls []spyErrorCall
	withValues [][]any
	withNames  []string
}

type spyInfoCall struct {
	level         int
	msg           string
	keysAndValues []any
}

type spyErrorCall struct {
	err           error
	msg           string
	keysAndValues []any
}

func newSpySink(enabledLevel int) *spySink {
	return &spySink{enabledLevel: enabledLevel}
}

func (s *spySink) Init(info logr.RuntimeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCalls = append(s.initCalls, info)
}

func (s *spySink) Enabled(level int) bool {
	return level <= s.enabledLevel
}

func (s *spySink) Info(level int, msg string, keysAndValues ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.infoCalls = append(s.infoCalls, spyInfoCall{level: level, msg: msg, keysAndValues: keysAndValues})
}

func (s *spySink) Error(err error, msg string, keysAndValues ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorCalls = append(s.errorCalls, spyErrorCall{err: err, msg: msg, keysAndValues: keysAndValues})
}

func (s *spySink) WithValues(keysAndValues ...any) logr.LogSink {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.withValues = append(s.withValues, keysAndValues)
	// Return a new spy that inherits the same enabled level.
	return newSpySink(s.enabledLevel)
}

func (s *spySink) WithName(name string) logr.LogSink {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.withNames = append(s.withNames, name)
	return newSpySink(s.enabledLevel)
}

// ── newMultiSink ─────────────────────────────────────────────────────────────

func TestNewMultiSink_HoldsAllSinks(t *testing.T) {
	a, b := newSpySink(0), newSpySink(0)
	ms := newMultiSink(a, b)
	assert.Len(t, ms.sinks, 2)
}

func TestNewMultiSink_Empty(t *testing.T) {
	ms := newMultiSink()
	assert.Empty(t, ms.sinks)
}

// ── Init ─────────────────────────────────────────────────────────────────────

func TestMultiSink_Init_DelegatesToAllSinks(t *testing.T) {
	a, b := newSpySink(0), newSpySink(0)
	ms := newMultiSink(a, b)

	info := logr.RuntimeInfo{CallDepth: 2}
	ms.Init(info)

	require.Len(t, a.initCalls, 1)
	require.Len(t, b.initCalls, 1)
	assert.Equal(t, info, a.initCalls[0])
	assert.Equal(t, info, b.initCalls[0])
}

// ── Enabled ───────────────────────────────────────────────────────────────────

func TestMultiSink_Enabled_TrueIfAnySinkEnabled(t *testing.T) {
	// a is enabled at level 0 only; b is enabled at level 3.
	a, b := newSpySink(0), newSpySink(3)
	ms := newMultiSink(a, b)

	assert.True(t, ms.Enabled(0), "level 0: both enabled")
	assert.True(t, ms.Enabled(2), "level 2: only b enabled — should still be true")
	assert.True(t, ms.Enabled(3), "level 3: b enabled")
	assert.False(t, ms.Enabled(4), "level 4: neither enabled")
}

func TestMultiSink_Enabled_FalseWhenNoSinkEnabled(t *testing.T) {
	a, b := newSpySink(-1), newSpySink(-1) // disabled for all levels ≥ 0
	ms := newMultiSink(a, b)
	assert.False(t, ms.Enabled(0))
}

func TestMultiSink_Enabled_FalseWhenEmpty(t *testing.T) {
	ms := newMultiSink()
	assert.False(t, ms.Enabled(0))
}

// ── Info ──────────────────────────────────────────────────────────────────────

func TestMultiSink_Info_OnlySendsToEnabledSinks(t *testing.T) {
	// a handles level 0 only; b handles up to level 2.
	a := newSpySink(0)
	b := newSpySink(2)
	ms := newMultiSink(a, b)

	ms.Info(0, "hello", "k", "v")
	ms.Info(2, "world", "k2", "v2")

	// Both receive level 0.
	require.Len(t, a.infoCalls, 1)
	assert.Equal(t, "hello", a.infoCalls[0].msg)

	// Only b receives level 2 (a is not enabled at level 2).
	require.Len(t, b.infoCalls, 2)
	assert.Equal(t, "hello", b.infoCalls[0].msg)
	assert.Equal(t, "world", b.infoCalls[1].msg)
}

func TestMultiSink_Info_ForwardsKeysAndValues(t *testing.T) {
	a := newSpySink(0)
	ms := newMultiSink(a)

	ms.Info(0, "msg", "key", "value", "count", 42)

	require.Len(t, a.infoCalls, 1)
	assert.Equal(t, []any{"key", "value", "count", 42}, a.infoCalls[0].keysAndValues)
}

func TestMultiSink_Info_NotCalledWhenNoSinkEnabled(t *testing.T) {
	a := newSpySink(-1) // disabled for all V-levels
	ms := newMultiSink(a)

	ms.Info(0, "should not arrive")

	assert.Empty(t, a.infoCalls)
}

// ── Error ─────────────────────────────────────────────────────────────────────

func TestMultiSink_Error_DelegatesToAllSinks(t *testing.T) {
	a, b := newSpySink(0), newSpySink(0)
	ms := newMultiSink(a, b)

	sentinel := errors.New("boom")
	ms.Error(sentinel, "something went wrong", "k", "v")

	require.Len(t, a.errorCalls, 1)
	require.Len(t, b.errorCalls, 1)
	assert.Equal(t, sentinel, a.errorCalls[0].err)
	assert.Equal(t, "something went wrong", a.errorCalls[0].msg)
}

func TestMultiSink_Error_CalledEvenOnDisabledSinks(t *testing.T) {
	// Errors bypass the Enabled gate — they should always be delivered.
	a := newSpySink(-1) // V-level disabled
	ms := newMultiSink(a)

	ms.Error(errors.New("err"), "msg")

	assert.Len(t, a.errorCalls, 1)
}

// ── WithValues ────────────────────────────────────────────────────────────────

func TestMultiSink_WithValues_ReturnsFreshMultiSink(t *testing.T) {
	a, b := newSpySink(1), newSpySink(2)
	ms := newMultiSink(a, b)

	child := ms.WithValues("env", "prod")

	// Must return a new multiSink wrapping child sinks from each original sink.
	cms, ok := child.(*multiSink)
	require.True(t, ok, "WithValues must return a *multiSink")
	assert.Len(t, cms.sinks, 2)

	// The call must have been propagated to every original sink.
	assert.Len(t, a.withValues, 1)
	assert.Len(t, b.withValues, 1)
}

func TestMultiSink_WithValues_PropagatesKeysToEachSink(t *testing.T) {
	a, b := newSpySink(0), newSpySink(0)
	ms := newMultiSink(a, b)

	ms.WithValues("region", "us-east-1")

	require.Len(t, a.withValues, 1)
	assert.Equal(t, []any{"region", "us-east-1"}, a.withValues[0])
	require.Len(t, b.withValues, 1)
	assert.Equal(t, []any{"region", "us-east-1"}, b.withValues[0])
}

// ── WithName ──────────────────────────────────────────────────────────────────

func TestMultiSink_WithName_ReturnsFreshMultiSink(t *testing.T) {
	a := newSpySink(0)
	ms := newMultiSink(a)

	child := ms.WithName("subsystem")

	cms, ok := child.(*multiSink)
	require.True(t, ok, "WithName must return a *multiSink")
	assert.Len(t, cms.sinks, 1)
}

func TestMultiSink_WithName_PropagatesNameToEachSink(t *testing.T) {
	a, b := newSpySink(0), newSpySink(0)
	ms := newMultiSink(a, b)

	ms.WithName("resolver")

	require.Len(t, a.withNames, 1)
	assert.Equal(t, "resolver", a.withNames[0])
	require.Len(t, b.withNames, 1)
	assert.Equal(t, "resolver", b.withNames[0])
}

// ── Implements logr.LogSink ───────────────────────────────────────────────────

func TestMultiSink_ImplementsLogrLogSink(t *testing.T) {
	var _ logr.LogSink = newMultiSink()
}
