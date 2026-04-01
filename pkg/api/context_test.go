// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

func TestNewHandlerContext(t *testing.T) {
	var shutting int32
	now := time.Now()

	hctx := NewHandlerContext(nil, nil, nil, logr.Discard(), &shutting, now)
	assert.NotNil(t, hctx)
	assert.False(t, hctx.ShuttingDown())
	assert.Equal(t, now, hctx.StartTime)
}

func TestHandlerContext_ShuttingDown(t *testing.T) {
	var shutting int32
	hctx := NewHandlerContext(nil, nil, nil, logr.Discard(), &shutting, time.Now())

	assert.False(t, hctx.ShuttingDown())

	atomic.StoreInt32(&shutting, 1)
	assert.True(t, hctx.ShuttingDown())
}
