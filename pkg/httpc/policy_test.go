// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestPolicyFromAppConfig(t *testing.T) {
	t.Parallel()

	allow := true
	deny := false

	tests := []struct {
		name string
		cfg  *config.HTTPClientConfig
		// probe is checked against the derived policy.
		probe       string
		wantAllowed bool
	}{
		{
			name:        "nil config denies private",
			cfg:         nil,
			probe:       "10.0.0.5",
			wantAllowed: false,
		},
		{
			name:        "zero config denies private",
			cfg:         &config.HTTPClientConfig{},
			probe:       "192.168.1.10",
			wantAllowed: false,
		},
		{
			name:        "public address allowed by default",
			cfg:         &config.HTTPClientConfig{},
			probe:       "203.0.113.7",
			wantAllowed: true,
		},
		{
			name:        "allowPrivateIPs permits private",
			cfg:         &config.HTTPClientConfig{AllowPrivateIPs: &allow},
			probe:       "10.0.0.5",
			wantAllowed: true,
		},
		{
			name:        "allowPrivateIPs false still denies",
			cfg:         &config.HTTPClientConfig{AllowPrivateIPs: &deny},
			probe:       "10.0.0.5",
			wantAllowed: false,
		},
		{
			name: "allowlist permits a named range",
			cfg: &config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16"),
			},
			probe:       "10.42.7.9",
			wantAllowed: true,
		},
		{
			name: "allowlist does not permit ranges outside it",
			cfg: &config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16"),
			},
			probe:       "10.43.0.1",
			wantAllowed: false,
		},
		{
			name: "bare address is treated as a single host",
			cfg: &config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.7.9"),
			},
			probe:       "10.42.7.9",
			wantAllowed: true,
		},
		{
			name: "bare address does not widen to its neighbours",
			cfg: &config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.7.9"),
			},
			probe:       "10.42.7.10",
			wantAllowed: false,
		},
		{
			name: "IPv6 range is honoured",
			cfg: &config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("fd00::/8"),
			},
			probe:       "fd00::1",
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy, err := PolicyFromAppConfig(tt.cfg)
			require.NoError(t, err)
			require.NotNil(t, policy)

			err = policy.CheckIP(net.ParseIP(tt.probe))
			if tt.wantAllowed {
				assert.NoError(t, err, "%s should be permitted", tt.probe)
			} else {
				assert.Error(t, err, "%s should be denied", tt.probe)
			}
		})
	}
}

// No configuration may make cloud metadata reachable. This is the single most
// important property of the policy: reaching it hands out instance credentials.
func TestPolicyFromAppConfig_MetadataNeverAllowed(t *testing.T) {
	t.Parallel()

	allow := true
	metadata := []string{
		"169.254.169.254", // AWS, Azure, GCP, DigitalOcean
		"169.254.170.23",  // EKS Pod Identity
		"100.100.100.200", // Alibaba
	}

	configs := map[string]*config.HTTPClientConfig{
		"allowPrivateIPs": {AllowPrivateIPs: &allow},
		"allowlist covering it": {
			AllowedPrivateCIDRs: config.PrivateCIDRList("169.254.0.0/16", "100.100.0.0/16"),
		},
		"both": {
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList("0.0.0.0/0"),
		},
	}

	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy, err := PolicyFromAppConfig(cfg)
			require.NoError(t, err)

			for _, addr := range metadata {
				assert.Error(t, policy.CheckIP(net.ParseIP(addr)),
					"%s must never be reachable", addr)
			}
		})
	}
}

func TestPolicyFromAppConfig_RejectsMalformedEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry string
	}{
		{name: "hostname", entry: "not-an-address"},
		{name: "prefix out of range", entry: "10.0.0.0/33"},
		{name: "missing prefix length", entry: "10.0.0.0/"},
		{name: "empty string", entry: ""},
		{name: "whitespace only", entry: "   "},
		{name: "double prefix", entry: "10.0.0.0/8/8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList(tt.entry),
			})
			require.Error(t, err, "%q must be rejected rather than silently dropped", tt.entry)
			assert.Nil(t, policy)
			assert.Contains(t, err.Error(), AllowedPrivateCIDRsKey,
				"the error must name the setting the user has to fix")
		})
	}
}

