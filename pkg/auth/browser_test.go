// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBrowserPreference(t *testing.T) {
	t.Parallel()

	ctxWithCfg := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})

	got, err := ResolveBrowserPreference(ctxWithCfg, "chrome")
	require.NoError(t, err)
	assert.Equal(t, "chrome", got, "explicit flag must override config")

	got, err = ResolveBrowserPreference(ctxWithCfg, " default ")
	require.NoError(t, err)
	assert.Equal(t, "default", got, "explicit default keyword must override config")

	got, err = ResolveBrowserPreference(ctxWithCfg, "'firefox'")
	require.NoError(t, err)
	assert.Equal(t, "firefox", got, "single-quoted preference must normalize to canonical token")

	got, err = ResolveBrowserPreference(ctxWithCfg, "")
	require.NoError(t, err)
	assert.Equal(t, "msedge", got, "config auth.browser must apply when flag is empty")

	got, err = ResolveBrowserPreference(config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "'brave'"},
	}), "")
	require.NoError(t, err)
	assert.Equal(t, "brave", got, "single-quoted config auth.browser must normalize to canonical token")

	got, err = ResolveBrowserPreference(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, got, "empty when both flag and config are unset")

	got, err = ResolveBrowserPreference(context.TODO(), "")
	require.NoError(t, err)
	assert.Empty(t, got, "empty when context carries no config")

	// Raw caller values that are non-empty must be validated before normalization,
	// never silently falling back to config or trimming invalid characters/length.
	for _, rawBad := range []string{
		"   ",
		"msedge\n",
		"  " + strings.Repeat("a", config.MaxAuthBrowserLength),
		"--headless",
	} {
		_, err = ResolveBrowserPreference(ctxWithCfg, rawBad)
		require.Error(t, err, "expected raw flag value %q to be rejected before normalization", rawBad)

		badCfg := config.WithConfig(context.Background(), &config.Config{
			Auth: config.GlobalAuthConfig{Browser: rawBad},
		})
		_, err = ResolveBrowserPreference(badCfg, "")
		require.Error(t, err, "expected raw config value %q to be rejected before normalization", rawBad)
	}
}

func TestValidateBrowserFlag(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateBrowserFlag("msedge"))
	require.NoError(t, ValidateBrowserFlag("default"))
	require.NoError(t, ValidateBrowserFlag("system"))
	require.NoError(t, ValidateBrowserFlag("/usr/bin/firefox"))

	for _, bad := range []string{"", "   ", "--headless", "-a", `"--flag"`, `'--flag'`, "msedge\n", strings.Repeat("x", config.MaxAuthBrowserLength+1)} {
		err := ValidateBrowserFlag(bad)
		require.Error(t, err, "expected error for %q", bad)
	}
}

func TestOpenBrowser_ValidationAndCancellation(t *testing.T) {
	t.Parallel()

	// Invalid preference fails closed before touching OS launchers or URL validation.
	for _, bad := range []string{
		"--remote-debugging-port=9222",
		"msedge\n",
		"   ",
		"  " + strings.Repeat("a", config.MaxAuthBrowserLength),
	} {
		err := OpenBrowser(context.Background(), "https://login.microsoftonline.com/authorize", bad)
		require.Error(t, err, "expected OpenBrowser to reject %q", bad)
		assert.Contains(t, err.Error(), "invalid browser preference")

		badCfgCtx := config.WithConfig(context.Background(), &config.Config{
			Auth: config.GlobalAuthConfig{Browser: bad},
		})
		err = OpenBrowser(badCfgCtx, "https://login.microsoftonline.com/authorize", "")
		require.Error(t, err, "expected OpenBrowser to reject config %q", bad)
		assert.Contains(t, err.Error(), "invalid browser preference")
	}

	// Invalid URL fails before touching OS launchers.
	err := OpenBrowser(context.Background(), "javascript:alert(1)", "msedge")
	require.Error(t, err)

	// Cancelled context fails before touching OS launchers.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = OpenBrowser(ctx, "https://login.microsoftonline.com/authorize", "msedge")
	require.Error(t, err)
}

