// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallAndUninstall_GitConfigOrderingForeignGuardAndShim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("default install writes forward-slashed exe, leading reset, useHttpPath, and warns on missing github repo scope", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "gitconfig")

		reg := auth.NewRegistry()
		gh := auth.NewMockHandler("github")
		gh.StatusResult = &auth.Status{
			Authenticated: true,
			Flow:          auth.FlowDeviceCode,
			Scopes:        []string{"read:packages"},
		}
		require.NoError(t, reg.Register(gh))

		appCfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				CustomOAuth2: []config.CustomOAuth2Config{
					{Name: "acme-git", GitHost: "git.example.com"},
				},
			},
		}

		res, err := Install(ctx, InstallOptions{
			BinaryName:     "scafctl",
			ExecutablePath: `C:\Program Files\scafctl\scafctl.exe`,
			File:           cfgFile,
			Config:         appCfg,
			AuthRegistry:   reg,
		})
		require.NoError(t, err)
		assert.Equal(t, `!"C:/Program Files/scafctl/scafctl.exe" git-credential`, res.HelperCommand)
		assert.Contains(t, res.ConfiguredURLs, "https://github.com")
		assert.Contains(t, res.ConfiguredURLs, "https://git.example.com")
		require.Len(t, res.Warnings, 1)
		assert.Contains(t, res.Warnings[0], "repo")

		// Verify multi-value helper starts with empty reset "" followed by the helper command.
		out, err := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get-all", "credential.https://github.com.helper").Output()
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
		assert.Equal(t, []string{"", `!"C:/Program Files/scafctl/scafctl.exe" git-credential`}, lines)

		useHTTPPath, err := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get", "credential.https://github.com.useHttpPath").Output()
		require.NoError(t, err)
		assert.Equal(t, "true", strings.TrimSpace(string(useHTTPPath)))
	})

	t.Run("transactional foreign helper check aborts before mutating any URL; --force replaces", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "gitconfig")

		// Pre-seed foreign helper on the SECOND target URL only.
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "credential.https://git.example.com.helper", "!foreign-tool").Run())

		_, err := Install(ctx, InstallOptions{
			BinaryName:     "scafctl",
			ExecutablePath: "/usr/local/bin/scafctl",
			File:           cfgFile,
			Hosts:          []string{"github.com", "git.example.com"},
		})
		require.ErrorIs(t, err, ErrForeignHelper)

		// First target URL must remain untouched!
		errCheck := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get", "credential.https://github.com.helper").Run()
		assert.Error(t, errCheck, "transactional validation must not write earlier hosts when a later host has a foreign helper")

		// With --force, both hosts are configured cleanly.
		res, err := Install(ctx, InstallOptions{
			BinaryName:     "scafctl",
			ExecutablePath: "/usr/local/bin/scafctl",
			File:           cfgFile,
			Hosts:          []string{"github.com", "git.example.com"},
			Force:          true,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"https://github.com", "https://git.example.com"}, res.ConfiguredURLs)
	})

	t.Run("preserves sub-path sections after host section across single, joint, repeated, and --global-helper installs", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "gitconfig")

		// Pre-seed a sub-path section AND a non-helper key on the host section.
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--add", "credential.https://github.com/work-org.helper", "").Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--add", "credential.https://github.com/work-org.helper", "scafctl").Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--add", "credential.https://github.com/special-org.helper", "").Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--add", "credential.https://github.com/special-org.helper", "!custom-subpath-helper").Run())

		// Install BOTH parent host and pre-existing work-org sub-path together, then repeat for idempotency.
		for range 2 {
			_, err := Install(ctx, InstallOptions{
				BinaryName:     "scafctl",
				ExecutablePath: "/usr/local/bin/scafctl",
				File:           cfgFile,
				Hosts:          []string{"https://github.com/work-org", "https://github.com"},
			})
			require.NoError(t, err)
		}

		rawBytes, err := os.ReadFile(cfgFile)
		require.NoError(t, err)
		raw := string(rawBytes)
		hostIdx := strings.Index(raw, `[credential "https://github.com"]`)
		workIdx := strings.Index(raw, `[credential "https://github.com/work-org"]`)
		subIdx := strings.Index(raw, `[credential "https://github.com/special-org"]`)
		require.NotEqual(t, -1, hostIdx)
		require.NotEqual(t, -1, workIdx)
		require.NotEqual(t, -1, subIdx)
		assert.Less(t, hostIdx, workIdx, "parent host section must precede co-installed work-org sub-path section")
		assert.Less(t, hostIdx, subIdx, "parent host section must precede pre-existing foreign sub-path section")

		// Verify idempotency: helper list has exactly 2 entries ("", helperCmd).
		workHelpers, err := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get-all", "credential.https://github.com/work-org.helper").Output()
		require.NoError(t, err)
		assert.Equal(t, []string{"", `!"/usr/local/bin/scafctl" git-credential`}, strings.Split(strings.TrimSuffix(string(workHelpers), "\n"), "\n"))

		// Installing --global-helper into a file that already has scoped sections moves scoped sections AFTER [credential]!
		_, err = Install(ctx, InstallOptions{
			BinaryName:     "scafctl",
			ExecutablePath: "/usr/local/bin/scafctl",
			File:           cfgFile,
			GlobalHelper:   true,
		})
		require.NoError(t, err)

		rawGlobalBytes, err := os.ReadFile(cfgFile)
		require.NoError(t, err)
		rawGlobal := string(rawGlobalBytes)
		unscopedIdx := strings.Index(rawGlobal, "[credential]\n")
		scopedAfterIdx := strings.Index(rawGlobal, `[credential "https://github.com/special-org"]`)
		require.NotEqual(t, -1, unscopedIdx)
		require.NotEqual(t, -1, scopedAfterIdx)
		assert.Less(t, unscopedIdx, scopedAfterIdx, "unscoped [credential] must precede scoped sections so leading reset does not clobber them")

		// Seed a custom non-helper key (username = octocat) inside github.com and verify Uninstall preserves it!
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "credential.https://github.com.username", "octocat").Run())

		unRes, err := Uninstall(ctx, UninstallOptions{
			BinaryName: "scafctl",
			File:       cfgFile,
		})
		require.NoError(t, err)
		assert.True(t, unRes.RemovedGlobalHelper)
		assert.Contains(t, unRes.RemovedURLs, "https://github.com")
		assert.Contains(t, unRes.RemovedURLs, "https://github.com/work-org")

		preservedUser, err := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get", "credential.https://github.com.username").Output()
		require.NoError(t, err)
		assert.Equal(t, "octocat", strings.TrimSpace(string(preservedUser)))

		subHelper, err := exec.CommandContext(ctx, "git", "config", "--file", cfgFile, "--get-all", "credential.https://github.com/special-org.helper").Output()
		require.NoError(t, err)
		assert.Contains(t, string(subHelper), "!custom-subpath-helper")
	})

	t.Run("shim install, atomic replacement, foreign symlink protection, and allowInsecureHttp", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "gitconfig")
		binDir := filepath.Join(tmpDir, "bin")
		fakeExe := filepath.Join(tmpDir, "mycli%dir", "mycli.exe")
		require.NoError(t, os.MkdirAll(filepath.Dir(fakeExe), 0o700))
		require.NoError(t, os.WriteFile(fakeExe, []byte("fake"), 0o600))

		// Windows mode writes a .cmd shim with REM header and %% escaped percent signs.
		res, err := Install(ctx, InstallOptions{
			BinaryName:     "mycli",
			ExecutablePath: fakeExe,
			File:           cfgFile,
			GlobalHelper:   true,
			Shim:           true,
			BinDir:         binDir,
			GOOS:           "windows",
		})
		require.NoError(t, err)
		assert.True(t, res.GlobalHelper)
		assert.Equal(t, ShimMethodShim, res.ShimMethod)
		shimData, err := os.ReadFile(res.ShimPath)
		require.NoError(t, err)
		assert.Contains(t, string(shimData), "REM mycli-managed forwarding shim")
		assert.Contains(t, string(shimData), "mycli%%dir")

		// POSIX shimContent single-quotes target so $ and backticks cannot expand.
		posixShim, err := shimContent("/opt/dir's/$HOME/mycli", "mycli", "linux")
		require.NoError(t, err)
		assert.Contains(t, posixShim, `exec '/opt/dir'\''s/$HOME/mycli' git-credential "$@"`)

		unRes, err := Uninstall(ctx, UninstallOptions{
			BinaryName:   "mycli",
			File:         cfgFile,
			GlobalHelper: true,
			Shim:         true,
			BinDir:       binDir,
			GOOS:         "windows",
		})
		require.NoError(t, err)
		assert.True(t, unRes.RemovedGlobalHelper)
		assert.True(t, unRes.ShimRemoved)
		assert.NoFileExists(t, res.ShimPath)

		// Foreign symlink at shim path is rejected on BOTH install (without --force) and uninstall!
		foreignTarget := filepath.Join(tmpDir, "foreign-cred-helper")
		require.NoError(t, os.WriteFile(foreignTarget, []byte("#!/bin/sh\n"), 0o600))
		posixShimPath := filepath.Join(binDir, "git-credential-mycli")
		require.NoError(t, os.Symlink(foreignTarget, posixShimPath))

		isShim, err := IsManagedShimFile(posixShimPath, "mycli")
		require.NoError(t, err)
		assert.False(t, isShim, "IsManagedShimFile must return false without following symlinks")

		_, err = Install(ctx, InstallOptions{
			BinaryName:     "mycli",
			ExecutablePath: fakeExe,
			File:           cfgFile,
			Hosts:          []string{"github.com"},
			Shim:           true,
			BinDir:         binDir,
			GOOS:           "linux",
		})
		require.ErrorIs(t, err, ErrForeignHelper)

		_, err = Uninstall(ctx, UninstallOptions{
			BinaryName: "mycli",
			File:       cfgFile,
			Shim:       true,
			BinDir:     binDir,
			GOOS:       "linux",
		})
		require.ErrorIs(t, err, ErrForeignHelper)
		linkDest, err := os.Readlink(posixShimPath)
		require.NoError(t, err)
		assert.Equal(t, foreignTarget, linkDest)

		// Plain HTTP non-loopback host is rejected by default, allowed when allowInsecureHttp=true, and uninstallable!
		_, err = Install(ctx, InstallOptions{
			BinaryName:     "mycli",
			ExecutablePath: "/usr/local/bin/mycli",
			File:           cfgFile,
			Hosts:          []string{"http://git.internal.example.com"},
		})
		require.ErrorIs(t, err, ErrInsecureTransport)

		insecureCfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				Git: &config.GitAuthConfig{AllowInsecureHTTP: true},
			},
		}
		httpRes, err := Install(ctx, InstallOptions{
			BinaryName:     "mycli",
			ExecutablePath: "/usr/local/bin/mycli",
			File:           cfgFile,
			Hosts:          []string{"http://git.internal.example.com"},
			Config:         insecureCfg,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"http://git.internal.example.com"}, httpRes.ConfiguredURLs)

		httpUn, err := Uninstall(ctx, UninstallOptions{
			BinaryName: "mycli",
			File:       cfgFile,
			Hosts:      []string{"http://git.internal.example.com"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"http://git.internal.example.com"}, httpUn.RemovedURLs)
	})

	t.Run("local repository scope, POSIX symlink shim, helper command flags, and error formatting", func(t *testing.T) {
		t.Parallel()
		repoDir := t.TempDir()
		require.NoError(t, exec.CommandContext(ctx, "git", "init", repoDir).Run())

		binDir := filepath.Join(t.TempDir(), "bin")
		fakeExe := filepath.Join(t.TempDir(), "scafctl")
		require.NoError(t, os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o600))

		res, err := Install(ctx, InstallOptions{
			BinaryName:     "scafctl",
			ExecutablePath: fakeExe,
			Local:          true,
			WorkingDir:     repoDir,
			Hosts:          []string{"github.com"},
			Profile:        "work",
			Handler:        "github",
			Shim:           true,
			BinDir:         binDir,
			GOOS:           "linux",
		})
		require.NoError(t, err)
		assert.Equal(t, "local", res.Scope)
		assert.Contains(t, res.HelperCommand, "--profile work --handler github")
		assert.Equal(t, ShimMethodSymlink, res.ShimMethod)

		unRes, err := Uninstall(ctx, UninstallOptions{
			BinaryName: "scafctl",
			Local:      true,
			WorkingDir: repoDir,
			Hosts:      []string{"github.com"},
			Shim:       true,
			BinDir:     binDir,
			GOOS:       "linux",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"https://github.com"}, unRes.RemovedURLs)
		assert.True(t, unRes.ShimRemoved)

		// Validation errors on mutually exclusive flags and non-repo --local.
		_, err = Install(ctx, InstallOptions{Local: true, File: filepath.Join(repoDir, "x")})
		require.Error(t, err)
		_, err = Install(ctx, InstallOptions{GlobalHelper: true, Hosts: []string{"github.com"}})
		require.Error(t, err)
		_, err = Install(ctx, InstallOptions{Local: true, WorkingDir: t.TempDir(), ExecutablePath: fakeExe, Hosts: []string{"github.com"}})
		require.Error(t, err)

		// IsManagedHelperValue distinguishes embedders, case-insensitive Windows paths, pipelines, and foreign helpers.
		assert.True(t, IsManagedHelperValue("scafctl", "scafctl"))
		assert.True(t, IsManagedHelperValue(`!"/opt/bin/git-credential-scafctl"`, "scafctl"))
		assert.True(t, IsManagedHelperValue(`!"C:/Tools/Scafctl.EXE" git-credential`, "scafctl"))
		assert.True(t, IsManagedHelperValue(`!"/opt/bin/scafctl-darwin-arm64" git-credential`, "scafctl"))
		assert.False(t, IsManagedHelperValue("!scafctl git-credential || gh auth git-credential", "scafctl"))
		assert.False(t, IsManagedHelperValue("!scafctl git-credential; echo pwn", "scafctl"))
		assert.False(t, IsManagedHelperValue("!/opt/homebrew/bin/gh auth git-credential", "scafctl"))
		assert.False(t, IsManagedHelperValue(`!"/opt/bin/othercli" git-credential`, "scafctl"))

		// Error methods on ReauthRequiredError and ForeignHelperError.
		re := &ReauthRequiredError{Handler: "github", Profile: "work", ServerURL: "https://github.com", Err: ErrCredentialsNotFound}
		assert.Contains(t, re.Error(), `profile "work"`)
		assert.ErrorIs(t, re, ErrCredentialsNotFound)
		reNoProf := &ReauthRequiredError{Handler: "github", ServerURL: "https://github.com", Err: ErrCredentialsNotFound}
		assert.NotContains(t, reNoProf.Error(), "profile")

		fe := &ForeignHelperError{Target: "credential.helper", Existing: "osxkeychain"}
		assert.Contains(t, fe.Error(), "osxkeychain")
		assert.ErrorIs(t, fe, ErrForeignHelper)
		feNoExisting := &ForeignHelperError{Target: "credential.helper"}
		assert.Contains(t, feNoExisting.Error(), "credential.helper")

		// Uninstall preserves a coexisting foreign helper in the same section when present.
		mixedCfg := filepath.Join(t.TempDir(), "mixed.gitconfig")
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", mixedCfg, "--add", "credential.https://git.example.com.helper", "").Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", mixedCfg, "--add", "credential.https://git.example.com.helper", `!"/opt/bin/scafctl" git-credential`).Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", mixedCfg, "--add", "credential.https://git.example.com.helper", "!keep-foreign").Run())
		require.NoError(t, exec.CommandContext(ctx, "git", "config", "--file", mixedCfg, "credential.https://git.example.com.useHttpPath", "true").Run())

		mixedUn, err := Uninstall(ctx, UninstallOptions{
			BinaryName: "scafctl",
			File:       mixedCfg,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"https://git.example.com"}, mixedUn.RemovedURLs)
		remHelpers, err := exec.CommandContext(ctx, "git", "config", "--file", mixedCfg, "--get-all", "credential.https://git.example.com.helper").Output()
		require.NoError(t, err)
		assert.Contains(t, string(remHelpers), "!keep-foreign")
		assert.NotContains(t, string(remHelpers), "scafctl")
	})
}

