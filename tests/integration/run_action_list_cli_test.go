// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const integrationAllExplicitActionsYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: ci-actions
  version: 1.0.0
spec:
  workflow:
    actions:
      build:
        alias: b
        description: Compile the binary
        explicit: true
        provider: message
        inputs:
          message: "building"
      lint:
        alias: l
        description: Run linters
        explicit: true
        provider: message
        inputs:
          message: "linting"
      test:
        description: Run unit tests
        explicit: true
        provider: message
        inputs:
          message: "testing"
`

func TestIntegration_RunAction_List(t *testing.T) {
	t.Parallel()

	t.Run("lists actions via auto-discovery, -f, -l, stdin, and CEL -w filters", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mixedYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mixed-actions
  version: 1.0.0
spec:
  workflow:
    actions:
      check:
        description: Default check
        provider: message
        inputs:
          message: "check"
      release:
        alias: rel
        description: Publish release
        explicit: true
        provider: message
        inputs:
          message: "release"
`
		actionsFile := filepath.Join(dir, "actions.yaml")
		require.NoError(t, os.WriteFile(actionsFile, []byte(mixedYAML), 0o600))

		// Auto-discovery with --list -o json
		stdout, stderr, code := runScafctlInDir(t, dir, "run", "action", "--list", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)

		var entries []map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &entries))
		require.Len(t, entries, 2)
		assert.Equal(t, "check", entries[0]["name"])
		assert.Equal(t, "", entries[0]["alias"])
		assert.Equal(t, true, entries[0]["default"])
		assert.Equal(t, "Default check", entries[0]["description"])
		assert.Equal(t, "release", entries[1]["name"])
		assert.Equal(t, "rel", entries[1]["alias"])
		assert.Equal(t, false, entries[1]["default"])

		// Shorthand -l with -f and -w '_.default'
		stdoutFilter, stderrFilter, codeFilter := runScafctl(t, "run", "action", "-l", "-f", actionsFile, "-o", "json", "-w", "_.default")
		require.Equal(t, exitcode.Success, codeFilter, "stderr: %s", stderrFilter)
		var defaultOnly []map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutFilter), &defaultOnly))
		require.Len(t, defaultOnly, 1)
		assert.Equal(t, "check", defaultOnly[0]["name"])

		// Stdin -f -
		stdoutStdin, stderrStdin, codeStdin := runScafctlWithStdin(t, strings.NewReader(integrationAllExplicitActionsYAML), "run", "action", "--list", "-f", "-", "-o", "json")
		require.Equal(t, exitcode.Success, codeStdin, "stderr: %s", stderrStdin)
		var stdinEntries []map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutStdin), &stdinEntries))
		require.Len(t, stdinEntries, 3)
		assert.Equal(t, "scafctl run action build -f -", stdinEntries[0]["command"])

		// Non-TTY default text output
		stdoutText, stderrText, codeText := runScafctlInDir(t, dir, "run", "action", "-l")
		require.Equal(t, exitcode.Success, codeText, "stderr: %s", stderrText)
		assert.Contains(t, stdoutText, "check")
		assert.Contains(t, stdoutText, "release")
		assert.NotContains(t, stdoutText, "scafctl run action release", "command column is hidden in table/text view")
	})
}

func TestIntegration_RunAction_List_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	actionsFile := filepath.Join(dir, "actions.yaml")
	require.NoError(t, os.WriteFile(actionsFile, []byte(integrationAllExplicitActionsYAML), 0o600))

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "action name with --list",
			args: []string{"run", "action", "build", "--list", "-f", actionsFile},
			want: "--list does not accept action names",
		},
		{
			name: "--dry-run with --list",
			args: []string{"run", "action", "--list", "--dry-run", "-f", actionsFile},
			want: "--list cannot be combined with --dry-run",
		},
		{
			name: "-r param with --list",
			args: []string{"run", "action", "--list", "-r", "env=prod", "-f", actionsFile},
			want: "--list does not accept resolver parameters",
		},
		{
			name: "positional param with --list",
			args: []string{"run", "action", "--list", "env=prod", "-f", actionsFile},
			want: "--list does not accept resolver parameters",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runScafctlInDir(t, dir, tc.args...)
			assert.Equal(t, exitcode.InvalidInput, code)
			assert.Contains(t, stderr, tc.want)
		})
	}
}

func TestIntegration_RunAction_List_AllExplicitBareRun(t *testing.T) {
	t.Parallel()

	t.Run("bare run action on all-explicit file lists actions on stderr and exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.yaml"), []byte(integrationAllExplicitActionsYAML), 0o600))
		outDir := filepath.Join(dir, "should-not-exist")

		stdout, stderr, code := runScafctlInDir(t, dir, "run", "action", "--output-dir", outDir)
		assert.Equal(t, exitcode.InvalidInput, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "Available actions:")
		assert.Contains(t, stderr, "build (b)")
		assert.Contains(t, stderr, "Compile the binary")
		assert.Contains(t, stderr, "no default actions; specify one of: build, lint, test")

		_, statErr := os.Stat(outDir)
		assert.True(t, os.IsNotExist(statErr), "--output-dir must not be created when run fails early")

		// -o json emits parseable failure envelope on stdout
		jsonStdout, jsonStderr, jsonCode := runScafctlInDir(t, dir, "run", "action", "-o", "json")
		assert.Equal(t, exitcode.InvalidInput, jsonCode)
		assert.Contains(t, jsonStderr, "Available actions:")
		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStdout), &envelope))
		assert.Equal(t, "failed", envelope["status"])
	})

	t.Run("inspect solution --usage suggests run action <name> and displays aliases", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		actionsFile := filepath.Join(dir, "actions.yaml")
		require.NoError(t, os.WriteFile(actionsFile, []byte(integrationAllExplicitActionsYAML), 0o600))

		stdout, stderr, code := runScafctl(t, "inspect", "solution", "-f", actionsFile, "--usage")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)
		assert.Contains(t, stdout, "Run: scafctl run action <name>")
		assert.Contains(t, stdout, "  build (b)\n")
		assert.Contains(t, stdout, "  lint (l)\n")
	})

	t.Run("finally-only workflow still succeeds on bare run action", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		finallyYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: finally-only
  version: 1.0.0
spec:
  workflow:
    finally:
      done:
        provider: message
        inputs:
          message: "cleanup ran"
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.yaml"), []byte(finallyYAML), 0o600))

		stdout, stderr, code := runScafctlInDir(t, dir, "run", "action", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		assert.Equal(t, "succeeded", result["status"])
	})
}
