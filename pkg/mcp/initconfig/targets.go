// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package initconfig wires MCP server configurations and Agent Skills files
// across supported AI coding harnesses without external dependencies.
package initconfig

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// Target and Scope identify a supported AI coding harness and configuration level.
type (
	Target string
	Scope  string
)

// Supported harness targets and configuration scopes.
const (
	TargetVSCode   Target = "vscode"
	TargetClaude   Target = "claude"
	TargetCursor   Target = "cursor"
	TargetOpenCode Target = "opencode"
	TargetAGY      Target = "agy"
	TargetAll             = "all"
	TargetStandard        = "standard"
	ScopeProject   Scope  = "project"
	ScopeUser      Scope  = "user"
	DefaultScope          = ScopeProject
)

// Env supplies filesystem roots and platform context for deterministic path resolution.
type (
	Env struct {
		WorkDir, HomeDir, ConfigHome, AppData, GOOS string
	}
	harnessSpec struct {
		target                              Target
		markers                             []string
		projCfg, projSkill, userCfg, userSk string
	}
)

func resolveEnv(in Env) Env {
	wd, _ := os.Getwd()
	hd, _ := paths.HomeDir()
	out := Env{cmp.Or(in.WorkDir, wd), cmp.Or(in.HomeDir, hd), in.ConfigHome, in.AppData, cmp.Or(in.GOOS, runtime.GOOS)}
	envDir := func(cur, key string, absOnly bool, rel ...string) string {
		if v := os.Getenv(key); cur == "" && v != "" && in.HomeDir == "" && (!absOnly || filepath.IsAbs(v)) {
			return v
		}
		if cur != "" || out.HomeDir == "" {
			return cur
		}
		return filepath.Join(append([]string{out.HomeDir}, rel...)...)
	}
	out.ConfigHome = envDir(out.ConfigHome, paths.EnvConfigHome, true, ".config")
	out.AppData = envDir(out.AppData, "APPDATA", false, "AppData", "Roaming")
	return out
}

var harnesses = []harnessSpec{
	{TargetVSCode, []string{".vscode", ".github/skills", ".github/copilot-instructions.md"}, ".vscode/mcp.json", ".github", "Code/User/mcp.json", ".copilot"},
	{TargetClaude, []string{".mcp.json", ".claude", "CLAUDE.md"}, ".mcp.json", ".claude", ".claude.json", ".claude"},
	{TargetCursor, []string{".cursor", ".cursorrules"}, ".cursor/mcp.json", ".cursor", ".cursor/mcp.json", ".cursor"},
	{TargetOpenCode, []string{"opencode.jsonc", "opencode.json", ".opencode"}, "opencode.jsonc", ".opencode", "opencode/opencode.jsonc", "opencode"},
	{TargetAGY, []string{".agents"}, ".agents/mcp_config.json", ".agents", ".gemini/config/mcp_config.json", ".gemini/config"},
}

// AllTargets returns every supported harness target in canonical order.
func AllTargets() []Target {
	return []Target{TargetVSCode, TargetClaude, TargetCursor, TargetOpenCode, TargetAGY}
}

// ParseTargets validates and deduplicates raw target names, expanding "all".
func ParseTargets(raw []string) ([]Target, error) {
	out, seen := make([]Target, 0, len(raw)), make(map[Target]bool, len(raw))
	add := func(t Target) {
		if !seen[t] {
			seen[t], out = true, append(out, t)
		}
	}
	for _, item := range raw {
		for part := range strings.SplitSeq(item, ",") {
			name := Target(strings.ToLower(strings.TrimSpace(part)))
			switch {
			case name == "":
			case name == TargetAll:
				for _, h := range harnesses {
					add(h.target)
				}
			case slices.ContainsFunc(harnesses, func(h harnessSpec) bool { return h.target == name }):
				add(name)
			default:
				return nil, fmt.Errorf("invalid target %q (valid: vscode, claude, cursor, opencode, agy, all)", part)
			}
		}
	}
	return out, nil
}

// ParseScope validates a scope string, defaulting empty input to ScopeProject.
func ParseScope(raw string) (Scope, error) {
	switch Scope(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ScopeProject:
		return ScopeProject, nil
	case ScopeUser:
		return ScopeUser, nil
	default:
		return "", fmt.Errorf("invalid scope %q (valid: project, user)", raw)
	}
}

