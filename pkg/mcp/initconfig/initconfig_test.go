// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package initconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func testEnv(t *testing.T, goos string) Env {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	home := filepath.Join(base, "home")
	cfg := filepath.Join(base, "xdg-config")
	appData := filepath.Join(base, "appdata")
	require.NoError(t, os.MkdirAll(work, 0o700))
	require.NoError(t, os.MkdirAll(home, 0o700))
	return Env{
		WorkDir:    work,
		HomeDir:    home,
		ConfigHome: cfg,
		AppData:    appData,
		GOOS:       goos,
	}
}

func TestParseTargetsAndScope(t *testing.T) {
	t.Parallel()

	t.Run("parses repeatable, comma-separated, and all targets", func(t *testing.T) {
		t.Parallel()
		got, err := ParseTargets([]string{"vscode,claude", "vscode", "all"})
		require.NoError(t, err)
		assert.Equal(t, AllTargets(), got)
	})

	t.Run("rejects unknown target", func(t *testing.T) {
		t.Parallel()
		_, err := ParseTargets([]string{"windsurf"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid target")
	})

	t.Run("parses valid scopes and rejects invalid", func(t *testing.T) {
		t.Parallel()
		s, err := ParseScope("")
		require.NoError(t, err)
		assert.Equal(t, ScopeProject, s)

		s, err = ParseScope("USER")
		require.NoError(t, err)
		assert.Equal(t, ScopeUser, s)

		_, err = ParseScope("global")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid scope")
	})
}

func TestDetectTargets(t *testing.T) {
	t.Parallel()

	t.Run("defaults to vscode on empty project and ignores bare .github", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o700))
		assert.Equal(t, []Target{TargetVSCode}, DetectTargets(dir))
	})

	t.Run("detects multiple harnesses from workspace markers", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "skills"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".cursorrules"), []byte("rules\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}\n"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents"), 0o700))

		assert.Equal(t, AllTargets(), DetectTargets(dir))
	})
}

