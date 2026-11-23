// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	oauth "github.com/oakwood-commons/oauth-helpers"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

const (
	browserPrefDefault = "default"
	browserPrefSystem  = "system"
	envBrowserName     = "BROWSER"
	envWSLDistro       = "WSL_DISTRO_NAME"
	envWSLInteropPath  = "WSL_INTEROP"
)

type browserWarningSinkKey struct{}

// WithBrowserWarningSink attaches a custom warning callback to ctx for browser
// fallback diagnostics. Interactive TUI screens use this to buffer warnings
// while Bubble Tea owns the terminal and flush them after the TUI exits.
func WithBrowserWarningSink(ctx context.Context, sink func(string)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, browserWarningSinkKey{}, sink)
}

func emitBrowserWarning(ctx context.Context, msg string) {
	if ctx == nil || msg == "" {
		return
	}
	if sink, ok := ctx.Value(browserWarningSinkKey{}).(func(string)); ok && sink != nil {
		sink(msg)
		return
	}
	if w := writer.FromContext(ctx); w != nil {
		w.Warningf("%s", msg)
	}
}

// ResolveBrowserPreference resolves the effective host-side browser preference
// using the priority chain:
//  1. Explicit flag or caller value (when non-empty)
//  2. Persistent config value (auth.browser from config.FromContext(ctx))
//  3. Empty string (allowing oauth-helpers to check $BROWSER, then OS default)
func ResolveBrowserPreference(ctx context.Context, flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	if ctx != nil {
		if cfg := config.FromContext(ctx); cfg != nil {
			if trimmed := strings.TrimSpace(cfg.Auth.Browser); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// ValidateBrowserFlag validates an explicit --browser CLI flag value.
// Unlike config.ValidateBrowser (where "" means unset), an explicit CLI flag
// must be non-empty; callers wanting to bypass $BROWSER or auth.browser should
// pass "default" or "system".
func ValidateBrowserFlag(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New(`--browser requires a non-empty browser name (use "default" to open the system default browser)`)
	}
	if err := config.ValidateBrowser(raw); err != nil {
		return fmt.Errorf("invalid --browser value: %w", err)
	}
	return nil
}

// OpenBrowser opens rawURL using the resolved browser preference
// (preference > auth.browser > $BROWSER > OS default), routing any fallback
// warnings through the context's warning sink or writer.FromContext(ctx).
// Invalid browser preference values fail closed before touching OS launchers.
func OpenBrowser(ctx context.Context, rawURL, preference string) error {
	effective := ResolveBrowserPreference(ctx, preference)
	if err := config.ValidateBrowser(effective); err != nil {
		return fmt.Errorf("invalid browser preference: %w", err)
	}
	return oauth.OpenBrowserWithPreference(
		ctx,
		rawURL,
		effective,
		oauth.WithWarningHandler(func(msg string) {
			emitBrowserWarning(ctx, msg)
		}),
	)
}

// browserProbeOps abstracts host OS inspection so unit tests can exercise
// Linux, WSL, Windows, and macOS browser resolution deterministically without
// spawning any process.
type browserProbeOps struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	stat     func(string) (os.FileInfo, error)
}

func defaultBrowserProbeOps() browserProbeOps {
	return browserProbeOps{
		goos:     runtime.GOOS,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		stat:     os.Stat,
	}
}

// WarnIfBrowserUnavailable checks whether the effective browser preference (or
// $BROWSER when preference is empty) can be resolved on the host without
// launching a browser, emitting a warning through the context's warning sink or
// writer.FromContext(ctx) when it cannot. Out-of-process auth handler plugins
// discard hclog output unless --debug is enabled, so the host runs this
// preflight before delegating an interactive login RPC to guarantee the user
// sees fallback warnings.
func WarnIfBrowserUnavailable(ctx context.Context, preference string) {
	warnIfBrowserUnavailableWithOps(ctx, preference, defaultBrowserProbeOps())
}

func warnIfBrowserUnavailableWithOps(ctx context.Context, preference string, ops browserProbeOps) {
	effective := cleanBrowserSpec(ResolveBrowserPreference(ctx, preference))
	if effective != "" {
		if isSystemDefaultBrowser(effective) {
			return
		}
		if err := probePreferredBrowser(&ops, effective); err != nil {
			emitBrowserWarning(ctx, fmt.Sprintf("configured browser %q could not be launched (%v); falling back to system default browser", effective, err))
		}
		return
	}

	rawEnv := ""
	if ops.getenv != nil {
		rawEnv = strings.TrimSpace(ops.getenv(envBrowserName))
	}
	if rawEnv == "" {
		return
	}

	entries := splitBrowserEnvList(ops.goos, rawEnv)
	if len(entries) == 0 {
		return
	}

	var lastErr error
	for _, entry := range entries {
		if isSystemDefaultBrowser(entry) {
			return
		}
		err := probePreferredBrowser(&ops, entry)
		if err == nil {
			return
		}
		lastErr = err
	}
	if lastErr != nil {
		emitBrowserWarning(ctx, fmt.Sprintf("$BROWSER (%q) could not be launched (%v); falling back to system default browser", rawEnv, lastErr))
	}
}

func cleanBrowserSpec(raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"':
		s = strings.TrimSpace(s[1 : len(s)-1])
	case len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'':
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func isSystemDefaultBrowser(token string) bool {
	return strings.EqualFold(token, browserPrefDefault) || strings.EqualFold(token, browserPrefSystem)
}

func splitBrowserEnvList(goos, raw string) []string {
	sep := ":"
	if goos == "windows" {
		sep = ";"
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if cleaned := cleanBrowserSpec(p); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

type knownBrowserFamily int

const (
	browserFamilyUnknown knownBrowserFamily = iota
	browserFamilyEdge
	browserFamilyChrome
	browserFamilyChromium
	browserFamilyFirefox
	browserFamilyBrave
	browserFamilySafari
)

func classifyBrowserFamily(token string) knownBrowserFamily {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "msedge", "edge", "microsoft-edge", "microsoft-edge-stable", "microsoft edge", "msedge.exe":
		return browserFamilyEdge
	case "chrome", "google-chrome", "google-chrome-stable", "google chrome", "chrome.exe":
		return browserFamilyChrome
	case "chromium", "chromium-browser", "chromium.exe":
		return browserFamilyChromium
	case "firefox", "mozilla-firefox", "firefox.exe":
		return browserFamilyFirefox
	case "brave", "brave-browser", "brave browser", "brave.exe":
		return browserFamilyBrave
	case "safari":
		return browserFamilySafari
	default:
		return browserFamilyUnknown
	}
}

func probePreferredBrowser(ops *browserProbeOps, token string) error {
	if err := config.ValidateBrowser(token); err != nil {
		return err
	}
	family := classifyBrowserFamily(token)
	switch ops.goos {
	case "linux":
		return probePreferredLinux(ops, family, token)
	case "windows":
		return probePreferredWindows(ops, family, token)
	case "darwin":
		return probePreferredDarwin(ops, family, token)
	default:
		return nil
	}
}

func probePreferredLinux(ops *browserProbeOps, family knownBrowserFamily, token string) error {
	if family == browserFamilySafari {
		return errors.New("safari is not supported on linux")
	}
	if binNames, wslExeNames, wslPaths, ok := linuxFamilyCandidates(family); ok {
		hasExeSuffix := strings.HasSuffix(strings.ToLower(token), ".exe")
		if !hasExeSuffix && ops.lookPath != nil {
			for _, name := range binNames {
				if bin, err := ops.lookPath(name); err == nil && bin != "" {
					return nil
				}
			}
		}
		wsl := ops.getenv != nil && (ops.getenv(envWSLDistro) != "" || ops.getenv(envWSLInteropPath) != "")
		if (wsl || hasExeSuffix) && ops.lookPath != nil {
			for _, exeName := range wslExeNames {
				if bin, err := ops.lookPath(exeName); err == nil && bin != "" {
					return nil
				}
			}
		}
		if wsl {
			for _, p := range wslPaths {
				if regularFileExists(ops.stat, p) {
					return nil
				}
			}
		}
		return fmt.Errorf("browser %q not found on PATH", token)
	}

	if strings.Contains(token, "/") {
		return checkRegularExecutableFile(ops.goos, ops.stat, token)
	}
	if ops.lookPath == nil {
		return fmt.Errorf("browser %q not found on PATH", token)
	}
	if bin, err := ops.lookPath(token); err != nil || bin == "" {
		if err != nil {
			return fmt.Errorf("browser %q not found on PATH: %w", token, err)
		}
		return fmt.Errorf("browser %q not found on PATH", token)
	}
	return nil
}

func probePreferredWindows(ops *browserProbeOps, family knownBrowserFamily, token string) error {
	if family == browserFamilySafari {
		return errors.New("safari is not supported on windows")
	}
	if exeNames, relPaths, ok := windowsFamilyCandidates(family); ok {
		if ops.lookPath != nil {
			for _, name := range exeNames {
				if bin, err := ops.lookPath(name); err == nil && bin != "" {
					return nil
				}
			}
		}
		for _, envKey := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			root := ""
			if ops.getenv != nil {
				root = strings.TrimSpace(ops.getenv(envKey))
			}
			if root == "" {
				continue
			}
			for _, rel := range relPaths {
				candidate := joinWinPath(root, rel...)
				if regularFileExists(ops.stat, candidate) {
					return nil
				}
			}
		}
		return fmt.Errorf("browser %q not found on PATH or in standard Windows install locations", token)
	}

	if strings.ContainsAny(token, `\/`) || hasDrivePrefix(token) {
		return checkRegularExecutableFile(ops.goos, ops.stat, token)
	}
	if ops.lookPath != nil {
		candidates := []string{token}
		if !strings.HasSuffix(strings.ToLower(token), ".exe") {
			candidates = append(candidates, token+".exe")
		}
		for _, cand := range candidates {
			if bin, err := ops.lookPath(cand); err == nil && bin != "" {
				return nil
			}
		}
	}
	return fmt.Errorf("browser %q not found on PATH", token)
}

func probePreferredDarwin(ops *browserProbeOps, family knownBrowserFamily, token string) error {
	if appName, ok := darwinFamilyAppName(family); ok {
		if darwinAppExists(ops, appName+".app") {
			return nil
		}
		return fmt.Errorf("macOS application %q not found", appName)
	}

	if strings.HasSuffix(strings.ToLower(token), ".app") {
		if strings.Contains(token, "/") {
			if ops.stat == nil {
				return fmt.Errorf("macOS application bundle %q cannot be verified", token)
			}
			if _, err := ops.stat(token); err != nil {
				return fmt.Errorf("macOS application bundle %q not found: %w", token, err)
			}
			return nil
		}
		if darwinAppExists(ops, token) {
			return nil
		}
		return fmt.Errorf("macOS application %q not found", token)
	}

	if strings.Contains(token, "/") {
		return checkRegularExecutableFile(ops.goos, ops.stat, token)
	}

	if ops.lookPath != nil {
		if bin, err := ops.lookPath(token); err == nil && bin != "" {
			return nil
		}
	}
	if darwinAppExists(ops, token+".app") {
		return nil
	}
	return fmt.Errorf("browser %q not found on PATH or in Applications", token)
}

func darwinAppExists(ops *browserProbeOps, bundleName string) bool {
	if ops.stat == nil {
		return false
	}
	dirs := []string{
		"/Applications",
		"/System/Applications",
		"/System/Volumes/Preboot/Cryptexes/App/System/Applications",
	}
	if ops.getenv != nil {
		if home := strings.TrimSpace(ops.getenv("HOME")); home != "" {
			dirs = append(dirs, filepath.Join(home, "Applications"))
		}
	}
	for _, dir := range dirs {
		if _, err := ops.stat(filepath.Join(dir, bundleName)); err == nil {
			return true
		}
	}
	return false
}

func darwinFamilyAppName(family knownBrowserFamily) (string, bool) {
	switch family {
	case browserFamilyEdge:
		return "Microsoft Edge", true
	case browserFamilyChrome:
		return "Google Chrome", true
	case browserFamilyChromium:
		return "Chromium", true
	case browserFamilyFirefox:
		return "Firefox", true
	case browserFamilyBrave:
		return "Brave Browser", true
	case browserFamilySafari:
		return "Safari", true
	case browserFamilyUnknown:
	}
	return "", false
}

func linuxFamilyCandidates(family knownBrowserFamily) ([]string, []string, []string, bool) {
	switch family {
	case browserFamilyEdge:
		return []string{"microsoft-edge", "microsoft-edge-stable", "msedge"},
			[]string{"msedge.exe"},
			[]string{
				"/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
				"/mnt/c/Program Files/Microsoft/Edge/Application/msedge.exe",
			},
			true
	case browserFamilyChrome:
		return []string{"google-chrome", "google-chrome-stable", "chrome"},
			[]string{"chrome.exe"},
			[]string{
				"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
				"/mnt/c/Program Files (x86)/Google/Chrome/Application/chrome.exe",
			},
			true
	case browserFamilyChromium:
		return []string{"chromium", "chromium-browser"},
			[]string{"chromium.exe"},
			[]string{
				"/mnt/c/Program Files/Chromium/Application/chrome.exe",
				"/mnt/c/Program Files (x86)/Chromium/Application/chrome.exe",
				"/mnt/c/Program Files/Chromium/Application/chromium.exe",
				"/mnt/c/Program Files (x86)/Chromium/Application/chromium.exe",
			},
			true
	case browserFamilyFirefox:
		return []string{"firefox", "mozilla-firefox"},
			[]string{"firefox.exe"},
			[]string{
				"/mnt/c/Program Files/Mozilla Firefox/firefox.exe",
				"/mnt/c/Program Files (x86)/Mozilla Firefox/firefox.exe",
			},
			true
	case browserFamilyBrave:
		return []string{"brave-browser", "brave"},
			[]string{"brave.exe"},
			[]string{
				"/mnt/c/Program Files/BraveSoftware/Brave-Browser/Application/brave.exe",
				"/mnt/c/Program Files (x86)/BraveSoftware/Brave-Browser/Application/brave.exe",
			},
			true
	case browserFamilySafari, browserFamilyUnknown:
	}
	return nil, nil, nil, false
}

func windowsFamilyCandidates(family knownBrowserFamily) ([]string, [][]string, bool) {
	switch family {
	case browserFamilyEdge:
		return []string{"msedge.exe", "msedge"},
			[][]string{{"Microsoft", "Edge", "Application", "msedge.exe"}},
			true
	case browserFamilyChrome:
		return []string{"chrome.exe", "chrome", "google-chrome"},
			[][]string{{"Google", "Chrome", "Application", "chrome.exe"}},
			true
	case browserFamilyChromium:
		return []string{"chromium.exe", "chromium"},
			[][]string{
				{"Chromium", "Application", "chrome.exe"},
				{"Chromium", "Application", "chromium.exe"},
			},
			true
	case browserFamilyFirefox:
		return []string{"firefox.exe", "firefox"},
			[][]string{{"Mozilla Firefox", "firefox.exe"}},
			true
	case browserFamilyBrave:
		return []string{"brave.exe", "brave"},
			[][]string{{"BraveSoftware", "Brave-Browser", "Application", "brave.exe"}},
			true
	case browserFamilySafari, browserFamilyUnknown:
	}
	return nil, nil, false
}

func regularFileExists(statFn func(string) (os.FileInfo, error), path string) bool {
	if statFn == nil {
		return false
	}
	info, err := statFn(path)
	return err == nil && info.Mode().IsRegular()
}

func checkRegularExecutableFile(goos string, statFn func(string) (os.FileInfo, error), path string) error {
	if statFn == nil {
		return fmt.Errorf("browser executable %q cannot be verified", path)
	}
	info, err := statFn(path)
	if err != nil {
		return fmt.Errorf("browser executable %q not found: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("browser executable %q is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("browser executable %q is not a regular file", path)
	}
	if goos != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("browser executable %q is not executable", path)
	}
	return nil
}

func joinWinPath(base string, elems ...string) string {
	out := strings.TrimRight(base, `\/`)
	for _, elem := range elems {
		trimmed := strings.Trim(elem, `\/`)
		if trimmed != "" {
			out += `\` + trimmed
		}
	}
	return out
}

func hasDrivePrefix(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
