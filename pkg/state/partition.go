// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Partition classifies a solution's resolvers by whether they can be resolved
// before state is loaded. A resolver is *state-dependent* when it reads state
// (via the state provider) or transitively depends on one that does; such a
// resolver cannot run in the pre-load phase. Every other resolver is
// *state-independent* and is a candidate for the two-phase pre-load.
//
// The partition is computed statically from the resolver graph, so the acyclic
// guarantee is decidable without executing anything.
type Partition struct {
	// stateDependent holds the names of resolvers that read state or depend on
	// one that does.
	stateDependent map[string]bool

	// known holds every resolver name defined in the solution, used to flag
	// references to non-existent resolvers.
	known map[string]bool
}

// BuildPartition computes the state partition for a solution's resolvers.
// stateProviderName is the name of the state read provider (threaded in by the
// caller to avoid a state -> stateprovider import cycle); a resolver using that
// provider in any phase seeds the state-dependent set, which is then closed over
// transitive dependents.
func BuildPartition(resolvers []*resolver.Resolver, lookup resolver.DescriptorLookup, calls map[string]*spec.Call, stateProviderName string) *Partition {
	known := make(map[string]bool, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			known[r.Name] = true
		}
	}
	return &Partition{
		stateDependent: resolver.ProviderDependencyClosure(resolvers, lookup, calls, stateProviderName),
		known:          known,
	}
}

// IsStateDependent reports whether the named resolver reads state or transitively
// depends on one that does.
func (p *Partition) IsStateDependent(name string) bool {
	if p == nil {
		return false
	}
	return p.stateDependent[name]
}

// IsKnown reports whether the named resolver is defined in the solution.
func (p *Partition) IsKnown(name string) bool {
	if p == nil {
		return false
	}
	return p.known[name]
}

// configRefLocation pairs a state config field's diagnostic path with the
// resolver references it makes.
type configRefLocation struct {
	location string
	refs     map[string]bool
	whole    bool
}

// stateConfigRefLocations extracts the resolver references made by each state
// config field that is evaluated at load time (enabled and every load input),
// inspecting only explicit ValueRef wrappers and skipping statically disabled
// configs. Save targets are excluded: they are resolved only at save time,
// after resolvers have run, so they may freely reference any resolver. The
// result is ordered deterministically (enabled first, then load inputs sorted
// by key).
func stateConfigRefLocations(cfg *Config) []configRefLocation {
	if cfg == nil || isStaticallyDisabledValueRef(cfg.Enabled) {
		return nil
	}

	var locations []configRefLocation

	if cfg.Enabled != nil {
		refs := make(map[string]bool)
		whole := resolver.ExtractWrapperRefsFromValueRef(cfg.Enabled, refs)
		delete(refs, "")
		if len(refs) > 0 || whole {
			locations = append(locations, configRefLocation{location: "state.enabled", refs: refs, whole: whole})
		}
	}

	if cfg.Load == nil {
		return locations
	}

	inputKeys := make([]string, 0, len(cfg.Load.Inputs))
	for key := range cfg.Load.Inputs {
		inputKeys = append(inputKeys, key)
	}
	sort.Strings(inputKeys)
	for _, key := range inputKeys {
		vr := cfg.Load.Inputs[key]
		if vr == nil {
			continue
		}
		refs := make(map[string]bool)
		whole := resolver.ExtractWrapperRefsFromValueRef(vr, refs)
		delete(refs, "")
		if len(refs) > 0 || whole {
			locations = append(locations, configRefLocation{
				location: "state.load.inputs." + key,
				refs:     refs,
				whole:    whole,
			})
		}
	}

	return locations
}

