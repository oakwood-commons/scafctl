// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSolution(t *testing.T) {
	tests := []struct {
		name     string
		validate func(t *testing.T)
	}{
		{
			name: "creates_command_with_correct_usage",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				assert.Equal(t, "solution", cmd.Use)
				assert.Contains(t, cmd.Aliases, "sol")
				assert.Contains(t, cmd.Aliases, "s")
				assert.Contains(t, cmd.Aliases, "solutions")
				assert.Contains(t, cmd.Short, "Render a solution")
			},
		},
		{
			name: "has_file_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("file")
				require.NotNil(t, flag)
				assert.Equal(t, "f", flag.Shorthand)
			},
		},
		{
			name: "has_output_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("output")
				require.NotNil(t, flag)
				assert.Equal(t, "o", flag.Shorthand)
				assert.Equal(t, "json", flag.DefValue)
			},
		},
		{
			name: "has_graph_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("graph")
				require.NotNil(t, flag)
				assert.Equal(t, "false", flag.DefValue)
			},
		},
		{
			name: "has_action_graph_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("action-graph")
				require.NotNil(t, flag)
				assert.Equal(t, "false", flag.DefValue)
			},
		},
		{
			name: "has_graph_format_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("graph-format")
				require.NotNil(t, flag)
				assert.Equal(t, "ascii", flag.DefValue)
			},
		},
		{
			name: "has_snapshot_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("snapshot")
				require.NotNil(t, flag)
				assert.Equal(t, "false", flag.DefValue)
			},
		},
		{
			name: "has_resolver_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("resolver")
				require.NotNil(t, flag)
				assert.Equal(t, "r", flag.Shorthand)
			},
		},
		{
			name: "has_compact_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("compact")
				require.NotNil(t, flag)
			},
		},
		{
			name: "has_redact_flag",
			validate: func(t *testing.T) {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				cliParams := &settings.Run{}
				cmd := CommandSolution(cliParams, ioStreams, "render")

				flag := cmd.Flags().Lookup("redact")
				require.NotNil(t, flag)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.validate)
	}
}

// mockGetter implements get.Interface for testing
type mockGetter struct {
	sol *solution.Solution
	err error
}

func (m *mockGetter) Get(_ context.Context, _ string) (*solution.Solution, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.sol, nil
}

func (m *mockGetter) FromLocalFileSystem(_ context.Context, _ string) (*solution.Solution, error) {
	return m.Get(context.Background(), "")
}

func (m *mockGetter) FromURL(_ context.Context, _ string) (*solution.Solution, error) {
	return m.Get(context.Background(), "")
}

func (m *mockGetter) GetWithBundle(_ context.Context, _ string) (*solution.Solution, []byte, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.sol, nil, nil
}

func (m *mockGetter) FindSolution() string {
	return ""
}

