// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_Lint_ExecSourceWithoutFailOnError verifies CLI lint, strict
// validation, and rule explanation behavior for exec-source-without-fail-on-error.
func TestIntegration_Lint_ExecSourceWithoutFailOnError(t *testing.T) {
	t.Parallel()

	t.Run("warns on non-final exec source without failOnError and fails validate --strict", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solutionFile := filepath.Join(tmpDir, "solution.yaml")

		solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-fallback-warning
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: latest
    - name: env
      kind: provider
      version: latest
spec:
  resolvers:
    token:
      description: CLI token with environment variable fallback
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli create token
              raw: true
          - provider: env
            inputs:
              operation: get
              name: FEDERATED_TOKEN
`
		require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o600))

		stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")
		assert.Equal(t, 0, exitCode, "warning-only lint should exit 0")

		var lintOut struct {
			ErrorCount int `json:"errorCount"`
			WarnCount  int `json:"warnCount"`
			Findings   []struct {
				RuleName string `json:"ruleName"`
				Severity string `json:"severity"`
				Category string `json:"category"`
				Location string `json:"location"`
				Line     int    `json:"line"`
			} `json:"findings"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &lintOut))
		assert.Equal(t, 0, lintOut.ErrorCount)
		assert.GreaterOrEqual(t, lintOut.WarnCount, 1)

		found := false
		for _, f := range lintOut.Findings {
			if f.RuleName == "exec-source-without-fail-on-error" {
				found = true
				assert.Equal(t, "warning", f.Severity)
				assert.Equal(t, "provider", f.Category)
				assert.Equal(t, "resolvers.token.resolve.with[0]", f.Location)
				assert.Positive(t, f.Line)
			}
		}
		assert.True(t, found, "expected exec-source-without-fail-on-error finding in %s", stdout)

		_, _, strictExit := runScafctl(t, "validate", "solution", "-f", solutionFile, "--strict")
		assert.NotEqual(t, 0, strictExit, "validate solution --strict should fail on exec-source-without-fail-on-error warning")
	})

	t.Run("passes cleanly when failOnError is true", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solutionFile := filepath.Join(tmpDir, "solution.yaml")

		solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exec-fallback-fixed
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: latest
    - name: env
      kind: provider
      version: latest
spec:
  resolvers:
    token:
      description: CLI token with environment variable fallback
      resolve:
        with:
          - provider: exec
            inputs:
              command: some-cli create token
              raw: true
              failOnError: true
          - provider: env
            inputs:
              operation: get
              name: FEDERATED_TOKEN
`
		require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o600))

		stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")
		assert.Equal(t, 0, exitCode)
		assert.NotContains(t, stdout, "exec-source-without-fail-on-error")
	})

	t.Run("lint rule explains exec-source-without-fail-on-error", func(t *testing.T) {
		t.Parallel()

		stdout, _, exitCode := runScafctl(t, "lint", "rule", "exec-source-without-fail-on-error", "-o", "json")
		assert.Equal(t, 0, exitCode)

		var ruleMeta struct {
			Rule        string   `json:"rule"`
			Severity    string   `json:"severity"`
			Category    string   `json:"category"`
			Description string   `json:"description"`
			Why         string   `json:"why"`
			Fix         string   `json:"fix"`
			Fixable     bool     `json:"fixable"`
			Examples    []string `json:"examples"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &ruleMeta))
		assert.Equal(t, "exec-source-without-fail-on-error", ruleMeta.Rule)
		assert.Equal(t, "warning", ruleMeta.Severity)
		assert.Equal(t, "provider", ruleMeta.Category)
		assert.False(t, ruleMeta.Fixable)
		assert.NotEmpty(t, ruleMeta.Description)
		assert.NotEmpty(t, ruleMeta.Why)
		assert.NotEmpty(t, ruleMeta.Fix)
		assert.NotEmpty(t, ruleMeta.Examples)
	})
}
