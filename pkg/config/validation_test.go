package config

import (
	"testing"

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
			Level: 0,
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
				Level:      0,
				Format:     LoggingFormatJSON,
				Timestamps: true,
			},
			wantErr: false,
		},
		{
			name: "valid debug level",
			cfg: LoggingConfig{
				Level:  -1,
				Format: LoggingFormatText,
			},
			wantErr: false,
		},
		{
			name: "valid error level",
			cfg: LoggingConfig{
				Level: 2,
			},
			wantErr: false,
		},
		{
			name: "empty format is valid",
			cfg: LoggingConfig{
				Level:  0,
				Format: "",
			},
			wantErr: false,
		},
		{
			name: "invalid level too low",
			cfg: LoggingConfig{
				Level: -2,
			},
			wantErr: true,
			errMsg:  "level: must be between -1 (Debug) and 2 (Error)",
		},
		{
			name: "invalid level too high",
			cfg: LoggingConfig{
				Level: 3,
			},
			wantErr: true,
			errMsg:  "level: must be between -1 (Debug) and 2 (Error)",
		},
		{
			name: "invalid format",
			cfg: LoggingConfig{
				Level:  0,
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
