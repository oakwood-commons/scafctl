// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// containing reports whether any warning mentions substr.
func containing(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// The API executes caller-submitted solutions, so binding a reachable address
// without authentication has to be announced. The default bind (127.0.0.1) is
// safe and must stay quiet, or the warning becomes noise operators learn to
// ignore.
func TestStartupWarnings_Exposure(t *testing.T) {
	t.Parallel()

	authEnabled := config.APIServerConfig{
		Host: "0.0.0.0",
		Auth: config.APIAuthConfig{AzureOIDC: config.APIAzureOIDCConfig{Enabled: true, TenantID: "tenant", ClientID: "client"}},
	}

	tests := []struct {
		name string
		api  config.APIServerConfig
		want bool
	}{
		{"non-loopback without auth warns", config.APIServerConfig{Host: "0.0.0.0"}, true},
		{"public address without auth warns", config.APIServerConfig{Host: "203.0.113.7"}, true},
		{"non-loopback with auth is silent", authEnabled, false},
		{"explicit loopback is silent", config.APIServerConfig{Host: "127.0.0.1"}, false},
		{"unset host defaults to loopback and is silent", config.APIServerConfig{}, false},
		{"IPv6 loopback is silent", config.APIServerConfig{Host: "::1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StartupWarnings(&config.Config{APIServer: tt.api})
			assert.Equal(t, tt.want, containing(got, "authentication DISABLED"))
		})
	}
}

// The remediation path is built from the configured API version. A hardcoded
// "/v1/admin/" would tell an operator running a different apiVersion to block a
// route that does not exist, leaving the real admin surface exposed.
func TestStartupWarnings_AdminPrefixFollowsAPIVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiVersion string
		wantPrefix string
	}{
		{"unset falls back to the default version", "", "/" + settings.DefaultAPIVersion + "/admin/"},
		{"explicit v1", "v1", "/v1/admin/"},
		{"explicit v2", "v2", "/v2/admin/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StartupWarnings(&config.Config{
				APIServer: config.APIServerConfig{Host: "0.0.0.0", APIVersion: tt.apiVersion},
			})
			require.NotEmpty(t, got, "the exposure warning must fire for a non-loopback bind")
			assert.True(t, containing(got, tt.wantPrefix),
				"expected the remediation path %q, got %v", tt.wantPrefix, got)
		})
	}
}

// The address policy is global config shared with the CLI and settable from the
// environment, so a server can inherit a permissive local setting. Starting one
// that way must be announced, or a deployment widens silently. A narrow
// allowlist is the recommended control and stays quiet.
func TestStartupWarnings_AllowPrivateIPs(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false

	tests := []struct {
		name       string
		httpClient config.HTTPClientConfig
		want       bool
	}{
		{"allowPrivateIPs true warns", config.HTTPClientConfig{AllowPrivateIPs: &enabled}, true},
		{"allowPrivateIPs false is silent", config.HTTPClientConfig{AllowPrivateIPs: &disabled}, false},
		{"unset is silent", config.HTTPClientConfig{}, false},
		{
			"narrow allowlist is silent",
			config.HTTPClientConfig{AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16")},
			false,
		},
		{
			"allowlist alongside the flag stays silent, since the allowlist wins",
			config.HTTPClientConfig{AllowPrivateIPs: &enabled, AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16")},
			false,
		},
		{
			"allowlist alongside the flag stays silent even when the allowlist is present-but-empty",
			config.HTTPClientConfig{AllowPrivateIPs: &enabled, AllowedPrivateCIDRs: config.PrivateCIDRList()},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StartupWarnings(&config.Config{HTTPClient: tt.httpClient})
			assert.Equal(t, tt.want, containing(got, "httpClient.allowPrivateIPs is enabled"))
		})
	}
}

// trustProxyResolution only has an effect once a proxy is actually trusted
// (httpClient.trustedProxy: true); with proxy routing disabled (the
// default), nothing is ever forwarded to a proxy, so the setting alone must
// stay silent rather than warn about behavior that cannot happen.
func TestStartupWarnings_TrustProxyResolution(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false

	tests := []struct {
		name       string
		httpClient config.HTTPClientConfig
		want       bool
	}{
		{"trustProxyResolution alone is silent (proxy routing disabled)", config.HTTPClientConfig{TrustProxyResolution: &enabled}, false},
		{"trustProxyResolution false is silent", config.HTTPClientConfig{TrustProxyResolution: &disabled}, false},
		{"unset is silent", config.HTTPClientConfig{}, false},
		{
			"trustProxyResolution warns once trustedProxy is also enabled",
			config.HTTPClientConfig{TrustProxyResolution: &enabled, TrustedProxy: &enabled},
			true,
		},
		{
			"trustedProxy alone does not trigger the trustProxyResolution warning",
			config.HTTPClientConfig{TrustedProxy: &enabled},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StartupWarnings(&config.Config{HTTPClient: tt.httpClient})
			assert.Equal(t, tt.want, containing(got, "httpClient.trustProxyResolution is enabled"))
		})
	}
}

