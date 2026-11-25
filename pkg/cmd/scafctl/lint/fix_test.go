// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
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

func TestRunLintFix_SourceResolution(t *testing.T) {
	t.Run("non-local source rejected", func(t *testing.T) {
		ioStreams, _, _ := terminal.NewTestIOStreams()
		opts := fixOptions("my-solution@1.0.0", ioStreams)
		opts.Fix = true

		err := runLintFix(testContext(ioStreams), opts)
		require.Error(t, err)
		assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "requires a local solution file")
	})

	t.Run("invalid reference", func(t *testing.T) {
		ioStreams, _, _ := terminal.NewTestIOStreams()
		opts := fixOptions("ftp://example.com/sol.yaml", ioStreams)
		opts.Fix = true

		err := runLintFix(testContext(ioStreams), opts)
		require.ErrorIs(t, err, source.ErrInvalidRef)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	})

	t.Run("ambiguous discovery rejected", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"solution.yaml", "scafctl.yaml"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(hyphenatedFixSolution), 0o600))
		}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		opts := fixOptions("", ioStreams)
		opts.Fix = true

		err := runLintFix(provider.WithWorkingDirectory(testContext(ioStreams), dir), opts)
		require.ErrorIs(t, err, load.ErrAmbiguousSolution)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})

	t.Run("nothing discovered", func(t *testing.T) {
		ioStreams, _, _ := terminal.NewTestIOStreams()
		opts := fixOptions("", ioStreams)
		opts.Fix = true

		err := runLintFix(provider.WithWorkingDirectory(testContext(ioStreams), t.TempDir()), opts)
		require.ErrorIs(t, err, load.ErrNoSolutionFound)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})

	t.Run("single discovered file is fixed", func(t *testing.T) {
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(hyphenatedFixSolution), 0o600))
		ioStreams, _, _ := terminal.NewTestIOStreams()
		opts := fixOptions("", ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(provider.WithWorkingDirectory(testContext(ioStreams), dir), opts))
		assert.Equal(t, solPath, opts.File)
		got, err := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, err)
		assert.Contains(t, string(got), "myServiceName:")
	})
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

