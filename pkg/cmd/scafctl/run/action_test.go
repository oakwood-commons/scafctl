// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandAction(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandAction(cliParams, streams, "")

	assert.Equal(t, "action [action-name...] [key=value...]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Contains(t, cmd.Aliases, "act")
	assert.Contains(t, cmd.Aliases, "a")
	assert.Contains(t, cmd.Aliases, "actions")

	// Verify flags exist
	flags := cmd.Flags()
	assert.NotNil(t, flags.Lookup("file"))
	assert.NotNil(t, flags.Lookup("resolver"))
	assert.NotNil(t, flags.Lookup("output"))
	assert.NotNil(t, flags.Lookup("action-timeout"))
	assert.NotNil(t, flags.Lookup("max-action-concurrency"))
	assert.NotNil(t, flags.Lookup("dry-run"))
	assert.NotNil(t, flags.Lookup("show-execution"))
	assert.NotNil(t, flags.Lookup("on-conflict"))
	assert.NotNil(t, flags.Lookup("force"))
	assert.NotNil(t, flags.Lookup("backup"))
	assert.NotNil(t, flags.Lookup("detailed-exit-code"))
}

func TestCommandAction_FlagDefaults(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandAction(cliParams, streams, "")
	flags := cmd.Flags()

	actionTimeout, err := flags.GetDuration("action-timeout")
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultActionTimeout, actionTimeout)

	maxConcurrency, err := flags.GetInt("max-action-concurrency")
	require.NoError(t, err)
	assert.Equal(t, 0, maxConcurrency)

	dryRun, err := flags.GetBool("dry-run")
	require.NoError(t, err)
	assert.False(t, dryRun)

	showExec, err := flags.GetBool("show-execution")
	require.NoError(t, err)
	assert.False(t, showExec)

	onConflict, err := flags.GetString("on-conflict")
	require.NoError(t, err)
	assert.Empty(t, onConflict)

	force, err := flags.GetBool("force")
	require.NoError(t, err)
	assert.False(t, force)

	backup, err := flags.GetBool("backup")
	require.NoError(t, err)
	assert.False(t, backup)

	noState, err := flags.GetBool("no-state")
	require.NoError(t, err)
	assert.False(t, noState)
}

func TestCommandAction_DetailedExitCodeFlag(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()

	t.Run("defaults to false when embedder default unset", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		cmd := CommandAction(cliParams, streams, "")
		v, err := cmd.Flags().GetBool("detailed-exit-code")
		require.NoError(t, err)
		assert.False(t, v)
	})

	t.Run("flag default reflects embedder default", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		cliParams.DetailedExitCode = true
		cmd := CommandAction(cliParams, streams, "")
		v, err := cmd.Flags().GetBool("detailed-exit-code")
		require.NoError(t, err)
		assert.True(t, v, "embedder default should seed the flag default")
	})
}

func TestParseActionArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantNames   []string
		wantDynamic []string
	}{
		{
			name:      "bare words are action names not catalog refs",
			args:      []string{"lint", "test"},
			wantNames: []string{"lint", "test"},
		},
		{
			name:        "key=value are dynamic args",
			args:        []string{"build", "version=1.0.0"},
			wantNames:   []string{"build"},
			wantDynamic: []string{"version=1.0.0"},
		},
		{
			name:        "@file args are dynamic args",
			args:        []string{"deploy", "@params.yaml"},
			wantNames:   []string{"deploy"},
			wantDynamic: []string{"@params.yaml"},
		},
		{
			name:      "URL stays a name",
			args:      []string{"https://example.com/solution.yaml", "lint"},
			wantNames: []string{"https://example.com/solution.yaml", "lint"},
		},
		{
			name:      "versioned ref stays a name",
			args:      []string{"my-app@1.0.0", "test"},
			wantNames: []string{"my-app@1.0.0", "test"},
		},
		{
			name:        "mixed args with bare words",
			args:        []string{"lint", "test", "env=prod"},
			wantNames:   []string{"lint", "test"},
			wantDynamic: []string{"env=prod"},
		},
		{
			name: "no args",
			args: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := &ActionOptions{}
			parseActionArgs(tc.args, opts)

			assert.Equal(t, tc.wantNames, opts.Names)
			assert.Equal(t, tc.wantDynamic, opts.DynamicArgs)
			assert.Empty(t, opts.File)
		})
	}
}

