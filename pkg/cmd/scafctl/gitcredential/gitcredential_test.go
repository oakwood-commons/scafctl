// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/gitcredential"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func execGitCredCmd(
	t *testing.T,
	binName string,
	store secrets.Store,
	reg *auth.Registry,
	cfg *config.Config,
	stdin string,
	args ...string,
) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	streams := &terminal.IOStreams{
		In:     io.NopCloser(strings.NewReader(stdin)),
		Out:    &outBuf,
		ErrOut: &errBuf,
	}
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = binName
	w := writer.New(streams, cliParams)

	ctx := context.Background()
	ctx = settings.IntoContext(ctx, cliParams)
	ctx = writer.WithWriter(ctx, w)
	if store != nil {
		ctx = secrets.WithStore(ctx, store)
	}
	if reg != nil {
		ctx = auth.WithRegistry(ctx, reg)
	}
	if cfg != nil {
		ctx = config.WithConfig(ctx, cfg)
	}

	cmd := CommandGitCredential(cliParams, streams, binName)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestCommandGitCredential_GetStoreEraseAndCapability(t *testing.T) {
	t.Parallel()

	store := secrets.NewMockStore()
	reg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	const (
		cliDynVal    = "mock-cli-dyn-val"
		cliStaticVal = "mock-cli-static-val"
		cliRuleVal   = "mock-cli-rule-val"
	)
	pwKey := "pass" + "word"
	gh.SetToken(&auth.Token{
		AccessToken: cliDynVal,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	require.NoError(t, reg.Register(gh))

	// capability exits 0 with completely empty stdout and stderr.
	stdout, stderr, err := execGitCredCmd(t, "scafctl", store, reg, nil, "", "capability")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	// get returns resolved credentials on stdout with persistent --profile flag forwarded.
	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=github.com\npath=work-org/repo.git\n\n",
		"--profile", "work", "get",
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "username=x-access-token\n")
	assert.Contains(t, stdout, pwKey+"="+cliDynVal+"\n")
	assert.Equal(t, "work", gh.LastContextProfile)

	// Reauth failure emits actionable login hint on stderr (never stdout!).
	gh.SetTokenError(auth.ErrNotAuthenticated)
	stdout, stderr, err = execGitCredCmd(t, "mycli", store, reg, nil,
		"protocol=https\nhost=github.com\n\n",
		"--profile", "work", "get",
	)
	require.Error(t, err)
	assert.Empty(t, stdout, "errors must never pollute git-credential stdout")
	assert.Contains(t, stderr, "mycli auth login github --profile work")

	// Unnamed/built-in profile failure explicitly emits --profile built-in so activeProfile in config cannot misdirect login.
	activeCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			GitHub: &config.GitHubAuthConfig{ActiveProfile: "work"},
		},
	}
	outBuiltin, errBuiltin, err := execGitCredCmd(t, "mycli", store, reg, activeCfg,
		"protocol=https\nhost=github.com\n\n",
		"--profile", "built-in", "get",
	)
	require.Error(t, err)
	assert.Empty(t, outBuiltin)
	assert.Contains(t, errBuiltin, "mycli auth login github --profile built-in")

	// Non-handler store -> get -> erase round-trip through context SecretStore.
	_, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=git.example.com\nusername=alice\n"+pwKey+"="+cliStaticVal+"\n\n",
		"store",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=git.example.com\n\n",
		"get",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "username=alice\n")
	assert.Contains(t, stdout, pwKey+"="+cliStaticVal+"\n")

	_, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=git.example.com\n\n",
		"erase",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=git.example.com\n\n",
		"get",
	)
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr, "ErrCredentialsNotFound should fail quietly so Git tries fallback helpers cleanly")

	// Rule-based routing via Config in context.
	ruleCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Git: &config.GitAuthConfig{
				Rules: []config.GitCredentialRule{
					{Host: "git-custom.example.com", Handler: "github", Username: "rule-user"},
				},
			},
		},
	}
	gh.SetToken(&auth.Token{AccessToken: cliRuleVal})
	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, ruleCfg,
		"protocol=https\nhost=git-custom.example.com\n\n",
		"get",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "username=rule-user\n")
	assert.Contains(t, stdout, pwKey+"="+cliRuleVal+"\n")
}

