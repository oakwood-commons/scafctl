// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestRegisterSnapshotEndpoints_ListEmpty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSnapshotEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/snapshots")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "items")
}

func TestRegisterSnapshotEndpoints_GetNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSnapshotEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/snapshots/nonexistent")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func BenchmarkSnapshotListEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterSnapshotEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/snapshots")
	}
}
