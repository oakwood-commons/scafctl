// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"time"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// CELConfigValues holds parsed CEL configuration values.
// This avoids circular dependencies between config and celexp packages.
type CELConfigValues struct {
	CacheSize          int   `json:"cacheSize" yaml:"cacheSize" doc:"CEL program cache size" maximum:"100000" example:"1000"`
	CostLimit          int64 `json:"costLimit" yaml:"costLimit" doc:"CEL evaluation cost limit" maximum:"1000000000" example:"100000"`
	UseASTBasedCaching bool  `json:"useASTBasedCaching" yaml:"useASTBasedCaching" doc:"Use AST-based cache keys for deduplication"`
	EnableMetrics      bool  `json:"enableMetrics" yaml:"enableMetrics" doc:"Enable CEL evaluation metrics"`
}

// ToCELValues converts CELConfig to a CELConfigValues struct.
// If a config value is zero/empty, the default value from settings is used.
func (c *CELConfig) ToCELValues() CELConfigValues {
	enableMetrics := true
	if c.EnableMetrics != nil {
		enableMetrics = *c.EnableMetrics
	}

	cacheSize := c.CacheSize
	if cacheSize == 0 {
		cacheSize = settings.DefaultCELCacheSize
	}

	costLimit := c.CostLimit
	if costLimit == 0 {
		costLimit = settings.DefaultCELCostLimit
	}

	return CELConfigValues{
		CacheSize:          cacheSize,
		CostLimit:          costLimit,
		UseASTBasedCaching: c.UseASTBasedCaching,
		EnableMetrics:      enableMetrics,
	}
}

// GoTemplateConfigValues holds parsed Go template config values.
// This avoids circular dependencies between config and gotmpl packages.
type GoTemplateConfigValues struct {
	CacheSize     int  `json:"cacheSize" yaml:"cacheSize" doc:"Template compilation cache size" maximum:"100000" example:"500"`
	EnableMetrics bool `json:"enableMetrics" yaml:"enableMetrics" doc:"Enable template execution metrics"`
}

// ToGoTemplateValues converts GoTemplateConfig to a GoTemplateConfigValues struct.
// If a config value is zero/empty, the default value from settings is used.
func (g *GoTemplateConfig) ToGoTemplateValues() GoTemplateConfigValues {
	enableMetrics := true
	if g.EnableMetrics != nil {
		enableMetrics = *g.EnableMetrics
	}

	cacheSize := g.CacheSize
	if cacheSize == 0 {
		cacheSize = settings.DefaultGoTemplateCacheSize
	}

	return GoTemplateConfigValues{
		CacheSize:     cacheSize,
		EnableMetrics: enableMetrics,
	}
}

// ResolverConfigValues holds parsed resolver config values with durations.
type ResolverConfigValues struct {
	Timeout        time.Duration `json:"timeout" yaml:"timeout" doc:"Per-resolver execution timeout"`
	PhaseTimeout   time.Duration `json:"phaseTimeout" yaml:"phaseTimeout" doc:"Per-phase execution timeout"`
	MaxConcurrency int           `json:"maxConcurrency" yaml:"maxConcurrency" doc:"Maximum concurrent resolver executions" maximum:"1000" example:"10"`
	WarnValueSize  int64         `json:"warnValueSize" yaml:"warnValueSize" doc:"Value size threshold for warnings (bytes)" maximum:"1073741824" example:"1048576"`
	MaxValueSize   int64         `json:"maxValueSize" yaml:"maxValueSize" doc:"Maximum allowed value size (bytes)" maximum:"1073741824" example:"10485760"`
	ValidateAll    bool          `json:"validateAll" yaml:"validateAll" doc:"Run all validators even if one fails"`
}

// ToResolverValues converts ResolverConfig to a ResolverConfigValues struct.
// Duration strings are parsed, and zero/empty values use defaults from settings.
func (r *ResolverConfig) ToResolverValues() (ResolverConfigValues, error) {
	timeout := settings.DefaultResolverTimeout
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return ResolverConfigValues{}, err
		}
		timeout = d
	}

	phaseTimeout := settings.DefaultPhaseTimeout
	if r.PhaseTimeout != "" {
		d, err := time.ParseDuration(r.PhaseTimeout)
		if err != nil {
			return ResolverConfigValues{}, err
		}
		phaseTimeout = d
	}

	warnValueSize := r.WarnValueSize
	if warnValueSize == 0 {
		warnValueSize = settings.DefaultWarnValueSize
	}

	maxValueSize := r.MaxValueSize
	if maxValueSize == 0 {
		maxValueSize = settings.DefaultMaxValueSize
	}

	return ResolverConfigValues{
		Timeout:        timeout,
		PhaseTimeout:   phaseTimeout,
		MaxConcurrency: r.MaxConcurrency,
		WarnValueSize:  warnValueSize,
		MaxValueSize:   maxValueSize,
		ValidateAll:    r.ValidateAll,
	}, nil
}

// ActionConfigValues holds parsed action config values with durations.
type ActionConfigValues struct {
	DefaultTimeout time.Duration `json:"defaultTimeout" yaml:"defaultTimeout" doc:"Default per-action execution timeout"`
	GracePeriod    time.Duration `json:"gracePeriod" yaml:"gracePeriod" doc:"Cancellation grace period"`
	MaxConcurrency int           `json:"maxConcurrency" yaml:"maxConcurrency" doc:"Maximum concurrent action executions" maximum:"1000" example:"5"`
	OutputDir      string        `json:"outputDir" yaml:"outputDir" doc:"Default target directory for action file operations"`
}

// ToActionValues converts ActionConfig to an ActionConfigValues struct.
// Duration strings are parsed, and zero/empty values use defaults from settings.
func (a *ActionConfig) ToActionValues() (ActionConfigValues, error) {
	defaultTimeout := settings.DefaultActionTimeout
	if a.DefaultTimeout != "" {
		d, err := time.ParseDuration(a.DefaultTimeout)
		if err != nil {
			return ActionConfigValues{}, err
		}
		defaultTimeout = d
	}

	gracePeriod := settings.DefaultGracePeriod
	if a.GracePeriod != "" {
		d, err := time.ParseDuration(a.GracePeriod)
		if err != nil {
			return ActionConfigValues{}, err
		}
		gracePeriod = d
	}

	return ActionConfigValues{
		DefaultTimeout: defaultTimeout,
		GracePeriod:    gracePeriod,
		MaxConcurrency: a.MaxConcurrency,
		OutputDir:      a.OutputDir,
	}, nil
}
