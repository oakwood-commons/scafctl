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
			Path:   "/path/to/solution.yaml",
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
			Path:   "https://example.com/solution.yaml",
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
			Path:   "",
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
			Path:   "/invalid/path",
		}

		err := options.GetSolutionWithGetter(context.Background(), mockGetter)
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
			Path:   "/path/to/solution.yaml",
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
			Path:      "/path/to/solution.yaml",
		}

		err := options.GetSolutionWithGetter(ctx, mockGetter)
		require.NoError(t, err)
		mockGetter.AssertExpectations(t)

		assert.NotEmpty(t, outBuf.String())
		assert.Contains(t, outBuf.String(), "context-solution")
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
			Path:   "/path/to/complex.yaml",
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
			Path:   "/nonexistent/solution.yaml",
		}

		err := options.GetSolution(context.Background())
		require.Error(t, err)
	})
}