func TestResolveGitConfigTarget_GlobalEnvAndXDG(t *testing.T) {
	ctx := context.Background()
	tmpHome := t.TempDir()
	xdgDir := filepath.Join(tmpHome, "xdg")
	xdgGit := filepath.Join(xdgDir, "git", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(xdgGit), 0o700))
	require.NoError(t, os.WriteFile(xdgGit, []byte(""), 0o600))

	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	gotPath, gotScope, err := resolveGitConfigTarget(ctx, false, "", "")
	require.NoError(t, err)
	assert.Equal(t, "global", gotScope)
	assert.Equal(t, xdgGit, gotPath)

	overrideGlobal := filepath.Join(tmpHome, "custom-global-gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", overrideGlobal)
	gotOverride, gotScope2, err := resolveGitConfigTarget(ctx, false, "", "")
	require.NoError(t, err)
	assert.Equal(t, "global", gotScope2)
	assert.Equal(t, overrideGlobal, gotOverride)

	// Relative --file resolves against workingDir when provided.
	relTarget, relScope, err := resolveGitConfigTarget(ctx, false, "sub/gitconfig", tmpHome)
	require.NoError(t, err)
	assert.Equal(t, "file", relScope)
	assert.Equal(t, filepath.Join(tmpHome, "sub", "gitconfig"), relTarget)

	// Builtin profile normalization and shell expansion character rejection in BuildHelperCommand.
	cmdBuiltin, err := BuildHelperCommand("/usr/local/bin/scafctl", "built-in", "")
	require.NoError(t, err)
	assert.Contains(t, cmdBuiltin, "--profile built-in")
	for _, badExe := range []string{`bad"quote`, "/tmp/$HOME/scafctl", "/tmp/`id`/scafctl", ""} {
		_, err = BuildHelperCommand(badExe, "", "")
		require.Error(t, err, "expected error for exePath=%q", badExe)
	}
	for _, badCfg := range []string{`bad"quote.yaml`, "/tmp/$HOME/work.yaml", "/tmp/`id`/work.yaml"} {
		_, err = BuildHelperCommandWithConfig("/usr/local/bin/scafctl", badCfg, "", "")
		require.ErrorIs(t, err, ErrInvalidProtocolInput, "expected error for configPath=%q", badCfg)
	}

	withCfgCmd, err := BuildHelperCommandWithConfig(`C:\Tools\scafctl.exe`, `C:\Users\alice\work.yaml`, "work", "github")
	require.NoError(t, err)
	assert.Equal(t, `!"C:/Tools/scafctl.exe" git-credential --config "C:/Users/alice/work.yaml" --profile work --handler github`, withCfgCmd)
	assert.True(t, IsManagedHelperValue(withCfgCmd, "scafctl"))

	// Install with relative ConfigPath resolves against WorkingDir and writes forwarding shim when --shim is enabled.
	gitCfgPath := filepath.Join(tmpHome, "with-config.gitconfig")
	binDir := filepath.Join(tmpHome, "shim-bin")
	fakeExe := filepath.Join(tmpHome, "scafctl")
	require.NoError(t, os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o600))

	insWithCfg, err := Install(ctx, InstallOptions{
		BinaryName:     "scafctl",
		ExecutablePath: fakeExe,
		ConfigPath:     "configs/work.yaml",
		WorkingDir:     tmpHome,
		File:           gitCfgPath,
		Hosts:          []string{"github.com"},
		Profile:        "work",
		Shim:           true,
		BinDir:         binDir,
		GOOS:           "linux",
	})
	require.NoError(t, err)
	expectedAbsCfg := filepath.ToSlash(filepath.Join(tmpHome, "configs", "work.yaml"))
	assert.Contains(t, insWithCfg.HelperCommand, `--config "`+expectedAbsCfg+`" --profile work`)
	assert.Equal(t, ShimMethodShim, insWithCfg.ShimMethod)
	shimBytes, err := os.ReadFile(insWithCfg.ShimPath)
	require.NoError(t, err)
	assert.Contains(t, string(shimBytes), "--config '"+filepath.Join(tmpHome, "configs", "work.yaml")+"'")

	assert.Equal(t, filepath.Join(tmpHome, "bin"), expandHome("~/bin"))
}
