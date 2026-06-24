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

func newResolverOptions(t *testing.T, solutionPath string, stdout, stderr *bytes.Buffer) *ResolverOptions {
	t.Helper()
	streams := &terminal.IOStreams{In: nil, Out: stdout, ErrOut: stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	return &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithValidation(t),
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

func TestResolverOptions_Run_FailOnValidationExitsNonZero(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.FailOnValidation = true

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.Error(t, err, "--fail-on-validation must produce an error on validation failure")
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
	// The validate command does not expose the fail-on-validation toggle —
	// it is always fatal.
	assert.Nil(t, cmd.Flags().Lookup("fail-on-validation"))
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
