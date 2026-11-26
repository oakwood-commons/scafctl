// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package initconfig

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ErrSymlink is returned when a target path, parent component, or .bak path is a symbolic link.
var (
	//go:embed skill_template.md
	embeddedSkillTemplate string
	ErrSymlink            = errors.New("refusing to follow symbolic link")
	unsafeBinRe           = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	skillSplitRe          = regexp.MustCompile(`[^a-z0-9]+`)
)

// Artifact kind, status, and permission constants.
const (
	KindConfig                  = "config"
	KindSkill                   = "skill"
	StatusCreated               = "created"
	StatusUpdated               = "updated"
	StatusUnchanged             = "unchanged"
	DefaultFilePerm fs.FileMode = 0o600
	DefaultDirPerm  fs.FileMode = 0o700
)

type skillSupplementContextKey struct{}

// WithSkillSupplement attaches optional embedder-supplied markdown to append to SKILL.md.
func WithSkillSupplement(ctx context.Context, supplement string) context.Context {
	return context.WithValue(ctx, skillSupplementContextKey{}, supplement)
}

// SkillSupplementFromContext retrieves the embedder-supplied skill supplement from ctx.
func SkillSupplementFromContext(ctx context.Context) (val string) {
	if ctx != nil {
		val, _ = ctx.Value(skillSupplementContextKey{}).(string)
	}
	return val
}

// Options, FileAction, Result, and PrintOutput model initconfig inputs and structured outputs.
type (
	Options struct {
		Targets                     []string
		Scope, SkillDir             string
		NoSkill, DryRun, Backup     bool
		BinaryName, SkillSupplement string
		Env                         Env
	}
	FileAction struct {
		Target     Target `json:"target,omitempty" yaml:"target,omitempty" doc:"Target harness identifier" maxLength:"32" example:"vscode"`
		Kind       string `json:"kind" yaml:"kind" doc:"Artifact kind (config or skill)" maxLength:"16" example:"config"`
		Path       string `json:"path" yaml:"path" doc:"Filesystem path inspected or written" maxLength:"4096" example:".vscode/mcp.json"`
		Status     string `json:"status" yaml:"status" doc:"Outcome status (created, updated, or unchanged)" maxLength:"16" example:"created"`
		BackupPath string `json:"backupPath,omitempty" yaml:"backupPath,omitempty" doc:"Backup .bak path written when --backup updated an existing file" maxLength:"4096" example:".vscode/mcp.json.bak"`
	}
	Result struct {
		Scope   Scope        `json:"scope" yaml:"scope" doc:"Configuration scope (project or user)" maxLength:"16" example:"project"`
		Targets []Target     `json:"targets,omitempty" yaml:"targets,omitempty" doc:"Resolved target harnesses" maxItems:"16"`
		DryRun  bool         `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Whether execution ran in dry-run mode without modifying files"`
		Files   []FileAction `json:"files" yaml:"files" doc:"Ordered per-file actions" maxItems:"64"`
	}
	PrintOutput struct {
		Target     string         `json:"target" yaml:"target" doc:"Target schema rendered (standard or harness name)" maxLength:"32" example:"standard"`
		ServerKey  string         `json:"serverKey" yaml:"serverKey" doc:"MCP server key" maxLength:"128" example:"scafctl"`
		ConfigJSON string         `json:"configJson" yaml:"configJson" doc:"Formatted JSON configuration block" maxLength:"65536" example:"{\n  \"mcpServers\": {}\n}"`
		Config     map[string]any `json:"config" yaml:"config" doc:"Parsed MCP configuration object"`
		Skill      string         `json:"skill,omitempty" yaml:"skill,omitempty" doc:"Rendered SKILL.md markdown when --no-skill is false" maxLength:"65536" example:"---\nname: \"scafctl\"\n---\n"`
	}
)

// Text formats PrintOutput for human/piped stdout.
func (p *PrintOutput) Text() string {
	cfg := strings.TrimRight(p.ConfigJSON, "\r\n") + "\n"
	if p.Skill == "" {
		return cfg
	}
	return "# MCP Configuration\n" + cfg + "\n# Agent Skill (SKILL.md)\n" + p.Skill
}

