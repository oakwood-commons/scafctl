// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// recordingLoader returns ms or err and records what it was asked to load.
type recordingLoader struct {
	ms     *MaterializedSolution
	err    error
	got    source.Classification
	called bool
}

func (l *recordingLoader) Load(_ context.Context, c source.Classification) (*MaterializedSolution, error) {
	l.got, l.called = c, true
	return l.ms, l.err
}

// stubSelector selects with a real Router but loads through loader.
type stubSelector struct {
	*Router
	loader Loader
}

func (s stubSelector) SelectionLoader(Selection) Loader { return s.loader }

func parsedValidSolution(t *testing.T) *solution.Solution {
	t.Helper()
	var sol solution.Solution
	require.NoError(t, sol.LoadFromBytes([]byte(validSolutionYAML)))
	return &sol
}

func templateBundle(t *testing.T) []byte {
	t.Helper()
	src := t.TempDir()
	writeTree(t, src, map[string]string{"templates/main.tmpl": "bundled"})
	files, err := bundler.ReadFileSnapshot(src, []bundler.FileEntry{{RelPath: "templates/main.tmpl", Source: bundler.StaticAnalysis}})
	require.NoError(t, err)
	data, _, err := bundler.CreateBundleTar(files, nil)
	require.NoError(t, err)
	return data
}

func TestLocal(t *testing.T) {
	t.Parallel()

	builtSol := &solution.Solution{APIVersion: "scafctl.io/v1", Kind: "Solution", Metadata: solution.Metadata{Name: "built"}}

	tests := []struct {
		name       string
		files      map[string]string
		ref        string
		localRef   bool // ref is joined to the test dir
		risk       DiscoveryRisk
		loader     *recordingLoader
		wantPath   string // relative to the test dir; "" skips the check
		wantKind   source.Kind
		wantFetch  bool
		wantBundle bool
		wantInFile string
		wantErr    error
		wantOut    string
	}{
		{
			name:     "relative file resolves against the working directory",
			files:    map[string]string{"app.yaml": validSolutionYAML},
			ref:      "app.yaml",
			loader:   &recordingLoader{},
			wantPath: "app.yaml",
		},
		{
			name:     "relative extensionless directory is not fetched",
			files:    map[string]string{"mysol/solution.yaml": validSolutionYAML},
			ref:      "mysol",
			loader:   &recordingLoader{},
			wantPath: "mysol",
		},
		{
			name:     "existing absolute directory is used as-is",
			files:    map[string]string{"solutions/app.yaml": validSolutionYAML},
			ref:      "solutions",
			localRef: true,
			loader:   &recordingLoader{},
			wantPath: "solutions",
		},
		{
			name:     "missing local file is returned for the caller to report",
			ref:      "missing.yaml",
			loader:   &recordingLoader{},
			wantPath: "missing.yaml",
		},
		{
			name:     "auto-discovers a solution file",
			files:    map[string]string{"solution.yaml": validSolutionYAML},
			loader:   &recordingLoader{},
			wantPath: "solution.yaml",
			wantOut:  "Using ",
		},
		{
			name:     "auto-discovery low risk uses first match",
			files:    map[string]string{"scafctl/solution.yaml": validSolutionYAML, "solution.yaml": validSolutionYAML},
			loader:   &recordingLoader{},
			wantPath: "scafctl/solution.yaml",
			wantOut:  "using first match",
		},
		{
			name:    "auto-discovery high risk rejects multiple matches",
			files:   map[string]string{"scafctl/solution.yaml": validSolutionYAML, "solution.yaml": validSolutionYAML},
			risk:    DiscoveryRiskHigh,
			loader:  &recordingLoader{},
			wantErr: ErrAmbiguousSolution,
		},
		{
			name:    "auto-discovery finds nothing",
			loader:  &recordingLoader{},
			wantErr: ErrNoSolutionFound,
		},
		{
			name:       "catalog reference is fetched with its bundle",
			ref:        "test-solution@1.0.0",
			loader:     &recordingLoader{ms: &MaterializedSolution{Solution: parsedValidSolution(t), Bundle: templateBundle(t)}},
			wantKind:   source.KindBareName,
			wantFetch:  true,
			wantBundle: true,
			wantInFile: "name: test-solution",
			wantOut:    "Fetched test-solution@1.0.0",
		},
		{
			name:       "URL without raw content is marshalled",
			ref:        "https://example.com/solution.yaml",
			loader:     &recordingLoader{ms: &MaterializedSolution{Solution: builtSol}},
			wantKind:   source.KindURL,
			wantFetch:  true,
			wantInFile: "name: built",
		},
		{
			name:     "load failure wraps ErrFetch",
			ref:      "ghcr.io/org/sol@1.0.0",
			loader:   &recordingLoader{err: errors.New("registry unreachable")},
			wantKind: source.KindRemoteRef,
			wantErr:  ErrFetch,
		},
		{
			name:     "corrupt bundle wraps ErrFetch",
			ref:      "test-solution@1.0.0",
			loader:   &recordingLoader{ms: &MaterializedSolution{Solution: parsedValidSolution(t), Bundle: []byte("not a tar")}},
			wantKind: source.KindBareName,
			wantErr:  ErrFetch,
		},
		{
			name:    "stdin is not supported",
			ref:     "-",
			loader:  &recordingLoader{},
			wantErr: ErrUnsupportedKind,
		},
		{
			name:    "invalid reference",
			ref:     "ftp://example.com/sol",
			loader:  &recordingLoader{},
			wantErr: source.ErrInvalidRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tt.files)
			ctx, errOut := verboseWriterCtx(provider.WithWorkingDirectory(t.Context(), dir))

			ref := tt.ref
			if tt.localRef {
				ref = filepath.Join(dir, ref)
			}

			local, err := Local(ctx, stubSelector{Router: NewRouter(""), loader: tt.loader}, ref, tt.risk)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, local)
				if tt.wantKind != source.KindUnknown {
					assert.Equal(t, tt.wantKind, tt.loader.got.Kind)
				}
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = local.Close() })

			assert.Equal(t, tt.wantFetch, local.Fetched())
			assert.Equal(t, tt.wantFetch, tt.loader.called, "only remote sources are loaded")
			if tt.wantOut != "" {
				assert.Contains(t, errOut.String(), tt.wantOut)
			}
			if tt.wantPath != "" {
				assert.Equal(t, filepath.Join(dir, filepath.FromSlash(tt.wantPath)), local.Path)
			}
			if !tt.wantFetch {
				return
			}

			assert.Equal(t, tt.wantKind, tt.loader.got.Kind)
			assert.Equal(t, fetchedFileName, filepath.Base(local.Path))
			content, err := os.ReadFile(local.Path)
			require.NoError(t, err)
			assert.Contains(t, string(content), tt.wantInFile)
			if tt.wantBundle {
				bundled, err := os.ReadFile(filepath.Join(filepath.Dir(local.Path), "templates", "main.tmpl"))
				require.NoError(t, err)
				assert.Equal(t, "bundled", string(bundled))
			}

			require.NoError(t, local.Close())
			_, statErr := os.Stat(filepath.Dir(local.Path))
			assert.True(t, os.IsNotExist(statErr), "Close removes the temporary directory")
		})
	}
}

