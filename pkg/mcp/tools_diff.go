// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
)

// registerDiffTools registers diff/comparison MCP tools.
func (s *Server) registerDiffTools() {
	// diff_solution
	diffSolutionTool := mcp.NewTool("diff_solution",
		mcp.WithDescription("Compare two solution files and show structural differences. Identifies added, removed, and changed resolvers, actions, metadata, and test cases. Useful for reviewing changes before committing or understanding what was modified."),
		mcp.WithTitleAnnotation("Diff Solution"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path_a",
			mcp.Required(),
			mcp.Description("Path to the first solution file (e.g., the original version)"),
		),
		mcp.WithString("path_b",
			mcp.Required(),
			mcp.Description("Path to the second solution file (e.g., the modified version)"),
		),
	)
	s.mcpServer.AddTool(diffSolutionTool, s.handleDiffSolution)
}

// handleDiffSolution compares two solutions structurally.
func (s *Server) handleDiffSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathA, err := request.RequireString("path_a")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	pathB, err := request.RequireString("path_b")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	solA, err := explain.LoadSolution(s.ctx, pathA)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution A (%s): %v", pathA, err)), nil
	}
	solB, err := explain.LoadSolution(s.ctx, pathB)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution B (%s): %v", pathB, err)), nil
	}

	type change struct {
		Field    string `json:"field"`
		Type     string `json:"type"` // "added", "removed", "changed"
		OldValue any    `json:"oldValue,omitempty"`
		NewValue any    `json:"newValue,omitempty"`
	}

	var changes []change

	// Compare metadata
	if solA.Metadata.Name != solB.Metadata.Name {
		changes = append(changes, change{Field: "metadata.name", Type: "changed", OldValue: solA.Metadata.Name, NewValue: solB.Metadata.Name})
	}
	if solA.Metadata.Description != solB.Metadata.Description {
		changes = append(changes, change{Field: "metadata.description", Type: "changed", OldValue: solA.Metadata.Description, NewValue: solB.Metadata.Description})
	}
	if solA.Metadata.Version != nil && solB.Metadata.Version != nil {
		if solA.Metadata.Version.String() != solB.Metadata.Version.String() {
			changes = append(changes, change{Field: "metadata.version", Type: "changed", OldValue: solA.Metadata.Version.String(), NewValue: solB.Metadata.Version.String()})
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
			changes = append(changes, change{Field: fmt.Sprintf("spec.resolvers.%s", name), Type: "added"})
		}
	}
	for name := range resolversA {
		if !resolversB[name] {
			changes = append(changes, change{Field: fmt.Sprintf("spec.resolvers.%s", name), Type: "removed"})
		}
	}

	// Check for changed resolvers (both exist)
	for name := range resolversA {
		if resolversB[name] {
			rA := solA.Spec.Resolvers[name]
			rB := solB.Spec.Resolvers[name]

			if rA.Type != rB.Type {
				changes = append(changes, change{
					Field: fmt.Sprintf("spec.resolvers.%s.type", name), Type: "changed",
					OldValue: string(rA.Type), NewValue: string(rB.Type),
				})
			}
			if rA.Description != rB.Description {
				changes = append(changes, change{
					Field: fmt.Sprintf("spec.resolvers.%s.description", name), Type: "changed",
					OldValue: rA.Description, NewValue: rB.Description,
				})
			}

			// Compare primary provider
			provA := ""
			provB := ""
			if rA.Resolve != nil && len(rA.Resolve.With) > 0 {
				provA = rA.Resolve.With[0].Provider
			}
			if rB.Resolve != nil && len(rB.Resolve.With) > 0 {
				provB = rB.Resolve.With[0].Provider
			}
			if provA != provB {
				changes = append(changes, change{
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
			changes = append(changes, change{Field: fmt.Sprintf("spec.workflow.actions.%s", name), Type: "added"})
		}
	}
	for name := range actionsA {
		if !actionsB[name] {
			changes = append(changes, change{Field: fmt.Sprintf("spec.workflow.actions.%s", name), Type: "removed"})
		}
	}

	// Check changed actions
	for name := range actionsA {
		if actionsB[name] {
			aA := solA.Spec.Workflow.Actions[name]
			aB := solB.Spec.Workflow.Actions[name]

			if aA.Provider != aB.Provider {
				changes = append(changes, change{
					Field: fmt.Sprintf("spec.workflow.actions.%s.provider", name), Type: "changed",
					OldValue: aA.Provider, NewValue: aB.Provider,
				})
			}
			if aA.Description != aB.Description {
				changes = append(changes, change{
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
			changes = append(changes, change{Field: fmt.Sprintf("spec.workflow.finally.%s", name), Type: "added"})
		}
	}
	for name := range finallyA {
		if !finallyB[name] {
			changes = append(changes, change{Field: fmt.Sprintf("spec.workflow.finally.%s", name), Type: "removed"})
		}
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
				changes = append(changes, change{Field: fmt.Sprintf("spec.testing.cases.%s", name), Type: "added"})
			}
		}
		for name := range testsA {
			if !testsB[name] {
				changes = append(changes, change{Field: fmt.Sprintf("spec.testing.cases.%s", name), Type: "removed"})
			}
		}
	}

	// Workflow added/removed
	if !solA.Spec.HasWorkflow() && solB.Spec.HasWorkflow() {
		changes = append(changes, change{Field: "spec.workflow", Type: "added"})
	} else if solA.Spec.HasWorkflow() && !solB.Spec.HasWorkflow() {
		changes = append(changes, change{Field: "spec.workflow", Type: "removed"})
	}

	// Testing added/removed
	if solA.Spec.Testing == nil && solB.Spec.Testing != nil {
		changes = append(changes, change{Field: "spec.testing", Type: "added"})
	} else if solA.Spec.Testing != nil && solB.Spec.Testing == nil {
		changes = append(changes, change{Field: "spec.testing", Type: "removed"})
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

	return mcp.NewToolResultJSON(map[string]any{
		"pathA":   pathA,
		"pathB":   pathB,
		"changes": changes,
		"summary": map[string]any{
			"total":   len(changes),
			"added":   added,
			"removed": removed,
			"changed": changed,
		},
	})
}
