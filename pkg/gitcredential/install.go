// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	// DefaultBinDir is the default directory for optional git-credential-<binary> shims.
	DefaultBinDir = "~/.local/bin"

	// ShimMethodSymlink indicates the helper alias was created as a symbolic link.
	ShimMethodSymlink = "symlink"

	// ShimMethodShim indicates the helper alias was created as a forwarding script.
	ShimMethodShim = "shim"

	shimHeaderScanBytes = 512
)

// DefaultGitHosts lists the built-in Git HTTPS remotes configured by default
// during "git-credential install" when neither --host nor --global-helper is passed.
func DefaultGitHosts() []string {
	return []string{
		"https://github.com",
		"https://gist.github.com",
		"https://dev.azure.com",
		"https://source.developers.google.com",
	}
}

// InstallOptions configures Install.
type InstallOptions struct {
	BinaryName     string
	ExecutablePath string
	ConfigPath     string
	Hosts          []string
	Local          bool
	File           string
	GlobalHelper   bool
	Force          bool
	Shim           bool
	BinDir         string
	WorkingDir     string
	GOOS           string
	Config         *config.Config
	AuthRegistry   *auth.Registry
	Profile        string
	Handler        string
}

// InstallResult reports the changes performed by Install.
type InstallResult struct {
	ConfigFile     string   `json:"configFile" yaml:"configFile" doc:"Modified Git configuration file path" maxLength:"4096" example:"~/.gitconfig"`
	Scope          string   `json:"scope" yaml:"scope" doc:"Configuration scope (global, local, or file)" maxLength:"32" example:"global"`
	GlobalHelper   bool     `json:"globalHelper,omitempty" yaml:"globalHelper,omitempty" doc:"Whether global credential.helper was configured"`
	ConfiguredURLs []string `json:"configuredUrls" yaml:"configuredUrls" doc:"Target URLs configured in gitconfig" maxItems:"100"`
	HelperCommand  string   `json:"helperCommand" yaml:"helperCommand" doc:"Configured Git credential helper command" maxLength:"4096" example:"!\"/usr/local/bin/scafctl\" git-credential"`
	ShimPath       string   `json:"shimPath,omitempty" yaml:"shimPath,omitempty" doc:"Installed symlink or forwarding shim path when --shim is set" maxLength:"4096" example:"~/.local/bin/git-credential-scafctl"`
	ShimMethod     string   `json:"shimMethod,omitempty" yaml:"shimMethod,omitempty" doc:"Installation method for --shim (symlink or shim)" maxLength:"32" example:"symlink"`
	Warnings       []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal setup warnings" maxItems:"10"`
}

// UninstallOptions configures Uninstall.
type UninstallOptions struct {
	BinaryName   string
	Hosts        []string
	Local        bool
	File         string
	GlobalHelper bool
	Shim         bool
	BinDir       string
	WorkingDir   string
	GOOS         string
}

// UninstallResult reports the changes performed by Uninstall.
type UninstallResult struct {
	ConfigFile          string   `json:"configFile" yaml:"configFile" doc:"Target Git configuration file path" maxLength:"4096" example:"~/.gitconfig"`
	Scope               string   `json:"scope" yaml:"scope" doc:"Configuration scope (global, local, or file)" maxLength:"32" example:"global"`
	RemovedGlobalHelper bool     `json:"removedGlobalHelper,omitempty" yaml:"removedGlobalHelper,omitempty" doc:"Whether global credential.helper was removed"`
	RemovedURLs         []string `json:"removedUrls" yaml:"removedUrls" doc:"Target URLs cleaned from gitconfig" maxItems:"100"`
	ShimPath            string   `json:"shimPath,omitempty" yaml:"shimPath,omitempty" doc:"Removed symlink or forwarding shim path" maxLength:"4096" example:"~/.local/bin/git-credential-scafctl"`
	ShimRemoved         bool     `json:"shimRemoved,omitempty" yaml:"shimRemoved,omitempty" doc:"Whether a symlink or managed shim was removed"`
}

// ShimSignatureFor returns the marker text embedded in forwarding shims
// generated for binaryName ("<binary>-managed forwarding shim").
func ShimSignatureFor(binaryName string) string {
	bin := resolveBinaryName(binaryName)
	return bin + "-managed forwarding shim"
}

// CredHelperFileName returns the on-disk filename for the Git credential helper
// alias ("git-credential-<binary>" on POSIX, "git-credential-<binary>.cmd" on Windows).
func CredHelperFileName(binaryName, goos string) string {
	name := AliasPrefix + resolveBinaryName(binaryName)
	if goos == "windows" {
		return name + ".cmd"
	}
	return name
}

// BuildHelperCommand builds the gitconfig helper command string with forward
// slashes in exePath so Windows Git / MSYS2 subshells never strip backslashes,
// rejecting characters that trigger double-quoted /bin/sh expansion.
func BuildHelperCommand(exePath, profile, handler string) (string, error) {
	return BuildHelperCommandWithConfig(exePath, "", profile, handler)
}