func TestLocal_TempDirUsesBinaryName(t *testing.T) {
	t.Parallel()

	ctx := settings.IntoContext(t.Context(), &settings.Run{BinaryName: "mycli"})
	loader := &recordingLoader{ms: &MaterializedSolution{Solution: parsedValidSolution(t)}}

	local, err := Local(ctx, stubSelector{Router: NewRouter(""), loader: loader}, "app@1.0.0", DiscoveryRiskLow)
	require.NoError(t, err)
	t.Cleanup(func() { _ = local.Close() })
	assert.Contains(t, filepath.Base(filepath.Dir(local.Path)), "mycli-solution-")
}

func BenchmarkLocal(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	require.NoError(b, os.WriteFile(path, []byte(validSolutionYAML), 0o600))
	var sol solution.Solution
	require.NoError(b, sol.LoadFromBytes([]byte(validSolutionYAML)))
	sel := stubSelector{Router: NewRouter(""), loader: &recordingLoader{ms: &MaterializedSolution{Solution: &sol}}}
	ctx := b.Context()

	b.Run("local path", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Local(ctx, sel, path, DiscoveryRiskLow); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("fetch to temp", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			local, err := Local(ctx, sel, "app@1.0.0", DiscoveryRiskLow)
			if err != nil {
				b.Fatal(err)
			}
			_ = local.Close()
		}
	})
}

func TestLocalSolution_Close(t *testing.T) {
	t.Parallel()

	var nilLocal *LocalSolution
	assert.NoError(t, nilLocal.Close())
	assert.False(t, nilLocal.Fetched())

	local := &LocalSolution{Path: "solution.yaml"}
	assert.NoError(t, local.Close())
	assert.False(t, local.Fetched())
}
