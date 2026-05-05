// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/stretchr/testify/assert"
)

func TestNewHandlerContext(t *testing.T) {
	var shutting int32
	now := time.Now()

	hctx := NewHandlerContext(nil, nil, nil, logr.Discard(), &shutting, now)
	assert.NotNil(t, hctx)
	assert.False(t, hctx.ShuttingDown())
	assert.Equal(t, now, hctx.StartTime)
	assert.Nil(t, hctx.PluginFetcher)
	assert.Nil(t, hctx.OfficialProviders)
	assert.Nil(t, hctx.ServerContext)
}

func TestHandlerContext_PluginFields(t *testing.T) {
	var shutting int32
	hctx := NewHandlerContext(nil, nil, nil, logr.Discard(), &shutting, time.Now())

	// Optional fields can be set after construction
	officialReg := official.NewRegistry()
	hctx.OfficialProviders = officialReg
	hctx.ServerContext = context.Background()

	assert.Equal(t, officialReg, hctx.OfficialProviders)
	assert.NotNil(t, hctx.ServerContext)
}

func TestHandlerContext_ShuttingDown(t *testing.T) {
	var shutting int32
	hctx := NewHandlerContext(nil, nil, nil, logr.Discard(), &shutting, time.Now())

	assert.False(t, hctx.ShuttingDown())

	atomic.StoreInt32(&shutting, 1)
	assert.True(t, hctx.ShuttingDown())
}
