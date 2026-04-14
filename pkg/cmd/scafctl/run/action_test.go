// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
}

func TestParseActionArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		fileExplicit bool
		initialFile  string
		wantNames    []string
		wantDynamic  []string
		wantFile     string
	}{
		{
			name:      "bare words are action names",
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
			name:      "URL detected as file when no -f",
			args:      []string{"https://example.com/solution.yaml", "lint"},
			wantFile:  "https://example.com/solution.yaml",
			wantNames: []string{"lint"},
		},
		{
			name:         "URL ignored when -f is set",
			args:         []string{"https://example.com/solution.yaml"},
			fileExplicit: true,
			initialFile:  "other.yaml",
			wantNames:    []string{"https://example.com/solution.yaml"},
		},
		{
			name:        "mixed args",
			args:        []string{"lint", "test", "env=prod", "@config.yaml"},
			wantNames:   []string{"lint", "test"},
			wantDynamic: []string{"env=prod", "@config.yaml"},
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
			if tc.initialFile != "" {
				opts.File = tc.initialFile
			}

			parseActionArgs(tc.args, opts, tc.fileExplicit)

			assert.Equal(t, tc.wantNames, opts.Names)
			assert.Equal(t, tc.wantDynamic, opts.DynamicArgs)
			if tc.wantFile != "" {
				assert.Equal(t, tc.wantFile, opts.File)
			}
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
