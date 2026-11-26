// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_MCPInit(t *testing.T) {
	t.Parallel()

	t.Run("auto-detects markers, preserves JSONC neighbors, and skips idempotently", func(t *testing.T) {
		t.Parallel()
		env := isolatedCatalogEnv(t)
		workDir := t.TempDir()

		// Seed existing opencode.jsonc with comments and a neighbor server.
		seedOpenCode := `{
  // Keep user comment
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "neighbor": {
      "type": "local",
      "command": ["npx", "neighbor-mcp"],
      "enabled": true,
    } // inline comment
  }
}
`
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "opencode.jsonc"), []byte(seedOpenCode), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, ".vscode"), 0o700))

		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--backup", "-o", "json")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)

		var result struct {
			Scope   string   `json:"scope"`
			Targets []string `json:"targets"`
			Files   []struct {
				Target     string `json:"target"`
				Kind       string `json:"kind"`
				Path       string `json:"path"`
				Status     string `json:"status"`
				BackupPath string `json:"backupPath"`
			} `json:"files"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		assert.Equal(t, "project", result.Scope)
		assert.Equal(t, []string{"vscode", "opencode"}, result.Targets)
		require.Len(t, result.Files, 4)

		openCodeAfter, err := os.ReadFile(filepath.Join(workDir, "opencode.jsonc"))
		require.NoError(t, err)
		assert.Contains(t, string(openCodeAfter), "// Keep user comment")
		assert.Contains(t, string(openCodeAfter), `"neighbor"`)
		assert.Contains(t, string(openCodeAfter), `"scafctl"`)
		assert.FileExists(t, filepath.Join(workDir, "opencode.jsonc.bak"))
		assert.FileExists(t, filepath.Join(workDir, ".github", "skills", "scafctl", "SKILL.md"))

		// Second run is idempotent (all unchanged).
		stdout2, stderr2, exitCode2 := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "-o", "json")
		require.Equal(t, 0, exitCode2, "stderr: %s", stderr2)
		require.NoError(t, json.Unmarshal([]byte(stdout2), &result))
		for _, f := range result.Files {
			assert.Equal(t, "unchanged", f.Status)
		}
	})

	t.Run("supports --dry-run, --print, --skill-dir, and user scope isolation", func(t *testing.T) {
		t.Parallel()
		env := isolatedCatalogEnv(t)
		fakeHome := t.TempDir()
		env["HOME"] = fakeHome
		env["USERPROFILE"] = fakeHome
		env["SCAFCTL_SECRET_KEY"] = "integration-test-secret-key"
		workDir := t.TempDir()

		// --help documents flags
		helpOut, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--help")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.Contains(t, helpOut, "--skill-dir")
		assert.Contains(t, helpOut, "--no-skill")

		// --dry-run writes nothing
		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--target", "claude", "--dry-run")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.Contains(t, stdout, "[dry-run] created")
		assert.NoFileExists(t, filepath.Join(workDir, ".mcp.json"))

		// --no-skill writes MCP config without SKILL.md
		_, stderr, exitCode = runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--target", "cursor", "--no-skill")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.FileExists(t, filepath.Join(workDir, ".cursor", "mcp.json"))
		assert.NoFileExists(t, filepath.Join(workDir, ".cursor", "skills", "scafctl", "SKILL.md"))

		// --print emits JSON + SKILL.md without creating files
		printOut, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--print")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.Contains(t, printOut, `"mcpServers"`)
		assert.Contains(t, printOut, `name: "scafctl"`)
		assert.NoFileExists(t, filepath.Join(workDir, ".vscode", "mcp.json"))

		// --skill-dir writes custom SKILL.md only
		_, stderr, exitCode = runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--skill-dir", ".windsurf/skills")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.FileExists(t, filepath.Join(workDir, ".windsurf", "skills", "scafctl", "SKILL.md"))

		// --scope user writes into fakeHome (and ~/.config/opencode when XDG_CONFIG_HOME is unset)
		_, stderr, exitCode = runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--target", "agy", "--scope", "user")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.FileExists(t, filepath.Join(fakeHome, ".gemini", "config", "mcp_config.json"))
		assert.FileExists(t, filepath.Join(fakeHome, ".gemini", "config", "skills", "scafctl", "SKILL.md"))

		noXDGEnv := map[string]string{
			"HOME":               fakeHome,
			"USERPROFILE":        fakeHome,
			"XDG_DATA_HOME":      env["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":     env["XDG_CACHE_HOME"],
			"SCAFCTL_SECRET_KEY": "integration-test-secret-key",
		}
		cfgFlag := filepath.Join(env["XDG_CONFIG_HOME"], "scafctl", "config.yaml")
		_, stderr, exitCode = runScafctlIsolatedEnv(t, []string{"XDG_CONFIG_HOME"}, noXDGEnv, "-C", workDir, "--config", cfgFlag, "mcp", "init", "--target", "opencode", "--scope", "user")
		require.Equal(t, 0, exitCode, "stderr: %s", stderr)
		assert.FileExists(t, filepath.Join(fakeHome, ".config", "opencode", "opencode.jsonc"))
		assert.FileExists(t, filepath.Join(fakeHome, ".config", "opencode", "skills", "scafctl", "SKILL.md"))
	})

	t.Run("fails safely on malformed existing config without overwriting", func(t *testing.T) {
		t.Parallel()
		env := isolatedCatalogEnv(t)
		workDir := t.TempDir()
		badFile := filepath.Join(workDir, ".mcp.json")
		require.NoError(t, os.WriteFile(badFile, []byte("{broken"), 0o600))

		_, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env, "mcp", "init", "--target", "claude")
		assert.Equal(t, exitcode.ConfigError, exitCode)
		assert.Contains(t, stderr, "invalid MCP configuration file")

		raw, err := os.ReadFile(badFile)
		require.NoError(t, err)
		assert.Equal(t, "{broken", string(raw))
	})
}
