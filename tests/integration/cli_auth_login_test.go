// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
)

func TestIntegration_AuthLogin_RootCommandAmbientAndGcloudADC(t *testing.T) {
	// Pin the process-wide XDG app name to the default binary before exercising
	// RootOptions.BinaryName overrides in-process so later integration tests see
	// paths.AppName() == settings.CliBinaryName.
	paths.SetAppName(settings.CliBinaryName)

	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgYAML := `settings:
  disableOfficialCatalog: true
  disableThirdPartyAuthHandlers: true
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgYAML), 0o600))

	newRootWithGCP := func(binaryName string, mockGCP *auth.MockFlowDetectorHandler) (*bytes.Buffer, *bytes.Buffer, func(args ...string) error) {
		outBuf := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		rootCmd, cleanup := scafctl.Root(&scafctl.RootOptions{
			BinaryName:          binaryName,
			ConfigPath:          cfgPath,
			BuiltinAuthHandlers: []auth.Handler{mockGCP},
			SecretStore:         secrets.NewMockStore(),
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(strings.NewReader("")),
				Out:    outBuf,
				ErrOut: errBuf,
			},
		})
		t.Cleanup(cleanup)
		return outBuf, errBuf, func(args ...string) error {
			rootCmd.SetArgs(args)
			return rootCmd.Execute()
		}
	}

	buildGCPHandler := func(status *auth.Status) *auth.MockFlowDetectorHandler {
		h := auth.NewMockFlowDetectorHandler("gcp")
		h.DisplayNameValue = "Google Cloud Platform"
		h.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowGcloudADC}
		h.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
		h.StatusResult = status
		h.DetectFlowsResult = []auth.FlowAvailability{
			{Flow: auth.FlowInteractive, Available: true, Reason: "workstation browser flow"},
			{Flow: auth.FlowGcloudADC, Available: true, Reason: "ADC credentials detected"},
		}
		h.LoginResult = &auth.Result{
			Claims:    &auth.Claims{Email: "user@example.com"},
			ExpiresAt: time.Now().Add(time.Hour),
		}
		return h
	}

	// 1. Ambient gcloud_adc active -> bare 'auth login gcp' proceeds with interactive login.
	gcpAmbient := buildGCPHandler(&auth.Status{
		Authenticated: true,
		Flow:          auth.FlowGcloudADC,
		Claims:        &auth.Claims{Name: "gcloud ADC (application default credentials)"},
	})
	out1, err1, exec1 := newRootWithGCP("mycli", gcpAmbient)
	require.NoError(t, exec1("auth", "login", "gcp"), "stderr: %s", err1.String())
	require.Len(t, gcpAmbient.LoginCalls, 1)
	assert.Equal(t, auth.FlowInteractive, gcpAmbient.LoginCalls[0].Flow)
	assert.Contains(t, out1.String()+err1.String(), "Logged in to Google Cloud Platform as user@example.com")
	assert.NotContains(t, out1.String()+err1.String(), "Already authenticated")

	// 2. Ambient gcloud_adc active -> '--skip-if-authenticated' skips cleanly.
	gcpSkip := buildGCPHandler(&auth.Status{
		Authenticated: true,
		Flow:          auth.FlowGcloudADC,
		Claims:        &auth.Claims{Name: "gcloud ADC (application default credentials)"},
	})
	out2, err2, exec2 := newRootWithGCP("scafctl", gcpSkip)
	require.NoError(t, exec2("auth", "login", "gcp", "--skip-if-authenticated"), "stderr: %s", err2.String())
	assert.Empty(t, gcpSkip.LoginCalls)
	assert.Contains(t, out2.String()+err2.String(), "skipping login")

	// 3. Explicit '--flow gcloud-adc' executes without --force even when a session exists.
	gcpExplicit := buildGCPHandler(&auth.Status{
		Authenticated: true,
		Flow:          auth.FlowInteractive,
		Claims:        &auth.Claims{Email: "existing@example.com"},
	})
	out3, err3, exec3 := newRootWithGCP("scafctl", gcpExplicit)
	require.NoError(t, exec3("auth", "login", "gcp", "--flow", "gcloud-adc"), "stderr: %s", err3.String())
	require.Len(t, gcpExplicit.LoginCalls, 1)
	assert.Equal(t, auth.FlowGcloudADC, gcpExplicit.LoginCalls[0].Flow)
	assert.NotContains(t, out3.String()+err3.String(), "Already authenticated")

	// 4. Stored interactive session active -> bare 'auth login gcp' prompts logout or --force.
	gcpStored := buildGCPHandler(&auth.Status{
		Authenticated: true,
		Flow:          auth.FlowInteractive,
		Claims:        &auth.Claims{Email: "stored@example.com"},
	})
	out4, err4, exec4 := newRootWithGCP("mycli", gcpStored)
	require.NoError(t, exec4("auth", "login", "gcp"), "stderr: %s", err4.String())
	assert.Empty(t, gcpStored.LoginCalls)
	combined4 := out4.String() + err4.String()
	assert.Contains(t, combined4, "Already authenticated as stored@example.com.")
	assert.Contains(t, combined4, "Use 'mycli auth logout gcp' to sign out first, or use --force to re-authenticate.")
}

func TestIntegration_AuthLogin_SubprocessAmbientAndSkipIfAuthenticated(t *testing.T) {
	t.Parallel()

	var deviceHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			deviceHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-code-123",
				"user_code":        "WXYZ-1234",
				"verification_uri": "https://auth.example.com/activate",
				"expires_in":       300,
				"interval":         1,
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "interactive-session-access-token",
				"refresh_token": "interactive-session-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := `httpClient:
  allowPrivateIPs: true
settings:
  disableOfficialCatalog: true
  disableThirdPartyAuthHandlers: true
auth:
  customOAuth2:
    - name: hybrid-cloud
      displayName: "Hybrid Cloud"
      deviceAuthURL: "` + srv.URL + `/device"
      tokenURL: "` + srv.URL + `/token"
      clientID: "hybrid-cli"
      defaultFlow: device_code
      deviceCodePollInterval: 1
      token: "env://TEST_AMBIENT_CLOUD_PAT"
      username: "ambient-robot"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	env := map[string]string{
		"XDG_CONFIG_HOME":        filepath.Join(tmpDir, "xdg-cfg"),
		"XDG_DATA_HOME":          filepath.Join(tmpDir, "xdg-data"),
		"XDG_CACHE_HOME":         filepath.Join(tmpDir, "xdg-cache"),
		"SCAFCTL_SECRET_KEY":     "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TEST_AMBIENT_CLOUD_PAT": "ambient-static-pat-value",
	}

	// Ambient PAT is active: '--skip-if-authenticated' skips cleanly, both for default flow and explicit '--flow pat'.
	stdout, stderr, code := runScafctlWithEnv(t, env, "--config", cfgPath, "auth", "login", "hybrid-cloud", "--skip-if-authenticated")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout+stderr, "skipping login")
	assert.Equal(t, int32(0), deviceHits.Load())

	stdout, stderr, code = runScafctlWithEnv(t, env, "--config", cfgPath, "auth", "login", "hybrid-cloud", "--flow", "pat", "--skip-if-authenticated")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout+stderr, "skipping login")
	assert.Equal(t, int32(0), deviceHits.Load())

	// Without '--skip-if-authenticated', bare 'auth login' proceeds past ambient PAT and establishes a stored device_code session.
	stdout, stderr, code = runScafctlWithEnv(t, env, "--config", cfgPath, "auth", "login", "hybrid-cloud")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, int32(1), deviceHits.Load())
	assert.Contains(t, stdout+stderr, "Logged in to Hybrid Cloud")
	assert.NotContains(t, stdout+stderr, "Already authenticated")

	// Now that a stored device_code session is active, bare 'auth login' warns Already authenticated instead of re-logging in.
	stdout, stderr, code = runScafctlWithEnv(t, env, "--config", cfgPath, "auth", "login", "hybrid-cloud")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, int32(1), deviceHits.Load(), "stored device_code session must not re-run device auth without --force")
	assert.Contains(t, stdout+stderr, "Already authenticated")
}