func TestRun_AllTargetsProjectAndUserScopes(t *testing.T) {
	t.Parallel()

	t.Run("project scope writes all 5 targets and is idempotent on re-run", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		res, err := Run(context.Background(), Options{
			Targets: []string{"all"},
			Scope:   "project",
			Env:     env,
		})
		require.NoError(t, err)
		require.Len(t, res.Files, 10)
		for _, f := range res.Files {
			assert.Equal(t, StatusCreated, f.Status, "expected created for %s", f.Path)
			info, statErr := os.Lstat(f.Path)
			require.NoError(t, statErr)
			if runtime.GOOS != "windows" {
				assert.Equal(t, DefaultFilePerm, info.Mode().Perm())
			}
		}

		// Verify vscode, claude, opencode, and agy schemas.
		vscodeRaw, err := os.ReadFile(filepath.Join(env.WorkDir, ".vscode", "mcp.json"))
		require.NoError(t, err)
		var vscodeDoc map[string]any
		require.NoError(t, json.Unmarshal(vscodeRaw, &vscodeDoc))
		servers := vscodeDoc["servers"].(map[string]any)
		assert.Equal(t, map[string]any{
			"type":    "stdio",
			"command": "scafctl",
			"args":    []any{"mcp", "serve"},
		}, servers["scafctl"])

		claudeRaw, err := os.ReadFile(filepath.Join(env.WorkDir, ".mcp.json"))
		require.NoError(t, err)
		var claudeDoc map[string]any
		require.NoError(t, json.Unmarshal(claudeRaw, &claudeDoc))
		mcpServers := claudeDoc["mcpServers"].(map[string]any)
		assert.Equal(t, map[string]any{
			"type":    "stdio",
			"command": "scafctl",
			"args":    []any{"mcp", "serve"},
		}, mcpServers["scafctl"])

		opencodeRaw, err := os.ReadFile(filepath.Join(env.WorkDir, "opencode.jsonc"))
		require.NoError(t, err)
		var opencodeDoc map[string]any
		require.NoError(t, json.Unmarshal(opencodeRaw, &opencodeDoc))
		assert.Equal(t, "https://opencode.ai/config.json", opencodeDoc["$schema"])
		mcpMap := opencodeDoc["mcp"].(map[string]any)
		assert.Equal(t, map[string]any{
			"type":    "local",
			"command": []any{"scafctl", "mcp", "serve"},
			"enabled": true,
		}, mcpMap["scafctl"])

		// Second run must report unchanged for all 10 files.
		res2, err := Run(context.Background(), Options{
			Targets: []string{"all"},
			Scope:   "project",
			Backup:  true,
			Env:     env,
		})
		require.NoError(t, err)
		for _, f := range res2.Files {
			assert.Equal(t, StatusUnchanged, f.Status)
			assert.Empty(t, f.BackupPath)
		}
	})

	t.Run("user scope resolves OS-specific vscode and harness paths", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			goos       string
			wantSuffix string
		}{
			{goos: "darwin", wantSuffix: filepath.Join("home", "Library", "Application Support", "Code", "User", "mcp.json")},
			{goos: "windows", wantSuffix: filepath.Join("appdata", "Code", "User", "mcp.json")},
			{goos: "linux", wantSuffix: filepath.Join("xdg-config", "Code", "User", "mcp.json")},
		} {
			t.Run(tc.goos, func(t *testing.T) {
				t.Parallel()
				env := testEnv(t, tc.goos)
				res, err := Run(context.Background(), Options{
					Targets: []string{"all"},
					Scope:   "user",
					Env:     env,
				})
				require.NoError(t, err)
				require.Len(t, res.Files, 10)
				assert.True(t, strings.HasSuffix(res.Files[0].Path, tc.wantSuffix), "got %s", res.Files[0].Path)
				assert.Equal(t, filepath.Join(env.HomeDir, ".copilot", "skills", "scafctl", "SKILL.md"), res.Files[1].Path)
				assert.Equal(t, filepath.Join(env.HomeDir, ".claude.json"), res.Files[2].Path)
				assert.Equal(t, filepath.Join(env.ConfigHome, "opencode", "opencode.jsonc"), res.Files[6].Path)
				assert.Equal(t, filepath.Join(env.HomeDir, ".gemini", "config", "mcp_config.json"), res.Files[8].Path)
			})
		}

		// Legacy unmigrated Antigravity 1.x directory (~/.gemini/antigravity) is honored when ~/.gemini/config is absent.
		legacyEnv := testEnv(t, "linux")
		require.NoError(t, os.MkdirAll(filepath.Join(legacyEnv.HomeDir, ".gemini", "antigravity"), 0o700))
		legacyRes, err := Run(context.Background(), Options{
			Targets: []string{"agy"},
			Scope:   "user",
			Env:     legacyEnv,
		})
		require.NoError(t, err)
		require.Len(t, legacyRes.Files, 2)
		assert.Equal(t, filepath.Join(legacyEnv.HomeDir, ".gemini", "antigravity", "mcp_config.json"), legacyRes.Files[0].Path)
		assert.Equal(t, filepath.Join(legacyEnv.HomeDir, ".gemini", "antigravity", "skills", "scafctl", "SKILL.md"), legacyRes.Files[1].Path)
	})

	t.Run("opencode prefers existing opencode.json when opencode.jsonc is absent", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		existingPath := filepath.Join(env.WorkDir, "opencode.json")
		seed := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"skills\": {\n    \"paths\": [\".github/skills\"]\n  }\n}\n"
		require.NoError(t, os.WriteFile(existingPath, []byte(seed), 0o600))

		res, err := Run(context.Background(), Options{
			Targets: []string{"opencode"},
			Env:     env,
		})
		require.NoError(t, err)
		assert.Equal(t, existingPath, res.Files[0].Path)
		assert.Equal(t, StatusUpdated, res.Files[0].Status)
		_, statErr := os.Lstat(filepath.Join(env.WorkDir, "opencode.jsonc"))
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})
}

