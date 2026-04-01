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

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

func newTestHandlerContext(t *testing.T) *api.HandlerContext {
	t.Helper()
	var shutting int32
	return &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
}

func TestRegisterSolutionEndpoints_LintRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	// Verify lint endpoint is registered and rejects empty body
	resp := testAPI.Post("/v1/solutions/lint", "{}")
	// Huma validation rejects because path is required (minLength:1)
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_InspectRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/inspect", "{}")
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_DryrunRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/dryrun", "{}")
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_RunRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/run", "{}")
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_RenderRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/render", "{}")
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_TestRegistered(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/test", "{}")
	assert.True(t, resp.Code >= 400, "expected 4xx for missing path, got %d", resp.Code)
}

func TestRegisterSolutionEndpoints_RunInvalidPath(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	// Absolute path must be rejected by rejectUnsafePath, not the file loader.
	resp := testAPI.Post("/v1/solutions/run", `{"path": "/nonexistent/solution.yaml"}`)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterSolutionEndpoints_RenderInvalidPath(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/render", `{"path": "/nonexistent/solution.yaml"}`)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRegisterSolutionEndpoints_TestInvalidPath(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/test", `{"path": "/nonexistent/solution.yaml"}`)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

// ── Path safety tests ──

func TestRejectUnsafePath_DotDot(t *testing.T) {
	unsafe := []string{
		"../secret",
		"foo/../../etc/passwd",
		"a/b/../../../c",
	}
	for _, endpoint := range []struct {
		url  string
		body func(p string) string
	}{
		{"/v1/solutions/lint", func(p string) string { return `{"path":"` + p + `"}` }},
		{"/v1/solutions/inspect", func(p string) string { return `{"path":"` + p + `"}` }},
		{"/v1/solutions/dryrun", func(p string) string { return `{"path":"` + p + `"}` }},
		{"/v1/solutions/test", func(p string) string { return `{"path":"` + p + `"}` }},
		{"/v1/solutions/render", func(p string) string { return `{"path":"` + p + `"}` }},
		{"/v1/solutions/run", func(p string) string { return `{"path":"` + p + `"}` }},
	} {
		_, testAPI := humatest.New(t)
		hctx := newTestHandlerContext(t)
		RegisterSolutionEndpoints(testAPI, hctx, "/v1")
		for _, p := range unsafe {
			resp := testAPI.Post(endpoint.url, strings.NewReader(endpoint.body(p)), "Content-Type: application/json")
			assert.Equal(t, http.StatusBadRequest, resp.Code, "expected 400 for path %q on %s", p, endpoint.url)
		}
	}
}

func TestRejectUnsafePath_AbsolutePath(t *testing.T) {
	absPaths := []string{"/etc/passwd", "/root/.ssh/id_rsa", "/tmp/evil"}
	for _, endpoint := range []string{
		"/v1/solutions/lint",
		"/v1/solutions/inspect",
		"/v1/solutions/dryrun",
		"/v1/solutions/test",
		"/v1/solutions/render",
		"/v1/solutions/run",
	} {
		_, testAPI := humatest.New(t)
		hctx := newTestHandlerContext(t)
		RegisterSolutionEndpoints(testAPI, hctx, "/v1")
		for _, p := range absPaths {
			body := strings.NewReader(`{"path":"` + p + `"}`)
			resp := testAPI.Post(endpoint, body, "Content-Type: application/json")
			assert.Equal(t, http.StatusBadRequest, resp.Code, "expected 400 for absolute path %q on %s", p, endpoint)
		}
	}
}

func TestRejectUnsafePath_HomeTilde(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/lint",
		strings.NewReader(`{"path":"~/secrets/solution.yaml"}`),
		"Content-Type: application/json")
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestSolutionRun_OutputDirUnsafe(t *testing.T) {
	tests := []struct {
		name      string
		outputDir string
	}{
		{"absolute", "/etc/cron.d"},
		{"traversal", "../../../etc"},
		{"tilde", "~/evil"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, testAPI := humatest.New(t)
			hctx := newTestHandlerContext(t)
			RegisterSolutionEndpoints(testAPI, hctx, "/v1")
			body := strings.NewReader(`{"path":"relative.yaml","outputDir":"` + tc.outputDir + `"}`)
			resp := testAPI.Post("/v1/solutions/run", body, "Content-Type: application/json")
			assert.Equal(t, http.StatusBadRequest, resp.Code, "expected 400 for unsafe outputDir %q", tc.outputDir)
		})
	}
}

func BenchmarkSolutionLintEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Post("/v1/solutions/lint", `{"path": "/nonexistent.yaml"}`)
	}
}

func BenchmarkSolutionRunEndpoint(b *testing.B) {
	_, testAPI := humatest.New(b)
	var shutting int32
	hctx := &api.HandlerContext{
		Config:         &config.Config{},
		IsShuttingDown: &shutting,
		StartTime:      time.Now(),
	}
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")
	for b.Loop() {
		testAPI.Post("/v1/solutions/run", `{"path": "/nonexistent.yaml"}`)
	}
}
