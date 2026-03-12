// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
)

// ValidateSpec performs validation specific to the spec section of a solution.
// It validates resolver naming conventions and checks for circular dependencies.
func (s *Solution) ValidateSpec() error {
	if s == nil {
		return nil
	}

	// Skip if no resolvers
	if !s.Spec.HasResolvers() {
		return nil
	}

	var problems []string

	// Build set of valid resolver names for reference validation
	resolverNames := make(map[string]bool)
	for name := range s.Spec.Resolvers {
		resolverNames[name] = true
	}

	// Validate resolver naming conventions and dependsOn references
	for name, r := range s.Spec.Resolvers {
		if err := validateResolverName(name); err != nil {
			problems = append(problems, err.Error())
		}

		if r == nil {
			problems = append(problems, fmt.Sprintf("resolver %q has a null value — a resolve block is required", name))
			// Mark that we have nil resolvers; skip cycle check later to avoid confusing cascading errors
			resolverNames[name] = false
			continue
		}

		// Validate dependsOn references point to existing resolvers
		for _, dep := range r.DependsOn {
			if dep == "" {
				problems = append(problems, fmt.Sprintf("resolver %q has empty dependsOn entry", name))
				continue
			}
			if dep == name {
				problems = append(problems, fmt.Sprintf("resolver %q cannot depend on itself", name))
				continue
			}
			if !resolverNames[dep] {
				problems = append(problems, fmt.Sprintf("resolver %q has dependsOn reference to non-existent resolver %q", name, dep))
			}
		}
	}

	// Check for circular dependencies by building phases, but only if there are no nil resolvers.
	// Nil resolvers cause ResolversToSlice to drop them, which can produce confusing cascading
	// dependency errors ("depends on X but X wasn't present").
	hasNilResolvers := false
	for _, valid := range resolverNames {
		if !valid {
			hasNilResolvers = true
			break
		}
	}

	// Note: We pass nil for the lookup since spec validation doesn't have access to
	// the provider registry. This means generic dependency extraction is used,
	// which is sufficient for detecting circular dependencies.
	if !hasNilResolvers {
		resolvers := s.Spec.ResolversToSlice()
		if len(resolvers) > 0 {
			_, err := resolver.BuildPhases(resolvers, nil)
			if err != nil {
				problems = append(problems, fmt.Sprintf("resolver dependency error: %v", err))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("spec validation failed: %s", strings.Join(problems, "; "))
	}

	return nil
}

// validateResolverName validates a resolver name according to naming conventions:
// - Cannot start with "__" (reserved for internal use)
// - Cannot contain whitespace
// - Must not be empty
func validateResolverName(name string) error {
	if name == "" {
		return fmt.Errorf("resolver name cannot be empty")
	}

	if strings.HasPrefix(name, "__") {
		return fmt.Errorf("resolver name %q cannot start with '__' (reserved for internal use)", name)
	}

	for _, r := range name {
		if unicode.IsSpace(r) {
			return fmt.Errorf("resolver name %q cannot contain whitespace", name)
		}
	}

	return nil
}