func TestJSONCUpsert_CommentsTrailingCommasAndMalformed(t *testing.T) {
	t.Parallel()

	t.Run("preserves comments, URL strings, key order, and inserts comma before inline comment", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		cfgPath := filepath.Join(env.WorkDir, "opencode.jsonc")
		initial := `{
  // Leading header comment with https://example.com//not-a-comment
  "$schema": "https://opencode.ai/config.json", /* block comment */
  "mcp": {
    "github": {
      "type": "local",
      "command": ["npx", "mcp-github"],
      "enabled": true,
    } // inline comment on previous server
  },
}
`
		require.NoError(t, os.WriteFile(cfgPath, []byte(initial), 0o600))

		res, err := Run(context.Background(), Options{
			Targets: []string{"opencode"},
			NoSkill: true,
			Backup:  true,
			Env:     env,
		})
		require.NoError(t, err)
		require.Len(t, res.Files, 1)
		assert.Equal(t, StatusUpdated, res.Files[0].Status)
		assert.Equal(t, cfgPath+".bak", res.Files[0].BackupPath)

		bakBytes, err := os.ReadFile(cfgPath + ".bak")
		require.NoError(t, err)
		assert.Equal(t, initial, string(bakBytes))
		if runtime.GOOS != "windows" {
			cfgInfo, statErr := os.Lstat(cfgPath)
			require.NoError(t, statErr)
			assert.Equal(t, DefaultFilePerm, cfgInfo.Mode().Perm())
			bakInfo, bakStatErr := os.Lstat(cfgPath + ".bak")
			require.NoError(t, bakStatErr)
			assert.Equal(t, DefaultFilePerm, bakInfo.Mode().Perm())
		}

		updated, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		text := string(updated)
		assert.Contains(t, text, "// Leading header comment with https://example.com//not-a-comment")
		assert.Contains(t, text, "/* block comment */")
		assert.Contains(t, text, "}, // inline comment on previous server")
		assert.Less(t, strings.Index(text, `"github"`), strings.Index(text, `"scafctl"`))
	})

	t.Run("updates stale existing server entry in place while preserving user fields and comments", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		vscodeDir := filepath.Join(env.WorkDir, ".vscode")
		require.NoError(t, os.MkdirAll(vscodeDir, 0o700))
		cfgPath := filepath.Join(vscodeDir, "mcp.json")
		seed := `{
  "servers": {
    "scafctl": {
      // Custom debug environment
      "command": "old-path",
      "env": {
        "SCAFCTL_LOG_LEVEL": "debug"
      },
      "cwd": "${workspaceFolder}"
    }
  }
}
`
		require.NoError(t, os.WriteFile(cfgPath, []byte(seed), 0o600))

		res, err := Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Env:     env,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusUpdated, res.Files[0].Status)

		raw, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		text := string(raw)
		assert.Contains(t, text, "// Custom debug environment")
		parsed, err := parseAndVerifyJSONC(raw, "servers")
		require.NoError(t, err)
		scafEntry := parsed["servers"].(map[string]any)["scafctl"].(map[string]any)
		assert.Equal(t, "stdio", scafEntry["type"])
		assert.Equal(t, "scafctl", scafEntry["command"])
		assert.Equal(t, []any{"mcp", "serve"}, scafEntry["args"])
		assert.Equal(t, map[string]any{"SCAFCTL_LOG_LEVEL": "debug"}, scafEntry["env"])
		assert.Equal(t, "${workspaceFolder}", scafEntry["cwd"])

		// Re-running when user fields (env, cwd) are present must report StatusUnchanged.
		res2, err := Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Env:     env,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusUnchanged, res2.Files[0].Status)

		// Existing remote Claude entry ("type": "http" or "sse") converts to "stdio" while preserving env/cwd and stays unchanged on re-run.
		claudeEnv := testEnv(t, "linux")
		claudeCfg := filepath.Join(claudeEnv.WorkDir, ".mcp.json")
		claudeSeed := `{
  "mcpServers": {
    "scafctl": {
      "type": "http",
      "env": {
        "SCAFCTL_LOG_LEVEL": "info"
      },
      "cwd": "${workspaceFolder}"
    }
  }
}
`
		require.NoError(t, os.WriteFile(claudeCfg, []byte(claudeSeed), 0o600))
		cRes1, err := Run(context.Background(), Options{
			Targets: []string{"claude"},
			NoSkill: true,
			Env:     claudeEnv,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusUpdated, cRes1.Files[0].Status)

		cRaw, err := os.ReadFile(claudeCfg)
		require.NoError(t, err)
		cParsed, err := parseAndVerifyJSONC(cRaw, "mcpServers")
		require.NoError(t, err)
		cEntry := cParsed["mcpServers"].(map[string]any)["scafctl"].(map[string]any)
		assert.Equal(t, "stdio", cEntry["type"])
		assert.Equal(t, "scafctl", cEntry["command"])
		assert.Equal(t, []any{"mcp", "serve"}, cEntry["args"])
		assert.Equal(t, map[string]any{"SCAFCTL_LOG_LEVEL": "info"}, cEntry["env"])
		assert.Equal(t, "${workspaceFolder}", cEntry["cwd"])

		cRes2, err := Run(context.Background(), Options{
			Targets: []string{"claude"},
			NoSkill: true,
			Env:     claudeEnv,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusUnchanged, cRes2.Files[0].Status)
	})

	t.Run("never clobbers malformed JSONC or non-object parent", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{
			"{bad json",
			"{,}\n",
			"{\"other\": [,], \"servers\": {}}\n",
			"{\"a\": tru/*x*/e}\n",
			"[\"top-level-array\"]\n",
			"{\"servers\": \"not-an-object\"}\n",
			"{\"a\": \"unterminated\n",
			"{ /* unterminated comment }",
		} {
			env := testEnv(t, "linux")
			vscodeDir := filepath.Join(env.WorkDir, ".vscode")
			require.NoError(t, os.MkdirAll(vscodeDir, 0o700))
			cfgPath := filepath.Join(vscodeDir, "mcp.json")
			require.NoError(t, os.WriteFile(cfgPath, []byte(bad), 0o600))

			_, err := Run(context.Background(), Options{
				Targets: []string{"vscode"},
				NoSkill: true,
				Env:     env,
			})
			require.ErrorIs(t, err, ErrInvalidConfig)

			after, readErr := os.ReadFile(cfgPath)
			require.NoError(t, readErr)
			assert.Equal(t, bad, string(after))
		}
	})
}

