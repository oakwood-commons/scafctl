// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	solutionpkg "github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xdgMu serialises tests that mutate the global xdg.DataHome variable.
var xdgMu sync.Mutex

// mockLoader implements load.Loader for testing. It records the
// classification it was asked to load.
type mockLoader struct {
	sol *solutionpkg.Solution
	err error
	got source.Classification
}

var _ load.Loader = (*mockLoader)(nil)

func (m *mockLoader) Load(_ context.Context, c source.Classification) (*load.MaterializedSolution, error) {
	m.got = c
	if m.err != nil {
		return nil, m.err
	}
	return &load.MaterializedSolution{Solution: m.sol, Source: c}, nil
}

func testSolution(name, description string) *solutionpkg.Solution {
	return &solutionpkg.Solution{
		APIVersion: "scafctl.io/v1",
		Kind:       "Solution",
		Metadata:   solutionpkg.Metadata{Name: name, Description: description},
	}
}

func TestCmdOptionsVersion_GetSolution_Loader(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		output   string
		sol      *solutionpkg.Solution
		wantKind source.Kind
		wantOut  []string
	}{
		{
			name:     "local file with json output",
			file:     "/path/to/solution.yaml",
			output:   "json",
			sol:      testSolution("test-solution", ""),
			wantKind: source.KindLocalFile,
			wantOut:  []string{"test-solution", "{", "}"},
		},
		{
			name:     "URL with yaml output",
			file:     "https://example.com/solution.yaml",
			output:   "yaml",
			sol:      testSolution("url-solution", ""),
			wantKind: source.KindURL,
			wantOut:  []string{"url-solution"},
		},
		{
			name:     "empty path auto-discovers",
			file:     "",
			output:   "json",
			sol:      testSolution("auto-discovered-solution", ""),
			wantKind: source.KindAutoDiscover,
			wantOut:  []string{"auto-discovered-solution"},
		},
		{
			name:     "catalog name",
			file:     "my-solution@1.0.0",
			output:   "json",
			sol:      testSolution("my-solution", ""),
			wantKind: source.KindBareName,
			wantOut:  []string{"my-solution"},
		},
		{
			name:     "remote reference",
			file:     "ghcr.io/org/my-solution@1.0.0",
			output:   "json",
			sol:      testSolution("my-solution", ""),
			wantKind: source.KindRemoteRef,
			wantOut:  []string{"my-solution"},
		},
		{
			name:     "default output",
			file:     "/path/to/solution.yaml",
			output:   "",
			sol:      testSolution("default-output-solution", ""),
			wantKind: source.KindLocalFile,
			wantOut:  []string{"default-output-solution"},
		},
		{
			name:     "table output",
			file:     "/path/to/solution.yaml",
			output:   "table",
			sol:      testSolution("table-output-solution", ""),
			wantKind: source.KindLocalFile,
			wantOut:  []string{"table-output-solution"},
		},
		{
			name:     "solution with description",
			file:     "/path/to/complex.yaml",
			output:   "yaml",
			sol:      testSolution("complex-solution", "A solution with detailed metadata"),
			wantKind: source.KindLocalFile,
			wantOut:  []string{"complex-solution", "A solution with detailed metadata"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outBuf := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: &bytes.Buffer{}}
			cliParams := &settings.Run{NoColor: true, BinaryName: "scafctl"}
			ml := &mockLoader{sol: tc.sol}

			options := &CmdOptionsVersion{
				IOStreams: ioStreams,
				CliParams: cliParams,
				Output:    tc.output,
				File:      tc.file,
				loader:    ml,
			}

			err := options.GetSolution(settings.IntoContext(context.Background(), cliParams))
			require.NoError(t, err)
			assert.Equal(t, tc.wantKind, ml.got.Kind)
			assert.Equal(t, tc.file, ml.got.Ref)
			for _, want := range tc.wantOut {
				assert.Contains(t, outBuf.String(), want)
			}
		})
	}
}