func TestActionOptions_getEffectiveActionConfig_Defaults(t *testing.T) {
	t.Parallel()

	opts := &ActionOptions{}
	opts.ActionTimeout = 30 * time.Second
	opts.MaxActionConcurrency = 4

	ctx := context.Background()
	result := opts.getEffectiveActionConfig(ctx)

	assert.Equal(t, 30*time.Second, result.DefaultTimeout)
	assert.Equal(t, 4, result.MaxConcurrency)
	assert.Equal(t, settings.DefaultGracePeriod, result.GracePeriod)
}

func TestActionOptions_getEffectiveActionConfig_NilConfig(t *testing.T) {
	t.Parallel()

	opts := &ActionOptions{}
	opts.ActionTimeout = 45 * time.Second
	opts.MaxActionConcurrency = 2

	// No config in context
	ctx := context.Background()
	result := opts.getEffectiveActionConfig(ctx)

	assert.Equal(t, 45*time.Second, result.DefaultTimeout)
	assert.Equal(t, 2, result.MaxConcurrency)
}

func TestActionOptions_getEffectiveActionConfig_WithConfig(t *testing.T) {
	t.Parallel()

	t.Run("config overrides when flags not set", func(t *testing.T) {
		t.Parallel()

		appCfg := &config.Config{
			Action: config.ActionConfig{
				DefaultTimeout: "60s",
				MaxConcurrency: 8,
				OutputDir:      "/from/config",
			},
		}
		ctx := config.WithConfig(context.Background(), appCfg)

		opts := &ActionOptions{}
		opts.ActionTimeout = settings.DefaultActionTimeout
		opts.flagsChanged = map[string]bool{}

		result := opts.getEffectiveActionConfig(ctx)
		assert.Equal(t, 60*time.Second, result.DefaultTimeout)
		assert.Equal(t, 8, result.MaxConcurrency)
		assert.Equal(t, "/from/config", result.OutputDir)
	})

	t.Run("CLI flags take precedence over config", func(t *testing.T) {
		t.Parallel()

		appCfg := &config.Config{
			Action: config.ActionConfig{
				DefaultTimeout: "60s",
				MaxConcurrency: 8,
				OutputDir:      "/from/config",
			},
		}
		ctx := config.WithConfig(context.Background(), appCfg)

		opts := &ActionOptions{}
		opts.ActionTimeout = 90 * time.Second
		opts.MaxActionConcurrency = 16
		opts.OutputDir = "/from/flag"
		opts.flagsChanged = map[string]bool{
			"action-timeout":         true,
			"max-action-concurrency": true,
			"output-dir":             true,
		}

		result := opts.getEffectiveActionConfig(ctx)
		assert.Equal(t, 90*time.Second, result.DefaultTimeout)
		assert.Equal(t, 16, result.MaxConcurrency)
		assert.Empty(t, result.OutputDir)
	})
}

func TestActionOptions_exitWithCode(t *testing.T) {
	t.Parallel()

	t.Run("writes error to writer", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		streams := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
		cliParams := settings.NewCliParams()
		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		opts := &ActionOptions{}
		err := opts.exitWithCode(ctx, assert.AnError, 42)
		assert.Error(t, err)
		assert.Contains(t, buf.String(), assert.AnError.Error())
	})

	t.Run("nil writer does not panic", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		opts := &ActionOptions{}
		err := opts.exitWithCode(ctx, assert.AnError, 1)
		assert.Error(t, err)
	})
}

