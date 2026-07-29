// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// solutionWithFailingResolver hard-fails during the transform phase at runtime:
// int("hello") is a valid CEL expression that passes load-time validation but
// errors when evaluated, so the failure occurs at the resolver-execution site
// (not at solution load). It includes a workflow so the failure is not masked by
// the "no workflow defined" guard.
const solutionWithFailingResolver = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: failing-resolver-envelope
  version: 1.0.0
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
      transform:
        with:
          - provider: cel
            inputs:
              expression: 'int(__self)'
  workflow:
    actions:
      greet:
        provider: static
        inputs:
          value: world
`

// testRegistryWithCel returns a registry with the static and cel providers,
// used to exercise runtime transform failures.
func testRegistryWithCel(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(celprovider.NewCelProvider()))
	return reg
}

// TestResolverOptions_Run_ValidationFailure_StructuredEnvelope verifies that a
// resolver validation failure attaches the reserved __status/__diagnostics keys
// to the machine-readable stdout document so callers piping to jq can detect and
// inspect the failure programmatically (issue: empty stdout on failure).
func TestResolverOptions_Run_ValidationFailure_StructuredEnvelope(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", "yaml"} {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
			require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

			var stdout, stderr bytes.Buffer
			opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
			opts.Output = format

			ctx := logger.WithLogger(context.Background(), logger.Get(0))
			err := opts.Run(ctx)

			// Validation is non-fatal by default: exit 0, values still shown.
			require.NoError(t, err)

			decoded := decodeStructured(t, format, stdout.Bytes())
			assert.Equal(t, execute.StatusFailed, decoded[execute.StatusKey],
				"structured output must carry the reserved status key on failure")
			diags, ok := decoded[execute.DiagnosticsKey].([]any)
			require.True(t, ok, "structured output must carry the reserved diagnostics key")
			assert.NotEmpty(t, diags, "diagnostics must not be empty on validation failure")
			assert.Contains(t, stdout.String(), "Bob", "resolved values must still be present")
		})
	}
}

// TestResolverOptions_Run_ValidationFailure_TableUnchanged verifies that
// non-structured output formats keep the prior stderr-only behavior and do not
// inject the reserved envelope keys.
func TestResolverOptions_Run_ValidationFailure_TableUnchanged(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.Output = "table"

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	require.NoError(t, opts.Run(ctx))

	assert.NotContains(t, stdout.String(), execute.StatusKey,
		"table output must not inject the reserved status key")
	assert.NotContains(t, stdout.String(), execute.DiagnosticsKey,
		"table output must not inject the reserved diagnostics key")
	assert.Contains(t, stderr.String(), "failed validation",
		"diagnostics must still be rendered to stderr")
}

// TestValidateResolver_Run_ValidationFailure_ExitNonZeroWithEnvelope verifies
// that the gate mode (FailOnValidation) exits non-zero AND still emits the
// structured failure envelope on stdout.
func TestValidateResolver_Run_ValidationFailure_ExitNonZeroWithEnvelope(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failingValidationSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.FailOnValidation = true

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.Error(t, err, "gate mode must exit non-zero on validation failure")
	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, execute.StatusFailed, decoded[execute.StatusKey])
	assert.NotEmpty(t, decoded[execute.DiagnosticsKey])
}

// TestSolutionOptions_Run_ResolverFailure_StructuredEnvelope verifies that a
// hard resolver failure during `run solution` emits a parseable
// {status, diagnostics} document on stdout instead of an empty stdout.
func TestSolutionOptions_Run_ResolverFailure_StructuredEnvelope(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", "yaml"} {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
			require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithFailingResolver), 0o600))

			var stdout, stderr bytes.Buffer
			streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &stderr}
			cliParams := settings.NewCliParams()
			cliParams.ExitOnError = false

			opts := &SolutionOptions{
				sharedResolverOptions: sharedResolverOptions{
					IOStreams:       streams,
					CliParams:       cliParams,
					File:            solutionPath,
					KvxOutputFlags:  flags.KvxOutputFlags{Output: format},
					ResolverTimeout: 30 * time.Second,
					PhaseTimeout:    5 * time.Minute,
					registry:        testRegistryWithCel(t),
				},
			}

			ctx := logger.WithLogger(context.Background(), logger.Get(0))
			ctx = writer.WithWriter(ctx, writer.New(streams, cliParams))
			err := opts.Run(ctx)

			require.Error(t, err, "a hard resolver failure must exit non-zero")
			decoded := decodeStructured(t, format, stdout.Bytes())
			assert.Equal(t, execute.StatusFailed, decoded[execute.StatusFieldKey],
				"solution failure envelope must carry the status field")
			assert.NotEmpty(t, decoded[execute.DiagnosticsFieldKey],
				"solution failure envelope must carry diagnostics")
			assert.NotEmpty(t, stderr.String(), "a human-readable error must still reach stderr")
		})
	}
}

// TestSolutionOptions_Run_ResolverFailure_TableUnchanged verifies the human
// output path is unaffected by the structured envelope feature.
func TestSolutionOptions_Run_ResolverFailure_TableUnchanged(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithFailingResolver), 0o600))

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "table"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithCel(t),
		},
	}

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(streams, cliParams))
	err := opts.Run(ctx)

	require.Error(t, err)
	assert.NotContains(t, stdout.String(), execute.DiagnosticsFieldKey,
		"table output must not emit a structured failure envelope on stdout")
}

// TestSolutionOptions_Run_ResolverFailure_UnsupportedStructuredFormatFallsBack
// verifies that a structured format the solution/action envelope cannot
// serialize (csv/toml/mermaid -- only json/yaml are supported) falls back to the
// stderr-only error path instead of emitting an empty stdout document.
func TestSolutionOptions_Run_ResolverFailure_UnsupportedStructuredFormatFallsBack(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithFailingResolver), 0o600))

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "csv"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithCel(t),
		},
	}

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(streams, cliParams))
	err := opts.Run(ctx)

	require.Error(t, err, "an unsupported structured format must still exit non-zero")
	assert.Empty(t, bytes.TrimSpace(stdout.Bytes()),
		"unsupported structured format must not emit a blank/partial stdout document")
	assert.NotEmpty(t, stderr.String(), "the error must reach stderr")
}

// TestActionOptions_Run_ResolverFailure_StructuredEnvelope verifies the same
// failure-envelope behavior for `run action`.
func TestActionOptions_Run_ResolverFailure_StructuredEnvelope(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithFailingResolver), 0o600))

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ActionOptions{}
	opts.IOStreams = streams
	opts.CliParams = cliParams
	opts.File = solutionPath
	opts.Output = "json"
	opts.ResolverTimeout = 30 * time.Second
	opts.PhaseTimeout = 5 * time.Minute
	opts.registry = testRegistryWithCel(t)

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(streams, cliParams))
	err := opts.Run(ctx)

	require.Error(t, err, "a hard resolver failure must exit non-zero")
	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, execute.StatusFailed, decoded[execute.StatusFieldKey])
	assert.NotEmpty(t, decoded[execute.DiagnosticsFieldKey])
}

// sizeLimitSolution resolves a value that is small but larger than the byte
// limit used by the value-size tests below.
const sizeLimitSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: value-size-envelope
  version: 1.0.0
spec:
  resolvers:
    big:
      resolve:
        with:
          - provider: static
            inputs:
              value: this-value-exceeds-the-limit
`