// ValidateStateRefs enforces the acyclic guarantee for state config references.
// It returns a *CycleError if any load-time field (enabled or a load input)
// references a state-dependent resolver, or a *UnknownStateRefError if it
// references a resolver that does not exist. References to state-independent
// resolvers are permitted -- they are resolved in the two-phase pre-load. The
// first offending location (in deterministic order) is reported.
func ValidateStateRefs(cfg *Config, part *Partition) error {
	for _, loc := range stateConfigRefLocations(cfg) {
		var unknown, dependent []string
		for ref := range loc.refs {
			switch {
			case !part.IsKnown(ref):
				unknown = append(unknown, ref)
			case part.IsStateDependent(ref):
				dependent = append(dependent, ref)
			}
		}
		if loc.whole && part != nil {
			for ref := range part.stateDependent {
				if !loc.refs[ref] {
					dependent = append(dependent, ref)
				}
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return &UnknownStateRefError{Location: loc.location, Refs: unknown}
		}
		if len(dependent) > 0 {
			sort.Strings(dependent)
			return &CycleError{Location: loc.location, Refs: dependent}
		}
	}
	return nil
}

// PhaseARoots returns the state-independent resolver references made by the
// load-time state config fields. These are the roots whose transitive
// dependencies form the minimal pre-load (Phase A) set. Callers should run
// ValidateStateRefs first so that only known, state-independent references
// remain; any unknown reference is skipped here defensively.
func PhaseARoots(cfg *Config, part *Partition) map[string]bool {
	roots := make(map[string]bool)
	for _, loc := range stateConfigRefLocations(cfg) {
		if loc.whole && part != nil {
			for ref := range part.known {
				if !part.IsStateDependent(ref) {
					roots[ref] = true
				}
			}
		}
		for ref := range loc.refs {
			if part.IsKnown(ref) && !part.IsStateDependent(ref) {
				roots[ref] = true
			}
		}
	}
	return roots
}

// ExtractConfigResolverRefs returns the sorted, deduplicated set of resolver
// names referenced via explicit ValueRef wrappers across active state config
// fields: state.enabled, state.load.inputs, and every non-statically-disabled
// state.save target's enabled condition and inputs (after materializing any
// extends: load inheritance). Plain literal strings in state inputs are left
// untouched, matching Manager.resolveInputs. Returns nil when cfg is nil,
// statically disabled, or references no resolvers.
func ExtractConfigResolverRefs(cfg *Config) []string {
	refs, _ := scanConfigResolverRefs(cfg)
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for name := range refs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ConfigReferencesWholeNamespace reports whether any active expression or
// template wrapper in cfg accesses the root resolver namespace as a whole.
func ConfigReferencesWholeNamespace(cfg *Config) bool {
	_, whole := scanConfigResolverRefs(cfg)
	return whole
}

func scanConfigResolverRefs(cfg *Config) (map[string]bool, bool) {
	if cfg == nil {
		return nil, false
	}

	materialized := materializeExtends(cfg)
	if isStaticallyDisabledValueRef(materialized.Enabled) {
		return nil, false
	}

	refs := make(map[string]bool)
	whole := false

	if materialized.Enabled != nil {
		if resolver.ExtractWrapperRefsFromValueRef(materialized.Enabled, refs) {
			whole = true
		}
	}
	if materialized.Load != nil {
		for _, vr := range materialized.Load.Inputs {
			if resolver.ExtractWrapperRefsFromValueRef(vr, refs) {
				whole = true
			}
		}
	}
	for i := range materialized.Save {
		target := &materialized.Save[i]
		if isStaticallyDisabledValueRef(target.Enabled) {
			continue
		}
		if target.Enabled != nil {
			if resolver.ExtractWrapperRefsFromValueRef(target.Enabled, refs) {
				whole = true
			}
		}
		for _, vr := range target.Inputs {
			if resolver.ExtractWrapperRefsFromValueRef(vr, refs) {
				whole = true
			}
		}
	}

	delete(refs, "")
	return refs, whole
}

func isStaticallyDisabledValueRef(vr *spec.ValueRef) bool {
	if vr == nil {
		return false
	}
	if vr.Resolver != nil || vr.Tmpl != nil || vr.Secret != nil || vr.Token != nil {
		return false
	}
	if vr.Expr != nil {
		return strings.TrimSpace(string(*vr.Expr)) == "false"
	}
	if m, ok := vr.Literal.(map[string]any); ok && spec.IsValueRefMap(m) {
		return false
	}
	return !isTruthy(vr.Literal)
}
