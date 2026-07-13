// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// callSite describes a single host location that may invoke a call definition
// (a resolver source/transform/validation step or a workflow action). It
// normalizes the embedded spec.CallRef plus the host provider so that call
// validation can be applied uniformly across all host kinds.
type callSite struct {
	location string
	call     string
	provider string
	args     map[string]*spec.ValueRef
}

// validateCalls validates call definitions and every call site in the solution.
// It returns a slice of human-readable problem strings (empty when valid). It is
// invoked from ValidateSpec and does not require a provider registry: all checks
// are structural and operate on the declared spec.
func (s *Solution) validateCalls() []string {
	if s == nil {
		return nil
	}

	defs := s.validateCallDefinitions()
	sites := s.validateCallSites()
	problems := make([]string, 0, len(defs)+len(sites))
	problems = append(problems, defs...)
	problems = append(problems, sites...)
	return problems
}

// validateCallDefinitions validates each spec.calls entry: the provider must be
// set, no argument may be both required and carry a default, and the definition
// must be strictly isolated (its inputs may reference only its declared args
// plus always-available globals, never a solution resolver by name).
func (s *Solution) validateCallDefinitions() []string {
	if !s.Spec.HasCalls() {
		return nil
	}

	resolverNames := make(map[string]bool, len(s.Spec.Resolvers))
	for name := range s.Spec.Resolvers {
		resolverNames[name] = true
	}

	var problems []string
	for _, name := range sortedCallNames(s.Spec.Calls) {
		def := s.Spec.Calls[name]
		if def == nil {
			problems = append(problems, fmt.Sprintf("call %q has a null value — a provider is required", name))
			continue
		}
		if def.Provider == "" {
			problems = append(problems, fmt.Sprintf("call %q must declare a provider", name))
		}
		for _, argName := range sortedArgNames(def.Args) {
			argDef := def.Args[argName]
			if argDef == nil {
				continue
			}
			if argDef.Required && argDef.Default != nil {
				problems = append(problems,
					fmt.Sprintf("call %q argument %q cannot be required and also declare a default", name, argName))
			}
		}
		// Strict isolation: a definition input may not reference a solution
		// resolver by name. Data must be passed in as a declared argument. The
		// reserved args namespace is exempt so that inputs using _.args.x or
		// {{ .args.x }} are not misread as a resolver reference when a solution
		// happens to declare a resolver named "args".
		for _, ref := range resolver.ExtractRefsFromValueRefs(def.Inputs) {
			if ref == call.ArgsNamespace {
				continue
			}
			if resolverNames[ref] {
				problems = append(problems,
					fmt.Sprintf("call %q input references solution resolver %q — call definitions are isolated and may reference only their declared args; pass %q in as an argument", name, ref, ref))
			}
		}
	}
	return problems
}

// validateCallSites collects every call site in the solution and applies the
// exclusivity, args-without-call, call-exists, unknown-argument, and
// missing-required-argument rules.
func (s *Solution) validateCallSites() []string {
	sites := s.collectCallSites()
	if len(sites) == 0 {
		return nil
	}

	var problems []string
	for _, site := range sites {
		problems = append(problems, s.validateCallSite(site)...)
	}
	return problems
}

// validateCallSite validates a single call site.
func (s *Solution) validateCallSite(site callSite) []string {
	var problems []string

	// A call site with args but no call has nowhere to bind those args.
	// A missing provider (neither call nor provider) is intentionally left to
	// action/resolver validation and lint, which surface it as a "provider is
	// required" finding rather than a fatal load-time error.
	if site.call == "" {
		if len(site.args) > 0 {
			problems = append(problems,
				fmt.Sprintf("%s declares args but no call — args are only valid alongside call", site.location))
		}
		return problems
	}

	// Exclusivity: a host may set either call or provider, never both.
	if site.provider != "" {
		problems = append(problems,
			fmt.Sprintf("%s sets both call %q and provider %q — they are mutually exclusive", site.location, site.call, site.provider))
	}

	def, ok := s.Spec.Calls[site.call]
	if !ok || def == nil {
		problems = append(problems,
			fmt.Sprintf("%s references undefined call %q", site.location, site.call))
		return problems
	}

	problems = append(problems, validateSiteArgs(site, def)...)
	return problems
}

