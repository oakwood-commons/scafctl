// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"bytes"
	"context"
	"errors"
	"testing"

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