func TestOpenBrowser_RoutesFallbackWarningToWriterAndSink(t *testing.T) {
	// Isolate PATH so neither bogus browser nor system default launcher runs a real GUI browser.
	emptyBin := t.TempDir()
	t.Setenv("PATH", emptyBin)
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")

	var outBuf, errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	ctx = config.WithConfig(ctx, &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "nonexistent-browser-xyz"},
	})

	// System default launcher will also fail on empty PATH (on Linux/Windows),
	// AFTER WithWarningHandler has emitted the configured-browser warning.
	_ = OpenBrowser(ctx, "https://example.com/oauth/authorize", "")
	assert.Contains(t, outBuf.String(), `configured browser "nonexistent-browser-xyz" could not be launched`)

	// Direct WarnIfBrowserUnavailable call exercises defaultBrowserProbeOps.
	outBuf.Reset()
	WarnIfBrowserUnavailable(ctx, "nonexistent-direct-probe-browser")
	assert.Contains(t, outBuf.String(), `configured browser "nonexistent-direct-probe-browser" could not be launched`)
	WarnIfBrowserUnavailable(ctx, "default")

	// Custom warning sink overrides writer output, and single-quoted preferences
	// are normalized before reaching oauth-helpers.
	var sinkMsgs []string
	sinkCtx := WithBrowserWarningSink(ctx, func(msg string) {
		sinkMsgs = append(sinkMsgs, msg)
	})
	outBuf.Reset()
	_ = OpenBrowser(sinkCtx, "https://example.com/oauth/authorize", "'another-missing-browser'")
	assert.Empty(t, outBuf.String(), "warning must go to custom sink instead of writer")
	require.Len(t, sinkMsgs, 1)
	assert.Contains(t, sinkMsgs[0], `configured browser "another-missing-browser" could not be launched`)

	// Nil sink returns original context unchanged.
	assert.Same(t, ctx, WithBrowserWarningSink(ctx, nil))
}