// TestResolverOptions_Run_ValueSizeFailure_StructuredEnvelope covers the
// value-size failure branch of failStructured: the resolved values plus the
// reserved __status/__diagnostics keys are emitted on stdout.
func TestResolverOptions_Run_ValueSizeFailure_StructuredEnvelope(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(sizeLimitSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.MaxValueSize = 4 // bytes — forces checkValueSizes to fail

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.Error(t, err, "exceeding max-value-size must exit non-zero")
	decoded := decodeStructured(t, "json", stdout.Bytes())
	assert.Equal(t, execute.StatusFailed, decoded[execute.StatusKey])
	assert.NotEmpty(t, decoded[execute.DiagnosticsKey])
}

// TestResolverOptions_Run_ValueSizeFailure_TableUnchanged covers the
// non-structured branch of failStructured: table output keeps the stderr-only
// error behavior and does not inject the reserved keys.
func TestResolverOptions_Run_ValueSizeFailure_TableUnchanged(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(sizeLimitSolution), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptions(t, solutionPath, &stdout, &stderr)
	opts.Output = "table"
	opts.MaxValueSize = 4

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	err := opts.Run(ctx)

	require.Error(t, err)
	assert.NotContains(t, stdout.String(), execute.DiagnosticsKey,
		"table output must not inject the reserved diagnostics key")
	assert.NotContains(t, stdout.String(), execute.StatusKey,
		"table output must not inject the reserved status key")
}

// solutionWithGoodAndBadResolvers has two independent resolvers: "good" resolves
// to a plain string, while "bad" hard-fails in the resolve phase (an undefined
// Go-template function fails to parse, so the source produces no value at all).
// This mirrors the issue's exact repro and exercises the
// partial-values-on-resolve-failure path for `run resolver`.
const solutionWithGoodAndBadResolvers = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: good-and-bad-resolvers
  version: 1.0.0
spec:
  resolvers:
    good:
      resolve:
        with:
          - provider: static
            inputs:
              value: i-resolved
    bad:
      resolve:
        with:
          - provider: go-template
            inputs:
              template: "{{ undefinedFunc .x }}"
              data:
                x: hello
`

// testRegistryWithGoTemplate returns a registry with the static and go-template
// providers, used to trigger a resolve-phase failure that produces no value.
func testRegistryWithGoTemplate(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(gotmplprovider.NewGoTemplateProvider()))
	return reg
}

// newResolverOptionsForResolveFailure builds ResolverOptions wired with the
// static+go-template registry (needed to trigger a resolve-phase failure) and
// the given output format.
func newResolverOptionsForResolveFailure(t *testing.T, solutionPath string, stdout, stderr *bytes.Buffer, format string) *ResolverOptions {
	t.Helper()
	streams := &terminal.IOStreams{In: nil, Out: stdout, ErrOut: stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	return &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: format},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithGoTemplate(t),
		},
	}
}

// TestResolverOptions_Run_ResolvePhaseFailure_PreservesValues verifies that a
// resolve/transform-phase failure keeps every successfully-resolved value in the
// machine-readable output (alongside __status/__diagnostics) instead of dropping
// the entire values map -- while still exiting non-zero, since a resolver that
// could not produce a value is a hard failure (issue: run resolver -o json drops
// the values map on a resolve-phase error).
func TestResolverOptions_Run_ResolvePhaseFailure_PreservesValues(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", "yaml"} {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
			require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithGoodAndBadResolvers), 0o600))

			var stdout, stderr bytes.Buffer
			opts := newResolverOptionsForResolveFailure(t, solutionPath, &stdout, &stderr, format)

			ctx := logger.WithLogger(context.Background(), logger.Get(0))
			ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
			err := opts.Run(ctx)

			// A resolve/transform failure is a HARD failure: it exits non-zero
			// even without --fail-on-validation, unlike a validation-only failure.
			require.Error(t, err, "a resolve/transform failure must exit non-zero")
			assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err),
				"a resolve/transform failure must use the general-error exit code")

			decoded := decodeStructured(t, format, stdout.Bytes())
			assert.Equal(t, "i-resolved", decoded["good"],
				"a successfully-resolved value must survive a sibling resolve/transform failure")
			assert.NotContains(t, decoded, "bad",
				"a resolver that produced no value must be absent from the values map")
			assert.Equal(t, execute.StatusFailed, decoded[execute.StatusKey],
				"structured output must carry the reserved status key on failure")

			diags, ok := decoded[execute.DiagnosticsKey].([]any)
			require.True(t, ok, "structured output must carry the reserved diagnostics key")
			require.NotEmpty(t, diags, "diagnostics must not be empty on a resolve/transform failure")
			var namedBad bool
			for _, d := range diags {
				if m, ok := d.(map[string]any); ok && m["resolver"] == "bad" {
					namedBad = true
				}
			}
			assert.True(t, namedBad, "diagnostics must name the failed resolver 'bad'")
		})
	}
}

// TestResolverOptions_Run_ResolvePhaseFailure_TableShowsValues verifies that the
// human (table) output still renders the successfully-resolved values on stdout
// and summarizes the failure on stderr (as a plain failure, not a "validation"
// failure), while exiting non-zero and not injecting the reserved envelope keys.
func TestResolverOptions_Run_ResolvePhaseFailure_TableShowsValues(t *testing.T) {
	t.Parallel()

	solutionPath := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionWithGoodAndBadResolvers), 0o600))

	var stdout, stderr bytes.Buffer
	opts := newResolverOptionsForResolveFailure(t, solutionPath, &stdout, &stderr, "table")

	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = writer.WithWriter(ctx, writer.New(opts.IOStreams, opts.CliParams))
	err := opts.Run(ctx)

	require.Error(t, err, "a resolve/transform failure must exit non-zero")
	assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
	assert.Contains(t, stdout.String(), "i-resolved",
		"resolved values must still be shown on stdout in human output")
	assert.Contains(t, stderr.String(), "resolver(s) failed",
		"a hard failure must be summarized on stderr")
	assert.NotContains(t, stderr.String(), "failed validation",
		"a resolve/transform failure must not be mislabeled as a validation failure")
	assert.NotContains(t, stdout.String(), execute.DiagnosticsKey,
		"table output must not inject the reserved diagnostics key")
}

// decodeStructured parses JSON or YAML structured output into a map for
// assertions. It fails the test if the payload is empty or unparseable, which is
// the exact regression this feature guards against.
func decodeStructured(t *testing.T, format string, data []byte) map[string]any {
	t.Helper()
	require.NotEmpty(t, bytes.TrimSpace(data), "structured output must not be empty on failure")

	out := map[string]any{}
	switch format {
	case "yaml":
		require.NoError(t, yaml.Unmarshal(data, &out))
	default:
		require.NoError(t, json.Unmarshal(data, &out))
	}
	return out
}
