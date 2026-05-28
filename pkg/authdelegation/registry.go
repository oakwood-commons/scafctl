// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// DelegatorRegistry maps auth provider names to TokenDelegator implementations.
// Registration happens at server startup; lookups happen per-request concurrently.
type DelegatorRegistry struct {
	mu         sync.RWMutex
	delegators map[string]TokenDelegator
}

// NewDelegatorRegistry creates an empty registry.
func NewDelegatorRegistry() *DelegatorRegistry {
	return &DelegatorRegistry{delegators: make(map[string]TokenDelegator)}
}

// Register adds a delegator under the given name.
// Panics if name is empty (startup-time programming error).
func (r *DelegatorRegistry) Register(name string, d TokenDelegator) {
	if name == "" {
		panic("authdelegation: Register called with empty name")
	}
	r.mu.Lock()
	r.delegators[name] = d
	r.mu.Unlock()
}

// Get returns the delegator registered under name.
// Returns nil, false if no delegator is registered for that name.
func (r *DelegatorRegistry) Get(name string) (TokenDelegator, bool) {
	r.mu.RLock()
	d, ok := r.delegators[name]
	r.mu.RUnlock()
	return d, ok
}

// Has reports whether a delegator is registered under name.
func (r *DelegatorRegistry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.delegators[name]
	r.mu.RUnlock()
	return ok
}

// Names returns a sorted list of all registered delegator names.
func (r *DelegatorRegistry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.delegators))
	for name := range r.delegators {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

func BuildDelegationRegistry(ctx context.Context, cfg *config.APIServerConfig, lgr *logr.Logger) (*DelegatorRegistry, error) {
	reg := NewDelegatorRegistry()

	err := registerEntraDelegator(ctx, cfg, lgr, reg)
	if err != nil {
		return nil, fmt.Errorf("building Entra delegator: %w", err)
	}
	err = registerPassThroughDelegators(cfg, lgr, reg)
	if err != nil {
		return nil, fmt.Errorf("building pass-through delegators: %w", err)
	}
	if len(reg.Names()) == 0 {
		return nil, nil
	}
	return reg, nil
}

func registerEntraDelegator(ctx context.Context, cfg *config.APIServerConfig, lgr *logr.Logger, reg *DelegatorRegistry) error {
	if cfg.Identity.Entra == nil {
		return nil
	}
	if err := cfg.Identity.Entra.Validate(); err != nil {
		return fmt.Errorf("invalid Entra identity configuration: %w", err)
	}

	lgr.V(0).Info("building token delegation registry", "provider", "entra")

	delegator, err := NewEntraDelegatorFromConfig(ctx, cfg.Identity.Entra)
	if err != nil {
		return fmt.Errorf("entra delegator: %w", err)
	}

	reg.Register("entra", delegator)
	return nil
}

func registerPassThroughDelegators(cfg *config.APIServerConfig, lgr *logr.Logger, reg *DelegatorRegistry) error {
	allowedHeaders := cfg.TokenPassThroughAllowedHeaders()

	if cfg.TokenPassThrough != nil {
		if err := cfg.TokenPassThrough.Validate(); err != nil {
			return fmt.Errorf("invalid token pass-through configuration: %w", err)
		}
	}

	for _, header := range allowedHeaders {
		delegator, err := NewPassThroughDelegator(header)
		if err != nil {
			return fmt.Errorf("creating pass-through delegator for header %q: %w", header, err)
		}
		name := PassThroughDelegatorName(header)
		reg.Register(name, delegator)
		lgr.V(0).Info("registered token pass-through delegator", "provider", name, "header", header)
	}
	return nil
}
