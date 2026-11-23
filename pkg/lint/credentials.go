// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/walk"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

var validAuthHandlerNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// substituteNestedValueRefPlaceholders returns a copy of val where any inline
// nested ValueRef map ({rslvr:...}, {expr:...}, {tmpl:...}, {scrt:...}, {tkn:...})
// is replaced with a placeholder string so provider JSON Schema validation does
// not reject dynamic/credential ValueRef maps inside object/array literals.
func substituteNestedValueRefPlaceholders(val any) any {
	return substituteNestedValueRefDepth(val, 0)
}

func substituteNestedValueRefDepth(val any, depth int) any {
	if depth > 32 || val == nil {
		return val
	}
	switch typed := val.(type) {
	case map[string]any:
		if spec.IsValueRefMap(typed) {
			return "placeholder"
		}
		out := make(map[string]any, len(typed))
		for k, elem := range typed {
			out[k] = substituteNestedValueRefDepth(elem, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			out[i] = substituteNestedValueRefDepth(elem, depth+1)
		}
		return out
	default:
		return val
	}
}

// lintCredentials runs invalid-credential-ref and unmarked-sensitive-credential-resolver
// across all ValueRefs in the solution.
func lintCredentials(sol *solution.Solution, result *Result) {
	if sol == nil {
		return
	}

	appPrefix := secrets.InternalSecretPrefixFor(paths.AppName())

	validateVR := func(basePath string, vr *spec.ValueRef) {
		if vr == nil {
			return
		}
		vr.VisitCredentialRefs(basePath, func(loc string, scrt *string, tkn *spec.TokenRef, parseErr error) {
			if parseErr != nil {
				result.addFinding(
					SeverityError,
					"security",
					loc,
					fmt.Sprintf("invalid credential reference: %v", parseErr),
					"Use 'scrt: <secret-name>' or 'tkn: <handler>' / 'tkn: {handler: <name>, ...}'",
					"invalid-credential-ref",
				)
				return
			}
			if scrt != nil {
				name := *scrt
				if err := secrets.ValidateName(name); err != nil {
					result.addFinding(
						SeverityError,
						"security",
						loc,
						fmt.Sprintf("invalid secret name %q: %v", name, err),
						"Secret names must be 1-255 characters of alphanumeric, '-', '_', or '.', not starting with '.' or '-' and without '..'",
						"invalid-credential-ref",
					)
					return
				}
				if secrets.IsInternalSecret(name) || secrets.IsInternalSecretFor(name, appPrefix) {
					result.addFinding(
						SeverityError,
						"security",
						loc,
						fmt.Sprintf("secret %q is in a reserved internal namespace and cannot be referenced via scrt:", name),
						"Store and reference a user secret name instead of an internal auth/system secret",
						"invalid-credential-ref",
					)
				}
			}
			if tkn != nil {
				handler := strings.TrimSpace(tkn.Handler)
				if handler == "" || len(handler) > 128 || !validAuthHandlerNameRegex.MatchString(handler) {
					result.addFinding(
						SeverityError,
						"security",
						loc,
						fmt.Sprintf("invalid auth handler name %q in tkn: reference", tkn.Handler),
						"Set 'handler' to lowercase alphanumeric characters and hyphens (e.g., 'github', 'gcp', 'entra')",
						"invalid-credential-ref",
					)
				}
				if tkn.Profile != "" {
					norm := auth.NormalizeProfileName(tkn.Profile)
					if norm != "" {
						if err := auth.ValidateProfileName(norm); err != nil {
							result.addFinding(
								SeverityError,
								"security",
								loc,
								fmt.Sprintf("invalid auth profile %q in tkn: reference: %v", tkn.Profile, err),
								"Use 1-64 ASCII alphanumeric, hyphen, or underscore characters for 'profile'",
								"invalid-credential-ref",
							)
						}
					}
				}
				if rawMin := strings.TrimSpace(tkn.MinValidFor); rawMin != "" {
					dur, err := time.ParseDuration(rawMin)
					if err != nil || dur <= 0 {
						result.addFinding(
							SeverityError,
							"security",
							loc,
							fmt.Sprintf("invalid minValidFor %q in tkn: reference: must be a positive duration (e.g. 5m, 30s)", tkn.MinValidFor),
							"Set 'minValidFor' to a positive Go duration string such as '5m' or '30s'",
							"invalid-credential-ref",
						)
					}
				}
			}
		})
	}

	_ = walk.Walk(sol, &walk.Visitor{
		ValueRef: func(path string, vr *spec.ValueRef) error {
			validateVR(strings.TrimPrefix(path, "spec."), vr)
			return nil
		},
	})

	// Also walk spec.calls, state, and bundle.plugins[*].defaults, which walk.Walk skips.
	if len(sol.Spec.Calls) > 0 {
		callNames := make([]string, 0, len(sol.Spec.Calls))
		for k := range sol.Spec.Calls {
			callNames = append(callNames, k)
		}
		sort.Strings(callNames)
		for _, cname := range callNames {
			c := sol.Spec.Calls[cname]
			if c == nil {
				continue
			}
			for _, inKey := range sortedValueRefKeys(c.Inputs) {
				validateVR(fmt.Sprintf("calls.%s.inputs.%s", cname, inKey), c.Inputs[inKey])
			}
		}
	}

	if sol.State != nil {
		validateVR("state.enabled", sol.State.Enabled)
		if sol.State.Load != nil {
			for _, inKey := range sortedValueRefKeys(sol.State.Load.Inputs) {
				validateVR(fmt.Sprintf("state.load.inputs.%s", inKey), sol.State.Load.Inputs[inKey])
			}
		}
		for i, save := range sol.State.Save {
			validateVR(fmt.Sprintf("state.save[%d].enabled", i), save.Enabled)
			for _, inKey := range sortedValueRefKeys(save.Inputs) {
				validateVR(fmt.Sprintf("state.save[%d].inputs.%s", i, inKey), save.Inputs[inKey])
			}
		}
	}

	for i, bp := range sol.Bundle.Plugins {
		for _, defKey := range sortedValueRefKeys(bp.Defaults) {
			validateVR(fmt.Sprintf("bundle.plugins[%d].defaults.%s", i, defKey), bp.Defaults[defKey])
		}
	}

	lintUnmarkedSensitiveCredentialResolvers(sol, result)
}

func lintUnmarkedSensitiveCredentialResolvers(sol *solution.Solution, result *Result) {
	if len(sol.Spec.Resolvers) == 0 {
		return
	}

	callUsesCredentials := make(map[string]bool, len(sol.Spec.Calls))
	for cname, c := range sol.Spec.Calls {
		if c == nil {
			continue
		}
		for _, vr := range c.Inputs {
			if vr.ContainsCredentialRef() {
				callUsesCredentials[cname] = true
				break
			}
		}
	}

	names := make([]string, 0, len(sol.Spec.Resolvers))
	for name := range sol.Spec.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		res := sol.Spec.Resolvers[name]
		if res == nil || res.Sensitive {
			continue
		}
		if resolverReferencesCredentials(res, callUsesCredentials) {
			result.addFinding(
				SeverityWarning,
				"security",
				fmt.Sprintf("resolvers.%s", name),
				fmt.Sprintf("resolver '%s' references credentials via scrt: or tkn: without 'sensitive: true'", name),
				"Add 'sensitive: true' to the resolver so downstream steps receive the real credential while snapshots, tables, and state redact it",
				"unmarked-sensitive-credential-resolver",
			)
		}
	}
}

