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

func TestRegisterExplainEndpoints_Explain(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterExplainEndpoints(testAPI, hctx, "/v1")

	sol := `{"solution": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test\nspec:\n  resolvers:\n    greeting:\n      provider: cel\n      expression: \"'hello'\""}` //nolint:lll
	resp := testAPI.Post("/v1/explain", strings.NewReader(sol), "Content-Type: application/json")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "test")
	assert.Contains(t, resp.Body.String(), "resolverCount")
}

func TestRegisterExplainEndpoints_Explain_Empty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterExplainEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/explain", strings.NewReader(`{"solution": ""}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterExplainEndpoints_Explain_InvalidYAML(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterExplainEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/explain", strings.NewReader(`{"solution": "not: [valid yaml"}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterExplainEndpoints_Diff_Empty(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterExplainEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/diff", strings.NewReader(`{"solutionA": "", "solutionB": ""}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func BenchmarkExplainEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterExplainEndpoints(testAPI, hctx, "/v1")
	sol := `{"solution": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test\nspec:\n  resolvers:\n    greeting:\n      provider: cel\n      expression: \"'hello'\""}`
	for b.Loop() {
		testAPI.Post("/v1/explain", strings.NewReader(sol), "Content-Type: application/json")
	}
}
