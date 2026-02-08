// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/dag"
	"k8s.io/apimachinery/pkg/util/sets"
)

// PhaseGroup represents a group of resolvers that can execute concurrently
type PhaseGroup struct {
	Phase     int         `json:"phase" yaml:"phase" doc:"Phase number (1-based)" minimum:"1"`
	Resolvers []*Resolver `json:"resolvers" yaml:"resolvers" doc:"Resolvers in this phase" minItems:"1"`
}

// resolverDagObject implements the dag.Object interface for resolvers
type resolverDagObject struct {
	resolver *Resolver
	deps     []string // Pre-computed dependencies
}

// DagKey returns the unique key for this DAG object (resolver name)
func (r *resolverDagObject) DagKey() string {
	return r.resolver.Name
}

// GetDependencyKeys returns the dependency keys for this resolver
// For simplicity, we just use the resolver names directly as keys
func (r *resolverDagObject) GetDependencyKeys(_ map[string]string, _ map[string][]string, _ map[string]string) []string {
	// Dependencies are pre-computed and stored in the struct
	return r.deps
}

// resolverObjectsWithLookup wraps resolvers with a lookup function for dependency extraction
type resolverObjectsWithLookup struct {
	resolvers []*Resolver
	lookup    DescriptorLookup
}

// DagItems returns the slice of dag.Object items with pre-computed dependencies
func (r *resolverObjectsWithLookup) DagItems() []dag.Object {
	items := make([]dag.Object, len(r.resolvers))
	for i, resolver := range r.resolvers {
		items[i] = &resolverDagObject{
			resolver: resolver,
			deps:     extractDependencies(resolver, r.lookup),
		}
	}
	return items
}

// BuildPhases groups resolvers into execution phases based on dependencies.
// Phase numbers are 1-based, with phase 1 being root resolvers (no dependencies).
// If lookup is provided, provider-specific ExtractDependencies functions will be used
// when available for more accurate dependency detection.
func BuildPhases(resolvers []*Resolver, lookup DescriptorLookup) ([]*PhaseGroup, error) {
	if len(resolvers) == 0 {
		return []*PhaseGroup{}, nil
	}

	// Build dependency map
	deps := make(map[string][]string)
	resolverMap := make(map[string]*Resolver)

	for _, r := range resolvers {
		resolverMap[r.Name] = r
		deps[r.Name] = extractDependencies(r, lookup)
	}

	// Build DAG using existing package
	objects := &resolverObjectsWithLookup{resolvers: resolvers, lookup: lookup}
	graph, err := dag.Build(objects, deps)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Group into phases by traversing the graph level by level
	phases := make([]*PhaseGroup, 0)
	doneDagObjects := sets.Set[string]{}
	phaseNum := 1

	for {
		// Get schedulable resolvers for this phase
		candidateNames, err := dag.GetCandidateDagObjects(graph, sets.List(doneDagObjects)...)
		if err != nil {
			return nil, fmt.Errorf("failed to get candidate resolvers for phase %d: %w", phaseNum, err)
		}

		// If no more candidates, we're done
		if candidateNames.Len() == 0 {
			break
		}

		// Create phase group
		phaseResolvers := make([]*Resolver, 0, candidateNames.Len())
		for name := range candidateNames {
			if resolver, ok := resolverMap[name]; ok {
				phaseResolvers = append(phaseResolvers, resolver)
			}
		}

		phases = append(phases, &PhaseGroup{
			Phase:     phaseNum,
			Resolvers: phaseResolvers,
		})

		// Mark these as done for next iteration
		for name := range candidateNames {
			doneDagObjects.Insert(name)
		}

		phaseNum++

		// Safety check to prevent infinite loops
		if phaseNum > len(resolvers)+1 {
			return nil, fmt.Errorf("infinite loop detected in phase grouping (exceeded resolver count)")
		}
	}

	// Verify all resolvers are accounted for
	if doneDagObjects.Len() != len(resolvers) {
		missing := []string{}
		for _, r := range resolvers {
			if !doneDagObjects.Has(r.Name) {
				missing = append(missing, r.Name)
			}
		}
		return nil, fmt.Errorf("not all resolvers were grouped into phases; missing: %v", missing)
	}

	return phases, nil
}

// GetPhaseForResolver returns the phase number for a given resolver name
func GetPhaseForResolver(phases []*PhaseGroup, resolverName string) int {
	for _, phase := range phases {
		for _, resolver := range phase.Resolvers {
			if resolver.Name == resolverName {
				return phase.Phase
			}
		}
	}
	return 0 // Not found
}

// GetMaxPhase returns the maximum phase number in the phase groups
func GetMaxPhase(phases []*PhaseGroup) int {
	maxPhase := 0
	for _, phase := range phases {
		if phase.Phase > maxPhase {
			maxPhase = phase.Phase
		}
	}
	return maxPhase
}

// GetResolversInPhase returns all resolvers in a specific phase
func GetResolversInPhase(phases []*PhaseGroup, phaseNum int) []*Resolver {
	for _, phase := range phases {
		if phase.Phase == phaseNum {
			return phase.Resolvers
		}
	}
	return nil
}
