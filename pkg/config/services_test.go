// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCELConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  CELConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  CELConfig{},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: CELConfig{
				CacheSize:          10000,
				CostLimit:          1000000,
				UseASTBasedCaching: true,
				EnableMetrics:      ptrBool(true),
			},
			wantErr: false,
		},
		{
			name: "negative cache size",
			config: CELConfig{
				CacheSize: -1,
			},
			wantErr: true,
			errMsg:  "cacheSize: must be non-negative",
		},
		{
			name: "negative cost limit",
			config: CELConfig{
				CostLimit: -100,
			},
			wantErr: true,
			errMsg:  "costLimit: must be non-negative",
		},
		{
			name: "zero cost limit is valid (disables limiting)",
			config: CELConfig{
				CostLimit: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestResolverConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ResolverConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  ResolverConfig{},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: ResolverConfig{
				Timeout:        "30s",
				PhaseTimeout:   "5m",
				MaxConcurrency: 10,
				WarnValueSize:  1048576,
				MaxValueSize:   10485760,
				ValidateAll:    true,
			},
			wantErr: false,
		},
		{
			name: "invalid timeout duration",
			config: ResolverConfig{
				Timeout: "invalid",
			},
			wantErr: true,
			errMsg:  "timeout: invalid duration",
		},
		{
			name: "invalid phase timeout duration",
			config: ResolverConfig{
				PhaseTimeout: "bad",
			},
			wantErr: true,
			errMsg:  "phaseTimeout: invalid duration",
		},
		{
			name: "negative max concurrency",
			config: ResolverConfig{
				MaxConcurrency: -1,
			},
			wantErr: true,
			errMsg:  "maxConcurrency: must be non-negative",
		},
		{
			name: "negative warn value size",
			config: ResolverConfig{
				WarnValueSize: -1,
			},
			wantErr: true,
			errMsg:  "warnValueSize: must be non-negative",
		},
		{
			name: "negative max value size",
			config: ResolverConfig{
				MaxValueSize: -1,
			},
			wantErr: true,
			errMsg:  "maxValueSize: must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestActionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ActionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  ActionConfig{},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: ActionConfig{
				DefaultTimeout: "5m",
				GracePeriod:    "30s",
				MaxConcurrency: 5,
			},
			wantErr: false,
		},
		{
			name: "invalid default timeout",
			config: ActionConfig{
				DefaultTimeout: "bad",
			},
			wantErr: true,
			errMsg:  "defaultTimeout: invalid duration",
		},
		{
			name: "invalid grace period",
			config: ActionConfig{
				GracePeriod: "invalid",
			},
			wantErr: true,
			errMsg:  "gracePeriod: invalid duration",
		},
		{
			name: "negative max concurrency",
			config: ActionConfig{
				MaxConcurrency: -5,
			},
			wantErr: true,
			errMsg:  "maxConcurrency: must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCELConfig_ToCELValues(t *testing.T) {
	tests := []struct {
		name   string
		config CELConfig
		want   CELConfigValues
	}{
		{
			name:   "empty config uses defaults",
			config: CELConfig{},
			want: CELConfigValues{
				CacheSize:          settings.DefaultCELCacheSize,
				CostLimit:          settings.DefaultCELCostLimit,
				UseASTBasedCaching: false,
				EnableMetrics:      true, // default
			},
		},
		{
			name: "custom values",
			config: CELConfig{
				CacheSize:          5000,
				CostLimit:          500000,
				UseASTBasedCaching: true,
				EnableMetrics:      ptrBool(false),
			},
			want: CELConfigValues{
				CacheSize:          5000,
				CostLimit:          500000,
				UseASTBasedCaching: true,
				EnableMetrics:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ToCELValues()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolverConfig_ToResolverValues(t *testing.T) {
	tests := []struct {
		name    string
		config  ResolverConfig
		want    ResolverConfigValues
		wantErr bool
	}{
		{
			name:   "empty config uses defaults",
			config: ResolverConfig{},
			want: ResolverConfigValues{
				Timeout:        settings.DefaultResolverTimeout,
				PhaseTimeout:   settings.DefaultPhaseTimeout,
				MaxConcurrency: 0,
				WarnValueSize:  settings.DefaultWarnValueSize,
				MaxValueSize:   settings.DefaultMaxValueSize,
				ValidateAll:    false,
			},
		},
		{
			name: "custom values",
			config: ResolverConfig{
				Timeout:        "60s",
				PhaseTimeout:   "10m",
				MaxConcurrency: 5,
				WarnValueSize:  2000000,
				MaxValueSize:   20000000,
				ValidateAll:    true,
			},
			want: ResolverConfigValues{
				Timeout:        60 * time.Second,
				PhaseTimeout:   10 * time.Minute,
				MaxConcurrency: 5,
				WarnValueSize:  2000000,
				MaxValueSize:   20000000,
				ValidateAll:    true,
			},
		},
		{
			name: "invalid duration returns error",
			config: ResolverConfig{
				Timeout: "bad",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.ToResolverValues()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActionConfig_ToActionValues(t *testing.T) {
	tests := []struct {
		name    string
		config  ActionConfig
		want    ActionConfigValues
		wantErr bool
	}{
		{
			name:   "empty config uses defaults",
			config: ActionConfig{},
			want: ActionConfigValues{
				DefaultTimeout: settings.DefaultActionTimeout,
				GracePeriod:    settings.DefaultGracePeriod,
				MaxConcurrency: 0,
				OutputDir:      "",
			},
		},
		{
			name: "custom values",
			config: ActionConfig{
				DefaultTimeout: "10m",
				GracePeriod:    "1m",
				MaxConcurrency: 3,
				OutputDir:      "/custom/output",
			},
			want: ActionConfigValues{
				DefaultTimeout: 10 * time.Minute,
				GracePeriod:    1 * time.Minute,
				MaxConcurrency: 3,
				OutputDir:      "/custom/output",
			},
		},
		{
			name: "invalid duration returns error",
			config: ActionConfig{
				DefaultTimeout: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.ToActionValues()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkActionConfig_ToActionValues(b *testing.B) {
	cfg := ActionConfig{
		DefaultTimeout: "10s",
		GracePeriod:    "5s",
		MaxConcurrency: 4,
		OutputDir:      "/bench/output",
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = cfg.ToActionValues()
	}
}

func TestConfig_Validate_WithNewConfigTypes(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "invalid CEL config",
			config: Config{
				CEL: CELConfig{CacheSize: -1},
			},
			wantErr: true,
			errMsg:  "cel:",
		},
		{
			name: "invalid resolver config",
			config: Config{
				Resolver: ResolverConfig{MaxConcurrency: -1},
			},
			wantErr: true,
			errMsg:  "resolver:",
		},
		{
			name: "invalid action config",
			config: Config{
				Action: ActionConfig{MaxConcurrency: -1},
			},
			wantErr: true,
			errMsg:  "action:",
		},
		{
			name: "valid full config",
			config: Config{
				Version: 1,
				CEL: CELConfig{
					CacheSize: 10000,
					CostLimit: 1000000,
				},
				Resolver: ResolverConfig{
					Timeout:        "30s",
					PhaseTimeout:   "5m",
					MaxConcurrency: 10,
				},
				Action: ActionConfig{
					DefaultTimeout: "5m",
					GracePeriod:    "30s",
					MaxConcurrency: 5,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ptrBool returns a pointer to a bool value
func ptrBool(b bool) *bool {
	return &b
}

func TestGoTemplateConfig_ToGoTemplateValues(t *testing.T) {
	// Default (zero value) - should use settings defaults
	g := &GoTemplateConfig{}
	v := g.ToGoTemplateValues()
	assert.Equal(t, settings.DefaultGoTemplateCacheSize, v.CacheSize)
	assert.True(t, v.EnableMetrics) // default is true when nil
	assert.False(t, v.AllowEnvFunctions)

	// With explicit values
	cacheSize := 42
	g2 := &GoTemplateConfig{
		CacheSize:         cacheSize,
		AllowEnvFunctions: true,
	}
	enableMetrics := false
	g2.EnableMetrics = &enableMetrics
	v2 := g2.ToGoTemplateValues()
	assert.Equal(t, 42, v2.CacheSize)
	assert.False(t, v2.EnableMetrics)
	assert.True(t, v2.AllowEnvFunctions)
}