func TestActionOptions_getActionIOStreams(t *testing.T) {
	t.Parallel()

	t.Run("nil IOStreams returns nil", func(t *testing.T) {
		t.Parallel()

		opts := &ActionOptions{}
		assert.Nil(t, opts.getActionIOStreams())
	})

	t.Run("default output returns provider IOStreams", func(t *testing.T) {
		t.Parallel()

		var stdout, stderr bytes.Buffer
		opts := &ActionOptions{}
		opts.IOStreams = &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		result := opts.getActionIOStreams()
		require.NotNil(t, result)
		assert.NotNil(t, result.Out)
		assert.NotNil(t, result.ErrOut)
	})

	t.Run("structured output suppresses IOStreams", func(t *testing.T) {
		t.Parallel()

		for _, format := range []string{"json", "yaml", "quiet", "test"} {
			var stdout, stderr bytes.Buffer
			opts := &ActionOptions{}
			opts.IOStreams = &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
			opts.Output = format

			assert.Nil(t, opts.getActionIOStreams(), "output=%s should suppress IOStreams", format)
		}
	})
}

func TestActionOptions_Run_InvalidOnConflict(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "./testdata/basic.yaml"
	opts.OnConflict = "invalid-strategy"

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --on-conflict value")
}

func TestActionOptions_Run_ForceOverridesOnConflict(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "/nonexistent/solution.yaml"
	opts.Force = true

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	// Will fail for missing file, but Force should have set OnConflict
	_ = opts.Run(ctx)
	assert.Equal(t, "skip-unchanged", opts.OnConflict)
}

