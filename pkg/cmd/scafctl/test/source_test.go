// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoader implements load.Loader for testing. It records the
// classification it was asked to load.
type fakeLoader struct {
	ms     *load.MaterializedSolution
	err    error
	got    source.Classification
	called bool
}

var _ load.Loader = (*fakeLoader)(nil)

func (f *fakeLoader) Load(_ context.Context, c source.Classification) (*load.MaterializedSolution, error) {
	f.got, f.called = c, true
	if f.err != nil {
		return nil, f.err
	}
	return f.ms, nil
}

// fakeSelector selects with a real router but loads through a fakeLoader.
type fakeSelector struct {
	*load.Router
	loader *fakeLoader
}

func (f fakeSelector) SelectionLoader(load.Selection) load.Loader { return f.loader }

func newFakeSelector(l *fakeLoader) fakeSelector {
	return fakeSelector{Router: load.NewRouter(""), loader: l}
}

// fetchedSolutionYAML is a solution with one test case, as a fetch returns it.
const fetchedSolutionYAML = discoverySolutionYAML + `  testing:
    cases:
      renders:
        command: [run, resolver]
        assertions:
          - expression: __exitCode == 0
`

// parsedSolution parses fetchedSolutionYAML so RawContent is populated.
func parsedSolution(t *testing.T) *solution.Solution {
	t.Helper()
	var sol solution.Solution
	require.NoError(t, sol.LoadFromBytes([]byte(fetchedSolutionYAML)))
	return &sol
}

// bundleTar builds a bundle tar holding templates/main.tmpl.
func bundleTar(t *testing.T) []byte {
	t.Helper()
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "templates", "main.tmpl"), []byte("bundled"), 0o600))
	files, err := bundler.ReadFileSnapshot(src, []bundler.FileEntry{{RelPath: "templates/main.tmpl", Source: bundler.StaticAnalysis}})
	require.NoError(t, err)
	data, _, err := bundler.CreateBundleTar(files, nil)
	require.NoError(t, err)
	return data
}

func TestLocalSolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		testsPath string
		ref       string
		hint      string
		loader    *fakeLoader
		discover  bool
		wantPath  string
		wantFetch bool
		wantIs    error
		wantCode  int
		wantErr   string
	}{
		{
			name:      "tests path wins over ref",
			testsPath: "./tests",
			ref:       "app@1.0.0",
			loader:    &fakeLoader{},
			wantPath:  "./tests",
		},
		{
			name:      "catalog reference is fetched",
			ref:       "discovered@1.0.0",
			loader:    &fakeLoader{ms: &load.MaterializedSolution{Solution: parsedSolution(t)}},
			wantFetch: true,
		},
		{
			name:     "fetch failure is a catalog error",
			ref:      "ghcr.io/org/sol@1.0.0",
			loader:   &fakeLoader{err: errors.New("registry unreachable")},
			wantIs:   load.ErrFetch,
			wantCode: exitcode.CatalogError,
			wantErr:  "registry unreachable",
		},
		{
			name:     "invalid reference is invalid input",
			ref:      "ftp://example.com/sol",
			loader:   &fakeLoader{},
			wantIs:   source.ErrInvalidRef,
			wantCode: exitcode.InvalidInput,
			wantErr:  "invalid solution reference",
		},
		{
			name:     "no solution found carries the hint",
			discover: true,
			hint:     testsPathHint,
			loader:   &fakeLoader{},
			wantIs:   load.ErrNoSolutionFound,
			wantCode: exitcode.InvalidInput,
			wantErr:  "--tests-path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tc.discover {
				ctx, _ = discoveryDir(t, false)
			}
			streams, _, errOut := terminal.NewTestIOStreams()
			w := writer.New(streams, settings.NewCliParams())

			local, err := localSolution(ctx, w, newFakeSelector(tc.loader), tc.testsPath, tc.ref, tc.hint)
			if tc.wantCode != 0 {
				require.ErrorIs(t, err, tc.wantIs)
				assert.Equal(t, tc.wantCode, exitcode.GetCode(err))
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.NotEmpty(t, errOut.String(), "the error is written")
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = local.Close() })

			assert.Equal(t, tc.wantFetch, local.Fetched())
			assert.Equal(t, tc.wantFetch, tc.loader.called)
			if tc.wantPath != "" {
				assert.Equal(t, tc.wantPath, local.Path)
			}
		})
	}
}

func TestLocalSolution_DefaultRouter(t *testing.T) {
	t.Parallel()

	ctx, dir := discoveryDir(t, true)
	streams, _, _ := terminal.NewTestIOStreams()
	w := writer.New(streams, settings.NewCliParams())

	local, err := localSolution(ctx, w, nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "solution.yaml"), local.Path)
	assert.False(t, local.Fetched())
}
