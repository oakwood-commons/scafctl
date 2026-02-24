// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewObservabilityHooks(t *testing.T) {
	t.Run("returns non-nil hooks", func(t *testing.T) {
		lgr := logr.Discard()
		hooks := newObservabilityHooks(lgr)
		require.NotNil(t, hooks)
	})
}

func TestToolTimingMiddleware(t *testing.T) {
	t.Run("returns non-nil middleware", func(t *testing.T) {
		lgr := logr.Discard()
		mw := toolTimingMiddleware(lgr)
		require.NotNil(t, mw)
	})
}

func TestResourceTimingMiddleware(t *testing.T) {
	t.Run("returns non-nil middleware", func(t *testing.T) {
		lgr := logr.Discard()
		mw := resourceTimingMiddleware(lgr)
		require.NotNil(t, mw)
	})
}

func TestNewServerWithHooks(t *testing.T) {
	// Verify that hooks are wired up during server creation (no panics).
	srv, err := NewServer(WithServerLogger(logr.Discard()))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}