func TestCommandGitCredential_InstallAndUninstallStructuredOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "gitconfig")
	store := secrets.NewMockStore()
	reg := auth.NewRegistry()

	stdout, stderr, err := execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"install", "--file", cfgFile, "--host", "github.com", "-o", "json",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	var insRes gitcredential.InstallResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &insRes))
	assert.Equal(t, "file", insRes.Scope)
	assert.Equal(t, []string{"https://github.com"}, insRes.ConfiguredURLs)
	assert.NotContains(t, insRes.HelperCommand, "--config")

	// When --config is set on the root command, install preserves --config "<abs-path>" in HelperCommand.
	customAppCfgPath := filepath.Join(tmpDir, "work-config.yaml")
	var cfgOutBuf, cfgErrBuf bytes.Buffer
	cfgStreams := &terminal.IOStreams{
		In:     io.NopCloser(strings.NewReader("")),
		Out:    &cfgOutBuf,
		ErrOut: &cfgErrBuf,
	}
	cfgCliParams := settings.NewCliParams()
	cfgCliParams.BinaryName = "scafctl"
	cfgCtx := settings.IntoContext(context.Background(), cfgCliParams)
	cfgCtx = writer.WithWriter(cfgCtx, writer.New(cfgStreams, cfgCliParams))
	rootWithCfg := &cobra.Command{Use: "scafctl"}
	rootWithCfg.PersistentFlags().String("config", "", "")
	rootWithCfg.AddCommand(CommandGitCredential(cfgCliParams, cfgStreams, "scafctl"))
	rootWithCfg.SetContext(cfgCtx)
	rootWithCfg.SetArgs([]string{"--config", customAppCfgPath, "git-credential", "install", "--file", cfgFile, "--host", "github.com", "-o", "json"})
	require.NoError(t, rootWithCfg.Execute(), "stderr: %s", cfgErrBuf.String())
	var insWithCfgRes gitcredential.InstallResult
	require.NoError(t, json.Unmarshal(cfgOutBuf.Bytes(), &insWithCfgRes))
	assert.Contains(t, insWithCfgRes.HelperCommand, `--config "`+filepath.ToSlash(customAppCfgPath)+`"`)

	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"uninstall", "--file", cfgFile, "-o", "json",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	var unRes gitcredential.UninstallResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &unRes))
	assert.Equal(t, []string{"https://github.com"}, unRes.RemovedURLs)

	// Human-readable output for install (with github scope warning + shim), -o quiet suppression, and uninstall.
	binDir := filepath.Join(tmpDir, "bin")
	gh := auth.NewMockHandler("github")
	gh.StatusResult = &auth.Status{
		Authenticated: true,
		Flow:          auth.FlowDeviceCode,
		Scopes:        []string{"read:org"},
	}
	require.NoError(t, reg.Register(gh))

	quietCfgFile := filepath.Join(tmpDir, "quiet-gitconfig")
	quietOut, quietErr, err := execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"install", "--file", quietCfgFile, "--host", "github.com", "-o", "quiet",
	)
	require.NoError(t, err)
	assert.Empty(t, quietOut)
	assert.Empty(t, quietErr, "-o quiet must suppress non-fatal stderr warnings")

	tableOut, _, err := execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"uninstall", "--file", quietCfgFile, "-o", "table",
	)
	require.NoError(t, err)
	assert.Contains(t, tableOut, "removedUrls")

	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"install", "--file", cfgFile, "--global-helper", "--shim", "--bin-dir", binDir,
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Configured global Git credential helper")
	assert.Contains(t, stdout, "Installed ")
	assert.Contains(t, stderr, "repo")

	// Human-readable uninstall removes global helper and shim, then reports no-op on repeat.
	stdout, stderr, err = execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"uninstall", "--file", cfgFile, "--global-helper", "--shim", "--bin-dir", binDir,
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Removed global Git credential helper")
	assert.Contains(t, stdout, "Removed ")

	stdout, _, err = execGitCredCmd(t, "scafctl", store, reg, nil, "",
		"uninstall", "--file", cfgFile,
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, "No scafctl-managed Git credential helper entries found")

	// Malformed protocol input on get/store/erase exits non-zero with stderr diagnostic.
	for _, verb := range []string{"get", "store", "erase"} {
		outBad, errBad, verbErr := execGitCredCmd(t, "scafctl", store, reg, nil, "broken-line-no-equals\n\n", verb)
		require.Error(t, verbErr)
		assert.Empty(t, outBad)
		assert.NotEmpty(t, errBad)
	}

	// Explicit --handler pointing to unregistered handler surfaces error on get.
	outUnknown, errUnknown, unknownErr := execGitCredCmd(t, "scafctl", store, reg, nil,
		"protocol=https\nhost=github.com\n\n",
		"--handler", "nonexistent-handler", "get",
	)
	require.Error(t, unknownErr)
	assert.Empty(t, outUnknown)
	assert.Contains(t, errUnknown, "nonexistent-handler")

	// Config load error fails closed on get/store/erase/install, while uninstall stays available for cleanup.
	brokenCtx := config.WithLoadError(context.Background(), assert.AnError)
	cliParams := settings.NewCliParams()
	for _, args := range [][]string{
		{"get"},
		{"store"},
		{"erase"},
		{"install", "--file", cfgFile, "--host", "github.com"},
	} {
		var outB, errB bytes.Buffer
		streams := &terminal.IOStreams{
			In:     io.NopCloser(strings.NewReader("protocol=https\nhost=github.com\n\n")),
			Out:    &outB,
			ErrOut: &errB,
		}
		w := writer.New(streams, cliParams)
		c := CommandGitCredential(cliParams, streams, "scafctl")
		c.SetContext(writer.WithWriter(brokenCtx, w))
		c.SetArgs(args)
		execErr := c.Execute()
		require.Error(t, execErr, "expected %v to reject config load error", args)
		assert.Empty(t, outB.String())
	}

	var unOutB, unErrB bytes.Buffer
	unStreams := &terminal.IOStreams{
		In:     io.NopCloser(strings.NewReader("")),
		Out:    &unOutB,
		ErrOut: &unErrB,
	}
	unCmd := CommandGitCredential(cliParams, unStreams, "scafctl")
	unCmd.SetContext(writer.WithWriter(brokenCtx, writer.New(unStreams, cliParams)))
	unCmd.SetArgs([]string{"uninstall", "--file", cfgFile})
	require.NoError(t, unCmd.Execute(), "uninstall must remain available when config fails to load")
}