// validateSiteArgs checks a call site's supplied argument names against the
// definition: unknown names are rejected and required arguments must be present.
func validateSiteArgs(site callSite, def *spec.Call) []string {
	var problems []string

	// Reject argument names the definition does not declare.
	for _, argName := range sortedValueRefKeys(site.args) {
		if _, declared := def.Args[argName]; !declared {
			problems = append(problems,
				fmt.Sprintf("%s supplies unknown argument %q to call %q", site.location, argName, site.call))
		}
	}

	// Every required argument must be supplied at the call site.
	for _, argName := range sortedArgNames(def.Args) {
		argDef := def.Args[argName]
		if argDef == nil || !argDef.Required {
			continue
		}
		if _, supplied := site.args[argName]; !supplied {
			problems = append(problems,
				fmt.Sprintf("%s is missing required argument %q for call %q", site.location, argName, site.call))
		}
	}
	return problems
}

// collectCallSites gathers every resolver step and workflow action that could
// invoke a call, in a stable order for deterministic diagnostics.
func (s *Solution) collectCallSites() []callSite {
	var sites []callSite

	if s.Spec.HasResolvers() {
		for _, name := range sortedResolverNames(s.Spec.Resolvers) {
			r := s.Spec.Resolvers[name]
			if r == nil {
				continue
			}
			sites = append(sites, resolverCallSites(name, r)...)
		}
	}

	if s.Spec.HasWorkflow() && s.Spec.Workflow != nil {
		sites = append(sites, actionCallSites("action", s.Spec.Workflow.Actions)...)
		sites = append(sites, actionCallSites("finally action", s.Spec.Workflow.Finally)...)
	}

	return sites
}

// resolverCallSites returns the call sites contributed by a single resolver's
// resolve, transform, and validate phases.
func resolverCallSites(name string, r *resolver.Resolver) []callSite {
	var sites []callSite

	if r.Resolve != nil {
		for i := range r.Resolve.With {
			src := r.Resolve.With[i]
			sites = append(sites, callSite{
				location: fmt.Sprintf("resolver %q resolve step %d", name, i),
				call:     src.Call,
				provider: src.Provider,
				args:     src.Args,
			})
		}
	}
	if r.Transform != nil {
		for i := range r.Transform.With {
			t := r.Transform.With[i]
			sites = append(sites, callSite{
				location: fmt.Sprintf("resolver %q transform step %d", name, i),
				call:     t.Call,
				provider: t.Provider,
				args:     t.Args,
			})
		}
	}
	if r.Validate != nil {
		for i := range r.Validate.With {
			v := r.Validate.With[i]
			sites = append(sites, callSite{
				location: fmt.Sprintf("resolver %q validate step %d", name, i),
				call:     v.Call,
				provider: v.Provider,
				args:     v.Args,
			})
		}
	}
	return sites
}

// actionCallSites returns the call sites contributed by a map of actions.
func actionCallSites(kind string, actions map[string]*action.Action) []callSite {
	if len(actions) == 0 {
		return nil
	}
	var sites []callSite
	for _, name := range sortedActionNames(actions) {
		a := actions[name]
		if a == nil {
			continue
		}
		sites = append(sites, callSite{
			location: fmt.Sprintf("%s %q", kind, name),
			call:     a.Call,
			provider: a.Provider,
			args:     a.Args,
		})
	}
	return sites
}

func sortedCallNames(m map[string]*spec.Call) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedResolverNames(m map[string]*resolver.Resolver) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedActionNames(m map[string]*action.Action) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedArgNames(m map[string]*spec.ArgDef) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedValueRefKeys(m map[string]*spec.ValueRef) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