func TestDryRunSkillDirEmbedderAndSymlinkGuard(t *testing.T) {
	t.Parallel()

	t.Run("dry-run computes actions without writing files", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		res, err := Run(context.Background(), Options{
			Targets: []string{"vscode"},
			DryRun:  true,
			Env:     env,
		})
		require.NoError(t, err)
		assert.True(t, res.DryRun)
		require.Len(t, res.Files, 2)
		for _, f := range res.Files {
			assert.Equal(t, StatusCreated, f.Status)
			_, statErr := os.Lstat(f.Path)
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		}
	})

	t.Run("custom --skill-dir alone writes only custom skill with embedder binary name and supplement", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: "mycli"})
		ctx = WithSkillSupplement(ctx, "## Platform Notes\n\nUse `mycli auth login` before catalog commands.")

		res, err := Run(ctx, Options{
			SkillDir: ".windsurf/skills",
			Env:      env,
		})
		require.NoError(t, err)
		require.Len(t, res.Files, 1)
		wantPath := filepath.Join(env.WorkDir, ".windsurf", "skills", "mycli", "SKILL.md")
		assert.Equal(t, wantPath, res.Files[0].Path)

		skillRaw, err := os.ReadFile(wantPath)
		require.NoError(t, err)
		skillStr := string(skillRaw)
		assert.Contains(t, skillStr, `name: "mycli"`)
		assert.Contains(t, skillStr, "## Platform Notes")
		assert.NotContains(t, skillStr, "scafctl")
	})

	t.Run("print renders standard and target-specific output without touching disk", func(t *testing.T) {
		t.Parallel()
		out, err := RenderPrint(context.Background(), Options{
			BinaryName: "mycli",
		})
		require.NoError(t, err)
		assert.Equal(t, TargetStandard, out.Target)
		assert.Equal(t, "mycli", out.ServerKey)
		assert.Contains(t, out.Text(), "# MCP Configuration")
		assert.Contains(t, out.Text(), "# Agent Skill (SKILL.md)")

		noSkillOut, err := RenderPrint(context.Background(), Options{
			Targets: []string{"opencode"},
			NoSkill: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "opencode", noSkillOut.Target)
		assert.Empty(t, noSkillOut.Skill)
		assert.NotContains(t, noSkillOut.Text(), "# Agent Skill")
		assert.Contains(t, noSkillOut.Text(), `"https://opencode.ai/config.json"`)
	})

	t.Run("rejects symlinked config file, parent dir, and backup file", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("symlink privileges vary on windows")
		}
		env := testEnv(t, "linux")
		outside := filepath.Join(t.TempDir(), "secret.json")
		require.NoError(t, os.WriteFile(outside, []byte("{}\n"), 0o600))

		vscodeDir := filepath.Join(env.WorkDir, ".vscode")
		require.NoError(t, os.MkdirAll(vscodeDir, 0o700))
		require.NoError(t, os.Symlink(outside, filepath.Join(vscodeDir, "mcp.json")))

		_, err := Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Env:     env,
		})
		require.ErrorIs(t, err, ErrSymlink)

		// Symlinked parent directory inside workDir
		env2 := testEnv(t, "linux")
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(env2.WorkDir, ".vscode")))
		_, err = Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Env:     env2,
		})
		require.ErrorIs(t, err, ErrSymlink)

		// Symlinked .bak file on update and dry-run
		env3 := testEnv(t, "linux")
		v3 := filepath.Join(env3.WorkDir, ".vscode")
		require.NoError(t, os.MkdirAll(v3, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(v3, "mcp.json"), []byte("{}\n"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(v3, "mcp.json.bak")))
		_, err = Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Backup:  true,
			DryRun:  true,
			Env:     env3,
		})
		require.ErrorIs(t, err, ErrSymlink)
		_, err = Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Backup:  true,
			Env:     env3,
		})
		require.ErrorIs(t, err, ErrSymlink)

		// Symlinked ancestor on both missing and existing absolute --skill-dir paths
		env4 := testEnv(t, "linux")
		absContainer := t.TempDir()
		redirected := t.TempDir()
		require.NoError(t, os.Symlink(redirected, filepath.Join(absContainer, "linked")))
		require.NoError(t, os.Symlink(redirected, filepath.Join(env4.WorkDir, "linked")))

		for _, absSkillDir := range []string{
			filepath.Join(absContainer, "linked", "missing-skills"),
			filepath.Join(env4.WorkDir, "linked", "missing-skills"),
		} {
			_, err = Run(context.Background(), Options{
				SkillDir: absSkillDir,
				Env:      env4,
			})
			require.ErrorIs(t, err, ErrSymlink)
			assert.NoDirExists(t, filepath.Join(redirected, "missing-skills"))
		}

		existingTarget := filepath.Join(redirected, "existing-skills", "scafctl")
		require.NoError(t, os.MkdirAll(existingTarget, 0o700))
		sentinelPath := filepath.Join(existingTarget, "SKILL.md")
		require.NoError(t, os.WriteFile(sentinelPath, []byte("keep-original\n"), 0o600))
		for _, absSkillDir := range []string{
			filepath.Join(absContainer, "linked", "existing-skills"),
			filepath.Join(env4.WorkDir, "linked", "existing-skills"),
		} {
			_, err = Run(context.Background(), Options{
				SkillDir: absSkillDir,
				Env:      env4,
			})
			require.ErrorIs(t, err, ErrSymlink)
		}
		sentinelAfter, readErr := os.ReadFile(sentinelPath)
		require.NoError(t, readErr)
		assert.Equal(t, "keep-original\n", string(sentinelAfter))

		// Real project WorkDir nested beneath a symlinked parent directory (e.g. ~/code -> /mnt/code) succeeds
		realCodeRoot := filepath.Join(t.TempDir(), "app")
		require.NoError(t, os.MkdirAll(realCodeRoot, 0o700))
		require.NoError(t, os.Symlink(filepath.Dir(realCodeRoot), filepath.Join(env4.HomeDir, "code")))
		linkedWorkEnv := env4
		linkedWorkEnv.WorkDir = filepath.Join(env4.HomeDir, "code", "app")
		resLinkedWork, err := Run(context.Background(), Options{
			Targets: []string{"vscode"},
			NoSkill: true,
			Env:     linkedWorkEnv,
		})
		require.NoError(t, err)
		assert.Equal(t, StatusCreated, resLinkedWork.Files[0].Status)
	})

	t.Run("rejects mutually exclusive and escaping flags", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, "linux")
		_, err := Run(context.Background(), Options{NoSkill: true, SkillDir: "skills", Env: env})
		require.Error(t, err)

		_, err = Run(context.Background(), Options{SkillDir: "../outside", Env: env})
		require.Error(t, err)

		_, err = RenderPrint(context.Background(), Options{Targets: []string{"vscode", "claude"}})
		require.Error(t, err)
	})

	t.Run("inserts parent key into existing JSON/JSONC layouts and handles absolute skill-dir", func(t *testing.T) {
		t.Parallel()
		for name, seed := range map[string]string{
			"empty-braces":          "{}\n",
			"comment-only-object":   "{\n  // existing note\n}\n",
			"jsdoc-header-comment":  "/*\n * Header comment\n */\n{\n  \"other\": 1\n}\n",
			"single-line-object":    `{"numStartups": 3}`,
			"tab-indented-neighbor": "{\n\t\"mcpServers\": {\n\t\t\"other\": {\n\t\t\t\"command\": \"x\"\n\t\t} /* inline block */\n\t}\n}\n",
			"slash-star-in-line":    "{\n  \"other\": 1 // match /* glob\n  // trailing */\n}\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := testEnv(t, "linux")
				cfgPath := filepath.Join(env.WorkDir, ".mcp.json")
				require.NoError(t, os.WriteFile(cfgPath, []byte(seed), 0o600))

				res, err := Run(context.Background(), Options{
					Targets: []string{"claude"},
					NoSkill: true,
					Env:     env,
				})
				require.NoError(t, err)
				assert.Equal(t, StatusUpdated, res.Files[0].Status)

				after, err := os.ReadFile(cfgPath)
				require.NoError(t, err)
				parsed, err := parseAndVerifyJSONC(after, "mcpServers")
				require.NoError(t, err)
				assert.Contains(t, parsed["mcpServers"], "scafctl")
			})
		}

		// Absolute --skill-dir and current-dir "." --skill-dir
		absSkillDir := filepath.Join(t.TempDir(), "custom-skills")
		env := testEnv(t, "linux")
		res, err := Run(context.Background(), Options{
			SkillDir: absSkillDir,
			Env:      env,
		})
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(absSkillDir, "scafctl", "SKILL.md"))
		assert.Equal(t, StatusCreated, res.Files[0].Status)

		resDot, err := Run(context.Background(), Options{
			SkillDir: ".",
			Env:      env,
		})
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(env.WorkDir, "scafctl", "SKILL.md"))
		assert.Equal(t, StatusCreated, resDot.Files[0].Status)

		// Explicit --target combined with --skill-dir writes target config + custom skill path only
		envCombo := testEnv(t, "linux")
		resCombo, err := Run(context.Background(), Options{
			Targets:  []string{"cursor"},
			SkillDir: ".agents/skills",
			Env:      envCombo,
		})
		require.NoError(t, err)
		require.Len(t, resCombo.Files, 2)
		assert.Equal(t, filepath.Join(envCombo.WorkDir, ".cursor", "mcp.json"), resCombo.Files[0].Path)
		assert.Equal(t, filepath.Join(envCombo.WorkDir, ".agents", "skills", "scafctl", "SKILL.md"), resCombo.Files[1].Path)
		assert.NoFileExists(t, filepath.Join(envCombo.WorkDir, ".cursor", "skills", "scafctl", "SKILL.md"))
	})
}

