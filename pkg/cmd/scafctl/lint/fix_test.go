// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hyphenatedFixSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-test
  version: 1.0.0
spec:
  resolvers:
    # hyphenated
    my-service-name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
    consumer:
      dependsOn:
        - my-service-name
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: my-service-name
`

func writeFixSolution(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(hyphenatedFixSolution), 0o600))
	return solPath
}

func fixOptions(solPath string, ioStreams *terminal.IOStreams) *Options {
	return &Options{
		File:           solPath,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
	}
}

func TestRunLintFix_WritesFile(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.Fix = true

	require.NoError(t, runLintFix(ctx, opts))

	got, err := os.ReadFile(solPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	content := string(got)
	assert.Contains(t, content, "myServiceName:")
	assert.NotContains(t, content, "my-service-name")
	assert.Contains(t, content, "rslvr: myServiceName")
	// Comment preserved.
	assert.Contains(t, content, "# hyphenated")
	// Summary goes to stderr.
	assert.Contains(t, errBuf.String(), "fixed")
}

func TestRunLintFix_DryRunDoesNotWrite(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.FixDryRun = true

	// Pending fixes gate CI: a preview with changes exits validation-failed.
	err := runLintFix(ctx, opts)
	require.Error(t, err)
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))

	got, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
	require.NoError(t, rerr)
	assert.Equal(t, hyphenatedFixSolution, string(got), "--fix-dry-run must not modify the file")
	assert.Contains(t, errBuf.String(), "would fix")
}

func TestRunLintFix_DiffDoesNotWriteAndEmitsHunks(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.Fix = true
	opts.Diff = true

	// --diff is a preview: pending fixes exit validation-failed (ruff parity).
	err := runLintFix(ctx, opts)
	require.Error(t, err)
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))

	got, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
	require.NoError(t, rerr)
	assert.Equal(t, hyphenatedFixSolution, string(got), "--diff must not modify the file")

	out := outBuf.String()
	assert.Contains(t, out, "@@")
	assert.Contains(t, out, "--- a/")
	assert.Contains(t, out, "+++ b/")
	assert.Contains(t, out, "-    my-service-name:")
	assert.Contains(t, out, "+    myServiceName:")
}

func TestRunLintFix_DiffRequiresFixFlag(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.Diff = true // neither --fix nor --fix-dry-run

	err := runLintFix(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--diff requires")
}

func TestRunLintFix_FixAndDryRunMutuallyExclusive(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.Fix = true
	opts.FixDryRun = true

	err := runLintFix(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestRunLintFix_StdinRejected(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions("-", ioStreams)
	opts.Fix = true

	err := runLintFix(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin")
}

func TestRunLintFix_NonDefaultBinaryName(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.BinaryName = "mycli"
	opts.Fix = true

	require.NoError(t, runLintFix(ctx, opts))

	got, err := os.ReadFile(solPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(got), "myServiceName:")
}

func TestRunLintFix_DefaultsBinaryName(t *testing.T) {
	solPath := writeFixSolution(t)
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.BinaryName = ""
	opts.Fix = true

	require.NoError(t, runLintFix(ctx, opts))
	assert.Equal(t, settings.CliBinaryName, opts.BinaryName)
}

// noFixableFixSolution has no hyphenated resolvers, so a preview is a no-op.
const noFixableFixSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-clean
  version: 1.0.0
spec:
  resolvers:
    myServiceName:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`

func TestRunLintFix_DryRunNoChangesExitsZero(t *testing.T) {
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(noFixableFixSolution), 0o600))

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.FixDryRun = true

	// No pending fixes -> preview exits cleanly (0).
	require.NoError(t, runLintFix(ctx, opts))
	assert.Contains(t, errBuf.String(), "No auto-fixable findings")
}

// collisionFixSolution renames my-service -> myService, which already exists,
// so the fix must be skipped (reported, not applied).
const collisionFixSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-collision
  version: 1.0.0
spec:
  resolvers:
    my-service:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: a
    myService:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: b
`

func TestRunLintFix_CollisionSkippedNoWrite(t *testing.T) {
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(collisionFixSolution), 0o600))

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := fixOptions(solPath, ioStreams)
	opts.Fix = true

	require.NoError(t, runLintFix(ctx, opts))

	got, err := os.ReadFile(solPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, collisionFixSolution, string(got), "collision must not modify the file")

	stderr := errBuf.String()
	assert.Contains(t, stderr, "skipped")
	// The internal ErrNotFixable sentinel must be stripped from the reason.
	assert.NotContains(t, stderr, "finding is not auto-fixable")
}