// BuildHelperCommandWithConfig builds the gitconfig helper command string,
// preserving an explicit --config path when configPath is non-empty.
func BuildHelperCommandWithConfig(exePath, configPath, profile, handler string) (string, error) {
	if strings.TrimSpace(exePath) == "" {
		return "", fmt.Errorf("executable path must not be empty")
	}
	if strings.ContainsAny(exePath, "\"`$\r\n\x00") {
		return "", fmt.Errorf("%w: executable path %q contains characters unsafe for gitconfig", ErrInvalidProtocolInput, exePath)
	}
	forwardExe := strings.ReplaceAll(exePath, `\`, `/`)
	cmd := fmt.Sprintf(`!"%s" %s`, forwardExe, CommandName)

	if cfg := strings.TrimSpace(configPath); cfg != "" {
		if strings.ContainsAny(cfg, "\"`$\r\n\x00") {
			return "", fmt.Errorf("%w: config path %q contains characters unsafe for gitconfig", ErrInvalidProtocolInput, configPath)
		}
		forwardCfg := strings.ReplaceAll(cfg, `\`, `/`)
		cmd += fmt.Sprintf(` --config "%s"`, forwardCfg)
	}

	if p := strings.TrimSpace(profile); p != "" {
		norm := auth.NormalizeProfileName(p)
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid --profile: %w", err)
			}
			cmd += " --profile " + norm
		} else {
			cmd += " --profile " + auth.BuiltinProfileName
		}
	}
	if h := strings.TrimSpace(handler); h != "" {
		if err := validateSafeCLIToken(h); err != nil {
			return "", fmt.Errorf("invalid --handler: %w", err)
		}
		cmd += " --handler " + h
	}
	return cmd, nil
}

func validateSafeCLIToken(s string) error {
	for i := range len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			continue
		}
		return fmt.Errorf("contains invalid character %q", c)
	}
	return nil
}

// FindExecutable resolves the absolute, symlink-evaluated path of the running binary.
func FindExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("determine executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	return resolved, nil
}

// Install configures Git's credential.helper settings (and optional PATH shim)
// after pre-validating all target sections for foreign helpers.
func Install(ctx context.Context, opts InstallOptions) (*InstallResult, error) {
	binName := resolveBinaryName(opts.BinaryName)
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	if opts.Local && opts.File != "" {
		return nil, fmt.Errorf("--local and --file are mutually exclusive")
	}
	if opts.GlobalHelper && len(opts.Hosts) > 0 {
		return nil, fmt.Errorf("--global-helper and --host are mutually exclusive")
	}

	exePath := opts.ExecutablePath
	if exePath == "" {
		var err error
		exePath, err = FindExecutable()
		if err != nil {
			return nil, err
		}
	}

	resolvedConfigPath, err := resolveExplicitConfigPath(opts.ConfigPath, opts.WorkingDir)
	if err != nil {
		return nil, err
	}

	helperCmd, err := BuildHelperCommandWithConfig(exePath, resolvedConfigPath, opts.Profile, opts.Handler)
	if err != nil {
		return nil, err
	}

	cfgFile, scope, err := resolveGitConfigTarget(ctx, opts.Local, opts.File, opts.WorkingDir)
	if err != nil {
		return nil, err
	}

	allowInsecureHTTP := opts.Config != nil && opts.Config.Auth.Git != nil && opts.Config.Auth.Git.AllowInsecureHTTP

	var targetURLs []string
	if !opts.GlobalHelper {
		targetURLs, err = resolveInstallTargetURLs(opts.Hosts, opts.Config, allowInsecureHTTP)
		if err != nil {
			return nil, err
		}
	}

	// Pre-validate shim path BEFORE mutating gitconfig so a foreign file or symlink
	// in --bin-dir aborts cleanly without leaving partial gitconfig edits.
	var resolvedShimPath string
	if opts.Shim {
		binDir := opts.BinDir
		if binDir == "" {
			binDir = DefaultBinDir
		}
		resolvedShimPath = filepath.Join(expandHome(binDir), CredHelperFileName(binName, goos))
		if err := prevalidateShimTarget(resolvedShimPath, binName, opts.Force); err != nil {
			return nil, err
		}
	}

	existingEntries, err := readGitConfigEntries(ctx, cfgFile)
	if err != nil {
		return nil, err
	}

	// Validate ALL target URLs (or global credential.helper) for foreign helpers before writing any.
	if !opts.Force {
		if opts.GlobalHelper {
			if foreign := findForeignHelper(existingEntries, "credential.helper", binName); foreign != "" {
				return nil, &ForeignHelperError{
					Target:   "credential.helper",
					Existing: foreign,
				}
			}
		} else {
			for _, u := range targetURLs {
				key := "credential." + u + ".helper"
				if foreign := findForeignHelper(existingEntries, key, binName); foreign != "" {
					return nil, &ForeignHelperError{
						Target:   key,
						Existing: foreign,
					}
				}
			}
		}
	}

	if err := ensureParentDir(cfgFile); err != nil {
		return nil, err
	}

	if opts.GlobalHelper {
		if err := writeGlobalHelperPreservingScopedSections(ctx, cfgFile, existingEntries, helperCmd); err != nil {
			return nil, err
		}
	} else {
		if err := writeOrderedHostSections(ctx, cfgFile, existingEntries, targetURLs, helperCmd); err != nil {
			return nil, err
		}
	}

	result := &InstallResult{
		ConfigFile:     cfgFile,
		Scope:          scope,
		GlobalHelper:   opts.GlobalHelper,
		ConfiguredURLs: nonNilSlice(targetURLs),
		HelperCommand:  helperCmd,
	}

	if opts.Shim {
		if err := os.MkdirAll(filepath.Dir(resolvedShimPath), 0o750); err != nil {
			return nil, fmt.Errorf("create bin directory %s: %w", filepath.Dir(resolvedShimPath), err)
		}
		method, shimErr := installShimOrSymlink(exePath, resolvedConfigPath, resolvedShimPath, binName, goos, opts.Force)
		if shimErr != nil {
			return nil, shimErr
		}
		result.ShimPath = resolvedShimPath
		result.ShimMethod = method
	}

	if warn := checkGitHubScopesWarning(ctx, opts.AuthRegistry, binName, opts.Profile, opts.GlobalHelper, targetURLs, opts.Config); warn != "" {
		result.Warnings = append(result.Warnings, warn)
	}

	return result, nil
}

// Uninstall removes <binary>-managed Git credential helper entries from gitconfig
// (leaving foreign helpers intact) and optionally removes a managed shim/symlink.
func Uninstall(ctx context.Context, opts UninstallOptions) (*UninstallResult, error) {
	binName := resolveBinaryName(opts.BinaryName)
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	if opts.Local && opts.File != "" {
		return nil, fmt.Errorf("--local and --file are mutually exclusive")
	}
	if opts.GlobalHelper && len(opts.Hosts) > 0 {
		return nil, fmt.Errorf("--global-helper and --host are mutually exclusive")
	}

	cfgFile, scope, err := resolveGitConfigTarget(ctx, opts.Local, opts.File, opts.WorkingDir)
	if err != nil {
		return nil, err
	}

	// Pre-validate shim before mutating gitconfig when --shim is requested.
	var resolvedShimPath string
	if opts.Shim {
		binDir := opts.BinDir
		if binDir == "" {
			binDir = DefaultBinDir
		}
		resolvedShimPath = filepath.Join(expandHome(binDir), CredHelperFileName(binName, goos))
		if err := prevalidateShimRemoval(resolvedShimPath, binName); err != nil {
			return nil, err
		}
	}

	var explicitURLs map[string]struct{}
	if len(opts.Hosts) > 0 {
		// Pass allowInsecureHTTP=true on Uninstall so previously configured
		// http:// entries can always be cleaned up regardless of current config.
		normalized, normErr := normalizeHostTargets(opts.Hosts, true)
		if normErr != nil {
			return nil, normErr
		}
		explicitURLs = make(map[string]struct{}, len(normalized))
		for _, u := range normalized {
			explicitURLs[u] = struct{}{}
		}
	}

	entries, err := readGitConfigEntries(ctx, cfgFile)
	if err != nil {
		return nil, err
	}

	result := &UninstallResult{
		ConfigFile:  cfgFile,
		Scope:       scope,
		RemovedURLs: []string{},
	}

	if len(entries) > 0 {
		if opts.GlobalHelper || len(opts.Hosts) == 0 {
			removedGlobal, globalErr := removeManagedHelpersFromSection(ctx, cfgFile, entries, "", binName)
			if globalErr != nil {
				return nil, globalErr
			}
			result.RemovedGlobalHelper = removedGlobal
		}

		if !opts.GlobalHelper {
			sectionURLs := collectCredentialSectionURLs(entries)
			for _, u := range sectionURLs {
				if explicitURLs != nil {
					if _, ok := explicitURLs[u]; !ok {
						continue
					}
				}
				removed, secErr := removeManagedHelpersFromSection(ctx, cfgFile, entries, u, binName)
				if secErr != nil {
					return nil, secErr
				}
				if removed {
					result.RemovedURLs = append(result.RemovedURLs, u)
				}
			}
		}
	}

	if opts.Shim {
		removed, shimErr := removeManagedShimOrSymlink(resolvedShimPath, binName)
		if shimErr != nil {
			return nil, shimErr
		}
		result.ShimRemoved = removed
		if removed {
			result.ShimPath = resolvedShimPath
		}
	}

	return result, nil
}

type gitConfigEntry struct {
	key   string // exact key as lowered by git config --list (section.subsection.varname)
	value string
}

type savedSubSection struct {
	url     string
	entries []gitConfigEntry // key suffix after "credential.<url>."
}

func readGitConfigEntries(ctx context.Context, cfgFile string) ([]gitConfigEntry, error) {
	if _, err := os.Stat(cfgFile); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat git config %s: %w", cfgFile, err)
	}

	out, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, "--null", "--list")
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("read git config %s: %s", cfgFile, strings.TrimSpace(stderr))
	}

	chunks := bytes.Split(out, []byte{0})
	entries := make([]gitConfigEntry, 0, len(chunks))
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		// With --null --list, git separates key and value on the first '\n'.
		// An implicit boolean key without '=' has no '\n'.
		s := string(chunk)
		key, val, hasVal := strings.Cut(s, "\n")
		if !hasVal {
			val = "true"
		}
		entries = append(entries, gitConfigEntry{key: key, value: val})
	}
	return entries, nil
}

// IsManagedHelperValue reports whether a gitconfig credential.helper value is
// owned by binaryName. Recognizes the bare helper alias ("<binary>"), the
// forwarding shim ("!git-credential-<binary>"), and "!<path>/<binary>[-suffix] git-credential"
// while rejecting compound shell pipelines or foreign binaries.
func IsManagedHelperValue(val, binaryName string) bool {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return false
	}
	bin := resolveBinaryName(binaryName)

	// Short helper name: "credential.helper = scafctl" invokes git-credential-scafctl.
	if strings.EqualFold(trimmed, bin) {
		return true
	}

	if !strings.HasPrefix(trimmed, "!") {
		return false
	}
	afterBang := strings.TrimSpace(trimmed[1:])
	// Reject any compound shell operators, redirections, or command expansions
	// (e.g. "!scafctl git-credential || gh auth git-credential") so custom user
	// shell pipelines are never treated as managed.
	if strings.ContainsAny(afterBang, ";|&<>`$\r\n\x00") {
		return false
	}
	tokens := splitShellWords(afterBang)
	if len(tokens) == 0 {
		return false
	}
	exeBase := strings.ToLower(baseExecutableName(tokens[0]))
	alias := strings.ToLower(AliasPrefix + bin)
	if exeBase == alias {
		return true
	}
	if len(tokens) >= 2 && tokens[1] == CommandName {
		return isManagedExecutableBase(exeBase, bin)
	}
	return false
}