func TestCmdOptionsVersion_GetSolution_LoaderErrors(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("failed to get solution")
	tests := []struct {
		name    string
		file    string
		loadErr error
		wantIs  error
		wantMsg string
	}{
		{
			name:    "load error",
			file:    "/invalid/path",
			loadErr: loadErr,
			wantIs:  loadErr,
			wantMsg: "failed to get solution",
		},
		{
			name:    "invalid reference",
			file:    "ftp://example.com/solution",
			wantIs:  source.ErrInvalidRef,
			wantMsg: "invalid solution reference",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			outBuf := &bytes.Buffer{}
			errBuf := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}
			cliParams := &settings.Run{NoColor: true}
			options := &CmdOptionsVersion{
				IOStreams: ioStreams,
				CliParams: cliParams,
				Output:    "json",
				File:      tc.file,
				loader:    &mockLoader{err: tc.loadErr},
			}
			ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))

			err := options.GetSolution(ctx)
			require.ErrorIs(t, err, tc.wantIs)
			assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
			assert.Empty(t, outBuf.String())
			assert.Contains(t, errBuf.String(), tc.wantMsg, "error should be written to stderr")
		})
	}
}

const discoverySolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: discovered
  version: 1.0.0
`

func TestCmdOptionsVersion_GetSolution(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()

		ioStreams := &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{NoColor: true},
			Output:    "json",
			File:      "/nonexistent/solution.yaml",
		}

		err := options.GetSolution(context.Background())
		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})

	t.Run("local file", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(file, []byte(discoverySolutionYAML), 0o600))

		outBuf := &bytes.Buffer{}
		options := &CmdOptionsVersion{
			IOStreams: &terminal.IOStreams{Out: outBuf, ErrOut: &bytes.Buffer{}},
			CliParams: &settings.Run{NoColor: true},
			Output:    "json",
			File:      file,
		}

		require.NoError(t, options.GetSolution(t.Context()))
		assert.Contains(t, outBuf.String(), "discovered")
	})

	t.Run("auto-discovers from working directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(discoverySolutionYAML), 0o600))

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}
		cliParams := &settings.Run{NoColor: true, BinaryName: "mycli", Verbose: true}
		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: cliParams,
			Output:    "json",
			Local:     true,
		}
		ctx := provider.WithWorkingDirectory(settings.IntoContext(t.Context(), cliParams), dir)
		ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))

		require.NoError(t, options.GetSolution(ctx))
		assert.Contains(t, outBuf.String(), "discovered")
		assert.Contains(t, errBuf.String(), filepath.Join(dir, "solution.yaml"))
	})

	t.Run("auto-discovery finds nothing", func(t *testing.T) {
		t.Parallel()

		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: errBuf}
		cliParams := &settings.Run{NoColor: true}
		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: cliParams,
			Output:    "json",
			Local:     true,
		}
		ctx := provider.WithWorkingDirectory(t.Context(), t.TempDir())
		ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))

		err := options.GetSolution(ctx)
		require.ErrorIs(t, err, load.ErrNoSolutionFound)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		assert.Contains(t, errBuf.String(), "no solution file found")
	})
}

// TestCommandSolution_Validation tests the RunE validation branches in CommandSolution:
// positional path rejection, -f+positional conflict, and invalid output type.
func TestCommandSolution_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "rejects relative path as positional arg",
			args:    []string{"./solution.yaml"},
			wantErr: "local file paths must use -f/--file flag",
		},
		{
			name:    "rejects yaml extension as positional arg",
			args:    []string{"solution.yaml"},
			wantErr: "local file paths must use -f/--file flag",
		},
		{
			name:    "rejects absolute path as positional arg",
			args:    []string{"/tmp/my-solution.yaml"},
			wantErr: "local file paths must use -f/--file flag",
		},
		{
			name:    "rejects both -f and positional arg",
			args:    []string{"-f", "solution.yaml", "my-catalog"},
			wantErr: "cannot use both -f/--file",
		},
		{
			name:    "rejects invalid output type",
			args:    []string{"-f", "solution.yaml", "-o", "xml"},
			wantErr: "xml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outBuf := &bytes.Buffer{}
			errBuf := &bytes.Buffer{}
			ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}
			cliParams := &settings.Run{NoColor: true}
			cmd := CommandSolution(cliParams, ioStreams, "get")
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			require.Error(t, err)
			// Error may be in err.Error() or written to stderr
			combinedOutput := err.Error() + errBuf.String()
			assert.Contains(t, combinedOutput, tc.wantErr)
		})
	}
}

func BenchmarkCommandSolution_Structure(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSolution(cliParams, ioStreams, "get")
	}
}

func TestCommandSolution_LocalFlag(t *testing.T) {
	t.Run("--local flag is registered", func(t *testing.T) {
		cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd := CommandSolution(cliParams, ioStreams, "get")

		localFlag := cmd.PersistentFlags().Lookup("local")
		require.NotNil(t, localFlag, "--local flag should be registered")
		assert.Equal(t, "false", localFlag.DefValue)
	})
}

func TestCommandSolution_HelpText(t *testing.T) {
	t.Run("short description mentions listing", func(t *testing.T) {
		cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd := CommandSolution(cliParams, ioStreams, "get")

		assert.Contains(t, cmd.Short, "List or get")
	})

	t.Run("long description mentions --local", func(t *testing.T) {
		cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd := CommandSolution(cliParams, ioStreams, "get")

		assert.Contains(t, cmd.Long, "--local")
	})

	t.Run("embedder binary name in long description", func(t *testing.T) {
		cliParams := &settings.Run{NoColor: true, BinaryName: "mycli"}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd := CommandSolution(cliParams, ioStreams, "get")

		assert.Contains(t, cmd.Long, "mycli")
	})
}

func TestListSolutions_OutputFormat(t *testing.T) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}

	options := &CmdOptionsVersion{
		IOStreams: ioStreams,
		CliParams: &settings.Run{
			NoColor:    true,
			BinaryName: "testcli",
		},
		Output: "json",
	}

	w := writer.New(ioStreams, options.CliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err := options.ListSolutions(ctx)
	require.NoError(t, err)
	// Should produce JSON output (either empty hint or list of solutions)
	output := outBuf.String()
	assert.NotEmpty(t, output)
}

func TestListSolutions_WithLocalArtifacts(t *testing.T) {
	// Cannot be parallel: temporarily redirects xdg.DataHome.
	xdgMu.Lock()
	tmpDir := t.TempDir()
	origDataHome := xdg.DataHome
	xdg.DataHome = tmpDir
	t.Cleanup(func() {
		xdg.DataHome = origDataHome
		xdgMu.Unlock()
	})

	// Pre-populate a local catalog with a solution.
	lgr := logr.Discard()
	localCat, err := catalog.NewLocalCatalogAt(
		filepath.Join(tmpDir, "scafctl", "catalog"), lgr,
	)
	require.NoError(t, err)

	solYAML := []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: list-test\n  version: 1.0.0\nspec: {}\n")
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "list-test",
		Version: semver.MustParse("1.0.0"),
	}
	_, err = localCat.Store(context.Background(), ref, solYAML, nil, nil, false)
	require.NoError(t, err)

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}

	options := &CmdOptionsVersion{
		IOStreams: ioStreams,
		CliParams: &settings.Run{
			NoColor:    true,
			BinaryName: "testcli",
		},
		Output: "json",
	}

	w := writer.New(ioStreams, options.CliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = options.ListSolutions(ctx)
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "list-test")
	assert.Contains(t, output, "1.0.0")
}

func TestListSolutions_WithMultipleArtifacts(t *testing.T) {
	xdgMu.Lock()
	tmpDir := t.TempDir()
	origDataHome := xdg.DataHome
	xdg.DataHome = tmpDir
	t.Cleanup(func() {
		xdg.DataHome = origDataHome
		xdgMu.Unlock()
	})

	lgr := logr.Discard()
	localCat, err := catalog.NewLocalCatalogAt(
		filepath.Join(tmpDir, "scafctl", "catalog"), lgr,
	)
	require.NoError(t, err)

	for _, item := range []struct{ name, ver string }{
		{"alpha-sol", "1.0.0"},
		{"alpha-sol", "2.0.0"},
		{"beta-sol", "0.5.0"},
	} {
		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    item.name,
			Version: semver.MustParse(item.ver),
		}
		_, storeErr := localCat.Store(context.Background(), ref,
			[]byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: "+item.name+"\n  version: "+item.ver+"\nspec: {}\n"),
			nil, nil, false)
		require.NoError(t, storeErr)
	}

	outBuf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: &bytes.Buffer{}}

	options := &CmdOptionsVersion{
		IOStreams: ioStreams,
		CliParams: &settings.Run{
			NoColor:    true,
			BinaryName: "testcli",
		},
		Output: "json",
	}

	w := writer.New(ioStreams, options.CliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err = options.ListSolutions(ctx)
	require.NoError(t, err)

	output := outBuf.String()
	// Deduplication: alpha-sol should show 2.0.0 (highest), not 1.0.0.
	assert.Contains(t, output, "alpha-sol")
	assert.Contains(t, output, "2.0.0")
	assert.Contains(t, output, "beta-sol")
	assert.Contains(t, output, "0.5.0")
}

func TestDeduplicateAndFormatSolutions(t *testing.T) {
	t.Parallel()

	t.Run("deduplicates by name keeping highest version", func(t *testing.T) {
		t.Parallel()
		artifacts := []catalog.ArtifactInfo{
			{Reference: catalog.Reference{Name: "app-a", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")}, Catalog: "local"},
			{Reference: catalog.Reference{Name: "app-a", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("2.0.0")}, Catalog: "local"},
			{Reference: catalog.Reference{Name: "app-b", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("0.1.0")}, Catalog: "remote"},
		}
		items := deduplicateAndFormatSolutions(artifacts)
		require.Len(t, items, 2)
		assert.Equal(t, "app-a", items[0].Name)
		assert.Equal(t, "2.0.0", items[0].Version)
		assert.Equal(t, "app-b", items[1].Name)
		assert.Equal(t, "0.1.0", items[1].Version)
	})

	t.Run("sorted alphabetically by name", func(t *testing.T) {
		t.Parallel()
		artifacts := []catalog.ArtifactInfo{
			{Reference: catalog.Reference{Name: "zebra", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")}, Catalog: "local"},
			{Reference: catalog.Reference{Name: "alpha", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")}, Catalog: "local"},
			{Reference: catalog.Reference{Name: "mid", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")}, Catalog: "remote"},
		}
		items := deduplicateAndFormatSolutions(artifacts)
		require.Len(t, items, 3)
		assert.Equal(t, "alpha", items[0].Name)
		assert.Equal(t, "mid", items[1].Name)
		assert.Equal(t, "zebra", items[2].Name)
	})

	t.Run("nil version kept when no versioned duplicate", func(t *testing.T) {
		t.Parallel()
		artifacts := []catalog.ArtifactInfo{
			{Reference: catalog.Reference{Name: "app-c", Kind: catalog.ArtifactKindSolution}, Catalog: "local"},
		}
		items := deduplicateAndFormatSolutions(artifacts)
		require.Len(t, items, 1)
		assert.Equal(t, "app-c", items[0].Name)
		assert.Equal(t, "", items[0].Version)
	})

	t.Run("versioned beats nil version", func(t *testing.T) {
		t.Parallel()
		artifacts := []catalog.ArtifactInfo{
			{Reference: catalog.Reference{Name: "app-d", Kind: catalog.ArtifactKindSolution}, Catalog: "local"},
			{Reference: catalog.Reference{Name: "app-d", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")}, Catalog: "remote"},
		}
		items := deduplicateAndFormatSolutions(artifacts)
		require.Len(t, items, 1)
		assert.Equal(t, "1.0.0", items[0].Version)
		assert.Equal(t, "remote", items[0].Catalog)
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		t.Parallel()
		items := deduplicateAndFormatSolutions(nil)
		assert.Empty(t, items)
	})
}

func TestCommandSolution_NoArgsCallsListSolutions(t *testing.T) {
	// Cannot be parallel: temporarily redirects xdg.DataHome to an empty
	// catalog so this test is not polluted by solutions already present in
	// the machine's real local catalog (e.g. from prior `scafctl build`/`run`
	// invocations).
	xdgMu.Lock()
	tmpDir := t.TempDir()
	origDataHome := xdg.DataHome
	xdg.DataHome = tmpDir
	t.Cleanup(func() {
		xdg.DataHome = origDataHome
		xdgMu.Unlock()
	})

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{Out: outBuf, ErrOut: errBuf}
	cliParams := &settings.Run{NoColor: true, BinaryName: "testcli"}

	w := writer.New(ioStreams, cliParams)
	cmd := CommandSolution(cliParams, ioStreams, "get")
	cmd.SetArgs([]string{}) // no args
	ctx := writer.WithWriter(context.Background(), w)
	cmd.SetContext(ctx)

	// No args, no --file, no --local: should call ListSolutions.
	// With an isolated, empty local catalog this produces "No solutions
	// found" info message.
	err := cmd.Execute()
	require.NoError(t, err)

	combined := outBuf.String() + errBuf.String()
	assert.Contains(t, combined, "No solutions found")
}
