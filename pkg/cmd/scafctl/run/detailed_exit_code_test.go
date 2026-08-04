// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detailedExitPartialSolution ends in partial success: the "bad" action fails
// (undefined template function) but is tolerated via continueOnError, so the run
// completes without a hard failure. The actions are serialized via dependsOn so
// the in-process test IOStreams (a plain bytes.Buffer) are never written
// concurrently by the same-phase progress callback (which would race under
// -race; the real binary writes to os.Stderr and is unaffected).
const detailedExitPartialSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: detailed-exit-partial-unit
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      good:
        provider: message
        inputs:
          message: "GOOD_RAN"
          type: info
      bad:
        provider: go-template
        dependsOn: [good]
        continueOnError: true
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: hello
`

// registryForDetailedExit returns a registry with the providers the
// partial-success fixture needs (static for resolvers, message and go-template
// for the two actions).
func registryForDetailedExit(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))
	require.NoError(t, reg.Register(gotmplprovider.NewGoTemplateProvider()))
	return reg
}

// TestSolutionOptions_Run_DetailedExitCode drives SolutionOptions.Run() to the
// partial-success branch in-process so the exit-code policy (and its coverage)
// is exercised without a subprocess. Integration tests run the compiled binary,
// which the per-package coverage run does not measure.
func TestSolutionOptions_Run_DetailedExitCode(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(detailedExitPartialSolution), 0o600))

	run := func(t *testing.T, detailed bool) (string, error) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		opts := &SolutionOptions{DetailedExitCode: detailed, ActionTimeout: settings.DefaultActionTimeout}
		opts.IOStreams = streams
		opts.CliParams = cliParams
		opts.File = solutionPath
		opts.Output = "json"
		opts.registry = registryForDetailedExit(t)

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		runErr := opts.Run(ctx)
		return stdout.String(), runErr
	}

	t.Run("flag on returns PartialSuccess and prints stdout envelope", func(t *testing.T) {
		t.Parallel()
		stdout, err := run(t, true)
		require.Error(t, err)
		assert.Equal(t, exitcode.PartialSuccess, exitcode.GetCode(err),
			"partial success with --detailed-exit-code must exit 12")
		assert.Contains(t, stdout, "partial-success",
			"the full action envelope must still be written to stdout before the non-zero exit")
	})

	t.Run("flag off returns Success (non-breaking)", func(t *testing.T) {
		t.Parallel()
		_, err := run(t, false)
		require.NoError(t, err, "partial success without the flag must exit 0")
	})

	t.Run("flag on surfaces a writeActionOutput error before the exit code", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		opts := &SolutionOptions{DetailedExitCode: true, ActionTimeout: settings.DefaultActionTimeout}
		opts.IOStreams = streams
		opts.CliParams = cliParams
		opts.File = solutionPath
		opts.Output = "unsupported-format" // makes writeActionOutput fail on the partial branch
		opts.registry = registryForDetailedExit(t)

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format",
			"a write failure on the partial branch must be surfaced, not masked by the exit code")
	})
}

// TestActionOptions_Run_DetailedExitCode is the run action counterpart -- the
// two call sites are independent copies and must both be covered.
func TestActionOptions_Run_DetailedExitCode(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(detailedExitPartialSolution), 0o600))

	run := func(t *testing.T, detailed bool) error {
		t.Helper()
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		opts := &ActionOptions{DetailedExitCode: detailed, ActionTimeout: settings.DefaultActionTimeout}
		opts.IOStreams = streams
		opts.CliParams = cliParams
		opts.File = solutionPath
		opts.Output = "json"
		opts.registry = registryForDetailedExit(t)

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		return opts.Run(ctx)
	}

	t.Run("flag on returns PartialSuccess", func(t *testing.T) {
		t.Parallel()
		err := run(t, true)
		require.Error(t, err)
		assert.Equal(t, exitcode.PartialSuccess, exitcode.GetCode(err),
			"partial success with --detailed-exit-code must exit 12")
	})

	t.Run("flag off returns Success (non-breaking)", func(t *testing.T) {
		t.Parallel()
		err := run(t, false)
		require.NoError(t, err, "partial success without the flag must exit 0")
	})

	t.Run("flag on surfaces a writeActionOutput error before the exit code", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		opts := &ActionOptions{DetailedExitCode: true, ActionTimeout: settings.DefaultActionTimeout}
		opts.IOStreams = streams
		opts.CliParams = cliParams
		opts.File = solutionPath
		opts.Output = "unsupported-format" // makes writeActionOutput fail on the partial branch
		opts.registry = registryForDetailedExit(t)

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format",
			"a write failure on the partial branch must be surfaced, not masked by the exit code")
	})
}
