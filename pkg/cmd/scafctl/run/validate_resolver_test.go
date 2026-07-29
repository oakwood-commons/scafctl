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
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistryWithValidation returns a registry with the static and validation
// providers, used for exercising validation-failure behavior.
func testRegistryWithValidation(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(validationprovider.NewValidationProvider()))
	return reg
}

const failingValidationSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: validation-exit-test
  version: 1.0.0
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^Alice$"
              message: "name must be Alice"
`

// dependentOfFailingValidationSolution has a resolver (greeting) that depends on
// a resolver (name) which fails its validation rule. Under `run resolver` the
// dependent must still execute and read the produced value.
const dependentOfFailingValidationSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: validation-dependents-test
  version: 1.0.0
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^Alice$"
              message: "name must be Alice"
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: '"Hello " + _.name'
`

func newResolverOptions(t *testing.T, solutionPath string, stdout, stderr *bytes.Buffer) *ResolverOptions {
	t.Helper()
	streams := &terminal.IOStreams{In: nil, Out: stdout, ErrOut: stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	return &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:               streams,
			CliParams:               cliParams,
			File:                    solutionPath,
			KvxOutputFlags:          flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout:         30 * time.Second,
			PhaseTimeout:            5 * time.Minute,
			registry:                testRegistryWithValidation(t),
			ValidationPolicyDefault: settings.ValidationWarn,
		},
	}
}

func TestResolverOptions_Run_ValidationNonFatalByDefault(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.NoError(t, err, "validation failure must be non-fatal by default (exit 0)")
	assert.Contains(t, stdout.String(), "Bob", "resolved value must still be shown")
	assert.Contains(t, stderr.String(), "failed validation", "diagnostics must be rendered to stderr")
}

func TestResolverOptions_Run_ValidationFailureDoesNotSkipDependents(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(dependentOfFailingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.NoError(t, err, "validation failure must be non-fatal by default (exit 0)")
	assert.Contains(t, stdout.String(), "Bob", "the validation-failed resolver's value must still be shown")
	assert.Contains(t, stdout.String(), "Hello Bob",
		"a dependent must still execute and read the validation-failed resolver's value")
	assert.Contains(t, stderr.String(), "failed validation", "diagnostics must be rendered to stderr")
}

func TestResolverOptions_Run_OnValidationErrorExitsNonZero(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.OnValidationError = "error"

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.Error(t, err, "--on-validation-error error must produce an error on validation failure")
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
	assert.Contains(t, stdout.String(), "Bob", "resolved value must still be shown even when failing")
}

func TestCommandValidateResolver(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateResolver(cliParams, streams, "")

	assert.Equal(t, "resolver [resolver-name...] [key=value...]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	// The validate command exposes the shared --on-validation-error flag but
	// defaults to the fatal "error" policy (via ValidationPolicyDefault).
	assert.Nil(t, cmd.Flags().Lookup("fail-on-validation"))
	assert.NotNil(t, cmd.Flags().Lookup("on-validation-error"))
	assert.NotNil(t, cmd.Flags().Lookup("file"))
	assert.NotNil(t, cmd.Flags().Lookup("resolver"))
}

func TestCommandValidateResolver_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandValidateResolver(cliParams, streams, "")
	assert.Contains(t, cmd.Long, "mycli validate resolver", "help text must use the embedder binary name")
}

func TestCommandValidateResolver_HasStrictFlagAndLintGate(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateResolver(cliParams, streams, "")
	strict := cmd.Flags().Lookup("strict")
	require.NotNil(t, strict, "validate resolver must expose --strict")
	assert.Contains(t, strict.Usage, "lint warnings")
}

// cleanReferencedSolution has a resolver 'name' referenced by 'greeting', so
// there are no unused-resolver warnings and resolver validation passes.
const cleanReferencedSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: clean-referenced
  version: 1.0.0
spec:
  resolvers:
    name:
      type: string
      description: the subject's name
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
    greeting:
      description: a greeting using name
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: '"Hello " + _.name'
`

func TestResolverOptions_Run_LintGatePassesClean(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(cleanReferencedSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.LintAfterValidate = true

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	require.NoError(t, opts.Run(ctx), "clean solution must pass the lint gate")
}

func TestResolverOptions_Run_LintGateWarningFailsStrict(t *testing.T) {
	t.Parallel()

	// A lone unused resolver produces an unused-resolver warning while still
	// passing resolver validation.
	orphanSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: orphan-warn
  version: 1.0.0
spec:
  resolvers:
    orphan:
      type: string
      description: never referenced
      resolve:
        with:
          - provider: static
            inputs:
              value: "x"
`
	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(orphanSolution), 0o600))

	// Non-strict: warning does not fail the gate.
	var so1, se1 bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &so1, &se1)
	opts.LintAfterValidate = true
	require.NoError(t, opts.Run(ctx()), "warning must not fail the lint gate without --strict")

	// Strict: warning fails the gate.
	var so2, se2 bytes.Buffer
	strictOpts := newResolverOptions(t, solutionPath, &so2, &se2)
	strictOpts.LintAfterValidate = true
	strictOpts.Strict = true
	err := strictOpts.Run(ctx())
	require.Error(t, err, "warning must fail the lint gate under --strict")
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
}

// TestResolverOptions_Run_LintGateQuietSuppressesOutput verifies that -o quiet
// suppresses the lint gate's output entirely (header and findings) while still
// applying the pass/fail policy via the exit code.
func TestResolverOptions_Run_LintGateQuietSuppressesOutput(t *testing.T) {
	t.Parallel()

	orphanSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: orphan-quiet
  version: 1.0.0
spec:
  resolvers:
    orphan:
      type: string
      description: never referenced
      resolve:
        with:
          - provider: static
            inputs:
              value: "x"
`
	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(orphanSolution), 0o600))

	// Quiet, non-strict: no output, exit 0.
	var so1, se1 bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &so1, &se1)
	opts.LintAfterValidate = true
	opts.Output = "quiet"
	require.NoError(t, opts.Run(ctx()))
	assert.NotContains(t, se1.String(), "lint reported", "quiet must suppress the lint gate header")
	assert.NotContains(t, se1.String(), "unused-resolver", "quiet must suppress the findings table")

	// Quiet, strict: still fails via exit code, still no output.
	var so2, se2 bytes.Buffer
	strictOpts := newResolverOptions(t, solutionPath, &so2, &se2)
	strictOpts.LintAfterValidate = true
	strictOpts.Strict = true
	strictOpts.Output = "quiet"
	err := strictOpts.Run(ctx())
	require.Error(t, err)
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
	assert.NotContains(t, se2.String(), "lint reported", "quiet must suppress output even when failing")
}

// ctx returns a background context seeded with a logger, for lint-gate tests.
func ctx() context.Context {
	return logger.WithLogger(context.Background(), logger.Get(0))
}
