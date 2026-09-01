// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// callLintSite is a normalized view of a host location (resolver step or action)
// that may invoke a call definition.
type callLintSite struct {
	location string
	call     string
	provider string
	args     map[string]*spec.ValueRef
}

// lintCalls validates spec.calls definitions and every call site in the
// solution, emitting findings that mirror the structural call validation while
// adding advisory notices (unregistered definition provider) and enforcing the
// isolation contract (definition inputs referencing solution resolvers).
func lintCalls(sol *solution.Solution, result *Result, registry providerLookup) {
	if sol == nil || !sol.Spec.HasCalls() {
		// Call sites can still exist without any definitions; those surface as
		// call-not-found below, so keep walking even with no definitions.
		if sol == nil {
			return
		}
	}

	lintCallDefinitions(sol, result, registry)

	for _, site := range collectCallLintSites(sol) {
		lintCallSite(sol, site, result)
	}
}

// lintCallDefinitions checks each definition's provider registration and flags
// definition inputs that reference solution resolvers, which violate the strict
// isolation contract (definitions may reference only their declared args).
func lintCallDefinitions(sol *solution.Solution, result *Result, registry providerLookup) {
	if !sol.Spec.HasCalls() {
		return
	}

	resolverNames := make(map[string]bool, len(sol.Spec.Resolvers))
	for name := range sol.Spec.Resolvers {
		resolverNames[name] = true
	}

	for _, name := range sortedCallDefNames(sol.Spec.Calls) {
		def := sol.Spec.Calls[name]
		if def == nil {
			continue
		}
		location := fmt.Sprintf("spec.calls.%s", name)

		if registry != nil && def.Provider != "" && !registry.Has(def.Provider) {
			result.addFinding(SeverityWarning, "call", location,
				fmt.Sprintf("call '%s' names provider '%s' which is not registered", name, def.Provider),
				"Check spelling or register the provider; run list_providers to see available providers",
				"call-provider-not-found")
		}

		// The reserved args namespace is exempt: inputs referencing _.args.x or
		// {{ .args.x }} extract a root of "args", which must not be treated as a
		// resolver reference even when a solution declares a resolver named
		// "args".
		for _, ref := range resolver.ExtractRefsFromValueRefs(def.Inputs) {
			if ref == call.ArgsNamespace {
				continue
			}
			if resolverNames[ref] {
				result.addFinding(SeverityError, "call", location,
					fmt.Sprintf("call '%s' references resolver '%s'; call definitions are isolated and may reference only their declared args", name, ref),
					fmt.Sprintf("Pass '%s' in as an argument (declare it under the definition's 'args' and supply it at each call site)", ref),
					"call-definition-resolver-ref")
			}
		}
	}
}

// lintCallSite applies the exclusivity, args-without-call, call-exists,
// unknown-argument, and missing-required-argument rules to a single call site.
func lintCallSite(sol *solution.Solution, site callLintSite, result *Result) {
	if site.call == "" {
		if len(site.args) > 0 {
			result.addFinding(SeverityError, "call", site.location,
				"'args' is set but 'call' is not; arguments have nowhere to bind",
				"Add a 'call' referencing a definition, or remove the 'args' block",
				"call-args-without-call")
		}
		return
	}

	if site.provider != "" {
		result.addFinding(SeverityError, "call", site.location,
			fmt.Sprintf("both 'call' (%s) and 'provider' (%s) are set; they are mutually exclusive", site.call, site.provider),
			"Remove 'provider' when using 'call', or remove 'call' when naming a provider directly",
			"call-provider-exclusive")
	}

	def, ok := sol.Spec.Calls[site.call]
	if !ok || def == nil {
		result.addFinding(SeverityError, "call", site.location,
			fmt.Sprintf("call '%s' is not defined under spec.calls", site.call),
			"Add the definition under spec.calls, or correct the 'call' name",
			"call-not-found")
		return
	}

	// Unknown arguments.
	for _, argName := range sortedValueRefNames(site.args) {
		if _, declared := def.Args[argName]; !declared {
			result.addFinding(SeverityError, "call", site.location,
				fmt.Sprintf("unknown argument '%s' supplied to call '%s'", argName, site.call),
				"Correct the argument name, or declare it under the definition's 'args'",
				"call-unknown-arg")
		}
	}

	// Missing required arguments.
	for _, argName := range sortedArgDefNames(def.Args) {
		argDef := def.Args[argName]
		if argDef == nil || !argDef.Required {
			continue
		}
		if _, supplied := site.args[argName]; !supplied {
			result.addFinding(SeverityError, "call", site.location,
				fmt.Sprintf("required argument '%s' is not supplied to call '%s'", argName, site.call),
				"Supply the required argument under 'args', or make it optional in the definition",
				"call-missing-arg")
		}
	}
}

// collectCallLintSites gathers every resolver step and workflow action that may
// invoke a call, ordered for deterministic findings.
func collectCallLintSites(sol *solution.Solution) []callLintSite {
	var sites []callLintSite

	if sol.Spec.HasResolvers() {
		for _, name := range sortedResolverKeys(sol.Spec.Resolvers) {
			res := sol.Spec.Resolvers[name]
			if res == nil {
				continue
			}
			location := fmt.Sprintf("resolvers.%s", name)
			if res.Resolve != nil {
				for i := range res.Resolve.With {
					src := res.Resolve.With[i]
					sites = append(sites, callLintSite{
						location: fmt.Sprintf("%s.resolve.with[%d]", location, i),
						call:     src.Call,
						provider: src.Provider,
						args:     src.Args,
					})
				}
			}
			if res.Transform != nil {
				for i := range res.Transform.With {
					t := res.Transform.With[i]
					sites = append(sites, callLintSite{
						location: fmt.Sprintf("%s.transform.with[%d]", location, i),
						call:     t.Call,
						provider: t.Provider,
						args:     t.Args,
					})
				}
			}
			if res.Validate != nil {
				for i := range res.Validate.With {
					v := res.Validate.With[i]
					sites = append(sites, callLintSite{
						location: fmt.Sprintf("%s.validate.with[%d]", location, i),
						call:     v.Call,
						provider: v.Provider,
						args:     v.Args,
					})
				}
			}
		}
	}

	if sol.Spec.HasWorkflow() && sol.Spec.Workflow != nil {
		sites = append(sites, actionCallLintSites("workflow.actions", sol.Spec.Workflow.Actions)...)
		sites = append(sites, actionCallLintSites("workflow.finally", sol.Spec.Workflow.Finally)...)
	}

	return sites
}

func actionCallLintSites(prefix string, actions map[string]*action.Action) []callLintSite {
	if len(actions) == 0 {
		return nil
	}
	names := make([]string, 0, len(actions))
	for name := range actions {
		names = append(names, name)
	}
	sort.Strings(names)

	sites := make([]callLintSite, 0, len(actions))
	for _, name := range names {
		a := actions[name]
		if a == nil {
			continue
		}
		sites = append(sites, callLintSite{
			location: fmt.Sprintf("%s.%s", prefix, name),
			call:     a.Call,
			provider: a.Provider,
			args:     a.Args,
		})
	}
	return sites
}

func sortedCallDefNames(m map[string]*spec.Call) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedArgDefNames(m map[string]*spec.ArgDef) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedValueRefNames(m map[string]*spec.ValueRef) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedResolverKeys(m map[string]*resolver.Resolver) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
