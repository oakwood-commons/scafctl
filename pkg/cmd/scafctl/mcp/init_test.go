// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl"
	mcpcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/mcp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/mcp/initconfig"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInit_FlagsAndValidation(t *testing.T) {
	t.Parallel()

	t.Run("registers expected flags and mutual exclusion", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &out, &errOut, false)
		cmd := mcpcmd.CommandInit(&settings.Run{BinaryName: "scafctl"}, ioStreams, "scafctl/mcp")

		for _, flagName := range []string{"target", "scope", "print", "skill-dir", "no-skill", "dry-run", "backup", "output"} {
			assert.NotNil(t, cmd.Flags().Lookup(flagName), "missing flag --%s", flagName)
		}

		cmd.SetArgs([]string{"--no-skill", "--skill-dir", ".windsurf/skills"})
		cmd.SilenceErrors = true
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "none of the others can be")
	})

	t.Run("rejects invalid output format, scope, and target in PreRunE", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{
			{"-o", "mermaid"},
			{"--scope", "bogus"},
			{"--target", "unknown"},
			{"--print", "--skill-dir", "custom"},
		} {
			var out, errOut bytes.Buffer
			ioStreams := terminal.NewIOStreams(nil, &out, &errOut, false)
			cmd := mcpcmd.CommandInit(&settings.Run{BinaryName: "scafctl"}, ioStreams, "scafctl/mcp")
			cmd.SetArgs(args)
			cmd.SilenceErrors = true
			require.Error(t, cmd.Execute(), "expected error for args %v", args)
		}
	})
}

