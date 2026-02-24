// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package dryrun provides structured dry-run report generation for scafctl solutions.
//
// It coordinates resolver preview (dry-run mode) and action graph building to produce
// a unified report showing what a solution execution would do without side effects.
package dryrun

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Report is the full structured dry-run output.
type Report struct {
	DryRun        bool                `json:"dryRun" yaml:"dryRun"`
	Solution      string              `json:"solution" yaml:"solution"`
	Version       string              `json:"version,omitempty" yaml:"version,omitempty"`
	HasResolvers  bool                `json:"hasResolvers" yaml:"hasResolvers"`
	HasWorkflow   bool                `json:"hasWorkflow" yaml:"hasWorkflow"`
	Parameters    map[string]any      `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Resolvers     map[string]Resolver `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
	ActionPlan    []Action            `json:"actionPlan,omitempty" yaml:"actionPlan,omitempty"`
	TotalActions  int                 `json:"totalActions,omitempty" yaml:"totalActions,omitempty"`
	TotalPhases   int                 `json:"totalPhases,omitempty" yaml:"totalPhases,omitempty"`
	MockBehaviors []MockBehavior      `json:"mockBehaviors,omitempty" yaml:"mockBehaviors,omitempty"`
	Warnings      []string            `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// Resolver describes a single resolver's dry-run result.
type Resolver struct {
	Value  any    `json:"value" yaml:"value"`
	Status string `json:"status" yaml:"status"`
	DryRun bool   `json:"dryRun" yaml:"dryRun"`
}

// Action describes a single action in the dry-run plan.
type Action struct {
	Name               string            `json:"name" yaml:"name"`
	Provider           string            `json:"provider" yaml:"provider"`
	Description        string            `json:"description,omitempty" yaml:"description,omitempty"`
	MaterializedInputs map[string]any    `json:"materializedInputs,omitempty" yaml:"materializedInputs,omitempty"`
	DeferredInputs     map[string]string `json:"deferredInputs,omitempty" yaml:"deferredInputs,omitempty"`
	Dependencies       []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	When               string            `json:"when,omitempty" yaml:"when,omitempty"`
	Section            string            `json:"section" yaml:"section"`
	Phase              int               `json:"phase" yaml:"phase"`
	MockBehavior       string            `json:"mockBehavior,omitempty" yaml:"mockBehavior,omitempty"`
}

// MockBehavior describes what a provider does in dry-run mode.
type MockBehavior struct {
	Provider     string `json:"provider" yaml:"provider"`
	MockBehavior string `json:"mockBehavior" yaml:"mockBehavior"`
}

// Options controls the dry-run generation.
type Options struct {
	// Params are the resolver parameters that were (or would be) passed.
	Params map[string]any
	// Registry provides provider descriptors for mock behavior lookups.
	Registry *provider.Registry
	// ResolverData is the pre-executed resolver data (from dry-run mode execution).
	// Callers should execute resolvers with dry-run enabled before calling Generate.
	// If nil, the report will note that resolver data was not provided.
	ResolverData map[string]any
}

// Generate builds a structured dry-run report from a solution and pre-executed resolver data.
// Callers must execute resolvers (with dry-run mode) themselves and pass the data via Options.
func Generate(ctx context.Context, sol *solution.Solution, opts Options) (*Report, error) {
	reg := opts.Registry
	resolverData := opts.ResolverData
	if resolverData == nil {
		resolverData = make(map[string]any)
	}

	report := &Report{
		DryRun:       true,
		Solution:     sol.Metadata.Name,
		Version:      versionString(sol.Metadata.Version),
		HasResolvers: sol.Spec.HasResolvers(),
		HasWorkflow:  sol.Spec.HasWorkflow(),
		Parameters:   opts.Params,
		Resolvers:    make(map[string]Resolver),
	}

	// Build resolver entries from pre-executed data
	if sol.Spec.HasResolvers() {
		for name, val := range resolverData {
			report.Resolvers[name] = Resolver{
				Value:  val,
				Status: "resolved",
				DryRun: true,
			}
		}
		// Mark missing resolvers
		for name := range sol.Spec.Resolvers {
			if _, ok := report.Resolvers[name]; !ok {
				report.Resolvers[name] = Resolver{
					Status: "not-resolved",
					DryRun: true,
				}
				report.Warnings = append(report.Warnings, fmt.Sprintf("resolver %q did not produce a value", name))
			}
		}
	}

	// Phase 2: Build action plan
	if sol.Spec.HasWorkflow() {
		graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("action graph build failed: %v", err))
		} else {
			report.TotalActions = len(graph.Actions)
			report.TotalPhases = graph.TotalPhases()

			// Build phase map
			phaseMap := make(map[string]int)
			for i, phase := range graph.ExecutionOrder {
				for _, name := range phase {
					phaseMap[name] = i + 1
				}
			}
			for i, phase := range graph.FinallyOrder {
				for _, name := range phase {
					phaseMap[name] = i + 1
				}
			}

			// Collect sorted action entries
			names := make([]string, 0, len(graph.Actions))
			for name := range graph.Actions {
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				ea := graph.Actions[name]
				entry := Action{
					Name:               name,
					Provider:           ea.Provider,
					Description:        ea.Description,
					MaterializedInputs: ea.MaterializedInputs,
					Dependencies:       ea.Dependencies,
					Section:            ea.Section,
					Phase:              phaseMap[name],
				}
				if ea.When != nil && ea.When.Expr != nil {
					entry.When = string(*ea.When.Expr)
				}

				// Deferred inputs
				if len(ea.DeferredInputs) > 0 {
					entry.DeferredInputs = make(map[string]string, len(ea.DeferredInputs))
					for k, v := range ea.DeferredInputs {
						expr := v.OriginalExpr
						if expr == "" {
							expr = v.OriginalTmpl
						}
						if expr == "" && v.Deferred {
							expr = "(deferred)"
						}
						entry.DeferredInputs[k] = expr
					}
				}

				// Provider mock behavior
				if reg != nil {
					if p, found := reg.Get(ea.Provider); found {
						entry.MockBehavior = p.Descriptor().MockBehavior
					}
				}

				report.ActionPlan = append(report.ActionPlan, entry)
			}
		}
	}

	// Phase 3: Collect provider mock behaviors
	usedProviders := make(map[string]bool)
	for _, res := range sol.Spec.Resolvers {
		if res.Resolve != nil {
			for _, step := range res.Resolve.With {
				usedProviders[step.Provider] = true
			}
		}
	}
	if sol.Spec.HasWorkflow() {
		for _, act := range sol.Spec.Workflow.Actions {
			usedProviders[act.Provider] = true
		}
		for _, act := range sol.Spec.Workflow.Finally {
			usedProviders[act.Provider] = true
		}
	}

	providerNames := make([]string, 0, len(usedProviders))
	for name := range usedProviders {
		if name != "" {
			providerNames = append(providerNames, name)
		}
	}
	sort.Strings(providerNames)

	if reg != nil {
		for _, name := range providerNames {
			if p, found := reg.Get(name); found {
				report.MockBehaviors = append(report.MockBehaviors, MockBehavior{
					Provider:     name,
					MockBehavior: p.Descriptor().MockBehavior,
				})
			}
		}
	}

	return report, nil
}

func versionString(v fmt.Stringer) string {
	if v == nil {
		return ""
	}
	return v.String()
}
