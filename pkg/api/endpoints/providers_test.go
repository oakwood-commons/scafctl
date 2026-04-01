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

func TestRegisterProviderEndpoints_ListEmpty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers")
	require.Equal(t, http.StatusOK, resp.Code)
	// When registry is nil, returns empty response
	assert.Contains(t, resp.Body.String(), "items")
}

func TestRegisterProviderEndpoints_DetailNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers/nonexistent")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterProviderEndpoints_SchemaNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterProviderEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/providers/nonexistent/schema")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func BenchmarkProviderListEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterProviderEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/providers")
	}
}
