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

func TestRegisterEvalEndpoints_CEL(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/cel", strings.NewReader(`{"expression": "1 + 2"}`), "Content-Type: application/json")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "result")
}

func TestRegisterEvalEndpoints_CEL_EmptyExpression(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/cel", strings.NewReader(`{"expression": ""}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterEvalEndpoints_CEL_WithData(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/cel", strings.NewReader(`{"expression": "_.x + _.y", "data": {"x": 1, "y": 2}}`), "Content-Type: application/json")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "result")
}

func TestRegisterEvalEndpoints_CEL_InvalidExpression(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/cel", strings.NewReader(`{"expression": "???invalid"}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterEvalEndpoints_Template(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/template", strings.NewReader(`{"template": "Hello {{ .name }}", "data": {"name": "World"}}`), "Content-Type: application/json")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "Hello World")
}

func TestRegisterEvalEndpoints_Template_EmptyTemplate(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/template", strings.NewReader(`{"template": ""}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterEvalEndpoints_Template_InvalidSyntax(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterEvalEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/eval/template", strings.NewReader(`{"template": "{{ .bad }"}`), "Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func BenchmarkCELEvalEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterEvalEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Post("/v1/eval/cel", strings.NewReader(`{"expression": "1 + 2"}`), "Content-Type: application/json")
	}
}
