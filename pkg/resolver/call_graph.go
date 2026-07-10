// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// extractDependenciesWithCalls returns the resolver's dependencies. Call
// definitions are strictly isolated: their inputs may reference only their
// declared args plus always-available globals, so invoking a call never
// contributes additional resolver dependencies. Referencing a solution resolver
// from inside a definition is a validation error, not an auto-wired dependency.
// Call-site argument ValueRefs are part of the resolver's own inputs and are
// captured by extractDependencies.
func extractDependenciesWithCalls(r *Resolver, lookup DescriptorLookup, _ map[string]*spec.Call) []string {
	return extractDependencies(r, lookup)
}

// extractStrictDependenciesWithCalls returns the resolver's strict dependencies.
// As with extractDependenciesWithCalls, isolated call definitions contribute no
// additional dependencies.
func extractStrictDependenciesWithCalls(r *Resolver, _ map[string]*spec.Call) map[string]bool {
	return extractStrictDependencies(r)
}
