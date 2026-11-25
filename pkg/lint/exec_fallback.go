// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	execProviderName                 = "exec"
	execFailOnErrorInput             = "failOnError"
	ruleExecSourceWithoutFailOnError = "exec-source-without-fail-on-error"
)

// lintExecFallbackSources warns when a non-final 'exec' source in a resolver's
// resolve.with fallback chain does not enable failOnError. For the 'from' and
// 'transform' capabilities, exec defaults failOnError to false, so a non-zero
// exit code succeeds with empty output and prevents subsequent fallback sources
// from running.
func lintExecFallbackSources(sol *solution.Solution, result *Result) {
	if sol == nil || len(sol.Spec.Resolvers) == 0 {
		return
	}

	execDefaults := execPluginDefaults(sol)

	for _, name := range sortedResolverKeys(sol.Spec.Resolvers) {
		res := sol.Spec.Resolvers[name]
		if res == nil || res.Resolve == nil || len(res.Resolve.With) < 2 {
			continue
		}

		for i := 0; i < len(res.Resolve.With)-1; i++ {
			step := res.Resolve.With[i]

			switch {
			case step.Provider == execProviderName && step.Call == "":
				vr, exists := step.Inputs[execFailOnErrorInput]
				if !exists && execDefaults != nil {
					vr = execDefaults[execFailOnErrorInput]
				}
				if execFailOnErrorSatisfied(vr) {
					continue
				}

				location := fmt.Sprintf("resolvers.%s.resolve.with[%d]", name, i)
				result.addFinding(
					SeverityWarning,
					"provider",
					location,
					fmt.Sprintf("resolver '%s' uses 'exec' source at resolve.with[%d] before a fallback source without 'failOnError: true'; non-zero exit codes succeed with empty output and prevent subsequent sources from running", name, i),
					"Add 'failOnError: true' to the exec source inputs (or via bundle.plugins defaults) so non-zero exit codes fall through to the next source in resolve.with",
					ruleExecSourceWithoutFailOnError,
				)

			case step.Call != "" && step.Provider == "":
				def, ok := sol.Spec.Calls[step.Call]
				if !ok || def == nil || def.Provider != execProviderName {
					continue
				}
				// Call definitions do not inherit bundle.plugins defaults (matching MergePluginDefaults).
				if execFailOnErrorSatisfied(def.Inputs[execFailOnErrorInput]) {
					continue
				}

				location := fmt.Sprintf("resolvers.%s.resolve.with[%d]", name, i)
				result.addFinding(
					SeverityWarning,
					"provider",
					location,
					fmt.Sprintf("resolver '%s' invokes call '%s' (exec) at resolve.with[%d] before a fallback source without 'failOnError: true'; non-zero exit codes succeed with empty output and prevent subsequent sources from running", name, step.Call, i),
					fmt.Sprintf("Add 'failOnError: true' to spec.calls.%s.inputs so non-zero exit codes fall through to the next source in resolve.with", step.Call),
					ruleExecSourceWithoutFailOnError,
				)
			}
		}
	}
}

// execPluginDefaults returns the default inputs configured on a bundle.plugins
// provider entry whose local name is "exec", matching MergePluginDefaults.
// Runtime provider registration keys providers by the name returned from the
// plugin binary's GetProviders RPC ("exec") rather than the OCI artifact leaf.
func execPluginDefaults(sol *solution.Solution) map[string]*spec.ValueRef {
	var defaults map[string]*spec.ValueRef
	for _, p := range sol.Bundle.Plugins {
		if (p.Kind == "" || p.Kind == solution.PluginKindProvider) && p.LocalName() == execProviderName && len(p.Defaults) > 0 {
			defaults = p.Defaults
		}
	}
	return defaults
}

// execFailOnErrorSatisfied reports whether vr configures failOnError to a
// non-false value. Dynamic references (rslvr, tmpl, scrt, tkn, and CEL
// expressions other than literal "false") are treated as satisfied to avoid
// false positives when failOnError is parameterized.
func execFailOnErrorSatisfied(vr *spec.ValueRef) bool {
	if vr == nil {
		return false
	}
	if vr.Expr != nil {
		return strings.TrimSpace(string(*vr.Expr)) != "false"
	}
	if vr.Resolver != nil || vr.Tmpl != nil || vr.Secret != nil || vr.Token != nil {
		return true
	}
	b, ok := vr.Literal.(bool)
	return ok && b
}