func TestCommandInit_ExecutionAndOutputFormats(t *testing.T) {
	t.Parallel()

	t.Run("writes project files in working directory and supports -o json and -o yaml", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		var out, errOut bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &out, &errOut, false)
		cliParams := &settings.Run{BinaryName: "scafctl", NoColor: true}

		cmd := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmd.SetArgs([]string{"-t", "vscode", "-o", "json"})
		err := cmd.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir))
		require.NoError(t, err)

		var res initconfig.Result
		require.NoError(t, json.Unmarshal(out.Bytes(), &res))
		assert.Equal(t, initconfig.ScopeProject, res.Scope)
		require.Len(t, res.Files, 2)
		assert.Equal(t, initconfig.StatusCreated, res.Files[0].Status)

		// Second run in auto format prints unchanged status lines.
		out.Reset()
		cmd2 := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmd2.SetArgs([]string{"-t", "vscode"})
		require.NoError(t, cmd2.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), "unchanged")

		// --print with -o yaml and -o quiet
		out.Reset()
		cmdPrint := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdPrint.SetArgs([]string{"--print", "-t", "claude", "-o", "yaml"})
		require.NoError(t, cmdPrint.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), "serverKey: scafctl")

		out.Reset()
		cmdQuiet := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdQuiet.SetArgs([]string{"-t", "vscode", "--dry-run", "-o", "quiet"})
		require.NoError(t, cmdQuiet.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Empty(t, out.String())

		// Dry-run and backup human output formatting + print JSON format
		out.Reset()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, ".mcp.json"), []byte("{\n  \"other\": true\n}\n"), 0o600))
		cmdDry := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdDry.SetArgs([]string{"-t", "claude", "--dry-run", "--backup"})
		require.NoError(t, cmdDry.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), "[dry-run] updated")
		assert.Contains(t, out.String(), "[backup:")

		out.Reset()
		cmdBackup := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdBackup.SetArgs([]string{"-t", "claude", "--backup"})
		require.NoError(t, cmdBackup.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), "updated")
		assert.Contains(t, out.String(), "[backup:")

		out.Reset()
		cmdPrintJSON := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdPrintJSON.SetArgs([]string{"--print", "-o", "json"})
		require.NoError(t, cmdPrintJSON.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), `"serverKey": "scafctl"`)

		out.Reset()
		cmdPrintPlain := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdPrintPlain.SetArgs([]string{"--print", "--no-skill"})
		require.NoError(t, cmdPrintPlain.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Contains(t, out.String(), `"mcpServers"`)

		out.Reset()
		cmdPrintQuiet := mcpcmd.CommandInit(cliParams, ioStreams, "scafctl/mcp")
		cmdPrintQuiet.SetArgs([]string{"--print", "-o", "quiet"})
		require.NoError(t, cmdPrintQuiet.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		assert.Empty(t, out.String())

		// Non-print -o yaml and context BinaryName fallback when cliParams.BinaryName is empty
		out.Reset()
		cmdYAML := mcpcmd.CommandInit(&settings.Run{NoColor: true}, ioStreams, "scafctl/mcp")
		cmdYAML.SetArgs([]string{"-t", "cursor", "-o", "yaml"})
		ctxWithCustomBin := settings.IntoContext(provider.WithWorkingDirectory(t.Context(), workDir), &settings.Run{BinaryName: "ctxcli", NoColor: true})
		require.NoError(t, cmdYAML.ExecuteContext(ctxWithCustomBin))
		assert.Contains(t, out.String(), "scope: project")
		assert.FileExists(t, filepath.Join(workDir, ".cursor", "skills", "ctxcli", "SKILL.md"))
	})

	t.Run("handles domain errors cleanly with structured exit codes", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		var out, errOut bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &out, &errOut, false)

		// Multiple targets with --print errors in RenderPrint with InvalidInput
		cmdMultiPrint := mcpcmd.CommandInit(nil, ioStreams, "scafctl/mcp")
		cmdMultiPrint.SetArgs([]string{"--print", "-t", "vscode", "-t", "claude"})
		cmdMultiPrint.SilenceErrors = true
		err := cmdMultiPrint.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir))
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))

		// Malformed config returns ConfigError
		require.NoError(t, os.WriteFile(filepath.Join(workDir, ".mcp.json"), []byte("{bad"), 0o600))
		cmdBadCfg := mcpcmd.CommandInit(&settings.Run{BinaryName: "scafctl", NoColor: true}, ioStreams, "scafctl/mcp")
		cmdBadCfg.SetArgs([]string{"-t", "claude"})
		cmdBadCfg.SilenceErrors = true
		err = cmdBadCfg.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir))
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))

		// Escaping relative --skill-dir returns GeneralError
		cmdEscape := mcpcmd.CommandInit(&settings.Run{BinaryName: "scafctl", NoColor: true}, ioStreams, "scafctl/mcp")
		cmdEscape.SetArgs([]string{"--skill-dir", "../escape"})
		cmdEscape.SilenceErrors = true
		err = cmdEscape.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir))
		require.Error(t, err)
		assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
	})

	t.Run("Root wires BinaryName and MCPSkillSupplement for embedder CLI", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		cfgDir := t.TempDir()
		cfgFile := filepath.Join(cfgDir, "config.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte("settings:\n  noColor: true\n"), 0o600))

		var out, errOut bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &out, &errOut, false)

		supplement := "## Custom Embedder Guidance\n\nAlways validate platform tags with `mycli lint`."
		rootCmd, cleanup := scafctl.Root(&scafctl.RootOptions{
			BinaryName:         "mycli",
			ConfigPath:         cfgFile,
			IOStreams:          ioStreams,
			MCPSkillSupplement: supplement,
			ExitFunc:           func(_ int) {},
		})
		defer cleanup()

		rootCmd.SetArgs([]string{"-C", workDir, "mcp", "init", "--target", "vscode", "--target", "opencode"})
		require.NoError(t, rootCmd.Execute())

		vscodeCfg, err := os.ReadFile(filepath.Join(workDir, ".vscode", "mcp.json"))
		require.NoError(t, err)
		assert.Contains(t, string(vscodeCfg), `"mycli": {`)
		assert.Contains(t, string(vscodeCfg), `"command": "mycli"`)
		assert.NotContains(t, string(vscodeCfg), "scafctl")

		opencodeCfg, err := os.ReadFile(filepath.Join(workDir, "opencode.jsonc"))
		require.NoError(t, err)
		var ocDoc map[string]any
		require.NoError(t, json.Unmarshal(opencodeCfg, &ocDoc))
		assert.Equal(t, []any{"mycli", "mcp", "serve"}, ocDoc["mcp"].(map[string]any)["mycli"].(map[string]any)["command"])

		skillPath := filepath.Join(workDir, ".github", "skills", "mycli", "SKILL.md")
		skillRaw, err := os.ReadFile(skillPath)
		require.NoError(t, err)
		assert.Contains(t, string(skillRaw), `name: "mycli"`)
		assert.Contains(t, string(skillRaw), "## Custom Embedder Guidance")
		assert.NotContains(t, string(skillRaw), "scafctl")

		// Dotted and leading-dot executable names preserve resolved binary name and derive valid skill IDs.
		for _, tc := range []struct {
			rawBin    string
			wantBin   string
			wantSkill string
		}{
			{rawBin: "my.cli.exe", wantBin: "my.cli", wantSkill: "my-cli"},
			{rawBin: "my.cmd.exe", wantBin: "my.cmd", wantSkill: "my-cmd"},
			{rawBin: ".mycli.exe", wantBin: ".mycli", wantSkill: "mycli"},
		} {
			caseDir := t.TempDir()
			caseRoot, caseCleanup := scafctl.Root(&scafctl.RootOptions{
				BinaryName: tc.rawBin,
				ConfigPath: cfgFile,
				IOStreams:  ioStreams,
				ExitFunc:   func(_ int) {},
			})
			defer caseCleanup()

			caseRoot.SetArgs([]string{"-C", caseDir, "mcp", "init", "--target", "vscode"})
			require.NoError(t, caseRoot.Execute())

			caseCfg, err := os.ReadFile(filepath.Join(caseDir, ".vscode", "mcp.json"))
			require.NoError(t, err)
			assert.Contains(t, string(caseCfg), `"`+tc.wantBin+`": {`)
			assert.Contains(t, string(caseCfg), `"command": "`+tc.wantBin+`"`)

			caseSkill, err := os.ReadFile(filepath.Join(caseDir, ".github", "skills", tc.wantSkill, "SKILL.md"))
			require.NoError(t, err)
			assert.Contains(t, string(caseSkill), `name: "`+tc.wantSkill+`"`)
			assert.Contains(t, string(caseSkill), tc.wantBin+" run action")
		}
	})
}