// Precedence must match the upstream library, whose field names these mirror.
// The direction that matters: adding an allowlist to a legacy
// allowPrivateIPs: true configuration must TIGHTEN it, not be ignored.
func TestPolicyFromAppConfig_AllowlistOverridesAllowPrivateIPs(t *testing.T) {
	t.Parallel()

	allow := true

	t.Run("allowlist narrows a legacy allow-all", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16"),
		})
		require.NoError(t, err)

		assert.NoError(t, policy.CheckIP(net.ParseIP("10.42.7.9")),
			"the listed range stays reachable")
		assert.Error(t, policy.CheckIP(net.ParseIP("192.168.1.1")),
			"a private address outside the list must NOT be reachable, "+
				"even though allowPrivateIPs is true")
	})

	// A present-but-empty list is a deliberate "no exceptions".
	t.Run("empty allowlist still overrides", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList(),
		})
		require.NoError(t, err)
		assert.Error(t, policy.CheckIP(net.ParseIP("10.0.0.5")))
	})

	// Absent (nil) leaves the legacy setting in force.
	t.Run("absent allowlist leaves allowPrivateIPs in force", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowPrivateIPs: &allow,
		})
		require.NoError(t, err)
		assert.NoError(t, policy.CheckIP(net.ParseIP("10.0.0.5")))
	})
}

func TestPolicyFromAppConfig_TrustProxyResolution(t *testing.T) {
	t.Parallel()

	trust := true
	allow := true

	t.Run("defaults to false", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{})
		require.NoError(t, err)
		assert.False(t, policy.TrustProxyResolution)
	})

	// It must apply to whichever policy the rest of the config resolves to,
	// including the implicit deny-all one.
	t.Run("applies without an allowlist", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			TrustProxyResolution: &trust,
		})
		require.NoError(t, err)
		assert.True(t, policy.TrustProxyResolution)
		assert.Error(t, policy.CheckIP(net.ParseIP("10.0.0.5")),
			"trusting the proxy must not widen which addresses are allowed")
	})

	t.Run("applies alongside an allowlist", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowedPrivateCIDRs:  config.PrivateCIDRList("10.42.0.0/16"),
			TrustProxyResolution: &trust,
		})
		require.NoError(t, err)
		assert.True(t, policy.TrustProxyResolution)
		assert.NoError(t, policy.CheckIP(net.ParseIP("10.42.7.9")))
	})

	t.Run("applies alongside allowPrivateIPs", func(t *testing.T) {
		t.Parallel()
		policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowPrivateIPs:      &allow,
			TrustProxyResolution: &trust,
		})
		require.NoError(t, err)
		assert.True(t, policy.TrustProxyResolution)
		assert.NoError(t, policy.CheckIP(net.ParseIP("10.0.0.5")))
	})
}

func TestPolicyFromContext(t *testing.T) {
	t.Parallel()

	t.Run("no config denies private", func(t *testing.T) {
		t.Parallel()
		policy := PolicyFromContext(context.Background())
		require.NotNil(t, policy)
		assert.Error(t, policy.CheckIP(net.ParseIP("10.0.0.5")))
	})

	t.Run("config is honoured", func(t *testing.T) {
		t.Parallel()
		ctx := config.WithConfig(context.Background(), &config.Config{
			HTTPClient: config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("10.42.0.0/16"),
			},
		})
		policy := PolicyFromContext(ctx)
		assert.NoError(t, policy.CheckIP(net.ParseIP("10.42.7.9")))
	})

	// A configuration that cannot be parsed must narrow access, never widen it.
	t.Run("malformed config fails closed", func(t *testing.T) {
		t.Parallel()
		ctx := config.WithConfig(context.Background(), &config.Config{
			HTTPClient: config.HTTPClientConfig{
				AllowedPrivateCIDRs: config.PrivateCIDRList("nonsense"),
			},
		})
		policy := PolicyFromContext(ctx)
		require.NotNil(t, policy)
		assert.Error(t, policy.CheckIP(net.ParseIP("10.0.0.5")),
			"an unusable allowlist must fall back to denying private addresses")
	})
}

func TestTrustedProxy(t *testing.T) {
	t.Parallel()

	t.Run("nil cfg is false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, TrustedProxy(nil))
	})

	t.Run("unset field is false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, TrustedProxy(&config.HTTPClientConfig{}))
	})

	t.Run("explicit false", func(t *testing.T) {
		t.Parallel()
		trusted := false
		assert.False(t, TrustedProxy(&config.HTTPClientConfig{TrustedProxy: &trusted}))
	})

	t.Run("explicit true", func(t *testing.T) {
		t.Parallel()
		trusted := true
		assert.True(t, TrustedProxy(&config.HTTPClientConfig{TrustedProxy: &trusted}))
	})
}

