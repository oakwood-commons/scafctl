// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
)

// bundleReadSolutionYAML reads a bundled file through the file provider using
// a bundle-relative path, the way a solution author would.
const bundleReadSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: endpoint-bundle-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: data/greeting.txt
`

const bundledGreeting = "hello from the bundle"

// bundleWriteSolutionYAML reads the bundled greeting and writes it to a
// relative path from an action.
const bundleWriteSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: endpoint-bundle-run-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: data/greeting.txt
  workflow:
    actions:
      writeGreeting:
        provider: file
        inputs:
          operation: write
          path: out.txt
          content:
            expr: "_.greeting.content"
`

// bundledMaterialized returns a solution that reads data/greeting.txt, plus a
// bundle carrying that file.
func bundledMaterialized(t *testing.T) *load.MaterializedSolution {
	t.Helper()
	return bundledMaterializedYAML(t, bundleReadSolutionYAML)
}

// bundledMaterializedYAML returns the solution parsed from solYAML plus a
// bundle carrying data/greeting.txt.
func bundledMaterializedYAML(t *testing.T, solYAML string) *load.MaterializedSolution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.FromYAML([]byte(solYAML)))

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data", "greeting.txt"), []byte(bundledGreeting), 0o600))
	snap, err := bundler.ReadFileSnapshot(root, []bundler.FileEntry{{RelPath: "data/greeting.txt"}})
	require.NoError(t, err)
	data, _, err := bundler.CreateBundleTar(snap, nil)
	require.NoError(t, err)

	return &load.MaterializedSolution{Solution: sol, Bundle: data}
}

// newExecHandlerContext returns a handler context able to execute builtin-only
// solutions that use the file provider.
func newExecHandlerContext(t *testing.T, ms *load.MaterializedSolution) *api.HandlerContext {
	t.Helper()
	builtins := provider.NewRegistry()
	require.NoError(t, builtins.Register(fileprovider.NewFileProvider()))

	hctx := newTestHandlerContext(t)
	hctx.ProviderRegistry = builtins
	hctx.CompositeRegistry = provider.NewCompositeRegistryFromBase(builtins)
	hctx.CatalogIndex = catalogindex.FromConfig(&config.Config{})
	hctx.NewSolutionLoader = (&recordingLoader{ms: ms}).factory()
	return hctx
}

type renderGreeting struct {
	ResolverData struct {
		Greeting struct {
			Content string `json:"content"`
			Path    string `json:"path"`
		} `json:"greeting"`
	} `json:"resolverData"`
}

func renderBundled(t *testing.T, hctx *api.HandlerContext) renderGreeting {
	t.Helper()
	_, testAPI := humatest.New(t)
	RegisterSolutionEndpoints(testAPI, hctx, "/v1")

	resp := testAPI.Post("/v1/solutions/render", map[string]any{"path": "my-solution@1.0.0"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out renderGreeting
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	return out
}

func TestSolutionRender_ReadsBundledFile(t *testing.T) {
	t.Parallel()
	out := renderBundled(t, newExecHandlerContext(t, bundledMaterialized(t)))
	assert.Equal(t, bundledGreeting, out.ResolverData.Greeting.Content)
}

func TestSolutionRender_BundleWinsOverWorkingDirectory(t *testing.T) {
	// A same-named file in the server's working directory must not be read in
	// place of the bundled one.
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "data", "greeting.txt"), []byte("decoy from server cwd"), 0o600))
	t.Chdir(cwd)

	out := renderBundled(t, newExecHandlerContext(t, bundledMaterialized(t)))
	assert.Equal(t, bundledGreeting, out.ResolverData.Greeting.Content)
}

func TestSolutionRender_BundleDirRemovedAfterResponse(t *testing.T) {
	t.Parallel()
	out := renderBundled(t, newExecHandlerContext(t, bundledMaterialized(t)))

	readPath := out.ResolverData.Greeting.Path
	require.NotEmpty(t, readPath)
	assert.NoFileExists(t, readPath, "the per-request bundle dir must be removed once the request finishes")
}

