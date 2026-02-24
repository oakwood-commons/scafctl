// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package soldiff provides structural comparison of two solutions.
package soldiff

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Change represents a single structural difference between two solutions.
type Change struct {
	Field    string `json:"field"`
	Type     string `json:"type"` // "added", "removed", "changed"
	OldValue any    `json:"oldValue,omitempty"`
	NewValue any    `json:"newValue,omitempty"`
}

// Result contains the diff output.
type Result struct {
	PathA   string   `json:"pathA"`
	PathB   string   `json:"pathB"`
	Changes []Change `json:"changes"`
	Summary Summary  `json:"summary"`
}

// Summary counts changes by type.
type Summary struct {
	Total   int `json:"total"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

// Compare performs a structural comparison of two solutions.
func Compare(solA, solB *solution.Solution) *Result {
	var changes []Change

	// Compare metadata
	if solA.Metadata.Name != solB.Metadata.Name {
		changes = append(changes, Change{Field: "metadata.name", Type: "changed", OldValue: solA.Metadata.Name, NewValue: solB.Metadata.Name})
	}
	if solA.Metadata.Description != solB.Metadata.Description {
		changes = append(changes, Change{Field: "metadata.description", Type: "changed", OldValue: solA.Metadata.Description, NewValue: solB.Metadata.Description})
	}
	if solA.Metadata.Version != nil && solB.Metadata.Version != nil {
		if solA.Metadata.Version.String() != solB.Metadata.Version.String() {
			changes = append(changes, Change{Field: "metadata.version", Type: "changed", OldValue: solA.Metadata.Version.String(), NewValue: solB.Metadata.Version.String()})
		}
	}

	// Compare resolvers
	resolversA := make(map[string]bool)
	resolversB := make(map[string]bool)
	if solA.Spec.HasResolvers() {
		for name := range solA.Spec.Resolvers {
			resolversA[name] = true
		}
	}
	if solB.Spec.HasResolvers() {
		for name := range solB.Spec.Resolvers {
			resolversB[name] = true
		}
	}

	// Find added/removed resolvers
	for name := range resolversB {
		if !resolversA[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.resolvers.%s", name), Type: "added"})
		}
	}
	for name := range resolversA {
		if !resolversB[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.resolvers.%s", name), Type: "removed"})
		}
	}

	// Check for changed resolvers (both exist)
	for name := range resolversA {
		if resolversB[name] {
			rA := solA.Spec.Resolvers[name]
			rB := solB.Spec.Resolvers[name]

			if rA.Type != rB.Type {
				changes = append(changes, Change{
					Field: fmt.Sprintf("spec.resolvers.%s.type", name), Type: "changed",
					OldValue: string(rA.Type), NewValue: string(rB.Type),
				})
			}
			if rA.Description != rB.Description {
				changes = append(changes, Change{
					Field: fmt.Sprintf("spec.resolvers.%s.description", name), Type: "changed",
					OldValue: rA.Description, NewValue: rB.Description,
				})
			}

			// Compare primary provider
			provA := ""
			if rA.Resolve != nil && len(rA.Resolve.With) > 0 {
				provA = rA.Resolve.With[0].Provider
			}
			provB := ""
			if rB.Resolve != nil && len(rB.Resolve.With) > 0 {
				provB = rB.Resolve.With[0].Provider
			}
			if provA != provB {
				changes = append(changes, Change{
					Field: fmt.Sprintf("spec.resolvers.%s.provider", name), Type: "changed",
					OldValue: provA, NewValue: provB,
				})
			}
		}
	}

	// Compare actions
	actionsA := make(map[string]bool)
	actionsB := make(map[string]bool)
	if solA.Spec.HasWorkflow() && solA.Spec.Workflow.Actions != nil {
		for name := range solA.Spec.Workflow.Actions {
			actionsA[name] = true
		}
	}
	if solB.Spec.HasWorkflow() && solB.Spec.Workflow.Actions != nil {
		for name := range solB.Spec.Workflow.Actions {
			actionsB[name] = true
		}
	}

	for name := range actionsB {
		if !actionsA[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.workflow.actions.%s", name), Type: "added"})
		}
	}
	for name := range actionsA {
		if !actionsB[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.workflow.actions.%s", name), Type: "removed"})
		}
	}

	// Check changed actions
	for name := range actionsA {
		if actionsB[name] {
			aA := solA.Spec.Workflow.Actions[name]
			aB := solB.Spec.Workflow.Actions[name]

			if aA.Provider != aB.Provider {
				changes = append(changes, Change{
					Field: fmt.Sprintf("spec.workflow.actions.%s.provider", name), Type: "changed",
					OldValue: aA.Provider, NewValue: aB.Provider,
				})
			}
			if aA.Description != aB.Description {
				changes = append(changes, Change{
					Field: fmt.Sprintf("spec.workflow.actions.%s.description", name), Type: "changed",
					OldValue: aA.Description, NewValue: aB.Description,
				})
			}
		}
	}

	// Compare finally actions
	finallyA := make(map[string]bool)
	finallyB := make(map[string]bool)
	if solA.Spec.HasWorkflow() && solA.Spec.Workflow.Finally != nil {
		for name := range solA.Spec.Workflow.Finally {
			finallyA[name] = true
		}
	}
	if solB.Spec.HasWorkflow() && solB.Spec.Workflow.Finally != nil {
		for name := range solB.Spec.Workflow.Finally {
			finallyB[name] = true
		}
	}

	for name := range finallyB {
		if !finallyA[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.workflow.finally.%s", name), Type: "added"})
		}
	}
	for name := range finallyA {
		if !finallyB[name] {
			changes = append(changes, Change{Field: fmt.Sprintf("spec.workflow.finally.%s", name), Type: "removed"})
		}
	}

	// Workflow added/removed
	if !solA.Spec.HasWorkflow() && solB.Spec.HasWorkflow() {
		changes = append(changes, Change{Field: "spec.workflow", Type: "added"})
	} else if solA.Spec.HasWorkflow() && !solB.Spec.HasWorkflow() {
		changes = append(changes, Change{Field: "spec.workflow", Type: "removed"})
	}

	// Compare test cases
	var testsA, testsB map[string]bool
	if solA.Spec.Testing != nil && solA.Spec.Testing.Cases != nil {
		testsA = make(map[string]bool, len(solA.Spec.Testing.Cases))
		for name := range solA.Spec.Testing.Cases {
			testsA[name] = true
		}
	}
	if solB.Spec.Testing != nil && solB.Spec.Testing.Cases != nil {
		testsB = make(map[string]bool, len(solB.Spec.Testing.Cases))
		for name := range solB.Spec.Testing.Cases {
			testsB[name] = true
		}
	}

	if testsA != nil || testsB != nil {
		if testsA == nil {
			testsA = make(map[string]bool)
		}
		if testsB == nil {
			testsB = make(map[string]bool)
		}

		for name := range testsB {
			if !testsA[name] {
				changes = append(changes, Change{Field: fmt.Sprintf("spec.testing.cases.%s", name), Type: "added"})
			}
		}
		for name := range testsA {
			if !testsB[name] {
				changes = append(changes, Change{Field: fmt.Sprintf("spec.testing.cases.%s", name), Type: "removed"})
			}
		}
	}

	// Testing added/removed
	if solA.Spec.Testing == nil && solB.Spec.Testing != nil {
		changes = append(changes, Change{Field: "spec.testing", Type: "added"})
	} else if solA.Spec.Testing != nil && solB.Spec.Testing == nil {
		changes = append(changes, Change{Field: "spec.testing", Type: "removed"})
	}

	// Sort changes for deterministic output
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Field < changes[j].Field
	})

	// Count by type
	added, removed, changed := 0, 0, 0
	for _, c := range changes {
		switch c.Type {
		case "added":
			added++
		case "removed":
			removed++
		case "changed":
			changed++
		}
	}

	return &Result{
		Changes: changes,
		Summary: Summary{
			Total:   len(changes),
			Added:   added,
			Removed: removed,
			Changed: changed,
		},
	}
}

// CompareFiles loads two solution files and compares them structurally.
func CompareFiles(ctx context.Context, pathA, pathB string) (*Result, error) {
	solA, err := explain.LoadSolution(ctx, pathA)
	if err != nil {
		return nil, fmt.Errorf("loading solution A (%s): %w", pathA, err)
	}

	solB, err := explain.LoadSolution(ctx, pathB)
	if err != nil {
		return nil, fmt.Errorf("loading solution B (%s): %w", pathB, err)
	}

	result := Compare(solA, solB)
	result.PathA = pathA
	result.PathB = pathB

	return result, nil
}