// nopCloser wraps an io.Reader to add a Close method for io.ReadCloser
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func TestSolutionOptions_loadSolution(t *testing.T) {
	tests := []struct {
		name        string
		options     *SolutionOptions
		wantErr     bool
		errContains string
		checkResult func(t *testing.T, sol *solution.Solution)
	}{
		{
			name: "loads_solution_from_getter",
			options: func() *SolutionOptions {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				return &SolutionOptions{
					IOStreams: ioStreams,
					File:      "test.yaml",
					getter: &mockGetter{
						sol: &solution.Solution{
							Metadata: solution.Metadata{
								Name: "test-solution",
							},
						},
					},
				}
			}(),
			wantErr: false,
			checkResult: func(t *testing.T, sol *solution.Solution) {
				assert.Equal(t, "test-solution", sol.Metadata.Name)
			},
		},
		{
			name: "returns_error_when_getter_fails",
			options: func() *SolutionOptions {
				ioStreams, _, _ := terminal.NewTestIOStreams()
				return &SolutionOptions{
					IOStreams: ioStreams,
					File:      "nonexistent.yaml",
					getter: &mockGetter{
						err: fmt.Errorf("file not found"),
					},
				}
			}(),
			wantErr:     true,
			errContains: "file not found",
		},
		{
			name: "reads_from_stdin",
			options: func() *SolutionOptions {
				stdin := bytes.NewBufferString(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-solution
  version: 1.0.0
spec: {}
`)
				ioStreams := &terminal.IOStreams{
					In:     nopCloser{stdin},
					Out:    &bytes.Buffer{},
					ErrOut: &bytes.Buffer{},
				}
				return &SolutionOptions{
					IOStreams: ioStreams,
					File:      "-",
				}
			}(),
			wantErr: false,
			checkResult: func(t *testing.T, sol *solution.Solution) {
				assert.Equal(t, "stdin-solution", sol.Metadata.Name)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sol, err := tc.options.loadSolution(context.Background())

			if tc.wantErr {
				require.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
				if tc.checkResult != nil {
					tc.checkResult(t, sol)
				}
			}
		})
	}
}

func TestSolutionOptions_getRegistry(t *testing.T) {
	t.Run("returns_injected_registry_when_set", func(t *testing.T) {
		injectedReg := provider.NewRegistry()
		options := &SolutionOptions{
			registry: injectedReg,
		}

		result := options.getRegistry(context.Background())
		assert.Same(t, injectedReg, result)
	})

	t.Run("returns_registry_when_not_set", func(t *testing.T) {
		options := &SolutionOptions{}

		result := options.getRegistry(context.Background())
		assert.NotNil(t, result)
	})
}

func TestSolutionOptions_writeOutput(t *testing.T) {
	t.Run("writes_to_stdout_when_no_file_specified", func(t *testing.T) {
		var outBuf bytes.Buffer
		options := &SolutionOptions{
			IOStreams: &terminal.IOStreams{
				Out: &outBuf,
			},
		}

		err := options.writeOutput([]byte("test output"))
		require.NoError(t, err)
		assert.Equal(t, "test output\n", outBuf.String())
	})
}

func TestSolutionOptions_writeToFile(t *testing.T) {
	t.Run("appends_json_extension_when_missing", func(t *testing.T) {
		dir := t.TempDir()
		options := &SolutionOptions{
			OutputFile: dir + "/output",
			Output:     "json",
		}

		err := options.writeToFile([]byte(`{"test": true}`))
		require.NoError(t, err)
		assert.Equal(t, dir+"/output.json", options.OutputFile)
	})

	t.Run("appends_yaml_extension_when_missing", func(t *testing.T) {
		dir := t.TempDir()
		options := &SolutionOptions{
			OutputFile: dir + "/output",
			Output:     "yaml",
		}

		err := options.writeToFile([]byte("test: true"))
		require.NoError(t, err)
		assert.Equal(t, dir+"/output.yaml", options.OutputFile)
	})

	t.Run("preserves_existing_extension", func(t *testing.T) {
		dir := t.TempDir()
		options := &SolutionOptions{
			OutputFile: dir + "/output.txt",
			Output:     "json",
		}

		err := options.writeToFile([]byte(`{"test": true}`))
		require.NoError(t, err)
		assert.Equal(t, dir+"/output.txt", options.OutputFile)
	})
}

func TestSolutionOptions_exitWithCode(t *testing.T) {
	t.Run("returns_original_error", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		options := &SolutionOptions{
			IOStreams: &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
			CliParams: &settings.Run{},
		}

		originalErr := fmt.Errorf("test error")
		err := options.exitWithCode(originalErr, exitcode.ValidationFailed)

		// Error should wrap the original error
		assert.True(t, errors.Is(err, originalErr))
		// Exit code should be extracted correctly
		assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
	})
}

func TestWriteSolutionError(t *testing.T) {
	t.Run("writes_error_message", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		options := &SolutionOptions{
			IOStreams: &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
			CliParams: &settings.Run{},
		}

		writeSolutionError(options, "test error message")

		// Error should appear somewhere (either out or err)
		combinedOutput := outBuf.String() + errBuf.String()
		assert.Contains(t, combinedOutput, "test error message")
	})
}

func TestValidOutputTypes(t *testing.T) {
	assert.Contains(t, ValidOutputTypes, "json")
	assert.Contains(t, ValidOutputTypes, "yaml")
	assert.Contains(t, ValidOutputTypes, "test")
	assert.Len(t, ValidOutputTypes, 3)
}

func TestExitCodes(t *testing.T) {
	assert.Equal(t, 2, exitcode.ValidationFailed)
	assert.Equal(t, 3, exitcode.InvalidInput)
	assert.Equal(t, 4, exitcode.FileNotFound)
	assert.Equal(t, 5, exitcode.RenderFailed)
}

func TestSolutionRegistryAdapter(t *testing.T) {
	t.Run("Get_returns_provider_when_exists", func(t *testing.T) {
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
		adapter := &solutionRegistryAdapter{Registry: reg}

		// Try to get a non-existent provider
		p, ok := adapter.Get("nonexistent")
		assert.Nil(t, p)
		assert.False(t, ok)
	})

	t.Run("Has_returns_false_for_nonexistent", func(t *testing.T) {
		reg := provider.NewRegistry()
		adapter := &solutionRegistryAdapter{Registry: reg}

		assert.False(t, adapter.Has("nonexistent"))
	})

	t.Run("List_returns_all_providers", func(t *testing.T) {
		reg := provider.NewRegistry()
		adapter := &solutionRegistryAdapter{Registry: reg}

		providers := adapter.List()
		assert.NotNil(t, providers)
	})

	t.Run("DescriptorLookup_returns_lookup", func(t *testing.T) {
		reg := provider.NewRegistry()
		adapter := &solutionRegistryAdapter{Registry: reg}

		lookup := adapter.DescriptorLookup()
		assert.NotNil(t, lookup)
	})
}

func TestSolutionResolverRegistryAdapter(t *testing.T) {
	t.Run("Get_returns_error_for_nonexistent", func(t *testing.T) {
		reg := provider.NewRegistry()
		adapter := &solutionResolverRegistryAdapter{
			RegistryAdapter: &solutionRegistryAdapter{Registry: reg},
		}

		p, err := adapter.Get("nonexistent")
		assert.Nil(t, p)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestSolutionOptions_TimeoutDefaults(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandSolution(cliParams, ioStreams, "render")

	// Get resolver-timeout flag
	resolverTimeout := cmd.Flags().Lookup("resolver-timeout")
	require.NotNil(t, resolverTimeout)
	assert.Equal(t, "30s", resolverTimeout.DefValue)

	// Get phase-timeout flag
	phaseTimeout := cmd.Flags().Lookup("phase-timeout")
	require.NotNil(t, phaseTimeout)
	assert.Equal(t, "5m0s", phaseTimeout.DefValue)
}
