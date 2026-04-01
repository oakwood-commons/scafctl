// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestRegisterSchemaEndpoints_List(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/schemas")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "solution")
}

func TestRegisterSchemaEndpoints_GetSolution(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/schemas/solution")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "schema")
}

func TestRegisterSchemaEndpoints_GetNotFound(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Get("/v1/schemas/nonexistent")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestRegisterSchemaEndpoints_Validate_NilData(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/schemas/validate", strings.NewReader(`{"data": null}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterSchemaEndpoints_Validate_ValidData(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/schemas/validate", strings.NewReader(`{"data": {"apiVersion": "scafctl.io/v1", "kind": "Solution", "metadata": {"name": "test"}, "spec": {"resolvers": {}}}}`), "Content-Type: application/json") //nolint:lll
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "valid")
}

func BenchmarkSchemaListEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterSchemaEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Get("/v1/schemas")
	}
}
