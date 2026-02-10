// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"

	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// DynamicPathWarning describes a provider input that uses a dynamic path
// (CEL expression, Go template, or resolver binding) which cannot be
// statically analyzed for file bundling.
type DynamicPathWarning struct {
	// Location describes where in the solution the dynamic path was found
	// (e.g., "resolver 'templatePath'").
	Location string
	// Kind is the type of dynamic reference ("expr", "tmpl", "rslvr").
	Kind string
	// Expression is the dynamic expression value.
	Expression string
}

// DetectDynamicPaths scans a solution for provider inputs that use dynamic
// paths (CEL, Go templates, resolver bindings) in file-related fields.
// These paths cannot be statically analyzed and should be covered by
// bundle.include patterns.
func DetectDynamicPaths(sol *solution.Solution) []DynamicPathWarning {
	if sol == nil {
		return nil
	}

	var warnings []DynamicPathWarning

	// Check resolver inputs
	for name, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil {
			continue
		}
		for _, ps := range r.Resolve.With {
			switch ps.Provider {
			case "file":
				if w := checkDynamicInput(ps.Inputs, "path", fmt.Sprintf("resolver '%s' (file provider)", name)); w != nil {
					warnings = append(warnings, *w)
				}
			case "solution":
				if w := checkDynamicInput(ps.Inputs, "source", fmt.Sprintf("resolver '%s' (solution provider)", name)); w != nil {
					warnings = append(warnings, *w)
				}
			}
		}

		if r.Transform != nil {
			for _, pt := range r.Transform.With {
				if pt.Provider == "file" {
					if w := checkDynamicInput(pt.Inputs, "path", fmt.Sprintf("resolver '%s' transform (file provider)", name)); w != nil {
						warnings = append(warnings, *w)
					}
				}
			}
		}
	}

	// Check action inputs
	if sol.Spec.Workflow != nil {
		checkActionDynamicPaths(sol.Spec.Workflow.Actions, &warnings)
		checkActionDynamicPaths(sol.Spec.Workflow.Finally, &warnings)
	}

	return warnings
}

// checkActionDynamicPaths checks actions for dynamic file paths.
func checkActionDynamicPaths(actions map[string]*actionpkg.Action, warnings *[]DynamicPathWarning) {
	for name, a := range actions {
		if a == nil {
			continue
		}
		switch a.Provider {
		case "file":
			if w := checkDynamicInput(a.Inputs, "path", fmt.Sprintf("action '%s' (file provider)", name)); w != nil {
				*warnings = append(*warnings, *w)
			}
		case "solution":
			if w := checkDynamicInput(a.Inputs, "source", fmt.Sprintf("action '%s' (solution provider)", name)); w != nil {
				*warnings = append(*warnings, *w)
			}
		}
	}
}

// checkDynamicInput checks if a specific input field uses a dynamic reference.
func checkDynamicInput(inputs map[string]*spec.ValueRef, field, location string) *DynamicPathWarning {
	if inputs == nil {
		return nil
	}
	vr := inputs[field]
	if vr == nil {
		return nil
	}

	if vr.Expr != nil {
		return &DynamicPathWarning{
			Location:   location,
			Kind:       "expr",
			Expression: string(*vr.Expr),
		}
	}
	if vr.Tmpl != nil {
		return &DynamicPathWarning{
			Location:   location,
			Kind:       "tmpl",
			Expression: string(*vr.Tmpl),
		}
	}
	if vr.Resolver != nil {
		return &DynamicPathWarning{
			Location:   location,
			Kind:       "rslvr",
			Expression: *vr.Resolver,
		}
	}

	return nil
}
