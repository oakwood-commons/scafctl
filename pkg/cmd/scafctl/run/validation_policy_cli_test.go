// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solutionValidationFailWithWorkflow has a resolver that produces a usable value
// but fails a validation rule, plus a workflow action that echoes a produced
// value. It exercises the warn-vs-fatal branch in `run solution` / `run action`:
// under a fatal policy the workflow never runs; under warn it does.
const solutionValidationFailWithWorkflow = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: validation-policy-workflow
  version: 1.0.0
spec:
  resolvers:
    good:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "i-resolved"
    bad:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "x"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^longer-than-one$"
              message: "bad must be longer"
  workflow:
    actions:
      echo:
        provider: static
        inputs:
          value:
            expr: '_.good'
`

func testRegistryWithValidationAndWorkflow(t *testing.T) *provider.Registry {
	t.Helper()
	// The static + validation providers are sufficient for this fixture; reuse
	// the shared helper to avoid drift.
	return testRegistryWithValidation(t)
}

func newSolutionOptionsForPolicy(t *testing.T, solutionPath string, stdout, stderr *bytes.Buffer, format string) *SolutionOptions {
	t.Helper()
	streams := &terminal.IOStreams{In: nil, Out: stdout, ErrOut: stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	return &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:               streams,
			CliParams:               cliParams,
			File:                    solutionPath,
			KvxOutputFlags:          flags.KvxOutputFlags{Output: format},
			ResolverTimeout:         30 * time.Second,
			PhaseTimeout:            5 * time.Minute,
			registry:                testRegistryWithValidationAndWorkflow(t),
			ValidationPolicyDefault: settings.ValidationError,
		},
		ActionTimeout: 30 * time.Second,
	}
}

// TestSolutionOptions_Run_ValidationPolicy_ErrorIsFatal verifies that the
// default fatal policy for `run solution` aborts before the workflow runs and
// emits a structured failure envelope that still carries the resolved values.
func TestSolutionOptions_Run_ValidationPolicy_ErrorIsFatal(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionValidationFailWithWorkflow), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newSolutionOptionsForPolicy(t, solutionPath, &stdout, &stderr, "json")

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
	err := opts.Run(ctx)

	require.Error(t, err, "fatal policy must exit non-zero on validation failure")

	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, execute.StatusFailed, decoded[execute.StatusFieldKey],
		"fatal envelope must carry the failed status")
	assert.NotEmpty(t, decoded[execute.DiagnosticsFieldKey],
		"fatal envelope must carry diagnostics")

	resolvers, ok := decoded[execute.ResolversFieldKey].(map[string]any)
	require.True(t, ok, "fatal envelope must embed the partial resolvers map")
	assert.Equal(t, "i-resolved", resolvers["good"],
		"successfully resolved values must be preserved in the failure envelope")
}

// TestSolutionOptions_Run_ValidationPolicy_WarnContinuesToWorkflow verifies that
// the warn policy softens a validation-only failure: the workflow still runs,
// the command exits zero, and the output carries diagnostics + resolvers.
func TestSolutionOptions_Run_ValidationPolicy_WarnContinuesToWorkflow(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionValidationFailWithWorkflow), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newSolutionOptionsForPolicy(t, solutionPath, &stdout, &stderr, "json")
	opts.OnValidationError = "warn"

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
	err := opts.Run(ctx)

	require.NoError(t, err, "warn policy must not abort on a validation-only failure")

	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.NotEmpty(t, decoded[execute.DiagnosticsFieldKey],
		"warn output must surface the validation diagnostics")
	resolvers, ok := decoded[execute.ResolversFieldKey].(map[string]any)
	require.True(t, ok, "warn output must embed the resolvers map")
	assert.Equal(t, "i-resolved", resolvers["good"])
	assert.Contains(t, stderr.String(), "bad must be longer",
		"the validation diagnostic must still be reported on stderr")
}

func newActionOptionsForPolicy(t *testing.T, solutionPath string, stdout, stderr *bytes.Buffer, format string) *ActionOptions {
	t.Helper()
	streams := &terminal.IOStreams{In: nil, Out: stdout, ErrOut: stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	return &ActionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:               streams,
			CliParams:               cliParams,
			File:                    solutionPath,
			KvxOutputFlags:          flags.KvxOutputFlags{Output: format},
			ResolverTimeout:         30 * time.Second,
			PhaseTimeout:            5 * time.Minute,
			registry:                testRegistryWithValidationAndWorkflow(t),
			ValidationPolicyDefault: settings.ValidationError,
		},
		ActionTimeout: 30 * time.Second,
	}
}

// TestActionOptions_Run_ValidationPolicy_ErrorIsFatal verifies that `run action`
// defaults to a fatal validation policy: a validation-only failure aborts before
// any action runs and emits a structured failure envelope carrying the partial
// resolved values.
func TestActionOptions_Run_ValidationPolicy_ErrorIsFatal(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionValidationFailWithWorkflow), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newActionOptionsForPolicy(t, solutionPath, &stdout, &stderr, "json")

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
	err := opts.Run(ctx)

	require.Error(t, err, "run action must default to a fatal validation policy")

	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, execute.StatusFailed, decoded[execute.StatusFieldKey])
	resolvers, ok := decoded[execute.ResolversFieldKey].(map[string]any)
	require.True(t, ok, "fatal envelope must embed the partial resolvers map")
	assert.Equal(t, "i-resolved", resolvers["good"])
}

// TestActionOptions_Run_ValidationPolicy_WarnContinuesToWorkflow verifies parity
// with `run solution`: under warn, a validation-only failure is non-fatal, the
// action still runs, the command exits zero, and the output carries diagnostics
// plus the resolved values.
func TestActionOptions_Run_ValidationPolicy_WarnContinuesToWorkflow(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionValidationFailWithWorkflow), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newActionOptionsForPolicy(t, solutionPath, &stdout, &stderr, "json")
	opts.OnValidationError = "warn"

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
	err := opts.Run(ctx)

	require.NoError(t, err, "warn policy must not abort run action on a validation-only failure")

	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.NotEmpty(t, decoded[execute.DiagnosticsFieldKey],
		"warn output must surface the validation diagnostics")
	resolvers, ok := decoded[execute.ResolversFieldKey].(map[string]any)
	require.True(t, ok, "warn output must embed the resolvers map")
	assert.Equal(t, "i-resolved", resolvers["good"])
	assert.Contains(t, stderr.String(), "bad must be longer",
		"the validation diagnostic must still be reported on stderr")
}

// TestResolverOptions_Run_ValidationPolicy_IgnoreSkipsPhase verifies that
// `--on-validation-error ignore` skips the validation phase entirely for
// `run resolver`: values are emitted, the command exits zero, and NO validation
// diagnostics are produced (unlike warn, which reports them).
func TestResolverOptions_Run_ValidationPolicy_IgnoreSkipsPhase(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.OnValidationError = "ignore"

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	require.NoError(t, opts.Run(ctx), "ignore policy must not fail on a validation rule")

	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, "Bob", decoded["name"], "the resolved value must still be emitted")
	assert.NotContains(t, decoded, execute.StatusKey,
		"ignore must skip validation so no failure envelope is attached")
	assert.NotContains(t, stderr.String(), "name must be Alice",
		"ignore must not emit validation diagnostics")
}