// trustedProxy re-enables HTTP_PROXY/HTTPS_PROXY routing, so enabling it must
// warn on its own, independent of trustProxyResolution.
func TestStartupWarnings_TrustedProxy(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false

	tests := []struct {
		name       string
		httpClient config.HTTPClientConfig
		want       bool
	}{
		{"trustedProxy true warns", config.HTTPClientConfig{TrustedProxy: &enabled}, true},
		{"trustedProxy false is silent", config.HTTPClientConfig{TrustedProxy: &disabled}, false},
		{"unset is silent", config.HTTPClientConfig{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StartupWarnings(&config.Config{HTTPClient: tt.httpClient})
			assert.Equal(t, tt.want, containing(got, "httpClient.trustedProxy is enabled"))
		})
	}
}

// The allowPrivateIPs warning's metadata parenthetical promises an
// unconditional guarantee that is no longer this client's to keep once a
// trusted proxy is in play -- it must say so instead of overstating the
// guarantee.
func TestStartupWarnings_AllowPrivateIPsMetadataNoteReflectsTrustedProxy(t *testing.T) {
	t.Parallel()

	enabled := true

	t.Run("without a trusted proxy the guarantee is unconditional", func(t *testing.T) {
		t.Parallel()
		got := StartupWarnings(&config.Config{
			HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &enabled},
		})
		assert.True(t, containing(got, "Cloud metadata addresses remain blocked regardless."))
	})

	t.Run("with a trusted proxy the guarantee depends on the proxy", func(t *testing.T) {
		t.Parallel()
		got := StartupWarnings(&config.Config{
			HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &enabled, TrustedProxy: &enabled},
		})
		assert.True(t, containing(got, "depends on the proxy enforcing its own egress policy"))
		assert.False(t, containing(got, "Cloud metadata addresses remain blocked regardless."),
			"the unqualified guarantee must not appear once a trusted proxy can decide the destination")
	})
}

// Both conditions can hold at once, and each has its own remedy, so neither may
// swallow the other.
func TestStartupWarnings_ReportsEveryCondition(t *testing.T) {
	t.Parallel()

	enabled := true
	got := StartupWarnings(&config.Config{
		APIServer:  config.APIServerConfig{Host: "0.0.0.0"},
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &enabled},
	})

	assert.Len(t, got, 2)
	assert.True(t, containing(got, "authentication DISABLED"))
	assert.True(t, containing(got, "httpClient.allowPrivateIPs is enabled"))
}

// allowPrivateIPs, trustedProxy, and trustProxyResolution are independent
// settings that can each widen exposure on their own; all three firing
// together must produce three warnings, not fewer.
func TestStartupWarnings_AllExposureWarningsFireTogether(t *testing.T) {
	t.Parallel()

	enabled := true
	got := StartupWarnings(&config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs:      &enabled,
			TrustedProxy:         &enabled,
			TrustProxyResolution: &enabled,
		},
	})

	assert.Len(t, got, 3)
	assert.True(t, containing(got, "httpClient.allowPrivateIPs is enabled"))
	assert.True(t, containing(got, "httpClient.trustedProxy is enabled"))
	assert.True(t, containing(got, "httpClient.trustProxyResolution is enabled"))
}

// A safe default configuration must produce no output at all, so that any
// warning a human sees is worth reading.
func TestStartupWarnings_SafeDefaultIsSilent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, StartupWarnings(&config.Config{}))
	assert.Empty(t, StartupWarnings(nil), "a nil config must not panic")
}

// The Server method is the path the CLI actually calls, so it has to agree with
// the package-level function rather than drifting from it.
func TestServer_StartupWarnings(t *testing.T) {
	t.Parallel()

	enabled := true
	cfg := &config.Config{
		APIServer:  config.APIServerConfig{Host: "0.0.0.0"},
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &enabled},
	}

	srv, err := NewServer(WithServerConfig(cfg))
	require.NoError(t, err)

	assert.Equal(t, StartupWarnings(cfg), srv.StartupWarnings())
	assert.Len(t, srv.StartupWarnings(), 2)
}
