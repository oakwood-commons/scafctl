// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_RunTreeOutput(t *testing.T) {
	t.Parallel()

	const solutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-output-demo
  version: 1.0.0
spec:
  resolvers:
    workspace_files:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                cmd:
                  - main.go
                pkg:
                  - runner.go
                  - tree.go
  workflow:
    actions:
      banner:
        description: Emit info message
        provider: message
        inputs:
          message: "BUILDING_TREE"
          type: info
      list-files:
        description: Return nested file map
        dependsOn: [banner]
        provider: static
        inputs:
          value:
            rslvr: workspace_files
      other:
        description: Unselected action
        provider: static
        inputs:
          value: skipped
`

	t.Run("run solution -a list-files -o tree --no-color renders tree cleanly", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(solutionYAML), 0o600))

		env := isolatedCatalogEnv(t)
		stdout, stderr, code := runScafctlWithEnvInDir(t, dir, env,
			"run", "solution", "-f", solFile, "-a", "list-files", "-o", "tree", "--no-color")

		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.NotContains(t, stdout, "\x1b[")
		assert.Contains(t, stdout, "actions")
		assert.Contains(t, stdout, "list-files")
		assert.Contains(t, stdout, "main.go")
		assert.Contains(t, stdout, "runner.go")
		assert.NotContains(t, stdout, "other")
		assert.True(t, strings.Contains(stdout, "├──") || strings.Contains(stdout, "└──"),
			"expected tree connectors in stdout:\n%s", stdout)
		assert.NotContains(t, stdout, "==> list-files", "stderr progress banners must not leak into stdout")

		filteredOut, filteredErr, filteredCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "solution", "-f", solFile, "-a", "list-files", "-o", "tree", "--no-color",
			"-e", `_.actions["list-files"].results`)
		require.Equal(t, 0, filteredCode, "stderr: %s", filteredErr)
		assert.Contains(t, filteredOut, "cmd")
		assert.Contains(t, filteredOut, "main.go")
		assert.NotContains(t, filteredOut, "startTime")
	})

	t.Run("run action list-files -f actions.yaml -o tree --no-color and -e filter succeed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		actFile := filepath.Join(dir, "actions.yaml")
		require.NoError(t, os.WriteFile(actFile, []byte(solutionYAML), 0o600))

		env := isolatedCatalogEnv(t)
		stdout, stderr, code := runScafctlWithEnvInDir(t, dir, env,
			"run", "action", "list-files", "-f", actFile, "-o", "tree", "--no-color")

		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.NotContains(t, stdout, "\x1b[")
		assert.Contains(t, stdout, "list-files")
		assert.Contains(t, stdout, "tree.go")
		assert.NotContains(t, stdout, "other")
		assert.True(t, strings.Contains(stdout, "├──") || strings.Contains(stdout, "└──"),
			"expected tree connectors in stdout:\n%s", stdout)

		filteredOut, filteredErr, filteredCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "action", "list-files", "-f", actFile, "-o", "tree", "--no-color",
			"-e", `_.actions["list-files"].results`)
		require.Equal(t, 0, filteredCode, "stderr: %s", filteredErr)
		assert.Contains(t, filteredOut, "pkg")
		assert.Contains(t, filteredOut, "runner.go")
		assert.NotContains(t, filteredOut, "startTime")
	})

	t.Run("dry-run -o tree renders WhatIf plan on run solution and run action", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(solutionYAML), 0o600))

		env := isolatedCatalogEnv(t)
		solOut, solErr, solCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "solution", "-f", solFile, "--dry-run", "-o", "tree", "--no-color")
		require.Equal(t, 0, solCode, "stderr: %s", solErr)
		assert.Contains(t, solOut, "tree-output-demo")
		assert.Contains(t, solOut, "actionPlan")
		assert.Contains(t, solOut, "list-files")

		actOut, actErr, actCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "action", "list-files", "-f", solFile, "--dry-run", "-o", "tree", "--no-color")
		require.Equal(t, 0, actCode, "stderr: %s", actErr)
		assert.Contains(t, actOut, "tree-output-demo")
		assert.Contains(t, actOut, "actionPlan")
		assert.Contains(t, actOut, "list-files")
		assert.NotContains(t, actOut, "other")
	})

	t.Run("help and shell completions list tree on run solution and run action", func(t *testing.T) {
		t.Parallel()
		solHelp, _, solHelpCode := runScafctl(t, "run", "solution", "--help")
		require.Equal(t, 0, solHelpCode)
		assert.Contains(t, solHelp, "tree")

		actHelp, _, actHelpCode := runScafctl(t, "run", "action", "--help")
		require.Equal(t, 0, actHelpCode)
		assert.Contains(t, actHelp, "tree")

		solComp, _, solCompCode := runScafctl(t, "__complete", "run", "solution", "-o", "")
		require.Equal(t, 0, solCompCode)
		assert.Contains(t, strings.Split(solComp, "\n"), "tree")

		actComp, _, actCompCode := runScafctl(t, "__complete", "run", "action", "-o", "")
		require.Equal(t, 0, actCompCode)
		assert.Contains(t, strings.Split(actComp, "\n"), "tree")
	})

	t.Run("embedder binary mycli supports -o tree on run solution and run action", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(solutionYAML), 0o600))

		env := isolatedCatalogEnv(t)
		solOut, solErr, solCode := runEmbedderMyCLI(t, dir, []string{"NO_COLOR"}, env,
			"run", "solution", "-f", solFile, "-a", "list-files", "-o", "tree", "--no-color")
		require.Equal(t, 0, solCode, "stderr: %s", solErr)
		assert.Contains(t, solOut, "list-files")
		assert.Contains(t, solOut, "main.go")

		actOut, actErr, actCode := runEmbedderMyCLI(t, dir, []string{"NO_COLOR"}, env,
			"run", "action", "list-files", "-f", solFile, "-o", "tree", "--no-color")
		require.Equal(t, 0, actCode, "stderr: %s", actErr)
		assert.Contains(t, actOut, "list-files")
		assert.Contains(t, actOut, "runner.go")
	})

	t.Run("error paths exit non-zero for failing action and invalid -e expression under -o tree", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(solutionYAML), 0o600))

		env := isolatedCatalogEnv(t)
		_, exprErr, exprCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "solution", "-f", solFile, "-a", "list-files", "-o", "tree", "--no-color",
			"-e", "_.nonexistent_field")
		assert.NotEqual(t, 0, exprCode)
		assert.Contains(t, exprErr, "expression evaluation failed")

		failFile := filepath.Join(dir, "fail.yaml")
		const failYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-fail-cli
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      fail-step:
        provider: go-template
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: 1
`
		require.NoError(t, os.WriteFile(failFile, []byte(failYAML), 0o600))

		failOut, _, failCode := runScafctlWithEnvInDir(t, dir, env,
			"run", "action", "fail-step", "-f", failFile, "-o", "tree", "--no-color")
		assert.Equal(t, 6, failCode)
		assert.Contains(t, failOut, "fail-step")
		assert.Contains(t, failOut, "failed")
	})
}