func resolverReferencesCredentials(res *resolver.Resolver, callUsesCredentials map[string]bool) bool {
	mapHasCred := func(m map[string]*spec.ValueRef) bool {
		for _, vr := range m {
			if vr.ContainsCredentialRef() {
				return true
			}
		}
		return false
	}

	if res.Resolve != nil {
		for _, step := range res.Resolve.With {
			if callUsesCredentials[step.Call] || mapHasCred(step.Inputs) || mapHasCred(step.Args) {
				return true
			}
			if step.ForEach != nil && step.ForEach.In.ContainsCredentialRef() {
				return true
			}
		}
	}
	if res.Transform != nil {
		for _, step := range res.Transform.With {
			if callUsesCredentials[step.Call] || mapHasCred(step.Inputs) || mapHasCred(step.Args) {
				return true
			}
			if step.ForEach != nil && step.ForEach.In.ContainsCredentialRef() {
				return true
			}
		}
	}
	if res.Validate != nil {
		for _, step := range res.Validate.With {
			if callUsesCredentials[step.Call] || mapHasCred(step.Inputs) || mapHasCred(step.Args) || step.Message.ContainsCredentialRef() {
				return true
			}
		}
	}
	if res.Messages != nil && res.Messages.Error.ContainsCredentialRef() {
		return true
	}
	return false
}

func sortedValueRefKeys(m map[string]*spec.ValueRef) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
