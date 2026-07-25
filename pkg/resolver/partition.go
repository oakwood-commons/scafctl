// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// ResolversUsingProvider returns the set of resolver names that directly use the
// named provider in any of their phases (resolve, transform, or validate),
// either via `provider: <name>` on a step or via a call definition (calls) whose
// provider is <name>. The returned map's keys are resolver names and every value
// is true; an empty (non-nil) map means no resolver uses the provider.
//
// It is the seed used to compute the set of resolvers that cannot be resolved
// before that provider's prerequisite is available (see ProviderDependencyClosure).
func ResolversUsingProvider(resolvers []*Resolver, calls map[string]*spec.Call, providerName string) map[string]bool {
	seed := make(map[string]bool)
	if providerName == "" {
		return seed
	}
	for _, r := range resolvers {
		if r == nil {
			continue
		}
		if resolverUsesProvider(r, calls, providerName) {
			seed[r.Name] = true
		}
	}
	return seed
}

// resolverUsesProvider reports whether any step in any phase of r uses the named
// provider (directly or through a call definition).
func resolverUsesProvider(r *Resolver, calls map[string]*spec.Call, providerName string) bool {
	if r.Resolve != nil {
		for i := range r.Resolve.With {
			step := &r.Resolve.With[i]
			if stepUsesProvider(step.Provider, step.Call, calls, providerName) {
				return true
			}
		}
	}
	if r.Transform != nil {
		for i := range r.Transform.With {
			step := &r.Transform.With[i]
			if stepUsesProvider(step.Provider, step.Call, calls, providerName) {
				return true
			}
		}
	}
	if r.Validate != nil {
		for i := range r.Validate.With {
			step := &r.Validate.With[i]
			if stepUsesProvider(step.Provider, step.Call, calls, providerName) {
				return true
			}
		}
	}
	return false
}

// stepUsesProvider reports whether a single step uses the named provider, either
// directly (providerField) or through a call definition whose provider matches.
func stepUsesProvider(providerField, callName string, calls map[string]*spec.Call, providerName string) bool {
	if providerField == providerName {
		return true
	}
	if callName != "" {
		if def, ok := calls[callName]; ok && def != nil && def.Provider == providerName {
			return true
		}
	}
	return false
}

// TransitiveDependents returns the seed set augmented with every resolver that
// transitively depends on any seed member. Dependency edges are computed with
// the same extraction logic used to build execution phases, so the result
// reflects the effective DAG. The returned map always contains every seed member
// and is safe to mutate by the caller. A nil/empty seed yields an empty result.
func TransitiveDependents(resolvers []*Resolver, lookup DescriptorLookup, calls map[string]*spec.Call, seed map[string]bool) map[string]bool {
	result := make(map[string]bool, len(seed))
	for name := range seed {
		result[name] = true
	}
	if len(result) == 0 {
		return result
	}

	// Build reverse adjacency: revDeps[dep] lists the resolvers that depend on
	// dep. A breadth-first walk from the seed then marks every transitive
	// dependent in O(V+E).
	revDeps := make(map[string][]string)
	for _, r := range resolvers {
		if r == nil {
			continue
		}
		for _, dep := range extractDependenciesWithCalls(r, lookup, calls) {
			revDeps[dep] = append(revDeps[dep], r.Name)
		}
	}

	queue := make([]string, 0, len(result))
	for name := range result {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range revDeps[current] {
			if !result[dependent] {
				result[dependent] = true
				queue = append(queue, dependent)
			}
		}
	}
	return result
}

// ProviderDependencyClosure returns the set of resolvers that cannot be resolved
// before the named provider's prerequisite is satisfied: those that directly use
// the provider plus all of their transitive dependents. It is the union of
// ResolversUsingProvider and TransitiveDependents.
func ProviderDependencyClosure(resolvers []*Resolver, lookup DescriptorLookup, calls map[string]*spec.Call, providerName string) map[string]bool {
	seed := ResolversUsingProvider(resolvers, calls, providerName)
	return TransitiveDependents(resolvers, lookup, calls, seed)
}

// TransitiveDependencies returns the roots set augmented with every resolver
// that any root transitively depends on. This is the minimal set of resolvers
// that must run before the roots can be evaluated. Dependency edges are computed
// with the same extraction logic used to build execution phases. The returned
// map always contains every root and is safe to mutate by the caller. A
// nil/empty roots yields an empty result.
//
// It is the forward-closure counterpart to TransitiveDependents (which walks the
// reverse edges).
func TransitiveDependencies(resolvers []*Resolver, lookup DescriptorLookup, calls map[string]*spec.Call, roots map[string]bool) map[string]bool {
	result := make(map[string]bool, len(roots))
	for name := range roots {
		result[name] = true
	}
	if len(result) == 0 {
		return result
	}

	// Forward adjacency: deps[name] lists the resolvers that name depends on.
	deps := make(map[string][]string, len(resolvers))
	for _, r := range resolvers {
		if r == nil {
			continue
		}
		deps[r.Name] = extractDependenciesWithCalls(r, lookup, calls)
	}

	queue := make([]string, 0, len(result))
	for name := range result {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dep := range deps[current] {
			if !result[dep] {
				result[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	return result
}
