// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// fakeEnsureAndAcquire satisfies the ensureAndAcquire interface for exercising
// the nil-guard paths of ensureProviderDependencies without a real plugin pool.
type fakeEnsureAndAcquire struct{}

func (fakeEnsureAndAcquire) EnsureAndAcquire(context.Context, []solution.PluginDependency) (func(), error) {
	return func() {}, nil
}

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

	// The default solution loader does not read local files.
	resp := testAPI.Post("/v1/solutions/run", map[string]any{"path": "/nonexistent/solution.yaml"})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "sources are not enabled")
}

func TestRegisterSolutionEndpoints_RenderInvalidPath(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/render", map[string]any{"path": "/nonexistent/solution.yaml"})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "sources are not enabled")
}

func TestRegisterSolutionEndpoints_TestInvalidPath(t *testing.T) {
	_, testAPI := humatest.New(t)
	hctx := newTestHandlerContext(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/test", map[string]any{"path": "/nonexistent/solution.yaml"})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "sources are not enabled")
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
		testAPI.Post("/v1/solutions/lint", map[string]any{"path": "/nonexistent.yaml"})
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
		testAPI.Post("/v1/solutions/run", map[string]any{"path": "/nonexistent.yaml"})
	}
}

// ── Direct unit tests for unexported helpers ──

