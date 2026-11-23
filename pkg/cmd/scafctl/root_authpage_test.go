// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/authpage"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestHelperBinary(t *testing.T, pkgRelPath, outName string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		outName += ".exe"
	}
	outBin := filepath.Join(t.TempDir(), outName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outBin, pkgRelPath) //nolint:gosec // test helper build
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build %s: %s", pkgRelPath, string(out))
	return outBin
}

func TestRootOptions_AuthPageRendererAndAlternateBinaryName(t *testing.T) {
	t.Run("non-auth command does not eagerly invoke AuthPageRenderer even with cached official plugin", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

		// Seed a cached official auth-handler binary in <cacheDir>/mycli/plugins/
		// so Root()'s cached-official LazyAuthHandlerWrapper loop executes.
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		cachedBinDir := filepath.Join(tmpDir, "cache", "mycli", "plugins", "auth-handler-entra", "0.4.1", runtime.GOOS+"-"+runtime.GOARCH)
		require.NoError(t, os.MkdirAll(cachedBinDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cachedBinDir, "auth-handler-entra"+ext), []byte("#!/bin/sh\nexit 1\n"), 0o755)) //nolint:gosec // test stub binary

		var seen []authpage.Data
		customRenderer := func(_ context.Context, d authpage.Data) ([]byte, error) {
			seen = append(seen, d)
			return []byte("<!doctype html><html><body>EMBEDDER:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
		}

		var outBuf, errBuf bytes.Buffer
		opts := &RootOptions{
			BinaryName:       "mycli",
			AuthPageRenderer: customRenderer,
			IOStreams: &terminal.IOStreams{
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
		}

		cmd, cleanup := Root(opts)
		defer cleanup()

		assert.Equal(t, "mycli", cmd.Use)
		cmd.SetArgs([]string{"version"})
		require.NoError(t, cmd.Execute())
		assert.Empty(t, seen, "AuthPageRenderer must not run on non-auth commands even when official auth plugins are cached")
	})

	t.Run("Root AuthPluginDirs configures plugin with rendered callback page bundle", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

		pluginBin := buildTestHelperBinary(t, "../../plugin/testdata/authhandler", "scafctl-plugin-auth-test")
		pluginDir := filepath.Dir(pluginBin)

		var seen []authpage.Data
		var outBuf, errBuf bytes.Buffer
		cmd, cleanup := Root(&RootOptions{
			BinaryName:     "mycli",
			AuthPluginDirs: []string{pluginDir},
			AuthPageRenderer: func(rCtx context.Context, d authpage.Data) ([]byte, error) {
				assert.NotNil(t, rCtx)
				seen = append(seen, d)
				return []byte("<!doctype html><html><body>EAGER:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
			},
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(strings.NewReader("")),
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
		})
		defer cleanup()

		cmd.SetArgs([]string{"auth", "list", "-o", "json"})
		require.NoError(t, cmd.Execute(), "stderr: %s", errBuf.String())
		assert.ElementsMatch(t, []authpage.Data{
			{BinaryName: "mycli", State: authpage.StateSuccess},
			{BinaryName: "mycli", State: authpage.StateFailure},
		}, seen)
	})

	t.Run("Root wires BinaryName and AuthPageRenderer into customOAuth2 interactive login", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))
		t.Setenv("MYCLI_SECRETS_KEY", "test-passphrase-for-mycli")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		defer srv.Close()

		fakeBrowserBin := buildTestHelperBinary(t, "../../../tests/integration/testdata/fakebrowser", "fakebrowser")
		captureFile := filepath.Join(tmpDir, "captured-callback.html")
		t.Setenv("BROWSER", fakeBrowserBin)
		t.Setenv("SCAFCTL_TEST_CAPTURE_BODY", captureFile)
		t.Setenv("SCAFCTL_TEST_OAUTH_CODE", "CORP-CODE")

		var seen []authpage.Data
		customRenderer := func(_ context.Context, d authpage.Data) ([]byte, error) {
			seen = append(seen, d)
			return []byte("<!doctype html><html><body>WHOLE-PAGE:" + d.BinaryName + ":" + string(d.State) + "</body></html>"), nil
		}

		cfgYAML := "settings:\n  requireSecureKeyring: false\nauth:\n  customOAuth2:\n    - name: corp-sso\n      clientId: cid\n      authorizeUrl: " + srv.URL + "/authorize\n      tokenUrl: " + srv.URL + "/token\n"
		cfgFile := filepath.Join(tmpDir, "config.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte(cfgYAML), 0o600))

		var outBuf, errBuf bytes.Buffer
		cmd, cleanup := Root(&RootOptions{
			BinaryName:       "mycli",
			ConfigPath:       cfgFile,
			AuthPageRenderer: customRenderer,
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(strings.NewReader("")),
				Out:    &outBuf,
				ErrOut: &errBuf,
			},
		})
		defer cleanup()

		cmd.SetArgs([]string{"auth", "login", "corp-sso", "--flow", "interactive", "--timeout", "10s", "--force"})
		require.NoError(t, cmd.Execute(), "stderr: %s", errBuf.String())

		assert.ElementsMatch(t, []authpage.Data{
			{BinaryName: "mycli", State: authpage.StateSuccess},
			{BinaryName: "mycli", State: authpage.StateFailure},
		}, seen)

		var htmlStr string
		require.Eventually(t, func() bool {
			b, err := os.ReadFile(captureFile) //nolint:gosec // test file
			if err != nil || len(b) == 0 {
				return false
			}
			htmlStr = string(b)
			return true
		}, 3*time.Second, 15*time.Millisecond)
		assert.Equal(t, "<!doctype html><html><body>WHOLE-PAGE:mycli:success</body></html>", htmlStr)
	})
}
