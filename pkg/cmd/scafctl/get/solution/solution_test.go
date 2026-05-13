// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	solutionpkg "github.com/oakwood-commons/scafctl/pkg/solution"
	solutionget "github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// xdgMu serialises tests that mutate the global xdg.DataHome variable.
var xdgMu sync.Mutex

func TestCmdOptionsVersion_GetSolutionWithGetter(t *testing.T) {
	t.Run("successful get from local file with json output", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "test-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "json",
			File:   "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "test-solution")
	})

	t.Run("successful get from URL with yaml output", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "url-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "https://example.com/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "yaml",
			File:   "https://example.com/solution.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "url-solution")
	})

	t.Run("empty path uses auto-discovery", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "auto-discovered-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "json",
			File:   "",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "auto-discovered-solution")
	})

	t.Run("getter returns error", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedError := errors.New("failed to get solution")

		mockGetter.On("Get", mock.Anything, "/invalid/path").
			Return(nil, expectedError)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "json",
			File:   "/invalid/path",
		}

		w := writer.New(ioStreams, options.CliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err := options.GetSolutionWithGetter(ctx, mockGetter)
		require.Error(t, err)
		assert.True(t, errors.Is(err, expectedError), "error should wrap the original error")
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err), "should return FileNotFound exit code")
		mockGetter.AssertExpectations(t)

		assert.Empty(t, outBuf.String())
		assert.Contains(t, errBuf.String(), "failed to get solution", "error should be written to stderr")
	})

	t.Run("json output format explicitly", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "json-output-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "json",
			File:   "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		// Verify it's valid JSON format
		assert.Contains(t, outBuf.String(), "{")
		assert.Contains(t, outBuf.String(), "}")
	})

	t.Run("context with values", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "context-solution",
			},
		}

		cliParams := &settings.Run{
			NoColor: true,
		}
		ctx := settings.IntoContext(context.Background(), cliParams)

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: cliParams,
			Output:    "json",
			File:      "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(ctx, mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "context-solution")
	})

	t.Run("default output does not crash", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "default-output-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor:    true,
				BinaryName: "scafctl",
			},
			Output: "",
			File:   "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "default-output-solution")
	})

	t.Run("table output does not crash", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name: "table-output-solution",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor:    true,
				BinaryName: "scafctl",
			},
			Output: "table",
			File:   "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "table-output-solution")
	})

	t.Run("solution with complex data", func(t *testing.T) {
		mockGetter := &solutionget.MockGetter{}
		expectedSolution := &solutionpkg.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solutionpkg.Metadata{
				Name:        "complex-solution",
				Description: "A solution with detailed metadata",
			},
		}

		mockGetter.On("Get", mock.Anything, "/path/to/complex.yaml").
			Return(expectedSolution, nil)

		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "yaml",
			File:   "/path/to/complex.yaml",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		output := outBuf.String()
		assert.NotEmpty(t, output)
		assert.Contains(t, output, "complex-solution")
		assert.Contains(t, output, "A solution with detailed metadata")
	})
}

func TestCmdOptionsVersion_GetSolution(t *testing.T) {
	t.Run("delegates to GetSolutionWithGetter", func(t *testing.T) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		ioStreams := &terminal.IOStreams{
			Out:    outBuf,
			ErrOut: errBuf,
		}

		options := &CmdOptionsVersion{
			IOStreams: ioStreams,
			CliParams: &settings.Run{
				NoColor: true,
			},
			Output: "json",
			File:   "/nonexistent/solution.yaml",
		}

		err := options.GetSolution(context.Background())
		require.Error(t, err)
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
		filepath.Join(tmpDir, "scafctl", "catalog"), lgr)
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
		filepath.Join(tmpDir, "scafctl", "catalog"), lgr)
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
	t.Parallel()

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
	// Without a real catalog this produces "No solutions found" info message.
	err := cmd.Execute()
	require.NoError(t, err)

	combined := outBuf.String() + errBuf.String()
	assert.Contains(t, combined, "No solutions found")
}