func TestRejectUnsafePath_Direct(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"../secrets", true},
		{"foo/../../etc/passwd", true},
		{"/absolute/path", true},
		{"~/home-relative", true},
		{"relative/safe/path", false},
		{"just-a-name", false},
		{"subdir/nested", false},
		{"C:foo", true},
		{"D:dir/file", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := rejectUnsafePath(tt.path, "test-op")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSolutionRun_UnsafeOutputDir_ViaURL passes a valid HTTP URL for path and
// verifies rejectUnsafePath blocks the unsafe outputDir before anything loads.
func TestSolutionRun_UnsafeOutputDir_ViaURL(t *testing.T) {
	unsafe := []string{"/etc/passwd", "../secret", "~/evil"}
	for _, tc := range unsafe {
		t.Run(tc, func(t *testing.T) {
			_, testAPI := humatest.New(t)
			hctx := newTestHandlerContext(t)
			loader := &recordingLoader{err: errors.New("must not load")}
			hctx.NewSolutionLoader = loader.factory()
			RegisterSolutionEndpoints(testAPI, hctx, "/v1")

			body := strings.NewReader(`{"path":"https://example.com/solution.yaml","outputDir":"` + tc + `"}`)
			resp := testAPI.Post("/v1/solutions/run", body, "Content-Type: application/json")
			assert.Equal(t, http.StatusBadRequest, resp.Code, "expected 400 for unsafe outputDir %q", tc)
			assert.Empty(t, loader.got, "outputDir must be validated before the solution loads")
		})
	}
}

// ── loadSolution tests ──

const endpointTestSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: endpoint-loader-test
  version: 1.0.0
spec:
  resolvers: {}
`

// recordingLoader is a load.Loader that records every classification it is
// asked to load and returns a fixed result.
type recordingLoader struct {
	ms   *load.MaterializedSolution
	err  error
	got  []source.Classification
	opts []load.RouterOption
}

func (l *recordingLoader) Load(_ context.Context, c source.Classification) (*load.MaterializedSolution, error) {
	l.got = append(l.got, c)
	if l.err != nil {
		return nil, l.err
	}
	return l.ms, nil
}

// factory returns a SolutionLoaderFactory that records the caller's options
// and always returns l.
func (l *recordingLoader) factory() api.SolutionLoaderFactory {
	return func(_ context.Context, opts ...load.RouterOption) load.Loader {
		l.opts = opts
		return l
	}
}

// testMaterialized returns a MaterializedSolution holding a valid solution.
func testMaterialized(t *testing.T) *load.MaterializedSolution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.FromYAML([]byte(endpointTestSolutionYAML)))
	return &load.MaterializedSolution{Solution: sol}
}

// requireStatus asserts err is a Huma error with the given status and returns
// its message.
func requireStatus(t *testing.T, err error, status int) string {
	t.Helper()
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, status, statusErr.GetStatus())
	return err.Error()
}

func TestLoadSolution_DefaultLoaderRejectsLocalSources(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/etc/solution.yaml",
		"./solution.yaml",
		"../solution.yaml",
		"~/solutions/my-app.yaml",
		"dir\\solution.yaml",
		"C:\\solutions\\app.yaml",
		"solution.yaml",
		"config.json",
		"configs/secret",
		"dir/../../etc/passwd",
		"a/b/c/solution",
		"",
		"-",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			ms, err := loadSolution(t.Context(), newTestHandlerContext(t), p, "test-op")
			assert.Nil(t, ms)
			msg := requireStatus(t, err, http.StatusBadRequest)
			assert.Contains(t, msg, "sources are not enabled on this server")
		})
	}
}

func TestLoadSolution_InvalidReference(t *testing.T) {
	t.Parallel()

	paths := []string{
		"file:///etc/passwd",
		"FILE:///etc/passwd",
		"ftp://example.com/solution",
		"ghcr.io/../../etc/passwd",
		"oci://ghcr.io/org/../name",
		"a@b@c",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			loader := &recordingLoader{ms: testMaterialized(t)}
			hctx := newTestHandlerContext(t)
			hctx.NewSolutionLoader = loader.factory()

			ms, err := loadSolution(t.Context(), hctx, p, "test-op")
			assert.Nil(t, ms)
			msg := requireStatus(t, err, http.StatusBadRequest)
			assert.Contains(t, msg, "invalid solution reference")
			assert.Empty(t, loader.got, "an invalid reference must not reach the loader")
		})
	}
}

func TestLoadSolution_RemoteSourcesReachLoader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want source.Kind
	}{
		{"https://example.com/solution.yaml", source.KindURL},
		{"http://example.com/solution.yaml", source.KindURL},
		{"my-solution", source.KindBareName},
		{"my-solution@1.0.0", source.KindBareName},
		{"ghcr.io/org/solution", source.KindRemoteRef},
		{"localhost:5000/solution", source.KindRemoteRef},
		{"oci://ghcr.io/org/solution:1.0.0", source.KindRemoteRef},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			want := testMaterialized(t)
			loader := &recordingLoader{ms: want}
			hctx := newTestHandlerContext(t)
			hctx.NewSolutionLoader = loader.factory()

			got, err := loadSolution(t.Context(), hctx, tt.path, "test-op")
			require.NoError(t, err)
			assert.Same(t, want, got)
			require.Len(t, loader.got, 1)
			assert.Equal(t, tt.want, loader.got[0].Kind)
			assert.Equal(t, tt.path, loader.got[0].Ref)
		})
	}
}

func TestLoadSolution_LoaderErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "fetch failure hides the raw error",
			err:     errors.New("dial tcp 10.0.0.1:443: internal detail"),
			wantMsg: "failed to load solution",
		},
		{
			name:    "source not enabled names the kind",
			err:     fmt.Errorf("%w: url", load.ErrSourceNotEnabled),
			wantMsg: "test-op: url sources are not enabled on this server",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loader := &recordingLoader{err: tt.err}
			hctx := newTestHandlerContext(t)
			hctx.NewSolutionLoader = loader.factory()

			ms, err := loadSolution(t.Context(), hctx, "https://example.com/solution.yaml", "test-op")
			assert.Nil(t, ms)
			msg := requireStatus(t, err, http.StatusBadRequest)
			assert.Contains(t, msg, tt.wantMsg)
			assert.NotContains(t, msg, "internal detail")
		})
	}
}

func TestSolutionEndpoints_UseHandlerLoader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		endpoint string
		wantOpts int
	}{
		{"/v1/solutions/lint", 1}, // load.WithLenient
		{"/v1/solutions/inspect", 0},
		{"/v1/solutions/test", 0},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			t.Parallel()
			_, testAPI := humatest.New(t)
			loader := &recordingLoader{ms: testMaterialized(t)}
			hctx := newTestHandlerContext(t)
			hctx.ProviderRegistry = provider.NewRegistry()
			hctx.NewSolutionLoader = loader.factory()
			RegisterSolutionEndpoints(testAPI, hctx, "/v1")

			resp := testAPI.Post(tt.endpoint, map[string]any{"path": "my-solution@1.0.0"})
			assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			require.Len(t, loader.got, 1)
			assert.Equal(t, source.KindBareName, loader.got[0].Kind)
			assert.Len(t, loader.opts, tt.wantOpts)
		})
	}
}

func TestSolutionRun_CatalogNameReachesLoader(t *testing.T) {
	t.Parallel()

	_, testAPI := humatest.New(t)
	loader := &recordingLoader{err: errors.New("not in catalog")}
	hctx := newTestHandlerContext(t)
	hctx.NewSolutionLoader = loader.factory()
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/run", map[string]any{"path": "entra-user-groups"})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	require.Len(t, loader.got, 1)
	assert.Equal(t, source.KindBareName, loader.got[0].Kind)
}

func TestLoadAndPrepareSolution_Lock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		lock       []byte
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "malformed lock is a client error",
			lock:       []byte("{not json"),
			wantStatus: http.StatusBadRequest,
			wantMsg:    "failed to parse solution lock",
		},
		{
			// A valid lock parses and the request proceeds to dependency
			// resolution, which fails here only because no composite
			// registry is wired.
			name:       "valid lock proceeds to dependency resolution",
			lock:       []byte(`{"version":1}`),
			wantStatus: http.StatusBadGateway,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ms := testMaterialized(t)
			ms.Lock = tt.lock
			loader := &recordingLoader{ms: ms}
			hctx := newTestHandlerContext(t)
			hctx.NewSolutionLoader = loader.factory()

			sol, reg, release, err := loadAndPrepareSolution(t.Context(), hctx, "my-solution", "", "solution-dryrun")
			releaseFunc(release)
			assert.Nil(t, sol)
			assert.Nil(t, reg)
			msg := requireStatus(t, err, tt.wantStatus)
			if tt.wantMsg != "" {
				assert.Contains(t, msg, tt.wantMsg)
			}
		})
	}
}

// ── ensureProviderDependencies nil-guard tests ──

func TestEnsureProviderDependencies_NilArgs(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	reg := provider.NewCompositeRegistry()
	fetcher := &plugin.Fetcher{}
	var ensure ensureAndAcquire = fakeEnsureAndAcquire{}

	tests := []struct {
		name    string
		sol     *solution.Solution
		reg     *provider.CompositeRegistry
		fetcher *plugin.Fetcher
		ensure  ensureAndAcquire
		wantErr error
	}{
		{
			name:    "nil solution",
			sol:     nil,
			reg:     reg,
			fetcher: fetcher,
			ensure:  ensure,
			wantErr: errEnsureNilSolution,
		},
		{
			name:    "nil composite registry",
			sol:     sol,
			reg:     nil,
			fetcher: fetcher,
			ensure:  ensure,
			wantErr: errEnsureNilCompositeReg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scoped, release, err := ensureProviderDependencies(
				context.Background(), tt.sol, tt.reg, nil, tt.fetcher, tt.ensure, nil, prepare.LockModeStrict,
			)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, scoped)
			assert.Nil(t, release)
		})
	}
}

// TestEnsureProviderDependencies_NilAliasAllowed verifies a nil-pointer
// *catalogindex.Index alias resolver is accepted: ScopeSolutionProviders
// consults the resolver only for fully-qualified refs and a nil-pointer index
// is safe to read.
func TestEnsureProviderDependencies_NilAliasAllowed(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	reg := provider.NewCompositeRegistry()
	fetcher := &plugin.Fetcher{}

	scoped, release, err := ensureProviderDependencies(
		context.Background(), sol, reg, (*catalogindex.Index)(nil), fetcher, fakeEnsureAndAcquire{}, nil, prepare.LockModeStrict,
	)
	assert.NoError(t, err)
	assert.NotNil(t, scoped)
	assert.NotNil(t, release)
	if release != nil {
		release()
	}
}

func TestHandleEnsureError_MissingLockFileMapsTo400(t *testing.T) {
	t.Parallel()

	// A missing lock file surfaces from BuildExecutionRegistryAPI as
	// prepare.ErrMissingLockFile; handleEnsureError must classify it as a
	// client error (400), ahead of the pool-error mapping whose default is 502.
	wrapped := fmt.Errorf("provider %q: strict mode requires a lock file: %w", "my-plugin", prepare.ErrMissingLockFile)

	err := handleEnsureError(context.Background(), wrapped, "solution-render")

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

func TestHandleEnsureError_GenericFallsThroughToPoolMapping(t *testing.T) {
	t.Parallel()

	// A non-lock, non-pool error falls through to PoolErrorHTTPStatus, whose
	// default classification is 502. This guards the ordering: a generic error
	// must NOT be misclassified as the 400 missing-lock case.
	err := handleEnsureError(context.Background(), errors.New("boom"), "solution-render")

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadGateway, statusErr.GetStatus())
}