func TestResolveEnv_UnsetXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	resolved := resolveEnv(Env{})
	require.NotEmpty(t, resolved.HomeDir)
	assert.Equal(t, filepath.Join(resolved.HomeDir, ".config"), resolved.ConfigHome)
}

func TestBinaryNormalizationAndSkillIdentifier(t *testing.T) {
	t.Parallel()

	t.Run("preserves dotted and context-resolved binary names without double-stripping suffixes", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			raw     string
			wantBin string
			wantID  string
		}{
			{raw: "my.cli", wantBin: "my.cli", wantID: "my-cli"},
			{raw: "my.cli.exe", wantBin: "my.cli", wantID: "my-cli"},
			{raw: "my.cmd.exe", wantBin: "my.cmd", wantID: "my-cmd"},
			{raw: ".mycli.exe", wantBin: ".mycli", wantID: "mycli"},
		} {
			env := testEnv(t, "linux")
			res, err := Run(context.Background(), Options{
				Targets:    []string{"vscode"},
				BinaryName: tc.raw,
				Env:        env,
			})
			require.NoError(t, err)
			require.Len(t, res.Files, 2)

			cfgRaw, err := os.ReadFile(filepath.Join(env.WorkDir, ".vscode", "mcp.json"))
			require.NoError(t, err)
			assert.Contains(t, string(cfgRaw), `"`+tc.wantBin+`": {`)
			assert.Contains(t, string(cfgRaw), `"command": "`+tc.wantBin+`"`)

			skillPath := filepath.Join(env.WorkDir, ".github", "skills", tc.wantID, "SKILL.md")
			skillRaw, err := os.ReadFile(skillPath)
			require.NoError(t, err)
			assert.Contains(t, string(skillRaw), `name: "`+tc.wantID+`"`)
			assert.Contains(t, string(skillRaw), tc.wantBin+" run action")
		}

		// Context-resolved BinaryName (already sanitized once by Root) must not strip .cmd or leading dot again.
		for _, tc := range []struct {
			ctxBin string
			wantID string
		}{
			{ctxBin: "my.cmd", wantID: "my-cmd"},
			{ctxBin: ".mycli", wantID: "mycli"},
		} {
			ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: tc.ctxBin})
			printOut, err := RenderPrint(ctx, Options{})
			require.NoError(t, err)
			assert.Equal(t, tc.ctxBin, printOut.ServerKey)
			assert.Contains(t, printOut.Skill, `name: "`+tc.wantID+`"`)
			assert.Contains(t, printOut.Skill, tc.ctxBin+" run action")
		}

		directSkill := RenderSkill("my.cli.exe", "")
		assert.Contains(t, directSkill, `name: "my-cli"`)
		assert.Contains(t, directSkill, "my.cli run action")

		printOut, err := RenderPrint(context.Background(), Options{BinaryName: "my.cli"})
		require.NoError(t, err)
		assert.Equal(t, "my.cli", printOut.ServerKey)
		assert.Contains(t, printOut.Skill, `name: "my-cli"`)
		assert.Contains(t, printOut.Skill, "my.cli run action")
	})

	t.Run("derives valid Agent Skills identifier and preserves YAML string frontmatter types", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "my-cli", SkillName("MY_CLI"))
		assert.Equal(t, "my-cmd", SkillName("my.cmd"))
		assert.Equal(t, "mycli", SkillName(".mycli"))
		assert.Equal(t, "my-cli", SkillName("__MY--..CLI__"))
		assert.Equal(t, "scafctl", SkillName("___...---"))
		assert.Equal(t, "scafctl", cleanBin("___...---", true))
		assert.Equal(t, "my.cli", cleanBin("my.cli.exe", true))
		assert.Equal(t, "my.cmd", cleanBin("my.cmd.exe", true))
		assert.Equal(t, ".mycli", cleanBin(".mycli.exe", true))

		longName := strings.Repeat("a", 63) + "_tail"
		gotLong := SkillName(longName)
		assert.LessOrEqual(t, len(gotLong), 64)
		assert.False(t, strings.HasSuffix(gotLong, "-"))

		// YAML frontmatter round-trip: boolean/numeric-looking names must unmarshal as strings.
		for _, tc := range []struct {
			rawBin string
			wantID string
		}{
			{rawBin: "TRUE", wantID: "true"},
			{rawBin: "FALSE", wantID: "false"},
			{rawBin: "123", wantID: "123"},
			{rawBin: ".mycli", wantID: "mycli"},
			{rawBin: "MY_CLI", wantID: "my-cli"},
		} {
			rendered := RenderSkill(tc.rawBin, "")
			rest, ok := strings.CutPrefix(rendered, "---\n")
			require.True(t, ok)
			fmBlock, _, ok := strings.Cut(rest, "\n---\n")
			require.True(t, ok)
			var fm map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(fmBlock), &fm))
			nameStr, isString := fm["name"].(string)
			require.True(t, isString, "expected string frontmatter name for %q, got %T (%v)", tc.rawBin, fm["name"], fm["name"])
			assert.Equal(t, tc.wantID, nameStr)
			descStr, descIsString := fm["description"].(string)
			require.True(t, descIsString)
			assert.NotEmpty(t, descStr)
		}

		env := testEnv(t, "linux")
		_, err := Run(context.Background(), Options{
			Targets:    []string{"claude"},
			BinaryName: "MY_CLI",
			Env:        env,
		})
		require.NoError(t, err)

		cfgRaw, err := os.ReadFile(filepath.Join(env.WorkDir, ".mcp.json"))
		require.NoError(t, err)
		assert.Contains(t, string(cfgRaw), `"MY_CLI": {`)
		assert.Contains(t, string(cfgRaw), `"command": "MY_CLI"`)

		skillRaw, err := os.ReadFile(filepath.Join(env.WorkDir, ".claude", "skills", "my-cli", "SKILL.md"))
		require.NoError(t, err)
		assert.Contains(t, string(skillRaw), `name: "my-cli"`)
		assert.Contains(t, string(skillRaw), "MY_CLI run action")
	})
}
