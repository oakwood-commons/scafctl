// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package dryrun provides structured WhatIf report generation for scafctl solutions.
//
// It coordinates resolver execution (real data, no mocking) and action graph building
// to produce a unified report showing what a solution execution would do without
// performing side effects. Each action includes a provider-generated WhatIf message
// describing the specific operation it would perform with the resolved inputs.
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

// Report is the full structured WhatIf dry-run output.
type Report struct {
	DryRun       bool           `json:"dryRun" yaml:"dryRun" doc:"Whether this is a dry-run report"`
	Solution     string         `json:"solution" yaml:"solution" doc:"Solution name" maxLength:"256" example:"my-solution"`
	Version      string         `json:"version,omitempty" yaml:"version,omitempty" doc:"Solution version" maxLength:"64" example:"1.0.0"`
	HasWorkflow  bool           `json:"hasWorkflow" yaml:"hasWorkflow" doc:"Whether the solution has a workflow"`
	ActionPlan   []WhatIfAction `json:"actionPlan,omitempty" yaml:"actionPlan,omitempty" doc:"Planned action execution order with WhatIf descriptions" maxItems:"1000"`
	TotalActions int            `json:"totalActions,omitempty" yaml:"totalActions,omitempty" doc:"Total number of planned actions" maximum:"1000" example:"5"`
	TotalPhases  int            `json:"totalPhases,omitempty" yaml:"totalPhases,omitempty" doc:"Total number of execution phases" maximum:"100" example:"3"`
	Warnings     []string       `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Warnings about the dry-run" maxItems:"100"`
}

// WhatIfAction describes a single action in the WhatIf plan with a
// provider-generated description of what it would do.
type WhatIfAction struct {
	Name        string `json:"name" yaml:"name" doc:"Action name" maxLength:"256" example:"deploy-service"`
	Provider    string `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"128" example:"exec"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Action description" maxLength:"512" example:"Deploy the service"`
	// WhatIf is the Go field name because it represents the provider's WhatIf
	// capability ("can this provider describe what it would do?"). The JSON/YAML
	// tag is "wouldDo" to give API consumers a clearer name for the response
	// value ("here's what it would do") vs the internal concept.
	WhatIf             string            `json:"wouldDo" yaml:"wouldDo" doc:"Provider-generated description of what this action would do" maxLength:"1024" example:"Would execute command ./deploy.sh via bash in /app"`
	Phase              int               `json:"phase" yaml:"phase" doc:"Execution phase number" maximum:"100" example:"1"`
	Section            string            `json:"section" yaml:"section" doc:"Workflow section (actions or finally)" maxLength:"16" example:"actions"`
	Dependencies       []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty" doc:"Action dependencies" maxItems:"100"`
	When               string            `json:"when,omitempty" yaml:"when,omitempty" doc:"Conditional expression" maxLength:"2048" example:"_.enabled == true"`
	MaterializedInputs map[string]any    `json:"materializedInputs,omitempty" yaml:"materializedInputs,omitempty" doc:"Inputs resolved at plan time (only with --verbose)"`
	DeferredInputs     map[string]string `json:"deferredInputs,omitempty" yaml:"deferredInputs,omitempty" doc:"Inputs deferred until runtime"`
}

// Options controls the dry-run generation.
type Options struct {
	// Registry provides provider descriptors for WhatIf message generation.
	Registry *provider.Registry `json:"-" yaml:"-"`
	// ResolverData is the pre-executed resolver data (from real resolver execution).
	// Callers should execute resolvers normally (they are side-effect-free) before
	// calling Generate and pass the results here.
	// If nil, the report will generate WhatIf messages without resolver context.
	ResolverData map[string]any `json:"-" yaml:"-"`
	// Verbose includes MaterializedInputs in the report when true.
	Verbose bool `json:"-" yaml:"-"`
}

// Generate builds a structured WhatIf dry-run report from a solution and
// pre-executed resolver data. Callers must execute resolvers (normally, not
// mocked) themselves and pass the data via Options.ResolverData.
// For each action, the provider's DescribeWhatIf is called with the
// materialized inputs to generate a context-specific WhatIf message.
func Generate(ctx context.Context, sol *solution.Solution, opts Options) (*Report, error) {
	reg := opts.Registry
	resolverData := opts.ResolverData
	if resolverData == nil {
		resolverData = make(map[string]any)
	}

	report := &Report{
		DryRun:      true,
		Solution:    sol.Metadata.Name,
		Version:     versionString(sol.Metadata.Version),
		HasWorkflow: sol.Spec.HasWorkflow(),
	}

	// Build action plan with WhatIf messages
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
				entry := WhatIfAction{
					Name:         name,
					Provider:     ea.Provider,
					Description:  ea.Description,
					Dependencies: ea.Dependencies,
					Section:      ea.Section,
					Phase:        phaseMap[name],
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

				// Generate WhatIf message from the provider
				entry.WhatIf = describeWhatIf(ctx, reg, ea.Provider, ea.MaterializedInputs)

				// Only include MaterializedInputs in verbose mode
				if opts.Verbose && len(ea.MaterializedInputs) > 0 {
					entry.MaterializedInputs = ea.MaterializedInputs
				}

				report.ActionPlan = append(report.ActionPlan, entry)
			}
		}
	}

	return report, nil
}

// describeWhatIf generates a WhatIf message for an action using the provider's
// DescribeWhatIf method, falling back gracefully when the registry or provider
// is unavailable.
func describeWhatIf(ctx context.Context, reg *provider.Registry, providerName string, inputs map[string]any) string {
	if reg == nil || providerName == "" {
		if providerName != "" {
			return fmt.Sprintf("Would execute %s provider", providerName)
		}
		return "Would execute action"
	}
	p, found := reg.Get(providerName)
	if !found {
		return fmt.Sprintf("Would execute %s provider", providerName)
	}
	return p.Descriptor().DescribeWhatIf(ctx, inputs)
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