func TestSolutionRun_RelativeOutputDirSurvivesBundleCleanup(t *testing.T) {
	// A relative outputDir must resolve against the server's working
	// directory, not the per-request bundle dir that is deleted on return.
	cwd := t.TempDir()
	t.Chdir(cwd)

	_, testAPI := humatest.New(t)
	RegisterSolutionEndpoints(testAPI, newExecHandlerContext(t, bundledMaterializedYAML(t, bundleWriteSolutionYAML)), "/v1")

	resp := testAPI.Post("/v1/solutions/run", map[string]any{"path": "my-solution@1.0.0", "outputDir": "generated"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	got, err := os.ReadFile(filepath.Join(cwd, "generated", "out.txt"))
	require.NoError(t, err, "action output must outlive the request")
	assert.Equal(t, bundledGreeting, string(got))
}

func TestSolutionRun_NoOutputDir_WritesToWorkingDirectory(t *testing.T) {
	// With no outputDir in the request or server config, relative action
	// writes must land in the server's working directory (as in the CLI), not
	// the per-request bundle dir, while resolvers still read bundled inputs.
	cwd := t.TempDir()
	t.Chdir(cwd)

	_, testAPI := humatest.New(t)
	RegisterSolutionEndpoints(testAPI, newExecHandlerContext(t, bundledMaterializedYAML(t, bundleWriteSolutionYAML)), "/v1")

	resp := testAPI.Post("/v1/solutions/run", map[string]any{"path": "my-solution@1.0.0"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	got, err := os.ReadFile(filepath.Join(cwd, "out.txt"))
	require.NoError(t, err, "action output must outlive the request")
	assert.Equal(t, bundledGreeting, string(got), "the resolver must still read the bundled input")
}

func TestLoadAndPrepareSolution_BundleCleanedUpOnEnsureError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	hctx := newTestHandlerContext(t)
	hctx.NewSolutionLoader = (&recordingLoader{ms: bundledMaterialized(t)}).factory()
	// No composite registry: dependency resolution fails after staging.

	_, sol, reg, release, err := loadAndPrepareSolution(t.Context(), hctx, "my-solution", "", "solution-render")
	requireStatus(t, err, http.StatusBadGateway)
	assert.Nil(t, sol)
	assert.Nil(t, reg)

	require.NotNil(t, release, "staged bundle must be released by the caller")
	releaseFunc(release)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "bundle dir must be removed on the error path")
}

func TestLoadAndPrepareSolution_ContextCarriesSolutionDirectory(t *testing.T) {
	t.Parallel()
	hctx := newExecHandlerContext(t, bundledMaterialized(t))

	ctx, sol, reg, release, err := loadAndPrepareSolution(t.Context(), hctx, "my-solution", "", "solution-render")
	defer releaseFunc(release)
	require.NoError(t, err)
	require.NotNil(t, sol)
	require.NotNil(t, reg)

	dir, ok := provider.SolutionDirectoryFromContext(ctx)
	require.True(t, ok)
	assert.FileExists(t, filepath.Join(dir, "data", "greeting.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "solution.yaml"), "the solution must not be written to disk")

	execSettings, ok := provider.ExecutionSettingsFromContext(ctx)
	require.True(t, ok)
	var meta struct {
		Entrypoint string `json:"entrypoint"`
	}
	require.NoError(t, json.Unmarshal(execSettings["metadata"], &meta))
	assert.Equal(t, prepare.EntrypointAPI, meta.Entrypoint)
}

func TestLoadAndPrepareSolution_NoBundle_NoSolutionDirectory(t *testing.T) {
	t.Parallel()
	hctx := newExecHandlerContext(t, testMaterialized(t))

	ctx, sol, _, release, err := loadAndPrepareSolution(t.Context(), hctx, "my-solution", "", "solution-render")
	defer releaseFunc(release)
	require.NoError(t, err)
	require.NotNil(t, sol)

	_, ok := provider.SolutionDirectoryFromContext(ctx)
	assert.False(t, ok)
}

func TestLoadAndPrepareSolution_CorruptBundle(t *testing.T) {
	t.Parallel()
	ms := testMaterialized(t)
	ms.Bundle = []byte("not a tar archive")
	hctx := newExecHandlerContext(t, ms)

	_, sol, _, release, err := loadAndPrepareSolution(t.Context(), hctx, "my-solution", "", "solution-render")
	releaseFunc(release)
	assert.Nil(t, sol)
	msg := requireStatus(t, err, http.StatusBadRequest)
	assert.Contains(t, msg, "failed to extract solution bundle")
}

func TestHandleStageError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "invalid lock",
			err:        fmt.Errorf("%w: bad json", prepare.ErrInvalidLock),
			wantStatus: http.StatusBadRequest,
			wantMsg:    "failed to parse solution lock",
		},
		{
			name:       "bundle too large",
			err:        fmt.Errorf("%w: %w", prepare.ErrInvalidBundle, bundler.ErrBundleTooLarge),
			wantStatus: http.StatusBadRequest,
			wantMsg:    "solution bundle exceeds the maximum extracted size",
		},
		{
			name:       "corrupt bundle",
			err:        fmt.Errorf("%w: unexpected EOF", prepare.ErrInvalidBundle),
			wantStatus: http.StatusBadRequest,
			wantMsg:    "failed to extract solution bundle",
		},
		{
			name:       "server-side failure hides detail",
			err:        errors.New("mkdir /private/tmp/secret: permission denied"),
			wantStatus: http.StatusInternalServerError,
			wantMsg:    "failed to stage solution",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := requireStatus(t, handleStageError(t.Context(), tt.err, "solution-render"), tt.wantStatus)
			assert.Contains(t, msg, tt.wantMsg)
			assert.NotContains(t, msg, "/private/tmp")
		})
	}
}
