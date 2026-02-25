// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package render provides domain logic for rendering solutions, including
// resolver execution, registry adapters, and configuration merging.
package render

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// RegistryAdapter adapts provider.Registry to action.RegistryInterface and
// the base methods of resolver.RegistryInterface.
type RegistryAdapter struct {
	Registry *provider.Registry
}

func (r *RegistryAdapter) Get(name string) (provider.Provider, bool) {
	return r.Registry.Get(name)
}

func (r *RegistryAdapter) Has(name string) bool {
	_, ok := r.Registry.Get(name)
	return ok
}

func (r *RegistryAdapter) Register(p provider.Provider) error {
	return r.Registry.Register(p)
}

func (r *RegistryAdapter) List() []provider.Provider {
	return r.Registry.ListProviders()
}

func (r *RegistryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.Registry.DescriptorLookup()
}

// ResolverRegistryAdapter adapts RegistryAdapter to resolver.RegistryInterface
// by returning errors from Get instead of a bool.
type ResolverRegistryAdapter struct {
	*RegistryAdapter
}

// Get implements resolver.RegistryInterface with error return.
func (r *ResolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.Registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// ResolverConfig holds resolver execution configuration.
type ResolverConfig struct {
	Timeout        time.Duration
	PhaseTimeout   time.Duration
	MaxConcurrency int
}

// GetEffectiveResolverConfig returns resolver config values, using app config
// as defaults when CLI flags weren't explicitly set.
func GetEffectiveResolverConfig(ctx context.Context, timeout, phaseTimeout time.Duration, flagsChanged map[string]bool) ResolverConfig {
	result := ResolverConfig{
		Timeout:      timeout,
		PhaseTimeout: phaseTimeout,
	}

	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	configValues, err := cfg.Resolver.ToResolverValues()
	if err != nil {
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse resolver config, using CLI defaults", "error", err)
		return result
	}

	if flagsChanged != nil {
		if !flagsChanged["resolver-timeout"] {
			result.Timeout = configValues.Timeout
		}
		if !flagsChanged["phase-timeout"] {
			result.PhaseTimeout = configValues.PhaseTimeout
		}
	}

	result.MaxConcurrency = configValues.MaxConcurrency
	return result
}

// NewResolverExecutor creates a resolver.Executor from a provider.Registry and ResolverConfig.
func NewResolverExecutor(registry *provider.Registry, cfg ResolverConfig) *resolver.Executor {
	adapter := &RegistryAdapter{Registry: registry}
	resolverAdapter := &ResolverRegistryAdapter{RegistryAdapter: adapter}

	opts := []resolver.ExecutorOption{
		resolver.WithDefaultTimeout(cfg.Timeout),
		resolver.WithPhaseTimeout(cfg.PhaseTimeout),
	}
	if cfg.MaxConcurrency > 0 {
		opts = append(opts, resolver.WithMaxConcurrency(cfg.MaxConcurrency))
	}
	return resolver.NewExecutor(resolverAdapter, opts...)
}

// ExecuteResolvers runs resolver execution against a solution's resolvers
// with the given params and config, returning the resolved data map.
func ExecuteResolvers(ctx context.Context, sol *solution.Solution, params map[string]any, registry *provider.Registry, cfg ResolverConfig, lgr logr.Logger) (map[string]any, error) {
	resolvers := sol.Spec.ResolversToSlice()
	executor := NewResolverExecutor(registry, cfg)

	resultCtx, err := executor.Execute(ctx, resolvers, params)
	if err != nil {
		return nil, fmt.Errorf("resolver execution failed: %w", err)
	}

	resolverCtx, ok := resolver.FromContext(resultCtx)
	if !ok {
		return nil, fmt.Errorf("failed to retrieve resolver results")
	}

	resolverData := make(map[string]any)
	for name := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if ok && result.Status == resolver.ExecutionStatusSuccess {
			resolverData[name] = result.Value
		}
	}

	lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	return resolverData, nil
}

// GetDefaultRegistry returns the builtin default provider registry.
func GetDefaultRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		lgr := logger.Get(0)
		lgr.V(0).Info("warning: failed to register some providers", "error", err)
		return provider.GetGlobalRegistry()
	}
	return reg
}