func TestActionOptions_Run_NoFile(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = ""

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestActionOptions_Run_FileNotFound(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "/nonexistent/solution.yaml"

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestActionOptions_Run_EmptySolution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-solution
  version: 1.0.0
spec:
  resolvers: {}
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = solutionPath
	opts.registry = testRegistry()

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestActionOptions_Run_StdinConflict(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     io.NopCloser(&bytes.Buffer{}),
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "-"
	opts.ResolverParams = []string{"@-"}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both -f - and @-")
}

func TestActionOptions_Run_DefaultBinaryName(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{
		BinaryName: "", // empty, should be set to default
	}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "/nonexistent/solution.yaml"

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	_ = opts.Run(ctx)
	assert.Equal(t, settings.CliBinaryName, opts.BinaryName)
}

func TestActionOptions_Run_CustomBinaryName(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{
		BinaryName: "mycli",
	}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = "/nonexistent/solution.yaml"

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	_ = opts.Run(ctx)
	assert.Equal(t, "mycli", opts.BinaryName)
}

func TestCommandAction_BinaryNameSubstitution(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	streams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandAction(cliParams, streams, "")
	assert.Contains(t, cmd.Long, "mycli")
	assert.NotContains(t, cmd.Long, settings.CliBinaryName)
}

func TestActionOptions_Run_NilWriterGuard(t *testing.T) {
	t.Parallel()
	// When a writer is present in context the nil-guard in Run installs the
	// ActionProgressCallback. This test exercises the truthy branch of that guard
	// (the falsy/nil branch is exercised by all other Run tests that use a plain
	// context.Background() with no writer).
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	w := writer.New(streams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	opts := &ActionOptions{}
	opts.File = "/nonexistent/solution.yaml"
	opts.IOStreams = streams
	opts.CliParams = cliParams

	// Run will fail early (file not found), but the nil-guard is evaluated
	// before the file is loaded, so the covered lines are hit.
	_ = opts.Run(ctx)
}

func newCountingFailResolverProvider(calls *atomic.Int32) provider.Provider {
	return &provider.MockProvider{
		Name: "broken-from",
		ExecuteFunc: func() (*provider.Output, error) {
			calls.Add(1)
			return nil, assert.AnError
		},
	}
}

func actionScopedTestRegistry(extra ...provider.Provider) *provider.Registry {
	base := make([]provider.Provider, 0, 5+len(extra))
	base = append(base,
		celprovider.NewCelProvider(),
		fileprovider.NewFileProvider(),
		gotmplprovider.NewGoTemplateProvider(),
		messageprovider.NewMessageProvider(),
		parameterprovider.NewParameterProvider(),
	)
	base = append(base, extra...)
	return testRegistry(base...)
}

func TestActionOptions_Run_ScopesResolversAndResolveAll(t *testing.T) {
	t.Parallel()

	solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: scoped-action-test
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev
    unusedParam:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: deployRegion
          - provider: static
            inputs:
              value: us-east-1
    allowedEnvs:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - dev
                - prod
    checkedEnv:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "_.env"
      validate:
        with:
          - provider: cel
            inputs:
              expression: "__self in _.allowedEnvs"
    broken:
      resolve:
        with:
          - provider: broken-from
    finallyMsg:
      resolve:
        with:
          - provider: static
            inputs:
              value: done
  workflow:
    actions:
      clean:
        provider: message
        inputs:
          message: "rm -rf dist"
      verify:
        provider: message
        inputs:
          message:
            rslvr: checkedEnv
      deploy:
        provider: message
        inputs:
          message:
            rslvr: broken
    finally:
      cleanup:
        provider: message
        inputs:
          message:
            rslvr: finallyMsg
`

	t.Run("zero-direct-ref action skips broken resolver and runs only finally resolver", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o600))

		// Shared params file includes deployRegion consumed only by unselected resolver.
		paramsPath := filepath.Join(tmpDir, "params.yaml")
		require.NoError(t, os.WriteFile(paramsPath, []byte("env: dev\ndeployRegion: eu-west-1\n"), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			BinaryName:    "mycli",
			Names:         []string{"clean"},
			DynamicArgs:   []string{"@" + paramsPath},
			ActionTimeout: 10 * time.Second,
			ShowExecution: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load(), "unrelated broken resolver must not execute")

		var out map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &out))
		execMeta, ok := out["__execution"].(map[string]any)
		require.True(t, ok)
		resolversMeta, ok := execMeta["resolvers"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, resolversMeta, "finallyMsg")
		assert.NotContains(t, resolversMeta, "broken")
		assert.NotContains(t, resolversMeta, "env")
	})

	t.Run("cross-resolver validate dependency is pulled in when selected", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			Names:         []string{"verify"},
			ActionTimeout: 10 * time.Second,
			ShowExecution: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load())

		var out map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &out))
		execMeta := out["__execution"].(map[string]any)
		resolversMeta := execMeta["resolvers"].(map[string]any)
		assert.Contains(t, resolversMeta, "env")
		assert.Contains(t, resolversMeta, "allowedEnvs")
		assert.Contains(t, resolversMeta, "checkedEnv")
		assert.Contains(t, resolversMeta, "finallyMsg")
		assert.NotContains(t, resolversMeta, "broken")
	})

	t.Run("resolve-all executes broken resolver and fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			Names:         []string{"clean"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				ResolveAll:      true,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Equal(t, int32(1), brokenCalls.Load(), "--resolve-all must execute all resolvers")
	})

	t.Run("dry-run scopes resolvers to selected actions", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			Names:         []string{"clean"},
			ActionTimeout: 10 * time.Second,
			DryRun:        true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load(), "--dry-run must skip unrelated resolvers")
	})

	t.Run("positional URL source with no action names does not scope out resolvers", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: url-bare-action
  version: 1.0.0
spec:
  resolvers:
    onlyResolver:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      clean:
        provider: message
        inputs:
          message: "echo hi"
`))
		}))
		defer srv.Close()

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		// URL passed in o.Names (as parseActionArgs does); loader.Select consumes it,
		// leaving sel.Args empty -> bare run action executes all resolvers.
		opts := &ActionOptions{
			Names:         []string{srv.URL + "/solution.yaml"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		allowPriv := true
		appCfg := &config.Config{
			HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allowPriv},
		}
		w := writer.New(streams, cliParams)
		ctx := config.WithConfig(
			writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w),
			appCfg,
		)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Equal(t, int32(1), brokenCalls.Load(), "bare positional URL run must execute all resolvers")
	})

	t.Run("checkMissingLocks still guards unselected immutable resolvers on lock-less state replay", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		stateSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: immutable-lock-guard
  version: 1.0.0
spec:
  resolvers:
    appParam:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: app
          - provider: static
            inputs:
              value: demo
    clusterID:
      immutable: true
      resolve:
        with:
          - provider: static
            inputs:
              value: cluster-001
  workflow:
    actions:
      clean:
        provider: message
        inputs:
          message: "echo clean"
`
		require.NoError(t, os.WriteFile(solPath, []byte(stateSol), 0o600))

		intentPath := filepath.Join(tmpDir, "intent.json")
		intentJSON := `{"schemaVersion": 2, "parameters": {"app": "from-intent"}}`
		require.NoError(t, os.WriteFile(intentPath, []byte(intentJSON), 0o600))

		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			Names:         []string{"clean"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				StateFile:       intentPath,
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry:        actionScopedTestRegistry(),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--allow-missing-locks")
	})

	t.Run("disabled state block does not force state.save resolvers to run", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		disabledStateSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: disabled-state-scoped
  version: 1.0.0
state:
  enabled: false
  save:
    - provider: file
      inputs:
        path:
          rslvr: broken
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      clean:
        provider: message
        inputs:
          message: "echo clean"
`
		require.NoError(t, os.WriteFile(solPath, []byte(disabledStateSol), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &ActionOptions{
			Names:         []string{"clean"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load(), "disabled state save target must not execute broken resolver")
	})

	t.Run("executeResolvers preserves seeded results when scoped slice is empty", func(t *testing.T) {
		t.Parallel()
		shared := &sharedResolverOptions{CliParams: settings.NewCliParams()}
		seed := map[string]*resolver.ExecutionResult{
			"phaseARoot": {Status: resolver.ExecutionStatusSuccess, Value: "seeded-ok"},
			"partialVal": {Status: resolver.ExecutionStatusFailed, Value: "partial"},
			"nilEntry":   nil,
		}
		ctx := logger.WithLogger(context.Background(), logger.Get(0))
		data, rctx, err := shared.executeResolvers(ctx, &solution.Solution{}, []*resolver.Resolver{}, nil, nil, withSeededResults(seed))
		require.NoError(t, err)
		assert.Equal(t, "seeded-ok", data["phaseARoot"])
		assert.Equal(t, "partial", data["partialVal"])
		res, ok := rctx.GetResult("phaseARoot")
		require.True(t, ok)
		assert.Equal(t, "seeded-ok", res.Value)
	})

	t.Run("scoped execution handles render-tree entries, tmpl wrapper vs data, and inert message literals", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		regressionSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: template-and-literal-scoping
  version: 1.0.0
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                app: demo
    host:
      resolve:
        with:
          - provider: static
            inputs:
              value: api.example.com
    broken:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      inert-msg:
        provider: message
        inputs:
          message: "literal {{ .broken }} and _.broken"
      wrapped-tmpl:
        provider: go-template
        inputs:
          name: wrapped
          data:
            host: local-override
          template:
            tmpl: "resolved={{ .host }}"
      render-tree:
        provider: go-template
        inputs:
          operation: render-tree
          data:
            rslvr: config
          entries:
            - path: a.txt
              content: "app={{ .app }} host={{ ._.host }}"
`
		require.NoError(t, os.WriteFile(solPath, []byte(regressionSol), 0o600))

		for _, actionName := range []string{"inert-msg", "wrapped-tmpl", "render-tree"} {
			var brokenCalls atomic.Int32
			var stdout, stderr bytes.Buffer
			cliParams := settings.NewCliParams()
			cliParams.ExitOnError = false
			streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

			opts := &ActionOptions{
				Names:         []string{actionName},
				ActionTimeout: 10 * time.Second,
				ShowExecution: true,
				sharedResolverOptions: sharedResolverOptions{
					IOStreams:       streams,
					CliParams:       cliParams,
					File:            solPath,
					KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
					ResolverTimeout: 10 * time.Second,
					PhaseTimeout:    30 * time.Second,
					registry: actionScopedTestRegistry(
						newCountingFailResolverProvider(&brokenCalls),
					),
				},
			}

			w := writer.New(streams, cliParams)
			ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
			require.NoError(t, opts.Run(ctx), "action %s failed: %s", actionName, stderr.String())
			assert.Zero(t, brokenCalls.Load(), "action %s must not run broken resolver", actionName)
		}
	})

	t.Run("whole-namespace template access retains all resolvers while validation ignore and render unused entries skip broken resolvers", func(t *testing.T) {
		t.Parallel()
		wholePath := filepath.Join(t.TempDir(), "whole-ns.yaml")
		wholeSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: whole-ns
  version: 1.0.0
spec:
  resolvers:
    alpha:
      resolve:
        with:
          - provider: static
            inputs:
              value: one
    beta:
      resolve:
        with:
          - provider: static
            inputs:
              value: two
  workflow:
    actions:
      dump:
        provider: message
        inputs:
          message:
            tmpl: '{{ printf "%v" ._ }}'
`
		require.NoError(t, os.WriteFile(wholePath, []byte(wholeSol), 0o600))

		var wholeStdout, wholeStderr bytes.Buffer
		wholeParams := settings.NewCliParams()
		wholeParams.ExitOnError = false
		wholeStreams := &terminal.IOStreams{Out: &wholeStdout, ErrOut: &wholeStderr}
		wholeOpts := &ActionOptions{
			Names:         []string{"dump"},
			ActionTimeout: 10 * time.Second,
			ShowExecution: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       wholeStreams,
				CliParams:       wholeParams,
				File:            wholePath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry:        actionScopedTestRegistry(),
			},
		}
		wholeCtx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(wholeStreams, wholeParams))
		require.NoError(t, wholeOpts.Run(wholeCtx), wholeStderr.String())
		var wholePayload map[string]any
		require.NoError(t, json.Unmarshal(wholeStdout.Bytes(), &wholePayload))
		execMeta, _ := wholePayload["__execution"].(map[string]any)
		summary, _ := execMeta["summary"].(map[string]any)
		assert.Equal(t, float64(2), summary["resolverCount"])

		skipValPath := filepath.Join(t.TempDir(), "skip-val.yaml")
		skipValSol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: skip-val
  version: 1.0.0
spec:
  resolvers:
    brokenValDep:
      resolve:
        with:
          - provider: broken-from
    target:
      resolve:
        with:
          - provider: static
            inputs:
              value: ok
      validate:
        with:
          - provider: cel
            inputs:
              expression:
                expr: '__self != _.brokenValDep'
  workflow:
    actions:
      render-only:
        provider: go-template
        inputs:
          name: out
          template: "{{ ._.target }}"
          entries:
            - path: unused.txt
              content: "{{ ._.brokenValDep }}"
`
		require.NoError(t, os.WriteFile(skipValPath, []byte(skipValSol), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		opts := &ActionOptions{
			Names:         []string{"render-only"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:         streams,
				CliParams:         cliParams,
				File:              skipValPath,
				OnValidationError: "ignore",
				KvxOutputFlags:    flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout:   10 * time.Second,
				PhaseTimeout:      30 * time.Second,
				registry:          actionScopedTestRegistry(newCountingFailResolverProvider(&brokenCalls)),
			},
		}
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		require.NoError(t, opts.Run(ctx), stderr.String())
		assert.Zero(t, brokenCalls.Load())
	})
}