func TestTrustedProxyFromContext(t *testing.T) {
	t.Parallel()

	t.Run("no config defaults to false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, TrustedProxyFromContext(context.Background()))
	})

	t.Run("config is honoured", func(t *testing.T) {
		t.Parallel()
		trusted := true
		ctx := config.WithConfig(context.Background(), &config.Config{
			HTTPClient: config.HTTPClientConfig{TrustedProxy: &trusted},
		})
		assert.True(t, TrustedProxyFromContext(ctx))
	})
}

func TestExplainBlocked(t *testing.T) {
	t.Parallel()

	t.Run("nil stays nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ExplainBlocked(nil))
	})

	t.Run("unrelated error is unchanged", func(t *testing.T) {
		t.Parallel()
		orig := errors.New("connection refused")
		assert.Equal(t, orig, ExplainBlocked(orig))
	})

	t.Run("policy denial names the setting", func(t *testing.T) {
		t.Parallel()
		orig := ErrBlockedByPolicy
		got := ExplainBlocked(orig)

		require.Error(t, got)
		assert.ErrorIs(t, got, ErrBlockedByPolicy, "wrapping must preserve errors.Is")
		assert.Contains(t, got.Error(), AllowedPrivateCIDRsKey)
		assert.Contains(t, got.Error(), "metadata")
	})

	t.Run("wrapped policy denial is still recognised", func(t *testing.T) {
		t.Parallel()
		orig := errors.Join(errors.New("dial tcp"), ErrBlockedByPolicy)
		got := ExplainBlocked(orig)
		assert.Contains(t, got.Error(), AllowedPrivateCIDRsKey)
	})
}

func BenchmarkPolicyFromAppConfig(b *testing.B) {
	cfg := &config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", "192.168.0.0/16", "fd00::/8"),
	}
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = PolicyFromAppConfig(cfg)
	}
}

// An empty allowlist overrides allowPrivateIPs, so it has to survive a config
// save/reload. As a plain []string it did not: omitempty dropped the empty list
// on save, the next load read the field as absent, and the blanket
// allowPrivateIPs: true it was overriding silently regained force -- a config
// that looked restrictive behaving permissively. The field is a *[]string so
// that "absent" and "present but empty" stay distinguishable on disk.
func TestPolicySurvivesConfigRoundTrip(t *testing.T) {
	// Cannot use t.Parallel: Manager.Save writes to a shared path per subtest.
	loopback := net.ParseIP("127.0.0.1")
	require.NotNil(t, loopback)

	// Saves cfg, reloads it from disk, and reports whether the reloaded policy
	// still denies loopback.
	roundTrip := func(t *testing.T, mutate func(*config.HTTPClientConfig)) (denied, set bool, count int) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")

		mgr := config.NewManager(path)
		cfg, err := mgr.Load()
		require.NoError(t, err)
		mutate(&cfg.HTTPClient)
		require.NoError(t, mgr.Save())

		reloaded, err := config.NewManager(path).Load()
		require.NoError(t, err)

		entries, wasSet := reloaded.HTTPClient.PrivateCIDRs()
		policy, err := PolicyFromAppConfig(&reloaded.HTTPClient)
		require.NoError(t, err)

		return policy.CheckIP(loopback) != nil, wasSet, len(entries)
	}

	allow := true

	t.Run("an empty allowlist still overrides allowPrivateIPs after a save", func(t *testing.T) {
		denied, set, count := roundTrip(t, func(h *config.HTTPClientConfig) {
			h.AllowPrivateIPs = &allow
			h.AllowedPrivateCIDRs = config.PrivateCIDRList()
		})
		assert.True(t, set, "the empty list must survive the save as 'present'")
		assert.Equal(t, 0, count)
		assert.True(t, denied, "loopback must stay denied; the empty allowlist overrides allowPrivateIPs")
	})

	// The discriminating case: without the allowlist, the same flag DOES permit
	// loopback. If this passed too, the assertion above would be proving
	// nothing.
	t.Run("without an allowlist the same flag permits loopback", func(t *testing.T) {
		denied, set, _ := roundTrip(t, func(h *config.HTTPClientConfig) {
			h.AllowPrivateIPs = &allow
		})
		assert.False(t, set, "the field was never set, so it must reload as absent")
		assert.False(t, denied, "allowPrivateIPs alone permits loopback")
	})

	t.Run("a populated allowlist survives with its entries", func(t *testing.T) {
		denied, set, count := roundTrip(t, func(h *config.HTTPClientConfig) {
			h.AllowedPrivateCIDRs = config.PrivateCIDRList("10.42.0.0/16")
		})
		assert.True(t, set)
		assert.Equal(t, 1, count)
		assert.True(t, denied, "loopback is outside the listed range")
	})
}
