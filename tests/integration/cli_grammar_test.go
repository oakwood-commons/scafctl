// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const grammarCatalogSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: grammar-demo
  version: 1.0.0
  description: Demo solution for CLI grammar verification
spec:
  resolvers:
    env:
      description: Target environment
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
              default: dev
  workflow:
    actions:
      greet:
        description: Print greeting
        provider: message
        inputs:
          message:
            expr: "'hello-' + _.env"
      deploy:
        description: Explicit deploy task
        explicit: true
        provider: message
        inputs:
          message:
            expr: "'deploy-' + _.env"
  testing:
    cases:
      smoke:
        description: Verify resolver output
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`

func TestIntegration_CLIGrammar_RunBareHint(t *testing.T) {
	t.Parallel()

	_, stderr, exitCode := runScafctl(t, "run", "my-app")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, `"scafctl run solution my-app"`)
	assert.Contains(t, stderr, `"scafctl run action my-app"`)

	stdout, _, helpExit := runScafctl(t, "run")
	assert.Equal(t, 0, helpExit)
	assert.Contains(t, stdout, "SUBCOMMANDS:")
}

func TestIntegration_CLIGrammar_StrictFileFlagRejection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		wantHint string
	}{
		{name: "run solution -f catalog", args: []string{"run", "solution", "-f", "my-app"}, wantHint: "scafctl run solution my-app"},
		{name: "run action -f catalog", args: []string{"run", "action", "deploy", "-f", "my-app"}, wantHint: "scafctl run action -s my-app"},
		{name: "run resolver -f catalog", args: []string{"run", "resolver", "-f", "my-app"}, wantHint: "scafctl run resolver -s my-app"},
		{name: "validate resolver -f catalog", args: []string{"validate", "resolver", "-f", "my-app"}, wantHint: "scafctl validate resolver -s my-app"},
		{name: "validate -f catalog", args: []string{"validate", "-f", "my-app"}, wantHint: "scafctl validate my-app"},
		{name: "validate solution -f catalog", args: []string{"validate", "solution", "-f", "my-app"}, wantHint: "scafctl validate solution my-app"},
		{name: "lint -f catalog", args: []string{"lint", "-f", "my-app"}, wantHint: "scafctl lint my-app"},
		{name: "lint solution -f catalog", args: []string{"lint", "solution", "-f", "my-app"}, wantHint: "scafctl lint solution my-app"},
		{name: "test -f catalog", args: []string{"test", "-f", "my-app"}, wantHint: "scafctl test my-app"},
		{name: "render solution -f catalog", args: []string{"render", "solution", "-f", "my-app"}, wantHint: "scafctl render solution my-app"},
		{name: "inspect solution -f catalog", args: []string{"inspect", "solution", "-f", "my-app"}, wantHint: "scafctl inspect solution my-app"},
		{name: "get solution -f catalog", args: []string{"get", "solution", "-f", "my-app"}, wantHint: "scafctl get solution my-app"},
		{name: "diff solution -f catalog", args: []string{"diff", "solution", "-f", "my-app@1.0.0", "-f", "./b.yaml"}, wantHint: "scafctl diff solution my-app@1.0.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, exitCode := runScafctl(t, tc.args...)
			assert.NotEqual(t, 0, exitCode)
			assert.Contains(t, stderr, tc.wantHint)
		})
	}
}

func TestIntegration_CLIGrammar_CatalogWorkflowAndSymmetry(t *testing.T) {
	t.Parallel()

	env := isolatedCatalogEnv(t)
	solDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(grammarCatalogSolutionYAML), 0o600))

	// Package using the plural alias "solutions"
	_, stderr, exitCode := runScafctlWithEnv(t, env, "package", "solutions", "-f", solFile, "--skip-tests")
	require.Equal(t, 0, exitCode, "package solutions failed: %s", stderr)

	// 1. run solution with positional key=value and -a shorthand
	stdout, stderr, exitCode := runScafctlWithEnvInDir(t, t.TempDir(), env,
		"run", "solution", "grammar-demo", "-a", "deploy", "env=prod", "-o", "json")
	require.Equal(t, 0, exitCode, "run solution -a failed: %s", stderr)
	assert.Contains(t, stdout, "deploy-prod")
	assert.NotContains(t, stdout, "hello-prod")

	// 2. run solution rejects two bare positional args with actionable -a hint
	_, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env, "run", "solution", "grammar-demo", "deploy")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "scafctl run solution grammar-demo -a deploy")

	// 3. run action -s <catalog-name> and run action --list -s <catalog-name>
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env,
		"run", "action", "deploy", "-s", "grammar-demo", "env=stage", "-o", "json")
	require.Equal(t, 0, exitCode, "run action -s failed: %s", stderr)
	assert.Contains(t, stdout, "deploy-stage")

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env,
		"run", "action", "--list", "-s", "grammar-demo", "-o", "json")
	require.Equal(t, 0, exitCode, "run action --list -s failed: %s", stderr)
	assert.True(t, json.Valid([]byte(strings.TrimSpace(stdout))))
	assert.Contains(t, stdout, "scafctl run action deploy -s grammar-demo")

	// 4. run resolver -s <catalog-name>, -a shorthand, and --version dynamic help
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env,
		"run", "resolver", "-s", "grammar-demo", "-a", "deploy", "env=qa", "-o", "json")
	require.Equal(t, 0, exitCode, "run resolver -s -a failed: %s", stderr)
	assert.Contains(t, stdout, `"env": "qa"`)

	helpStdout, helpStderr, helpCode := runScafctlWithEnvInDir(t, t.TempDir(), env,
		"run", "resolver", "-s", "grammar-demo", "--version", "^1.0.0", "--help")
	require.Equal(t, 0, helpCode, "run resolver -s --version --help failed: %s", helpStderr)
	assert.Contains(t, helpStdout, "Target environment")

	// 5. validate resolver -s <catalog-name>
	_, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env,
		"validate", "resolver", "-s", "grammar-demo", "-o", "quiet")
	require.Equal(t, 0, exitCode, "validate resolver -s failed: %s", stderr)

	// 6. lint solution (-f and positional catalog ref, plus sol/s/solutions aliases)
	for _, sub := range []string{"solution", "sol", "s", "solutions"} {
		_, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env, "lint", sub, "grammar-demo", "-o", "quiet")
		require.Equal(t, 0, exitCode, "lint %s catalog failed: %s", sub, stderr)
	}
	_, stderr, exitCode = runScafctl(t, "lint", "solution", "-f", solFile, "-o", "quiet")
	require.Equal(t, 0, exitCode, "lint solution -f failed: %s", stderr)

	// 7. root validate (-f file, positional catalog ref, and -f - stdin)
	_, stderr, exitCode = runScafctl(t, "validate", "-f", solFile, "-o", "quiet")
	require.Equal(t, 0, exitCode, "validate -f failed: %s", stderr)

	_, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env, "validate", "grammar-demo", "-o", "quiet")
	require.Equal(t, 0, exitCode, "validate catalog ref failed: %s", stderr)

	_, stderr, exitCode = runScafctlWithStdin(t, strings.NewReader(grammarCatalogSolutionYAML), "validate", "-f", "-", "-o", "quiet")
	require.Equal(t, 0, exitCode, "validate -f - failed: %s", stderr)

	inspectStdinOut, inspectStdinErr, inspectStdinCode := runScafctlWithStdin(t, strings.NewReader(grammarCatalogSolutionYAML), "inspect", "solution", "-f", "-", "-o", "json")
	require.Equal(t, 0, inspectStdinCode, "inspect solution -f - failed: %s", inspectStdinErr)
	assert.Contains(t, inspectStdinOut, `"name": "grammar-demo"`)

	// 8. root test with FunctionalOptions flags (-f and --dry-run)
	_, stderr, exitCode = runScafctl(t, "test", "-f", solFile, "--dry-run", "-o", "quiet")
	require.Equal(t, 0, exitCode, "test -f --dry-run failed: %s", stderr)

	// 9. Normalized aliases across inspect, validate, get, diff, plugins
	for _, alias := range []string{"sol", "s", "solutions"} {
		_, stderr, exitCode = runScafctl(t, "inspect", alias, "-f", solFile, "-o", "quiet")
		require.Equal(t, 0, exitCode, "inspect %s failed: %s", alias, stderr)

		_, stderr, exitCode = runScafctl(t, "validate", alias, "-f", solFile, "-o", "quiet")
		require.Equal(t, 0, exitCode, "validate %s failed: %s", alias, stderr)

		_, stderr, exitCode = runScafctlWithEnvInDir(t, t.TempDir(), env, "get", alias, "grammar-demo", "-o", "json")
		require.Equal(t, 0, exitCode, "get %s failed: %s", alias, stderr)

		_, stderr, exitCode = runScafctl(t, "diff", alias, "-f", solFile, "-f", solFile, "-o", "quiet")
		require.Equal(t, 0, exitCode, "diff %s failed: %s", alias, stderr)
	}

	stdout, stderr, exitCode = runScafctlWithEnv(t, env, "plugin", "list", "-o", "json")
	require.Equal(t, 0, exitCode, "plugin alias failed: %s", stderr)
	assert.True(t, json.Valid([]byte(strings.TrimSpace(stdout))))
}

func TestIntegration_CLIGrammar_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	root, cleanup := scafctl.Root(&scafctl.RootOptions{
		BinaryName: "mycli",
		IOStreams:  streams,
		ExitFunc:   func(_ int) {},
	})
	defer cleanup()

	root.SetArgs([]string{"run", "my-app"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"mycli run solution my-app"`)
	assert.Contains(t, err.Error(), `"mycli run action my-app"`)
	assert.NotContains(t, err.Error(), "scafctl")
}