func TestWarnIfBrowserUnavailable_Platforms(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fakeExe := filepath.Join(tmpDir, "custom-browser")
	require.NoError(t, os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o700))

	nonExecFile := filepath.Join(tmpDir, "non-exec-browser")
	require.NoError(t, os.WriteFile(nonExecFile, []byte("#!/bin/sh\n"), 0o600))

	wslEdge := filepath.Join(tmpDir, "msedge.exe")
	require.NoError(t, os.WriteFile(wslEdge, []byte("MZ"), 0o600))

	macHome := filepath.Join(tmpDir, "mac-home")
	for _, app := range []string{
		"Microsoft Edge.app",
		"Google Chrome.app",
		"Chromium.app",
		"Firefox.app",
		"Brave Browser.app",
		"Safari.app",
		"Arc.app",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(macHome, "Applications", app), 0o755))
	}

	collectWarning := func(ctx context.Context, pref string, ops browserProbeOps) string {
		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())
		wCtx := writer.WithWriter(ctx, w)
		warnIfBrowserUnavailableWithOps(wCtx, pref, ops)
		return outBuf.String()
	}

	notFound := func(name string) (string, error) {
		return "", errors.New("not found: " + name)
	}

	t.Run("linux all known families on PATH emit no warning", func(t *testing.T) {
		t.Parallel()
		for _, tok := range []string{"msedge", "chrome", "chromium", "firefox", "brave"} {
			got := collectWarning(context.Background(), tok, browserProbeOps{
				goos: "linux",
				lookPath: func(name string) (string, error) {
					return "/usr/bin/" + name, nil
				},
			})
			assert.Empty(t, got, "token %s should resolve", tok)
		}
	})

	t.Run("linux missing browser emits warning", func(t *testing.T) {
		t.Parallel()
		got := collectWarning(context.Background(), "bogus-browser", browserProbeOps{
			goos:     "linux",
			lookPath: notFound,
		})
		assert.Contains(t, got, `configured browser "bogus-browser" could not be launched`)
		assert.Contains(t, got, "falling back to system default browser")
	})

	t.Run("default and system keywords never warn", func(t *testing.T) {
		t.Parallel()
		for _, kw := range []string{"default", "system", `"default"`, `'system'`} {
			got := collectWarning(context.Background(), kw, browserProbeOps{
				goos:     "linux",
				lookPath: notFound,
			})
			assert.Empty(t, got, "keyword %s should not warn", kw)
		}
	})

	t.Run("linux safari warns unsupported", func(t *testing.T) {
		t.Parallel()
		got := collectWarning(context.Background(), "safari", browserProbeOps{
			goos:     "linux",
			lookPath: notFound,
		})
		assert.Contains(t, got, "safari is not supported on linux")
	})

	t.Run("linux WSL discovers msedge.exe on PATH and wslPaths via stat", func(t *testing.T) {
		t.Parallel()
		got := collectWarning(context.Background(), "msedge", browserProbeOps{
			goos: "linux",
			getenv: func(k string) string {
				if k == "WSL_DISTRO_NAME" {
					return "Ubuntu"
				}
				return ""
			},
			lookPath: func(name string) (string, error) {
				if name == "msedge.exe" {
					return wslEdge, nil
				}
				return notFound(name)
			},
		})
		assert.Empty(t, got)

		regInfo, err := os.Stat(fakeExe)
		require.NoError(t, err)
		wslStatOut := collectWarning(context.Background(), "chrome", browserProbeOps{
			goos: "linux",
			getenv: func(k string) string {
				if k == "WSL_INTEROP" {
					return "/run/WSL/1_interop"
				}
				return ""
			},
			lookPath: notFound,
			stat: func(p string) (os.FileInfo, error) {
				if p == "/mnt/c/Program Files/Google/Chrome/Application/chrome.exe" {
					return regInfo, nil
				}
				return nil, os.ErrNotExist
			},
		})
		assert.Empty(t, wslStatOut)
	})

	t.Run("linux custom executable path checks regular file and execute bit", func(t *testing.T) {
		t.Parallel()
		okOut := collectWarning(context.Background(), fakeExe, browserProbeOps{
			goos: "linux",
			stat: os.Stat,
		})
		assert.Empty(t, okOut)

		nonExecOut := collectWarning(context.Background(), nonExecFile, browserProbeOps{
			goos: "linux",
			stat: os.Stat,
		})
		assert.Contains(t, nonExecOut, "is not executable")

		dirOut := collectWarning(context.Background(), tmpDir, browserProbeOps{
			goos: "linux",
			stat: os.Stat,
		})
		assert.Contains(t, dirOut, "is a directory")

		nilStatOut := collectWarning(context.Background(), fakeExe, browserProbeOps{
			goos: "linux",
		})
		assert.Contains(t, nilStatOut, "cannot be verified")

		missingPathOut := collectWarning(context.Background(), filepath.Join(tmpDir, "missing-bin"), browserProbeOps{
			goos: "linux",
			stat: os.Stat,
		})
		assert.Contains(t, missingPathOut, "not found")
	})

	t.Run("BROWSER env fallback list warns only when all entries fail", func(t *testing.T) {
		t.Parallel()
		okOut := collectWarning(context.Background(), "", browserProbeOps{
			goos: "linux",
			getenv: func(k string) string {
				if k == "BROWSER" {
					return "missing-one:chrome"
				}
				return ""
			},
			lookPath: func(name string) (string, error) {
				if name == "google-chrome" {
					return "/usr/bin/google-chrome", nil
				}
				return notFound(name)
			},
		})
		assert.Empty(t, okOut, "second entry in $BROWSER succeeded so no warning")

		defaultEnvOut := collectWarning(context.Background(), "", browserProbeOps{
			goos: "linux",
			getenv: func(k string) string {
				if k == "BROWSER" {
					return "missing-one:default"
				}
				return ""
			},
			lookPath: notFound,
		})
		assert.Empty(t, defaultEnvOut)

		warnOut := collectWarning(context.Background(), "", browserProbeOps{
			goos: "linux",
			getenv: func(k string) string {
				if k == "BROWSER" {
					return "missing-one:missing-two"
				}
				return ""
			},
			lookPath: notFound,
		})
		assert.Contains(t, warnOut, `$BROWSER ("missing-one:missing-two") could not be launched`)
	})

	t.Run("windows families, ProgramFiles fallback, drive paths, and safari rejection", func(t *testing.T) {
		t.Parallel()
		regFileInfo, err := os.Stat(nonExecFile)
		require.NoError(t, err)

		for _, tok := range []string{"msedge", "chrome", "chromium", "firefox", "brave"} {
			got := collectWarning(context.Background(), tok, browserProbeOps{
				goos: "windows",
				lookPath: func(name string) (string, error) {
					return `C:\bin\` + name, nil
				},
			})
			assert.Empty(t, got)
		}

		okOut := collectWarning(context.Background(), "msedge", browserProbeOps{
			goos: "windows",
			getenv: func(k string) string {
				if k == "ProgramFiles(x86)" {
					return `C:\Program Files (x86)`
				}
				return ""
			},
			lookPath: notFound,
			stat: func(p string) (os.FileInfo, error) {
				if p == `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe` {
					return regFileInfo, nil
				}
				return nil, os.ErrNotExist
			},
		})
		assert.Empty(t, okOut)

		// Windows drive-letter path accepts regular file without Unix execute bits.
		driveOut := collectWarning(context.Background(), `C:\Tools\custom-browser.exe`, browserProbeOps{
			goos: "windows",
			stat: func(p string) (os.FileInfo, error) {
				if p == `C:\Tools\custom-browser.exe` {
					return regFileInfo, nil
				}
				return nil, os.ErrNotExist
			},
		})
		assert.Empty(t, driveOut)

		safariOut := collectWarning(context.Background(), "safari", browserProbeOps{
			goos:     "windows",
			lookPath: notFound,
		})
		assert.Contains(t, safariOut, "safari is not supported on windows")

		winEnvOut := collectWarning(context.Background(), "", browserProbeOps{
			goos: "windows",
			getenv: func(k string) string {
				if k == "BROWSER" {
					return "missing1;customtool"
				}
				return ""
			},
			lookPath: func(name string) (string, error) {
				if name == "customtool.exe" {
					return `C:\Tools\customtool.exe`, nil
				}
				return notFound(name)
			},
		})
		assert.Empty(t, winEnvOut)
	})

	t.Run("darwin families, bundle paths, binary paths, lookPath, and non-exec check", func(t *testing.T) {
		t.Parallel()
		for _, tok := range []string{"msedge", "chrome", "chromium", "firefox", "brave", "safari", "Arc", "Arc.app"} {
			okOut := collectWarning(context.Background(), tok, browserProbeOps{
				goos: "darwin",
				getenv: func(k string) string {
					if k == "HOME" {
						return macHome
					}
					return ""
				},
				stat: os.Stat,
			})
			assert.Empty(t, okOut, "darwin token %s should resolve", tok)
		}

		// Explicit .app path with slash.
		appPathOut := collectWarning(context.Background(), filepath.Join(macHome, "Applications", "Arc.app"), browserProbeOps{
			goos: "darwin",
			stat: os.Stat,
		})
		assert.Empty(t, appPathOut)

		// Missing explicit .app path.
		missingAppPathOut := collectWarning(context.Background(), filepath.Join(macHome, "Applications", "Missing.app"), browserProbeOps{
			goos: "darwin",
			stat: os.Stat,
		})
		assert.Contains(t, missingAppPathOut, "not found")

		// Explicit executable binary path on darwin checks execute bit.
		assert.Empty(t, collectWarning(context.Background(), fakeExe, browserProbeOps{
			goos: "darwin",
			stat: os.Stat,
		}))
		assert.Contains(t, collectWarning(context.Background(), nonExecFile, browserProbeOps{
			goos: "darwin",
			stat: os.Stat,
		}), "is not executable")

		// Bare CLI binary on darwin found via lookPath.
		assert.Empty(t, collectWarning(context.Background(), "open-custom", browserProbeOps{
			goos: "darwin",
			lookPath: func(name string) (string, error) {
				if name == "open-custom" {
					return "/opt/homebrew/bin/open-custom", nil
				}
				return notFound(name)
			},
		}))

		missingOut := collectWarning(context.Background(), "brave", browserProbeOps{
			goos:     "darwin",
			lookPath: notFound,
			stat:     func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		})
		assert.Contains(t, missingOut, `configured browser "brave" could not be launched`)
	})

	t.Run("other GOOS defers without false warning", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, collectWarning(context.Background(), "firefox", browserProbeOps{
			goos: "freebsd",
		}))
	})
}