func isManagedExecutableBase(exeBaseLower, bin string) bool {
	binLower := strings.ToLower(bin)
	if exeBaseLower == "" {
		return false
	}
	if exeBaseLower == binLower ||
		exeBaseLower == strings.ToLower(AliasPrefix+bin) ||
		strings.HasPrefix(exeBaseLower, binLower+"-") ||
		strings.HasPrefix(exeBaseLower, binLower+"_") {
		return true
	}
	// During "go test", FindExecutable() resolves to "<pkg>.test"; treat the
	// running test binary as managed without matching unrelated host binaries.
	if currentExe, err := FindExecutable(); err == nil {
		curBase := strings.ToLower(filepath.Base(currentExe))
		if strings.HasSuffix(curBase, ".test") || strings.HasSuffix(curBase, ".test.exe") {
			if exeBaseLower == strings.ToLower(baseExecutableName(currentExe)) {
				return true
			}
		}
	}
	return false
}

func splitShellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var quote byte

	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case ' ', '\t':
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

func findForeignHelper(entries []gitConfigEntry, targetHelperKey, binName string) string {
	for _, e := range entries {
		if e.key != targetHelperKey {
			continue
		}
		if strings.TrimSpace(e.value) == "" {
			continue
		}
		if !IsManagedHelperValue(e.value, binName) {
			return e.value
		}
	}
	return ""
}

