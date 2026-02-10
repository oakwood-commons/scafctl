// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// MergePluginDefaults shallow-merges plugin default inputs beneath inline provider inputs
// in the solution's resolvers and actions. Inline inputs always win.
// This should be called before DAG construction so that the DAG sees the merged result.
func MergePluginDefaults(sol *solution.Solution) {
	if len(sol.Bundle.Plugins) == 0 {
		return
	}

	// Build a map of provider name → defaults
	defaultsMap := make(map[string]map[string]*spec.ValueRef)
	for _, p := range sol.Bundle.Plugins {
		if p.Kind == solution.PluginKindProvider && len(p.Defaults) > 0 {
			defaultsMap[p.Name] = p.Defaults
		}
	}

	if len(defaultsMap) == 0 {
		return
	}

	// Merge defaults into resolver provider inputs
	if sol.Spec.Resolvers != nil {
		for _, r := range sol.Spec.Resolvers {
			if r.Resolve != nil {
				for _, src := range r.Resolve.With {
					mergeInputDefaults(src.Provider, src.Inputs, defaultsMap)
				}
			}
			if r.Transform != nil {
				for _, tr := range r.Transform.With {
					mergeInputDefaults(tr.Provider, tr.Inputs, defaultsMap)
				}
			}
			if r.Validate != nil {
				for _, vl := range r.Validate.With {
					mergeInputDefaults(vl.Provider, vl.Inputs, defaultsMap)
				}
			}
		}
	}

	// Merge defaults into action provider inputs
	if sol.Spec.Workflow != nil {
		for _, a := range sol.Spec.Workflow.Actions {
			if defaults, ok := defaultsMap[a.Provider]; ok {
				if a.Inputs == nil {
					a.Inputs = make(map[string]*spec.ValueRef)
				}
				for key, defVal := range defaults {
					if _, exists := a.Inputs[key]; !exists {
						a.Inputs[key] = defVal
					}
				}
			}
		}
	}
}

// mergeInputDefaults merges defaults for a specific provider into its inputs.
// Inline inputs always take precedence over defaults.
func mergeInputDefaults(providerName string, inputs map[string]*spec.ValueRef, defaultsMap map[string]map[string]*spec.ValueRef) {
	defaults, ok := defaultsMap[providerName]
	if !ok || len(defaults) == 0 {
		return
	}

	for key, defVal := range defaults {
		if _, exists := inputs[key]; !exists {
			if inputs == nil {
				// Cannot assign to nil map from outside; caller must initialize.
				// In practice, provider source inputs are always initialized.
				continue
			}
			inputs[key] = defVal
		}
	}
}
