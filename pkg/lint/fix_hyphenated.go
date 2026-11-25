// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// hyphenatedNameRule is the rule whose findings this fixer resolves.
const hyphenatedNameRule = "hyphenated-name"

//nolint:gochecknoinits // Package-level registration of the auto-fixer, matching the concepts registry pattern.
func init() {
	RegisterFixer(hyphenatedNameRule, fixHyphenatedName)
}

// fixHyphenatedName resolves a hyphenated-name finding by renaming the resolver
// to its camelCase form and rewriting every reference. The reference rewriting
// is delegated wholesale to refactor.RenameResolver, which locates dependsOn
// entries, rslvr values, CEL '_.name'/'_["name"]' uses, and explicit template
// references byte-exact and refuses (all-or-nothing) if any reference cannot be
// located or the target name collides. Those refusals are surfaced as a wrapped
// ErrNotFixable so the finding is skipped rather than aborting the whole run.
func fixHyphenatedName(sol *solution.Solution, f *Finding) (*Fix, error) {
	name, ok := resolverNameFromLocation(f.Location)
	if !ok {
		return nil, fmt.Errorf("cannot determine resolver name from location %q: %w", f.Location, ErrNotFixable)
	}

	newName := hyphensToCamelCase(name)
	if _, exists := sol.Spec.Resolvers[newName]; exists {
		return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: resolver %q already exists: %w", name, newName, newName, ErrNotFixable)
	}
	for _, part := range sol.ComposedParts() {
		if len(part.RawContent) == 0 {
			continue
		}
		var partSol solution.Solution
		if err := partSol.LoadFromBytesLenient(part.RawContent); err != nil {
			return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: failed to inspect composed file %s: %w: %w", name, newName, part.Path, err, ErrNotFixable)
		}
		if collectReferencedResolvers(&partSol)[name] || testCasesMockResolver(&partSol, name) {
			return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: referenced in composed file %s: %w", name, newName, part.Path, ErrNotFixable)
		}
	}
	res, err := refactor.RenameResolver(sol, name, newName)
	if err != nil {
		// RenameResolver refuses on collision, unlocatable references, or an
		// invalid target name. Any of these means this finding is not safely
		// auto-fixable; record the reason and move on.
		return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: %w: %w", name, newName, err, ErrNotFixable)
	}
	if len(sol.RawContent()) > 0 {
		preview, applyErr := refactor.Apply(sol.RawContent(), res.Edits)
		if applyErr != nil {
			return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: %w: %w", name, newName, applyErr, ErrNotFixable)
		}
		var postPrimary solution.Solution
		if err := postPrimary.LoadFromBytesLenient(preview); err != nil {
			return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: %w: %w", name, newName, err, ErrNotFixable)
		}
		if collectReferencedResolvers(&postPrimary)[name] || testCasesMockResolver(&postPrimary, name) {
			return nil, fmt.Errorf("cannot auto-rename resolver %q to %q: unrewritten reference remains in solution: %w", name, newName, ErrNotFixable)
		}
	}

	return &Fix{
		Edits:  res.Edits,
		Detail: fmt.Sprintf("renamed resolver %q -> %q (%d reference(s))", name, newName, len(res.Edits)),
	}, nil
}

func testCasesMockResolver(sol *solution.Solution, resolverName string) bool {
	if sol == nil || sol.Spec.Testing == nil {
		return false
	}
	for _, tc := range sol.Spec.Testing.Cases {
		if tc == nil {
			continue
		}
		if _, ok := tc.Mocks[resolverName]; ok {
			return true
		}
	}
	return false
}

// resolverNameFromLocation extracts the resolver name from a finding location
// of the form "resolvers.<name>". It reports false when the location is not a
// resolver location.
func resolverNameFromLocation(location string) (string, bool) {
	const prefix = "resolvers."
	if !strings.HasPrefix(location, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(location, prefix)
	if name == "" || strings.Contains(name, ".") {
		return "", false
	}
	return name, true
}
