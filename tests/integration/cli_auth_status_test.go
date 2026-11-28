// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestIntegration_AuthStatus_MixedAuthenticatedAndUnauthenticatedHandlers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "cfg", "scafctl")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	configPath := filepath.Join(configDir, "config.yaml")
	configContent := `catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
  disableThirdPartyAuthHandlers: true
  requireSecureKeyring: false
auth:
  customOAuth2:
    - name: ci-pat
      displayName: "CI Robot PAT"
      token: "env://TEST_AUTH_STATUS_PAT_TOKEN"
      username: "ci-bot"
      defaultFlow: pat
    - name: svc-oauth
      displayName: "Service OAuth"
      authorizeURL: "https://auth.example.com/oauth/authorize"
      tokenURL: "https://auth.example.com/oauth/token"
      clientID: "svc-client"
      defaultFlow: interactive
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	env := map[string]string{
		"XDG_CONFIG_HOME":            filepath.Join(tmpDir, "cfg"),
		"XDG_DATA_HOME":              filepath.Join(tmpDir, "data"),
		"XDG_CACHE_HOME":             filepath.Join(tmpDir, "cache"),
		"SCAFCTL_SECRET_KEY":         base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32)),
		"TEST_AUTH_STATUS_PAT_TOKEN": "static-pat-token-xyz",
	}

	// Default table view must render both the authenticated handler and the unauthenticated handler.
	tableOut, stderr, code := runScafctlWithEnv(t, env, "--config", configPath, "--no-color", "auth", "status")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, tableOut, "ci-pat")
	assert.Contains(t, tableOut, "svc-oauth")
	assert.Contains(t, tableOut, "authenticated")
	assert.Contains(t, tableOut, "not logged in")

	// JSON output must include both handlers consistently.
	jsonOut, stderr, code := runScafctlWithEnv(t, env, "--config", configPath, "auth", "status", "-o", "json")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	var jsonRows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &jsonRows))
	require.Len(t, jsonRows, 2)
	assert.Equal(t, "ci-pat", jsonRows[0]["handler"])
	assert.Equal(t, true, jsonRows[0]["authenticated"])
	assert.Equal(t, "svc-oauth", jsonRows[1]["handler"])
	assert.Equal(t, false, jsonRows[1]["authenticated"])
	assert.Equal(t, "not logged in", jsonRows[1]["status"])

	// YAML output must include both handlers consistently.
	yamlOut, stderr, code := runScafctlWithEnv(t, env, "--config", configPath, "auth", "status", "-o", "yaml")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	var yamlRows []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlOut), &yamlRows))
	require.Len(t, yamlRows, 2)
	assert.Equal(t, "ci-pat", yamlRows[0]["handler"])
	assert.Equal(t, true, yamlRows[0]["authenticated"])
	assert.Equal(t, "svc-oauth", yamlRows[1]["handler"])
	assert.Equal(t, false, yamlRows[1]["authenticated"])
	assert.Equal(t, "not logged in", yamlRows[1]["status"])

	// Unfiltered --exit-code fails because svc-oauth is unauthenticated; scoped --exit-code reflects the target handler.
	_, _, exitAll := runScafctlWithEnv(t, env, "--config", configPath, "auth", "status", "--exit-code")
	assert.NotEqual(t, 0, exitAll)

	_, stderr, exitAuth := runScafctlWithEnv(t, env, "--config", configPath, "auth", "status", "ci-pat", "--exit-code")
	assert.Equal(t, 0, exitAuth, "stderr: %s", stderr)

	_, _, exitUnauth := runScafctlWithEnv(t, env, "--config", configPath, "auth", "status", "svc-oauth", "--exit-code")
	assert.NotEqual(t, 0, exitUnauth)
}

func TestIntegration_AuthStatus_EmbedderRoot_IncludesUnauthenticatedGithub(t *testing.T) {
	// Ensure package-level paths.AppName() sync.Once is anchored to the default
	// binary name before calling scafctl.Root with a custom BinaryName in-process.
	paths.SetAppName(settings.CliBinaryName)

	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `settings:
  disableOfficialCatalog: true
  disableThirdPartyAuthHandlers: true
  requireSecureKeyring: false
  noColor: true
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	execEmbedder := func(t *testing.T, args ...string) string {
		t.Helper()

		entra := auth.NewMockHandler("entra")
		entra.DisplayNameValue = "Microsoft Entra ID"
		entra.SetAuthenticated(&auth.Claims{Email: "dev@example.com"})
		entra.StatusResult.Flow = auth.FlowInteractive
		entra.StatusResult.ExpiresAt = time.Now().Add(time.Hour)

		gh := auth.NewMockHandler("github")
		gh.DisplayNameValue = "GitHub"
		gh.SetNotAuthenticated()

		var outBuf, errBuf bytes.Buffer
		rootCmd, cleanup := scafctl.Root(&scafctl.RootOptions{
			BinaryName:          "mycli",
			ConfigPath:          cfgPath,
			SecretStore:         secrets.NewMockStore(),
			BuiltinAuthHandlers: []auth.Handler{entra, gh},
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(strings.NewReader("")),
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
		})
		defer cleanup()
		rootCmd.SetArgs(args)
		require.NoError(t, rootCmd.Execute(), "args=%v stderr=%s", args, errBuf.String())
		return outBuf.String()
	}

	tableOut := execEmbedder(t, "auth", "status")
	assert.Contains(t, tableOut, "entra")
	assert.Contains(t, tableOut, "github")
	assert.Contains(t, tableOut, "not authenticated")

	jsonOut := execEmbedder(t, "auth", "status", "-o", "json")
	var jsonRows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &jsonRows))
	require.Len(t, jsonRows, 2)
	assert.Equal(t, "entra", jsonRows[0]["handler"])
	assert.Equal(t, true, jsonRows[0]["authenticated"])
	assert.Equal(t, "github", jsonRows[1]["handler"])
	assert.Equal(t, false, jsonRows[1]["authenticated"])
	assert.Equal(t, "run 'mycli auth login github' to authenticate", jsonRows[1]["hint"])

	yamlOut := execEmbedder(t, "auth", "status", "-o", "yaml")
	var yamlRows []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(yamlOut), &yamlRows))
	require.Len(t, yamlRows, 2)
	assert.Equal(t, "entra", yamlRows[0]["handler"])
	assert.Equal(t, "github", yamlRows[1]["handler"])
	assert.Equal(t, false, yamlRows[1]["authenticated"])
}