// writeGlobalHelperPreservingScopedSections writes unscoped credential.helper
// ("" reset + helperCmd) and credential.useHttpPath = true while moving any
// preceding scoped [credential "<url>"] sections after [credential] so the
// unscoped "" reset never wipes out per-host/per-org helpers above it.
func writeGlobalHelperPreservingScopedSections(ctx context.Context, cfgFile string, existing []gitConfigEntry, helperCmd string) error {
	currentEntries, err := readGitConfigEntries(ctx, cfgFile)
	if err != nil {
		return err
	}
	if currentEntries == nil {
		currentEntries = existing
	}

	var preserved []savedSubSection
	seenScoped := make(map[string]int)
	lastUnscopedIdx := -1
	firstScopedIdx := -1

	for idx, e := range currentEntries {
		if !strings.HasPrefix(e.key, "credential.") {
			continue
		}
		secURL, varName, isScoped := parseCredentialSectionKey(e.key)
		if !isScoped {
			lastUnscopedIdx = idx
			continue
		}
		if firstScopedIdx == -1 {
			firstScopedIdx = idx
		}
		pos, exists := seenScoped[secURL]
		if !exists {
			pos = len(preserved)
			seenScoped[secURL] = pos
			preserved = append(preserved, savedSubSection{url: secURL})
		}
		preserved[pos].entries = append(preserved[pos].entries, gitConfigEntry{key: varName, value: e.value})
	}

	needReorder := len(preserved) > 0 && (lastUnscopedIdx == -1 || firstScopedIdx < lastUnscopedIdx)
	if needReorder {
		for _, sec := range preserved {
			if err := gitConfigRemoveSection(ctx, cfgFile, "credential."+sec.url); err != nil {
				return err
			}
		}
	}

	if err := gitConfigReplaceAll(ctx, cfgFile, "credential.helper", ""); err != nil {
		return err
	}
	if err := gitConfigAdd(ctx, cfgFile, "credential.helper", helperCmd); err != nil {
		return err
	}
	if err := gitConfigSet(ctx, cfgFile, "credential.useHttpPath", "true"); err != nil {
		return err
	}

	if needReorder {
		for _, sec := range preserved {
			for _, kv := range sec.entries {
				if err := gitConfigAdd(ctx, cfgFile, "credential."+sec.url+"."+kv.key, kv.value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// writeOrderedHostSections writes credential.<url>.helper ("" reset + helperCmd)
// and credential.<url>.useHttpPath = true for every target URL, moving any
// existing sub-path credential.<host>/<subpath>.* sections AFTER their parent
// host section so the parent's "" reset never overrides more-specific sub-paths.
func writeOrderedHostSections(ctx context.Context, cfgFile string, existing []gitConfigEntry, targetURLs []string, helperCmd string) error {
	for _, u := range targetURLs {
		var preserved []savedSubSection
		seenSub := make(map[string]int)
		lastParentIdx := -1
		firstSubIdx := -1

		parentPrefix := "credential." + u + "."
		subPrefix := "credential." + u + "/"

		currentEntries, err := readGitConfigEntries(ctx, cfgFile)
		if err != nil {
			return err
		}
		if currentEntries == nil {
			currentEntries = existing
		}

		for idx, e := range currentEntries {
			if strings.HasPrefix(e.key, parentPrefix) {
				lastParentIdx = idx
			}
			if strings.HasPrefix(e.key, subPrefix) {
				subURL, varName, ok := parseCredentialSectionKey(e.key)
				if !ok {
					continue
				}
				if firstSubIdx == -1 {
					firstSubIdx = idx
				}
				pos, exists := seenSub[subURL]
				if !exists {
					pos = len(preserved)
					seenSub[subURL] = pos
					preserved = append(preserved, savedSubSection{url: subURL})
				}
				preserved[pos].entries = append(preserved[pos].entries, gitConfigEntry{key: varName, value: e.value})
			}
		}

		needReorder := len(preserved) > 0 && (lastParentIdx == -1 || firstSubIdx < lastParentIdx)
		if needReorder {
			for _, sub := range preserved {
				if err := gitConfigRemoveSection(ctx, cfgFile, "credential."+sub.url); err != nil {
					return err
				}
			}
		}

		if err := gitConfigReplaceAll(ctx, cfgFile, "credential."+u+".helper", ""); err != nil {
			return err
		}
		if err := gitConfigAdd(ctx, cfgFile, "credential."+u+".helper", helperCmd); err != nil {
			return err
		}
		if err := gitConfigSet(ctx, cfgFile, "credential."+u+".useHttpPath", "true"); err != nil {
			return err
		}

		if needReorder {
			for _, sub := range preserved {
				for _, kv := range sub.entries {
					if err := gitConfigAdd(ctx, cfgFile, "credential."+sub.url+"."+kv.key, kv.value); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func parseCredentialSectionKey(fullKey string) (sectionURL, varName string, ok bool) {
	if !strings.HasPrefix(fullKey, "credential.") {
		return "", "", false
	}
	rest := strings.TrimPrefix(fullKey, "credential.")
	lastDot := strings.LastIndexByte(rest, '.')
	if lastDot <= 0 || lastDot == len(rest)-1 {
		return "", "", false
	}
	return rest[:lastDot], rest[lastDot+1:], true
}

func collectCredentialSectionURLs(entries []gitConfigEntry) []string {
	seen := make(map[string]struct{})
	var urls []string
	for _, e := range entries {
		u, _, ok := parseCredentialSectionKey(e.key)
		if !ok {
			continue
		}
		if _, exists := seen[u]; !exists {
			seen[u] = struct{}{}
			urls = append(urls, u)
		}
	}
	return urls
}

func removeManagedHelpersFromSection(ctx context.Context, cfgFile string, entries []gitConfigEntry, sectionURL, binName string) (bool, error) {
	helperKey := "credential.helper"
	useHTTPPathKey := "credential.usehttppath"
	sectionName := "credential"
	sectionPrefix := "credential."
	if sectionURL != "" {
		helperKey = "credential." + sectionURL + ".helper"
		useHTTPPathKey = "credential." + sectionURL + ".usehttppath"
		sectionName = "credential." + sectionURL
		sectionPrefix = "credential." + sectionURL + "."
	}

	var (
		hasManaged      bool
		foreignHelpers  []string
		hasLeadingReset bool
		otherKeys       int
	)
	for _, e := range entries {
		if sectionURL == "" {
			if _, _, isScoped := parseCredentialSectionKey(e.key); isScoped {
				continue
			}
		}
		if e.key == helperKey {
			switch {
			case strings.TrimSpace(e.value) == "":
				hasLeadingReset = true
			case IsManagedHelperValue(e.value, binName):
				hasManaged = true
			default:
				foreignHelpers = append(foreignHelpers, e.value)
			}
			continue
		}
		if e.key == useHTTPPathKey {
			continue
		}
		if strings.HasPrefix(e.key, sectionPrefix) {
			otherKeys++
		}
	}

	if !hasManaged {
		return false, nil
	}

	if len(foreignHelpers) == 0 && otherKeys == 0 {
		if err := gitConfigRemoveSection(ctx, cfgFile, sectionName); err != nil {
			return false, err
		}
		return true, nil
	}

	// Preserve foreign helpers and any unrelated keys in the same section.
	if err := gitConfigUnsetAll(ctx, cfgFile, helperKey); err != nil {
		return false, err
	}
	if len(foreignHelpers) > 0 {
		if hasLeadingReset {
			if err := gitConfigAdd(ctx, cfgFile, helperKey, ""); err != nil {
				return false, err
			}
		}
		for _, fh := range foreignHelpers {
			if err := gitConfigAdd(ctx, cfgFile, helperKey, fh); err != nil {
				return false, err
			}
		}
	} else {
		if err := gitConfigUnsetAll(ctx, cfgFile, sectionName+".useHttpPath"); err != nil {
			return false, err
		}
	}
	return true, nil
}

func resolveInstallTargetURLs(explicitHosts []string, cfg *config.Config, allowInsecureHTTP bool) ([]string, error) {
	if len(explicitHosts) > 0 {
		return normalizeHostTargets(explicitHosts, allowInsecureHTTP)
	}

	candidates := DefaultGitHosts()
	if cfg != nil {
		if cfg.Auth.GitHub != nil {
			if h := normalizeConfiguredHost(cfg.Auth.GitHub.Hostname); h != "" && h != "github.com" {
				candidates = append(candidates, "https://"+h)
			}
			profileNames := make([]string, 0, len(cfg.Auth.GitHub.Profiles))
			for k := range cfg.Auth.GitHub.Profiles {
				profileNames = append(profileNames, k)
			}
			sort.Strings(profileNames)
			for _, k := range profileNames {
				p := cfg.Auth.GitHub.Profiles[k]
				if p == nil {
					continue
				}
				if h := normalizeConfiguredHost(p.Hostname); h != "" && h != "github.com" {
					candidates = append(candidates, "https://"+h)
				}
			}
		}
		for _, entry := range cfg.Auth.CustomOAuth2 {
			if h := normalizeConfiguredHost(entry.GitHost); h != "" {
				candidates = append(candidates, "https://"+h)
			}
		}
		if cfg.Auth.Git != nil {
			for _, rule := range cfg.Auth.Git.Rules {
				if h := normalizeConfiguredHost(rule.Host); h != "" {
					candidates = append(candidates, "https://"+h)
				}
			}
		}
	}

	return normalizeHostTargets(candidates, allowInsecureHTTP)
}

func normalizeHostTargets(rawTargets []string, allowInsecureHTTP bool) ([]string, error) {
	seen := make(map[string]struct{}, len(rawTargets))
	urls := make([]string, 0, len(rawTargets))

	for _, raw := range rawTargets {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid target host %q", raw)
		}
		scheme := strings.ToLower(u.Scheme)
		host := NormalizeHostForProtocol(scheme, u.Host)
		if err := config.ValidateGitHost(host); err != nil {
			return nil, fmt.Errorf("invalid target host %q: %w", raw, err)
		}
		if err := ValidateTransport(scheme, host, allowInsecureHTTP); err != nil {
			return nil, err
		}
		if err := ValidateRepoPath(u.EscapedPath()); err != nil {
			return nil, fmt.Errorf("invalid target path in %q: %w", raw, err)
		}
		cleanPath := CleanRepoPath(u.Path)
		if cleanPath != "" {
			if err := config.ValidateGitPathPrefix(cleanPath); err != nil {
				return nil, fmt.Errorf("invalid target path in %q: %w", raw, err)
			}
		}

		canonical := scheme + "://" + host
		if cleanPath != "" {
			canonical += "/" + cleanPath
		}
		if _, exists := seen[canonical]; !exists {
			seen[canonical] = struct{}{}
			urls = append(urls, canonical)
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("at least one target host is required")
	}

	// Sort so host-level sections always precede sub-path sections (and shorter
	// sub-paths precede deeper sub-paths), preserving input order on ties.
	sort.SliceStable(urls, func(i, j int) bool {
		return targetPathDepth(urls[i]) < targetPathDepth(urls[j])
	})

	return urls, nil
}

func targetPathDepth(canonicalURL string) int {
	u, err := url.Parse(canonicalURL)
	if err != nil {
		return 0
	}
	trimmed := strings.Trim(u.Path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

func resolveGitConfigTarget(ctx context.Context, local bool, explicitFile, workingDir string) (path, scope string, err error) {
	if explicitFile != "" {
		cleaned := filepath.Clean(expandHome(explicitFile))
		if !filepath.IsAbs(cleaned) && workingDir != "" {
			cleaned = filepath.Join(workingDir, cleaned)
		}
		abs, absErr := filepath.Abs(cleaned)
		if absErr == nil {
			cleaned = abs
		}
		return filepath.Clean(cleaned), "file", nil
	}

	if local {
		out, exitCode, stderr, gitErr := runGit(ctx, workingDir, "rev-parse", "--git-path", "config")
		if gitErr != nil {
			return "", "", gitErr
		}
		if exitCode != 0 {
			msg := strings.TrimSpace(stderr)
			if msg == "" {
				msg = "not inside a Git repository"
			}
			return "", "", fmt.Errorf("resolve --local git config: %s", msg)
		}
		gitConfigRel := strings.TrimSpace(string(out))
		if !filepath.IsAbs(gitConfigRel) && workingDir != "" {
			gitConfigRel = filepath.Join(workingDir, gitConfigRel)
		}
		abs, absErr := filepath.Abs(gitConfigRel)
		if absErr == nil {
			gitConfigRel = abs
		}
		return filepath.Clean(gitConfigRel), "local", nil
	}

	if envGlobal := strings.TrimSpace(os.Getenv("GIT_CONFIG_GLOBAL")); envGlobal != "" {
		return filepath.Clean(expandHome(envGlobal)), "global", nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("determine home directory for global gitconfig: %w", err)
	}
	dotGitconfig := filepath.Join(home, ".gitconfig")
	if _, statErr := os.Stat(dotGitconfig); statErr == nil {
		return dotGitconfig, "global", nil
	}

	xdgCfg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdgCfg == "" {
		xdgCfg = filepath.Join(home, ".config")
	}
	xdgGitConfig := filepath.Clean(filepath.Join(xdgCfg, "git", "config"))
	if _, statErr := os.Stat(xdgGitConfig); statErr == nil { //nolint:gosec // G703: XDG_CONFIG_HOME is standard user environment configuration
		return xdgGitConfig, "global", nil
	}

	return dotGitconfig, "global", nil
}

func ensureParentDir(cfgFile string) error {
	dir := filepath.Dir(cfgFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create gitconfig directory %s: %w", dir, err)
	}
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		if writeErr := os.WriteFile(cfgFile, nil, 0o600); writeErr != nil {
			return fmt.Errorf("initialize gitconfig %s: %w", cfgFile, writeErr)
		}
	}
	return nil
}

func gitConfigReplaceAll(ctx context.Context, cfgFile, key, val string) error {
	_, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, "--replace-all", key, val)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("git config --replace-all %s: %s", key, strings.TrimSpace(stderr))
	}
	return nil
}

func gitConfigAdd(ctx context.Context, cfgFile, key, val string) error {
	_, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, "--add", key, val)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("git config --add %s: %s", key, strings.TrimSpace(stderr))
	}
	return nil
}

func gitConfigSet(ctx context.Context, cfgFile, key, val string) error {
	_, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, key, val)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("git config %s: %s", key, strings.TrimSpace(stderr))
	}
	return nil
}

func gitConfigUnsetAll(ctx context.Context, cfgFile, key string) error {
	_, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, "--unset-all", key)
	if err != nil {
		return err
	}
	// git config --unset-all exits 5 when the key does not exist.
	if exitCode != 0 && exitCode != 5 {
		return fmt.Errorf("git config --unset-all %s: %s", key, strings.TrimSpace(stderr))
	}
	return nil
}

func gitConfigRemoveSection(ctx context.Context, cfgFile, section string) error {
	_, exitCode, stderr, err := runGitConfig(ctx, "--file", cfgFile, "--remove-section", section)
	if err != nil {
		return err
	}
	if exitCode == 0 {
		return nil
	}
	// git config --remove-section exits 128 with "no such section" when absent.
	if exitCode == 128 && strings.Contains(strings.ToLower(stderr), "no such section") {
		return nil
	}
	return fmt.Errorf("git config --remove-section %s: %s", section, strings.TrimSpace(stderr))
}

func runGitConfig(ctx context.Context, args ...string) ([]byte, int, string, error) {
	fullArgs := append([]string{"config"}, args...)
	return runGit(ctx, "", fullArgs...)
}

func runGit(ctx context.Context, workingDir string, args ...string) ([]byte, int, string, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return nil, 0, "", fmt.Errorf("git executable not found on PATH: %w", err)
	}
	// #nosec G204 -- invoking resolved git binary with structured argv
	cmd := exec.CommandContext(ctx, gitBin, args...) //nolint:gosec
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, stderr.String(), fmt.Errorf("execute git %s: %w", strings.Join(args, " "), ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return stdout.Bytes(), exitErr.ExitCode(), stderr.String(), nil
		}
		return nil, 0, stderr.String(), fmt.Errorf("execute git %s: %w", strings.Join(args, " "), runErr)
	}
	return stdout.Bytes(), 0, stderr.String(), nil
}

// --- Shim & Symlink Management ---

func shimContent(target, binName, goos string) (string, error) {
	return shimContentWithConfig(target, "", binName, goos)
}

func shimContentWithConfig(target, configPath, binName, goos string) (string, error) {
	if strings.ContainsAny(target, "\"\r\n\x00") {
		return "", fmt.Errorf("%w: target executable path %q contains unsafe characters for shim", ErrInvalidProtocolInput, target)
	}
	cfg := strings.TrimSpace(configPath)
	if strings.ContainsAny(cfg, "\"\r\n\x00") {
		return "", fmt.Errorf("%w: config path %q contains unsafe characters for shim", ErrInvalidProtocolInput, configPath)
	}
	sig := ShimSignatureFor(binName)
	if goos == "windows" {
		escapedTarget := strings.ReplaceAll(target, "%", "%%")
		cfgArg := ""
		if cfg != "" {
			escapedCfg := strings.ReplaceAll(strings.ReplaceAll(cfg, `\`, `/`), "%", "%%")
			cfgArg = ` --config "` + escapedCfg + `"`
		}
		return "@echo off\r\nREM " + sig + "\r\n\"" + escapedTarget + "\" " + CommandName + cfgArg + " %*\r\n", nil
	}
	singleQuoted := "'" + strings.ReplaceAll(target, "'", "'\\''") + "'"
	cfgArg := ""
	if cfg != "" {
		singleQuotedCfg := "'" + strings.ReplaceAll(cfg, "'", "'\\''") + "'"
		cfgArg = " --config " + singleQuotedCfg
	}
	return "#!/bin/sh\n# " + sig + "\nexec " + singleQuoted + " " + CommandName + cfgArg + " \"$@\"\n", nil
}

// IsManagedShimFile reports whether path is a regular file starting with a
// "# <binary>-managed forwarding shim" (POSIX) or "REM <binary>-managed forwarding shim"
// (Windows) header line. Symlinks, directories, and FIFOs return false without opening.
func IsManagedShimFile(path, binaryName string) (bool, error) {
	cleaned := filepath.Clean(path)
	fi, err := os.Lstat(cleaned)
	if err != nil {
		return false, fmt.Errorf("lstat %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return false, nil
	}

	f, err := os.Open(cleaned)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, shimHeaderScanBytes)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	sig := ShimSignatureFor(binaryName)
	posixLine := "# " + sig
	winLine := "REM " + sig

	for _, line := range strings.Split(string(header[:n]), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == posixLine || line == winLine {
			return true, nil
		}
	}
	return false, nil
}

func isManagedSymlinkTarget(shimPath, binName string) (bool, string, error) {
	target, err := os.Readlink(shimPath)
	if err != nil {
		return false, "", fmt.Errorf("readlink %s: %w", shimPath, err)
	}
	baseLower := strings.ToLower(baseExecutableName(target))
	return isManagedExecutableBase(baseLower, binName), target, nil
}

func prevalidateShimTarget(shimPath, binName string, force bool) error {
	fi, err := os.Lstat(shimPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", shimPath, err)
	}
	if force {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		managed, target, linkErr := isManagedSymlinkTarget(shimPath, binName)
		if linkErr != nil {
			return linkErr
		}
		if !managed {
			return &ForeignHelperError{
				Target:   shimPath,
				Existing: "foreign symlink -> " + target,
			}
		}
		return nil
	}
	managed, err := IsManagedShimFile(shimPath, binName)
	if err != nil {
		return err
	}
	if !managed {
		return &ForeignHelperError{
			Target:   shimPath,
			Existing: "unmanaged file",
		}
	}
	return nil
}

func prevalidateShimRemoval(shimPath, binName string) error {
	fi, err := os.Lstat(shimPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", shimPath, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		managed, target, linkErr := isManagedSymlinkTarget(shimPath, binName)
		if linkErr != nil {
			return linkErr
		}
		if !managed {
			return &ForeignHelperError{
				Target:   shimPath,
				Existing: "foreign symlink -> " + target,
			}
		}
		return nil
	}
	managed, err := IsManagedShimFile(shimPath, binName)
	if err != nil {
		return err
	}
	if !managed {
		return &ForeignHelperError{
			Target:   shimPath,
			Existing: "unmanaged file (missing '# " + ShimSignatureFor(binName) + "' header)",
		}
	}
	return nil
}

func installShimOrSymlink(targetExe, configPath, shimPath, binName, goos string, force bool) (string, error) {
	if _, err := os.Lstat(shimPath); err == nil {
		if err := prevalidateShimTarget(shimPath, binName, force); err != nil {
			return "", err
		}
	}

	dir := filepath.Dir(shimPath)
	if goos != "windows" && strings.TrimSpace(configPath) == "" {
		if err := atomicWriteSymlink(targetExe, dir, shimPath); err == nil {
			return ShimMethodSymlink, nil
		}
	}

	content, err := shimContentWithConfig(targetExe, configPath, binName, goos)
	if err != nil {
		return "", err
	}
	mode := os.FileMode(0o755)
	if goos == "windows" {
		mode = 0o600
	}
	if err := atomicWriteShimFile(dir, shimPath, []byte(content), mode); err != nil {
		return "", err
	}
	return ShimMethodShim, nil
}

func atomicWriteSymlink(targetExe, dir, destPath string) error {
	tmpFile, err := os.CreateTemp(dir, ".git-credential-link-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	if err := os.Symlink(targetExe, tmpPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func atomicWriteShimFile(dir, destPath string, data []byte, mode os.FileMode) error {
	tmpFile, err := os.CreateTemp(dir, ".git-credential-shim-*")
	if err != nil {
		return fmt.Errorf("create temp shim in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write shim %s: %w", destPath, err)
	}
	if err := tmpFile.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod shim %s: %w", destPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close shim %s: %w", destPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		// On Windows os.Rename may fail if destPath exists; remove and retry once.
		if runtime.GOOS == "windows" {
			_ = os.Remove(destPath)
			if retryErr := os.Rename(tmpPath, destPath); retryErr == nil {
				return nil
			}
		}
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace shim %s: %w", destPath, err)
	}
	return nil
}

func removeManagedShimOrSymlink(shimPath, binName string) (bool, error) {
	_, err := os.Lstat(shimPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", shimPath, err)
	}
	if err := prevalidateShimRemoval(shimPath, binName); err != nil {
		return false, err
	}
	if err := os.Remove(shimPath); err != nil {
		return false, fmt.Errorf("remove %s: %w", shimPath, err)
	}
	return true, nil
}

func checkGitHubScopesWarning(ctx context.Context, reg *auth.Registry, binName, profileOverride string, globalHelper bool, targetURLs []string, cfg *config.Config) string {
	if reg == nil {
		return ""
	}
	targetsGitHub := globalHelper
	for _, u := range targetURLs {
		if parsed, err := url.Parse(u); err == nil {
			if InferGitAuthHandlerForProtocol(parsed.Scheme, parsed.Host, cfg) == "github" {
				targetsGitHub = true
				break
			}
		}
	}
	if !targetsGitHub {
		return ""
	}

	// Only inspect an already-registered/cached handler; never trigger a network
	// plugin download as a side effect of writing gitconfig.
	gh, ok := reg.GetRegistered("github")
	if !ok || gh == nil {
		return ""
	}

	statusCtx := ctx
	if cfg != nil && config.FromContext(statusCtx) == nil {
		statusCtx = config.WithConfig(statusCtx, cfg)
	}
	if strings.TrimSpace(profileOverride) != "" {
		statusCtx = auth.WithProfile(statusCtx, auth.NormalizeProfileName(profileOverride))
		statusCtx = auth.WithProfileResolved(statusCtx)
	} else {
		prof := auth.ResolveActiveProfile(statusCtx, "github")
		statusCtx = auth.WithProfile(statusCtx, prof)
		statusCtx = auth.WithProfileResolved(statusCtx)
	}

	st, err := gh.Status(statusCtx)
	if err != nil || st == nil || !st.Authenticated {
		return ""
	}
	if st.Flow == auth.FlowGitHubApp || (st.Flow == auth.FlowPAT && len(st.Scopes) == 0) {
		return ""
	}
	if hasGitHubRepoScope(st.Scopes) {
		return ""
	}

	return fmt.Sprintf(
		"active 'github' session does not include 'repo' or 'public_repo' in granted scopes (%v); run '%s auth login github --force' so git push and private clone succeed",
		st.Scopes, binName,
	)
}

func hasGitHubRepoScope(scopes []string) bool {
	for _, raw := range scopes {
		for _, tok := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		}) {
			if tok == "repo" || tok == "public_repo" {
				return true
			}
		}
	}
	return false
}

func resolveBinaryName(binaryName string) string {
	s := settings.SanitizeBinaryName(strings.TrimSpace(binaryName))
	if s == "" {
		return settings.CliBinaryName
	}
	return s
}

func resolveExplicitConfigPath(rawPath, workingDir string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", nil
	}
	if strings.ContainsAny(trimmed, "\"`$\r\n\x00") {
		return "", fmt.Errorf("%w: config path %q contains characters unsafe for gitconfig", ErrInvalidProtocolInput, rawPath)
	}
	if len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/') {
		return trimmed, nil
	}
	cleaned := filepath.Clean(expandHome(trimmed))
	if !filepath.IsAbs(cleaned) && workingDir != "" {
		cleaned = filepath.Join(workingDir, cleaned)
	}
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	return filepath.Clean(cleaned), nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

func nonNilSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