func TestRunLintFix_ComposedResidualFindings(t *testing.T) {
	t.Run("composes partials so valid composed-only resolvers pass residual lint", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		partPath := filepath.Join(tmpDir, "resolvers.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-clean
  version: 1.0.0
compose:
  - resolvers.yaml
`
		part := `spec:
  resolvers:
    greeting:
      description: Greeting value
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(part), 0o600))

		ioStreams, _, _ := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))
	})

	t.Run("surfaces schema-violation in composed fragment during residual lint", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tests"), 0o700))
		partPath := filepath.Join(tmpDir, "tests", "svc.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-invalid
  version: 1.0.0
compose:
  - tests/svc.yaml
spec:
  resolvers:
    my-greeting:
      description: Greeting value
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		badPart := `spec:
  testing:
    config:
      services:
        - name: mocksvc
          type: bogus
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(badPart), 0o600))

		ioStreams, outBuf, _ := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true
		opts.KvxOutputFlags.Output = "json"

		err := runLintFix(ctx, opts)
		require.Error(t, err)
		assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))

		// Primary fix was still written.
		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Contains(t, string(gotPrimary), "myGreeting:")

		// Residual JSON output includes the composed fragment schema-violation.
		assert.Contains(t, outBuf.String(), "schema-violation")
		assert.Contains(t, outBuf.String(), "svc.yaml")
	})

	t.Run("skips rename when composed fragment references primary hyphenated resolver", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		partPath := filepath.Join(tmpDir, "resolvers.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-ref
  version: 1.0.0
compose:
  - resolvers.yaml
spec:
  resolvers:
    my-greeting:
      description: Greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		part := `spec:
  resolvers:
    consumer:
      description: Consumes primary resolver
      resolve:
        with:
          - provider: cel
            inputs:
              expression: '_["my-greeting"]'
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(part), 0o600))

		ioStreams, _, errBuf := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Equal(t, primary, string(gotPrimary), "must not rewrite primary when composed fragment holds an off-file reference")
		assert.Contains(t, errBuf.String(), "skipped")
	})

	t.Run("skips rename when composed fragment already defines target camelCase resolver", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		partPath := filepath.Join(tmpDir, "resolvers.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-collision
  version: 1.0.0
compose:
  - resolvers.yaml
spec:
  resolvers:
    my-greeting:
      description: Greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		part := `spec:
  resolvers:
    myGreeting:
      description: Already defined in composed fragment
      resolve:
        with:
          - provider: static
            inputs:
              value: existing
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(part), 0o600))

		ioStreams, _, errBuf := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Equal(t, primary, string(gotPrimary), "must not rewrite primary into a duplicate-resolver collision")
		assert.Contains(t, errBuf.String(), "skipped")
	})

	t.Run("resolves relative -f against --cwd context", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(hyphenatedFixSolution), 0o600))

		ioStreams, _, _ := terminal.NewTestIOStreams()
		ctx := provider.WithWorkingDirectory(testContext(ioStreams), tmpDir)
		opts := fixOptions("solution.yaml", ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		got, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Contains(t, string(got), "myServiceName:")
	})

	t.Run("skips rename when composed fragment references primary resolver via source forEach.in", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		partPath := filepath.Join(tmpDir, "resolvers.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-foreach
  version: 1.0.0
compose:
  - resolvers.yaml
spec:
  resolvers:
    my-items:
      description: Item list
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - a
                - b
`
		part := `spec:
  resolvers:
    mapped:
      description: Iterates over primary resolver in resolve source forEach
      resolve:
        with:
          - provider: cel
            forEach:
              item: entry
              in:
                rslvr: my-items
            inputs:
              expression: 'entry'
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(part), 0o600))

		ioStreams, _, errBuf := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Equal(t, primary, string(gotPrimary), "must not rewrite primary when composed resolve source forEach.in references it")
		assert.Contains(t, errBuf.String(), "skipped")
	})

	t.Run("rewrites source forEach.in reference in primary file", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-primary-foreach
  version: 1.0.0
spec:
  resolvers:
    my-items:
      description: Item list
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - a
                - b
    mapped:
      description: Iterates over sibling resolver in resolve source forEach
      resolve:
        with:
          - provider: cel
            forEach:
              item: entry
              in:
                rslvr: my-items
            inputs:
              expression: 'entry'
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))

		ioStreams, _, _ := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Contains(t, string(gotPrimary), "myItems:")
		assert.Contains(t, string(gotPrimary), "rslvr: myItems")
	})

	t.Run("skips rename when primary file holds unrewritten bundle.plugins defaults reference", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-primary-plugin-defaults
  version: 1.0.0
bundle:
  plugins:
    - name: custom
      kind: provider
      version: ^1.0.0
      defaults:
        items:
          rslvr: my-items
spec:
  resolvers:
    my-items:
      description: Item list
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - a
                - b
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))

		ioStreams, _, errBuf := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Equal(t, primary, string(gotPrimary), "must not rewrite primary when unindexed bundle.plugins defaults reference remains")
		assert.Contains(t, errBuf.String(), "unrewritten reference remains in solution")
	})

	t.Run("skips rename when composed test case mocks primary hyphenated resolver", func(t *testing.T) {
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		partPath := filepath.Join(tmpDir, "tests.yaml")

		primary := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fix-composed-mock
  version: 1.0.0
compose:
  - tests.yaml
spec:
  resolvers:
    my-greeting:
      description: Greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		part := `spec:
  testing:
    cases:
      smoke:
        description: Smoke test mocking primary resolver
        command:
          - render
          - solution
        mocks:
          my-greeting: mocked-hello
        assertions:
          - contains: mocked-hello
`
		require.NoError(t, os.WriteFile(solPath, []byte(primary), 0o600))
		require.NoError(t, os.WriteFile(partPath, []byte(part), 0o600))

		ioStreams, _, errBuf := terminal.NewTestIOStreams()
		ctx := testContext(ioStreams)
		opts := fixOptions(solPath, ioStreams)
		opts.Fix = true

		require.NoError(t, runLintFix(ctx, opts))

		gotPrimary, rerr := os.ReadFile(solPath) //nolint:gosec // test-controlled path
		require.NoError(t, rerr)
		assert.Equal(t, primary, string(gotPrimary), "must not rewrite primary when composed test case mocks the old resolver name")
		assert.Contains(t, errBuf.String(), "skipped")
	})
}