func cleanBin(raw string, stripExe bool) string {
	name := filepath.Base(raw)
	if stripExe && slices.Contains([]string{".exe", ".cmd", ".bat", ".com"}, strings.ToLower(filepath.Ext(name))) {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = strings.Trim(unsafeBinRe.ReplaceAllString(name, "_"), "_")
	if raw == "" || strings.Trim(name, ".-") == "" {
		return settings.CliBinaryName
	}
	return name
}

// SkillName derives an Agent Skills compliant identifier ([a-z0-9-], <=64 chars) from a resolved binary name.
func SkillName(binaryName string) string {
	s := strings.Trim(skillSplitRe.ReplaceAllString(strings.ToLower(filepath.Base(binaryName)), "-"), "-")
	return cmp.Or(strings.TrimRight(s[:min(len(s), 64)], "-"), settings.CliBinaryName)
}

func renderSkillResolved(bin, skillID, supplement string) string {
	out := strings.TrimRight(strings.NewReplacer("{{SKILL_NAME}}", skillID, "{{BINARY_NAME}}", bin).Replace(embeddedSkillTemplate), "\r\n") + "\n"
	if extra := strings.TrimSpace(supplement); extra != "" {
		out += "\n" + extra + "\n"
	}
	return out
}

// RenderSkill renders the embedded authoring SKILL.md for binaryName and optional supplement.
func RenderSkill(binaryName, supplement string) string {
	return renderSkillResolved(cleanBin(binaryName, true), SkillName(cleanBin(binaryName, true)), supplement)
}

func resolveInputs(ctx context.Context, opts Options) (Scope, []Target, string, string, string, error) {
	if opts.NoSkill && opts.SkillDir != "" {
		return "", nil, "", "", "", fmt.Errorf("--no-skill and --skill-dir cannot be used together")
	}
	scope, err := ParseScope(opts.Scope)
	if err != nil {
		return "", nil, "", "", "", err
	}
	targets, err := ParseTargets(opts.Targets)
	bin := cleanBin(settings.BinaryNameFromContext(ctx), false)
	if opts.BinaryName != "" {
		bin = cleanBin(opts.BinaryName, true)
	}
	return scope, targets, bin, SkillName(bin), cmp.Or(opts.SkillSupplement, SkillSupplementFromContext(ctx)), err
}

// RenderPrint builds a PrintOutput for stdout inspection without writing files.
func RenderPrint(ctx context.Context, opts Options) (*PrintOutput, error) {
	_, targets, bin, skillID, supp, err := resolveInputs(ctx, opts)
	if err != nil {
		return nil, err
	}
	if opts.SkillDir != "" || len(targets) > 1 {
		return nil, fmt.Errorf("--print requires at most one --target and cannot be combined with --skill-dir")
	}
	targetLabel, chosen := TargetStandard, Target("")
	if len(targets) == 1 {
		chosen, targetLabel = targets[0], string(targets[0])
	}
	cfgBytes, err := marshalNewConfig(bin, serverEntrySpec(chosen, bin))
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	_ = json.Unmarshal(cfgBytes, &parsed)
	var skill string
	if !opts.NoSkill {
		skill = renderSkillResolved(bin, skillID, supp)
	}
	return &PrintOutput{Target: targetLabel, ServerKey: bin, ConfigJSON: string(cfgBytes), Config: parsed, Skill: skill}, nil
}

// Run idempotently creates or updates MCP configuration files and SKILL.md files.
func Run(ctx context.Context, opts Options) (*Result, error) {
	scope, targets, bin, skillID, supp, err := resolveInputs(ctx, opts)
	if err != nil {
		return nil, err
	}
	env := resolveEnv(opts.Env)
	if len(opts.Targets) == 0 && opts.SkillDir == "" {
		targets = DetectTargets(env.WorkDir)
	}

	res := &Result{Scope: scope, Targets: targets, DryRun: opts.DryRun, Files: make([]FileAction, 0, len(targets)*2+1)}
	skillBytes := []byte(renderSkillResolved(bin, skillID, supp))
	seenSkills := make(map[string]bool, len(targets)+1)
	staticSkill := func([]byte, bool) ([]byte, error) { return skillBytes, nil }
	record := func(fullPath string, t Target, kind string, fn func([]byte, bool) ([]byte, error)) error {
		act, sErr := syncScopedFile(env, fullPath, t, kind, opts.Backup, opts.DryRun, fn)
		if sErr == nil {
			res.Files = append(res.Files, act)
		}
		return sErr
	}

	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		plan, err := resolveTargetPlan(t, scope, bin, skillID, env)
		if err != nil {
			return nil, err
		}
		if err := record(plan.configPath, t, KindConfig, func(cur []byte, existed bool) ([]byte, error) {
			return upsertJSONC(cur, existed, bin, plan.spec)
		}); err != nil {
			return nil, err
		}
		if !opts.NoSkill && opts.SkillDir == "" && !seenSkills[plan.skillPath] {
			seenSkills[plan.skillPath] = true
			if err := record(plan.skillPath, t, KindSkill, staticSkill); err != nil {
				return nil, err
			}
		}
	}

	if opts.SkillDir != "" {
		c := filepath.Clean(opts.SkillDir)
		skillPath := filepath.Join(c, skillID, "SKILL.md")
		if !filepath.IsAbs(c) {
			if env.WorkDir == "" || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("invalid relative --skill-dir %q", opts.SkillDir)
			}
			skillPath = filepath.Join(env.WorkDir, skillPath)
		}
		if err := record(skillPath, "", KindSkill, staticSkill); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func resolveTrustedAnchor(fullPath string, env Env) (string, string, error) {
	abs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", "", err
	}
	candidates := []string{env.WorkDir, env.ConfigHome, env.AppData, env.HomeDir, os.TempDir()}
	for i, dir := range candidates {
		clean, aErr := filepath.Abs(dir)
		if dir == "" || aErr != nil {
			continue
		}
		rel, rErr := filepath.Rel(clean, abs)
		if rErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		info, sErr := os.Lstat(clean)
		if sErr != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if i < len(candidates)-1 {
				return "", "", fmt.Errorf("%w: %s", ErrSymlink, clean)
			}
			if clean, sErr = filepath.EvalSymlinks(clean); sErr != nil {
				continue
			}
		} else if !info.IsDir() {
			continue
		}
		return clean, rel, nil
	}
	vol := filepath.VolumeName(abs)
	rootDir := vol + string(filepath.Separator)
	if runtime.GOOS == "darwin" && (strings.HasPrefix(abs, "/var/") || strings.HasPrefix(abs, "/tmp/") || strings.HasPrefix(abs, "/etc/")) {
		rootDir = "/private"
	}
	return rootDir, strings.TrimPrefix(abs[len(vol):], string(filepath.Separator)), nil
}