// DetectTargets inspects workDir for harness markers, defaulting to [TargetVSCode].
func DetectTargets(workDir string) []Target {
	root, err := os.OpenRoot(workDir)
	if workDir == "" || err != nil {
		return []Target{TargetVSCode}
	}
	defer func() { _ = root.Close() }()
	var detected []Target
	for _, h := range harnesses {
		if slices.ContainsFunc(h.markers, func(rel string) bool { _, e := root.Lstat(filepath.FromSlash(rel)); return e == nil }) {
			detected = append(detected, h.target)
		}
	}
	if len(detected) > 0 {
		return detected
	}
	return []Target{TargetVSCode}
}

type (
	entrySpec struct {
		parentKey, newFileSchema string
		entryVal                 map[string]any
		fieldKeys                []string
	}
	targetPlan struct {
		configPath, skillPath string
		spec                  entrySpec
	}
)

func serverEntrySpec(target Target, bin string) entrySpec {
	switch target { //nolint:exhaustive // Cursor, AGY, and standard share mcpServers command+args schema
	case TargetVSCode:
		return entrySpec{"servers", "", map[string]any{"type": "stdio", "command": bin, "args": []any{"mcp", "serve"}}, []string{"type", "command", "args"}}
	case TargetClaude:
		return entrySpec{"mcpServers", "", map[string]any{"type": "stdio", "command": bin, "args": []any{"mcp", "serve"}}, []string{"type", "command", "args"}}
	case TargetOpenCode:
		return entrySpec{"mcp", "https://opencode.ai/config.json", map[string]any{"type": "local", "command": []any{bin, "mcp", "serve"}, "enabled": true}, []string{"type", "command", "enabled"}}
	default:
		return entrySpec{"mcpServers", "", map[string]any{"command": bin, "args": []any{"mcp", "serve"}}, []string{"command", "args"}}
	}
}

func pickExistingRel(base, preferred, fallback string) string {
	if root, err := os.OpenRoot(base); base != "" && err == nil {
		defer func() { _ = root.Close() }()
		if _, pErr := root.Lstat(preferred); pErr != nil {
			if _, fErr := root.Lstat(fallback); fErr == nil {
				return fallback
			}
		}
	}
	return preferred
}

func resolveTargetPlan(t Target, scope Scope, bin, skillID string, env Env) (targetPlan, error) {
	idx := slices.IndexFunc(harnesses, func(h harnessSpec) bool { return h.target == t })
	if idx < 0 {
		return targetPlan{}, fmt.Errorf("unsupported target %q", t)
	}
	h, spec, skillLeaf := harnesses[idx], serverEntrySpec(t, bin), filepath.Join("skills", skillID, "SKILL.md")
	if scope == ScopeProject {
		if env.WorkDir == "" {
			return targetPlan{}, fmt.Errorf("working directory is required for project scope")
		}
		cfgRel := filepath.FromSlash(h.projCfg)
		if t == TargetOpenCode {
			cfgRel = pickExistingRel(env.WorkDir, "opencode.jsonc", "opencode.json")
		}
		return targetPlan{filepath.Join(env.WorkDir, cfgRel), filepath.Join(env.WorkDir, filepath.FromSlash(h.projSkill), skillLeaf), spec}, nil
	}
	if scope != ScopeUser || env.HomeDir == "" {
		return targetPlan{}, fmt.Errorf("home directory is required for %q scope", scope)
	}
	cfgBase, cfgRel := env.HomeDir, filepath.FromSlash(h.userCfg)
	skillBase, skillRel := env.HomeDir, filepath.Join(filepath.FromSlash(h.userSk), skillLeaf)
	switch t { //nolint:exhaustive // Claude and Cursor use layout defaults directly
	case TargetVSCode:
		switch env.GOOS {
		case "darwin":
			cfgRel = filepath.Join("Library", "Application Support", cfgRel)
		case "windows":
			if env.AppData == "" {
				return targetPlan{}, fmt.Errorf("APPDATA directory is required for user-scope vscode on windows")
			}
			cfgBase = env.AppData
		default:
			cfgBase = env.ConfigHome
		}
	case TargetOpenCode:
		cfgBase, skillBase, cfgRel = env.ConfigHome, env.ConfigHome, pickExistingRel(env.ConfigHome, filepath.Join("opencode", "opencode.jsonc"), filepath.Join("opencode", "opencode.json"))
	case TargetAGY:
		// Antigravity 2.0 uses ~/.gemini/config (https://antigravity.google/docs/mcp); fall back to ~/.gemini/antigravity if unmigrated.
		agyDir := pickExistingRel(env.HomeDir, filepath.Join(".gemini", "config"), filepath.Join(".gemini", "antigravity"))
		cfgRel, skillRel = filepath.Join(agyDir, "mcp_config.json"), filepath.Join(agyDir, skillLeaf)
	}
	return targetPlan{filepath.Join(cfgBase, cfgRel), filepath.Join(skillBase, skillRel), spec}, nil
}
