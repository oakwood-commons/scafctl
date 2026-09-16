// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientConfig_Validate_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		Timeout:                           "30s",
		RetryMax:                          3,
		RetryWaitMin:                      "1s",
		RetryWaitMax:                      "30s",
		CacheType:                         "filesystem",
		CacheTTL:                          "10m",
		CircuitBreakerOpenTimeout:         "30s",
		CircuitBreakerMaxFailures:         5,
		CircuitBreakerHalfOpenMaxRequests: 1,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestHTTPClientConfig_Validate_EmptyConfig(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestHTTPClientConfig_Validate_InvalidTimeout(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		Timeout: "invalid",
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestHTTPClientConfig_Validate_InvalidDurationWithoutUnit(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		Timeout: "30", // missing unit
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Contains(t, err.Error(), "missing unit")
}

func TestHTTPClientConfig_Validate_InvalidRetryWaitMin(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		RetryWaitMin: "abc",
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retryWaitMin")
}

func TestHTTPClientConfig_Validate_InvalidCacheType(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		CacheType: "disk", // invalid, should be "memory" or "filesystem"
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cacheType")
	assert.Contains(t, err.Error(), "invalid value")
	assert.Contains(t, err.Error(), "disk")
}

func TestHTTPClientConfig_Validate_ValidCacheTypes(t *testing.T) {
	t.Parallel()

	for _, cacheType := range []string{"memory", "filesystem"} {
		cfg := &HTTPClientConfig{
			CacheType: cacheType,
		}
		err := cfg.Validate()
		assert.NoError(t, err, "cache type %q should be valid", cacheType)
	}
}

func TestHTTPClientConfig_Validate_NegativeRetryMax(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		RetryMax: -1,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retryMax")
	assert.Contains(t, err.Error(), "non-negative")
}

func TestHTTPClientConfig_Validate_NegativeMemoryCacheSize(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		MemoryCacheSize: -100,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memoryCacheSize")
}

func TestHTTPClientConfig_Validate_NegativeMaxCacheFileSize(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		MaxCacheFileSize: -1,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxCacheFileSize")
}

func TestHTTPClientConfig_Validate_NegativeCircuitBreakerMaxFailures(t *testing.T) {
	t.Parallel()

	cfg := &HTTPClientConfig{
		CircuitBreakerMaxFailures: -5,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuitBreakerMaxFailures")
}

func TestConfig_Validate_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Version:  1,
		Settings: Settings{},
		Logging: LoggingConfig{
			Level: "info",
		},
		HTTPClient: HTTPClientConfig{
			Timeout:   "30s",
			CacheType: "filesystem",
		},
		Catalogs: []CatalogConfig{
			{
				Name: "test",
				Type: "filesystem",
				Path: "./test",
			},
		},
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_Validate_InvalidHTTPClient(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		HTTPClient: HTTPClientConfig{
			Timeout: "invalid",
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "httpClient")
	assert.Contains(t, err.Error(), "timeout")
}

func TestConfig_Validate_InvalidCatalogHTTPClient(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{
				Name: "test",
				Type: "http",
				HTTPClient: &HTTPClientConfig{
					CacheType: "invalid-type",
				},
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalogs[0]")
	assert.Contains(t, err.Error(), "httpClient")
	assert.Contains(t, err.Error(), "cacheType")
}

func TestConfig_Validate_InvalidCatalogType(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{
				Name: "test",
				Type: "invalid-type",
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalogs[0]")
	assert.Contains(t, err.Error(), "type")
}

func TestConfig_Validate_InvalidTokenPassThrough(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIServer: APIServerConfig{
			TokenPassThrough: &TokenPassThroughConfig{
				AllowedHeaders: []string{"Bad Header"},
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiServer")
	assert.Contains(t, err.Error(), "tokenPassThrough")
	assert.Contains(t, err.Error(), "invalid HTTP header suffix")
}

func TestTokenPassThroughConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *TokenPassThroughConfig
		wantErr string
	}{
		{
			name: "nil",
			cfg:  nil,
		},
		{
			name: "valid",
			cfg: &TokenPassThroughConfig{
				AllowedHeaders: []string{"Github", "Azure-Ad"},
			},
		},
		{
			name: "explicit empty list",
			cfg: &TokenPassThroughConfig{
				AllowedHeaders: []string{},
			},
		},
		{
			name: "empty suffix",
			cfg: &TokenPassThroughConfig{
				AllowedHeaders: []string{""},
			},
			wantErr: "allowedHeaders[0]: must not be empty",
		},
		{
			name: "prefixed suffix",
			cfg: &TokenPassThroughConfig{
				AllowedHeaders: []string{fmt.Sprintf("%s%s", middleware.TokenHeaderPrefix, "Github")},
			},
			wantErr: fmt.Sprintf("allowedHeaders[0]: must not include %s prefix", middleware.TokenHeaderPrefix),
		},
		{
			name: "invalid suffix",
			cfg: &TokenPassThroughConfig{
				AllowedHeaders: []string{"Bad Header"},
			},
			wantErr: `allowedHeaders[0]: invalid HTTP header suffix "Bad Header"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConfig_CheckVersion_Current(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Version: CurrentConfigVersion,
	}

	warning := cfg.CheckVersion()
	assert.Empty(t, warning)
}

func TestConfig_CheckVersion_Missing(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Version: 0,
	}

	warning := cfg.CheckVersion()
	assert.Contains(t, warning, "no version specified")
}

func TestConfig_CheckVersion_Outdated(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Version: CurrentConfigVersion - 1,
	}

	// Only test if there's a version before current
	if CurrentConfigVersion > 1 {
		warning := cfg.CheckVersion()
		assert.Contains(t, warning, "outdated")
	}
}

func TestConfig_CheckVersion_Future(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Version: CurrentConfigVersion + 1,
	}

	warning := cfg.CheckVersion()
	assert.Contains(t, warning, "newer than supported")
}

func TestValidHTTPClientCacheTypes(t *testing.T) {
	t.Parallel()

	types := ValidHTTPClientCacheTypes()
	assert.Contains(t, types, "memory")
	assert.Contains(t, types, "filesystem")
	assert.Len(t, types, 2)
}

func TestIsValidHTTPClientCacheType(t *testing.T) {
	t.Parallel()

	assert.True(t, IsValidHTTPClientCacheType("memory"))
	assert.True(t, IsValidHTTPClientCacheType("filesystem"))
	assert.False(t, IsValidHTTPClientCacheType("disk"))
	assert.False(t, IsValidHTTPClientCacheType(""))
	assert.False(t, IsValidHTTPClientCacheType("MEMORY"))
}

func TestValidDiscoveryStrategies(t *testing.T) {
	t.Parallel()

	strategies := ValidDiscoveryStrategies()
	assert.Contains(t, strategies, "auto")
	assert.Contains(t, strategies, "index")
	assert.Contains(t, strategies, "api")
	assert.Len(t, strategies, 3)
}

func TestIsValidDiscoveryStrategy(t *testing.T) {
	t.Parallel()

	assert.True(t, IsValidDiscoveryStrategy("auto"))
	assert.True(t, IsValidDiscoveryStrategy("index"))
	assert.True(t, IsValidDiscoveryStrategy("api"))
	assert.False(t, IsValidDiscoveryStrategy(""))
	assert.False(t, IsValidDiscoveryStrategy("fast"))
	assert.False(t, IsValidDiscoveryStrategy("AUTO"))
}

func TestConfig_Validate_InvalidDiscoveryStrategy(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{
				Name:              "test",
				Type:              "oci",
				DiscoveryStrategy: "invalid",
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalogs[0]")
	assert.Contains(t, err.Error(), "discoveryStrategy")
}

func TestConfig_Validate_ValidDiscoveryStrategy(t *testing.T) {
	t.Parallel()

	for _, strategy := range []DiscoveryStrategy{DiscoveryStrategyAuto, DiscoveryStrategyIndex, DiscoveryStrategyAPI} {
		cfg := &Config{
			Catalogs: []CatalogConfig{
				{
					Name:              "test",
					Type:              "oci",
					DiscoveryStrategy: strategy,
				},
			},
		}

		err := cfg.Validate()
		assert.NoError(t, err, "strategy %q should be valid", strategy)
	}
}

func TestLoggingConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     LoggingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid default config",
			cfg: LoggingConfig{
				Level:      "info",
				Format:     LoggingFormatJSON,
				Timestamps: true,
			},
			wantErr: false,
		},
		{
			name: "valid debug level",
			cfg: LoggingConfig{
				Level:  "debug",
				Format: LoggingFormatText,
			},
			wantErr: false,
		},
		{
			name: "valid error level",
			cfg: LoggingConfig{
				Level: "error",
			},
			wantErr: false,
		},
		{
			name: "valid trace level",
			cfg: LoggingConfig{
				Level: "trace",
			},
			wantErr: false,
		},
		{
			name: "valid numeric V-level",
			cfg: LoggingConfig{
				Level: "3",
			},
			wantErr: false,
		},
		{
			name: "valid none level",
			cfg: LoggingConfig{
				Level: "none",
			},
			wantErr: false,
		},
		{
			name: "empty format is valid",
			cfg: LoggingConfig{
				Level:  "info",
				Format: "",
			},
			wantErr: false,
		},
		{
			name: "invalid level string",
			cfg: LoggingConfig{
				Level: "verbose",
			},
			wantErr: true,
			errMsg:  "level: invalid log level",
		},
		{
			name: "valid console format",
			cfg: LoggingConfig{
				Level:  "info",
				Format: LoggingFormatConsole,
			},
			wantErr: false,
		},
		{
			name: "invalid format",
			cfg: LoggingConfig{
				Level:  "info",
				Format: "xml",
			},
			wantErr: true,
			errMsg:  "format: must be",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestAPIServerConfig_Validate_MaxHeaderBytes asserts the advertised 4MB
// ceiling is actually enforced. The `maximum` struct tag documents it for
// schema consumers but the config loader never applies struct tags, so without
// this check an operator could configure an unbounded per-connection header
// buffer despite the documented guarantee.
func TestAPIServerConfig_Validate_MaxHeaderBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"unset uses the default", 0, false},
		{"below the cap", 1 << 20, false},
		{"exactly at the cap", settings.MaxAPIMaxHeaderBytes, false},
		{"one byte over the cap", settings.MaxAPIMaxHeaderBytes + 1, true},
		{"far over the cap", 1 << 30, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{APIServer: APIServerConfig{MaxHeaderBytes: tt.value}}

			err := cfg.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "apiServer")
			assert.Contains(t, err.Error(), "maxHeaderBytes")
		})
	}
}

// TestAPIServerConfig_Validate_AllowedHosts asserts an allowlist that would be
// configured-but-inert is rejected at startup rather than silently accepting
// every Host, while both documented opt-outs stay valid. The advertised
// entry cap is enforced here too: `maxItems` is schema-only, and the allowlist
// is scanned on every request.
func TestAPIServerConfig_Validate_AllowedHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hosts   []string
		wantErr bool
	}{
		{"unset", nil, false},
		{"explicit wildcard opt-out", []string{"*"}, false},
		{"usable entries", []string{"api.example.com", "*.internal.example.com"}, false},
		{"only blanks", []string{"", "   "}, true},
		{"only the malformed wildcard", []string{"*."}, true},
		{"exactly at the entry cap", hostList(settings.MaxAPIAllowedHosts), false},
		{"one entry over the cap", hostList(settings.MaxAPIAllowedHosts + 1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{APIServer: APIServerConfig{AllowedHosts: tt.hosts}}

			err := cfg.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "apiServer")
			assert.Contains(t, err.Error(), "allowedHosts")
		})
	}
}

// hostList builds n distinct, individually valid allowlist entries so a
// length-boundary case is not conflated with a normalization failure.
func hostList(n int) []string {
	hosts := make([]string, 0, n)
	for i := range n {
		hosts = append(hosts, fmt.Sprintf("host%d.example.com", i))
	}
	return hosts
}

// TestAPIServerConfig_TagsMatchRuntimeLimits pins the struct tags that document
// the apiServer bounds to the constants that actually enforce them. The tags
// are what schema consumers read; the constants are what Validate checks.
// Nothing but this test ties the two together, and a schema that advertises a
// bound the runtime does not apply is exactly the drift these limits exist to
// close.
func TestAPIServerConfig_TagsMatchRuntimeLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		field string
		tag   string
		want  int
	}{
		{"MaxHeaderBytes", "maximum", settings.MaxAPIMaxHeaderBytes},
		{"AllowedHosts", "maxItems", settings.MaxAPIAllowedHosts},
	}

	typ := reflect.TypeOf(APIServerConfig{})
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()

			field, ok := typ.FieldByName(tt.field)
			require.True(t, ok, "APIServerConfig has no field %s", tt.field)

			raw, ok := field.Tag.Lookup(tt.tag)
			require.True(t, ok, "%s is missing its `%s` tag, so the schema no longer advertises the bound", tt.field, tt.tag)

			got, err := strconv.Atoi(raw)
			require.NoError(t, err, "%s `%s` tag is not an integer", tt.field, tt.tag)
			assert.Equal(t, tt.want, got,
				"%s `%s:%q` disagrees with the constant Validate enforces", tt.field, tt.tag, raw)
		})
	}
}

func TestNormalizeCIDR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry string
		want  string
		// wantErrContains is checked when the entry must be rejected.
		wantErrContains string
	}{
		{name: "IPv4 CIDR passes through", entry: "10.0.0.0/8", want: "10.0.0.0/8"},
		{name: "IPv6 CIDR passes through", entry: "fd00::/8", want: "fd00::/8"},
		{name: "surrounding space is tolerated", entry: "  10.0.0.0/8  ", want: "10.0.0.0/8"},
		{name: "bare IPv4 widens to /32", entry: "10.0.0.5", want: "10.0.0.5/32"},
		{name: "bare IPv6 widens to /128", entry: "fd00::1", want: "fd00::1/128"},
		{
			name:            "empty entry is rejected",
			entry:           "",
			wantErrContains: "empty",
		},
		{
			name:            "whitespace-only entry is rejected",
			entry:           "   ",
			wantErrContains: "empty",
		},
		{
			name:            "hostname is rejected",
			entry:           "example.com",
			wantErrContains: "not a valid IP address or CIDR block",
		},
		{
			name:            "out-of-range prefix is rejected",
			entry:           "10.0.0.0/33",
			wantErrContains: "not a valid CIDR block",
		},
		{
			name:            "malformed CIDR is rejected",
			entry:           "10.0.0.0/",
			wantErrContains: "not a valid CIDR block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeCIDR(tt.entry)

			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				assert.Empty(t, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHTTPClientConfig_Validate_AllowedPrivateCIDRs(t *testing.T) {
	t.Parallel()

	t.Run("valid entries are accepted", func(t *testing.T) {
		t.Parallel()
		cfg := &HTTPClientConfig{
			AllowedPrivateCIDRs: PrivateCIDRList("10.0.0.0/8", "192.168.1.5", "fd00::/8"),
		}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("an empty list is accepted", func(t *testing.T) {
		t.Parallel()
		cfg := &HTTPClientConfig{AllowedPrivateCIDRs: PrivateCIDRList()}
		assert.NoError(t, cfg.Validate())
	})

	// A malformed entry must fail loudly at startup rather than being dropped,
	// which would silently narrow the allowlist the operator asked for.
	t.Run("a malformed entry is rejected and located", func(t *testing.T) {
		t.Parallel()
		cfg := &HTTPClientConfig{
			AllowedPrivateCIDRs: PrivateCIDRList("10.0.0.0/8", "nonsense"),
		}
		err := cfg.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "allowedPrivateCIDRs[1]",
			"the error must say which entry is wrong")
		assert.Contains(t, err.Error(), "nonsense")
	})
}