func statNoSymlinks(root *os.Root, relPath string) (info fs.FileInfo, err error) {
	parts := strings.Split(filepath.Clean(relPath), string(filepath.Separator))
	var cur string
	for i, p := range parts {
		if p == "" || p == "." {
			continue
		}
		cur = filepath.Join(cur, p)
		if info, err = root.Lstat(cur); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: %s", ErrSymlink, filepath.Join(root.Name(), cur))
		} else if i < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("parent path %s is not a directory", filepath.Join(root.Name(), cur))
		}
	}
	return info, nil
}

func syncScopedFile(env Env, fullPath string, target Target, kind string, backup, dryRun bool, build func([]byte, bool) ([]byte, error)) (FileAction, error) {
	anchorDir, relPath, err := resolveTrustedAnchor(fullPath, env)
	if err != nil {
		return FileAction{}, err
	}
	root, err := os.OpenRoot(anchorDir)
	if err != nil {
		return FileAction{}, err
	}
	defer func() { _ = root.Close() }()

	fi, err := statNoSymlinks(root, relPath)
	if err != nil {
		return FileAction{}, err
	}
	var existing []byte
	if fi != nil {
		if fi.IsDir() {
			return FileAction{}, fmt.Errorf("target path %s is a directory", fullPath)
		}
		if existing, err = root.ReadFile(relPath); err != nil {
			return FileAction{}, err
		}
		if backup {
			if _, err = statNoSymlinks(root, relPath+".bak"); err != nil {
				return FileAction{}, err
			}
		}
	}
	desired, err := build(existing, fi != nil)
	if err != nil {
		return FileAction{}, fmt.Errorf("%s: %w", fullPath, err)
	}
	act := FileAction{Target: target, Kind: kind, Path: fullPath, Status: StatusCreated}
	if fi != nil {
		if bytes.Equal(existing, desired) {
			act.Status = StatusUnchanged
			return act, nil
		}
		act.Status = StatusUpdated
		if backup {
			act.BackupPath = fullPath + ".bak"
		}
	}
	if dryRun {
		return act, nil
	}
	if parent := filepath.Dir(relPath); parent != "." {
		if err := root.MkdirAll(parent, DefaultDirPerm); err != nil {
			return FileAction{}, err
		}
	}
	if fi != nil && backup {
		if err := writeAtomicInRoot(root, relPath+".bak", existing); err != nil {
			return FileAction{}, err
		}
	}
	return act, writeAtomicInRoot(root, relPath, desired)
}

func writeAtomicInRoot(root *os.Root, relPath string, data []byte) (retErr error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmpRel := filepath.Join(filepath.Dir(relPath), "."+filepath.Base(relPath)+".tmp."+hex.EncodeToString(rnd[:]))
	f, err := root.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, DefaultFilePerm)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = root.Remove(tmpRel)
		}
	}()
	_, wErr := f.Write(data)
	if err := errors.Join(wErr, f.Close(), root.Chmod(tmpRel, DefaultFilePerm)); err != nil {
		return err
	}
	return root.Rename(tmpRel, relPath)
}