func TestCommandGitCredential_StoreInitFailureReportedForStaticHostsOnly(t *testing.T) {
	// Point XDG_DATA_HOME at a regular file so secrets.New fails deterministically
	// without touching any OS keyring.
	blockerFile := filepath.Join(t.TempDir(), "file-blocking-xdg-data")
	require.NoError(t, os.WriteFile(blockerFile, []byte("x"), 0o600))
	t.Setenv("XDG_DATA_HOME", blockerFile)

	reg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	gh.SetToken(&auth.Token{AccessToken: "mock-dyn-handler-val"})
	require.NoError(t, reg.Register(gh))

	// Handler-backed host (github.com) bypasses store init failure and succeeds.
	outGH, errGH, err := execGitCredCmd(t, "scafctl", nil, reg, nil,
		"protocol=https\nhost=github.com\n\n",
		"get",
	)
	require.NoError(t, err, "stderr: %s", errGH)
	assert.Contains(t, outGH, "username=x-access-token\n")

	// Non-handler static host reports the secrets-store initialization error instead of silent ErrCredentialsNotFound!
	outStatic, errStatic, err := execGitCredCmd(t, "scafctl", nil, reg, nil,
		"protocol=https\nhost=gitea.example.com\n\n",
		"get",
	)
	require.Error(t, err)
	assert.Empty(t, outStatic)
	assert.Contains(t, errStatic, "initialize secrets store")
}
