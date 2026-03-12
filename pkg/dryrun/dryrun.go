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
	"reflect"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Report is the full structured dry-run output.
type Report struct {
	DryRun        bool                `json:"dryRun" yaml:"dryRun" doc:"Whether this is a dry-run report"`
	Solution      string              `json:"solution" yaml:"solution" doc:"Solution name" maxLength:"256" example:"my-solution"`
	Version       string              `json:"version,omitempty" yaml:"version,omitempty" doc:"Solution version" maxLength:"64" example:"1.0.0"`
	HasResolvers  bool                `json:"hasResolvers" yaml:"hasResolvers" doc:"Whether the solution has resolvers"`
	HasWorkflow   bool                `json:"hasWorkflow" yaml:"hasWorkflow" doc:"Whether the solution has a workflow"`
	Parameters    map[string]any      `json:"parameters,omitempty" yaml:"parameters,omitempty" doc:"Resolver parameters"`
	Resolvers     map[string]Resolver `json:"resolvers,omitempty" yaml:"resolvers,omitempty" doc:"Resolver dry-run results"`
	ActionPlan    []Action            `json:"actionPlan,omitempty" yaml:"actionPlan,omitempty" doc:"Planned action execution order" maxItems:"1000"`
	TotalActions  int                 `json:"totalActions,omitempty" yaml:"totalActions,omitempty" doc:"Total number of planned actions" maximum:"1000" example:"5"`
	TotalPhases   int                 `json:"totalPhases,omitempty" yaml:"totalPhases,omitempty" doc:"Total number of execution phases" maximum:"100" example:"3"`
	MockBehaviors []MockBehavior      `json:"mockBehaviors,omitempty" yaml:"mockBehaviors,omitempty" doc:"Provider mock behavior descriptions" maxItems:"100"`
	Warnings      []string            `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Warnings about the dry-run" maxItems:"100"`
}

// Resolver describes a single resolver's dry-run result.
type Resolver struct {
	Value  any    `json:"value" yaml:"value" doc:"Resolved value"`
	Status string `json:"status" yaml:"status" doc:"Resolution status" maxLength:"32" example:"success"`
	DryRun bool   `json:"dryRun" yaml:"dryRun" doc:"Whether the value came from dry-run mode"`
}

// Action describes a single action in the dry-run plan.
type Action struct {
	Name               string            `json:"name" yaml:"name" doc:"Action name" maxLength:"256" example:"deploy-service"`
	Provider           string            `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"128" example:"shell"`
	Description        string            `json:"description,omitempty" yaml:"description,omitempty" doc:"Action description" maxLength:"512" example:"Deploy the service"`
	MaterializedInputs map[string]any    `json:"materializedInputs,omitempty" yaml:"materializedInputs,omitempty" doc:"Inputs resolved at plan time"`
	DeferredInputs     map[string]string `json:"deferredInputs,omitempty" yaml:"deferredInputs,omitempty" doc:"Inputs deferred until runtime"`
	Dependencies       []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty" doc:"Action dependencies" maxItems:"100"`
	When               string            `json:"when,omitempty" yaml:"when,omitempty" doc:"Conditional expression" maxLength:"2048" example:"_.enabled == true"`
	Section            string            `json:"section" yaml:"section" doc:"Workflow section (actions or finally)" maxLength:"16" example:"actions"`
	Phase              int               `json:"phase" yaml:"phase" doc:"Execution phase number" maximum:"100" example:"1"`
	MockBehavior       string            `json:"mockBehavior,omitempty" yaml:"mockBehavior,omitempty" doc:"Provider mock behavior description" maxLength:"512" example:"Returns static mock data"`
}

// MockBehavior describes what a provider does in dry-run mode.
type MockBehavior struct {
	Provider     string `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"128" example:"shell"`
	MockBehavior string `json:"mockBehavior" yaml:"mockBehavior" doc:"Mock behavior description" maxLength:"512" example:"Returns exit code 0"`
}

// Options controls the dry-run generation.
type Options struct {
	// Params are the resolver parameters that were (or would be) passed.
	Params map[string]any `json:"-" yaml:"-" doc:"Resolver parameters"`
	// Registry provides provider descriptors for mock behavior lookups.
	Registry *provider.Registry `json:"-" yaml:"-" doc:"Provider registry"`
	// ResolverData is the pre-executed resolver data (from dry-run mode execution).
	// Callers should execute resolvers with dry-run enabled before calling Generate.
	// If nil, the report will note that resolver data was not provided.
	ResolverData map[string]any `json:"-" yaml:"-" doc:"Pre-executed resolver data"`
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
		if res == nil {
			continue
		}
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
	// Handle nil pointer wrapped in a non-nil interface
	// (e.g. (*semver.Version)(nil) passed as fmt.Stringer).
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return ""
	}
	return v.String()
}
