// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"gopkg.in/yaml.v3"
)

// QuickFixFor returns the source edits that resolve finding f when a
// deterministic fix exists, and ok=false when the rule has no quick fix. It is
// the single source of quick-fix logic: the LSP code-action handler maps these
// edits into a WorkspaceEdit rather than re-deriving fixes, so the fix stays in
// lockstep with the lint rules that produce the findings.
//
// registry is the same provider registry used to produce f (may be nil). It is
// threaded into the redundant-dependsOn fix so the redundant set is recomputed
// with the identical provider-aware dependency extraction the lint rule used --
// keeping the removal in exact lockstep with the finding rather than a possibly
// divergent generic recompute.
//
// Supported rules: "deprecated-field" (replace the deprecated key with its
// tagged replacement, translating the value), "redundant-depends-on" (remove the
// redundant dependsOn entry or entries), and "unused-resolver" (remove the whole
// resolver block). Any other rule -- or any finding whose fix cannot be computed
// deterministically from sol -- yields (nil, false). It never panics on odd
// input: a fix that cannot be built safely is reported as "no fix" rather than a
// broken edit.
func QuickFixFor(sol *solution.Solution, f *Finding, registry *provider.Registry) (edits []refactor.TextEdit, ok bool) {
	if sol == nil || f == nil {
		return nil, false
	}
	raw := sol.RawContent()
	if len(raw) == 0 {
		return nil, false
	}

	switch f.RuleName {
	case "deprecated-field":
		return quickFixDeprecatedField(raw, f)
	case "redundant-depends-on":
		return quickFixRedundantDependsOn(sol, raw, f, registry)
	case "unused-resolver":
		return quickFixUnusedResolver(sol, raw, f)
	default:
		return nil, false
	}
}

// nodeMapPath maps a finding Location to the NodeMap/SourceMap path. Lint
// Locations omit the "spec." prefix (e.g. "resolvers.foo"), while the node/source
// maps are spec.-prefixed. The prefixed variant is tried first, then the raw
// location as a fallback, matching addFinding's dual lookup. It returns the
// resolved path, or ok=false when neither form is present in nodes.
func nodeMapPath(nodes map[string]*yaml.Node, loc string) (string, bool) {
	if _, present := nodes["spec."+loc]; present {
		return "spec." + loc, true
	}
	if _, present := nodes[loc]; present {
		return loc, true
	}
	return "", false
}

// quickFixDeprecatedField builds the onError -> continueOnError replacement. The
// replacement key name is read from the struct tag via lookupDeprecatedField on
// the proto that matches the finding's location context, so it tracks lint
// rather than hardcoding "continueOnError". The current value is translated
// continue -> true / fail -> false (the only two valid onError values).
func quickFixDeprecatedField(raw []byte, f *Finding) ([]refactor.TextEdit, bool) {
	nodes, err := refindex.NodeMap(raw)
	if err != nil {
		return nil, false
	}
	path, ok := nodeMapPath(nodes, f.Location)
	if !ok {
		return nil, false
	}
	node := nodes[path]
	if node == nil {
		return nil, false
	}

	newValue, ok := translateOnErrorValue(node.Value)
	if !ok {
		return nil, false
	}

	meta, ok := lookupDeprecatedField(deprecatedProtoFor(f.Location), "OnError")
	if !ok || meta.replacement == "" {
		return nil, false
	}

	edit, err := refactor.ReplaceMappingKeyAndValue(raw, path, meta.replacement, newValue)
	if err != nil {
		return nil, false
	}
	return []refactor.TextEdit{edit}, true
}

// deprecatedProtoFor returns the struct prototype whose OnError tag describes the
// deprecation at loc, so lookupDeprecatedField reads the correct replacement.
// forEach is checked before the action forms because a forEach.onError lives
// under an action location.
func deprecatedProtoFor(loc string) any {
	switch {
	case strings.HasSuffix(loc, ".forEach.onError"):
		return spec.ForEachClause{}
	case strings.Contains(loc, ".resolve.with["):
		return resolver.ProviderSource{}
	case strings.Contains(loc, ".transform.with["):
		return resolver.ProviderTransform{}
	default:
		// workflow.actions.<name>.onError / workflow.finally.<name>.onError
		return action.Action{}
	}
}

// translateOnErrorValue maps a deprecated onError value to the boolean
// continueOnError equivalent: "continue" (keep going on error) -> "true",
// "fail" (stop on error) -> "false". Any other value yields ok=false so no fix
// is offered.
func translateOnErrorValue(v string) (string, bool) {
	switch strings.TrimSpace(v) {
	case string(spec.OnErrorContinue):
		return "true", true
	case string(spec.OnErrorFail):
		return "false", true
	default:
		return "", false
	}
}

// quickFixRedundantDependsOn recomputes the redundant dependsOn set from sol (the
// same way lintRedundantDependsOn does) and builds the removal edits: remove the
// whole dependsOn entry when every listed dependency is redundant, or remove just
// the redundant list elements otherwise. It returns (nil,false) when nothing is
// actually redundant, so a stale finding never yields an empty edit.
func quickFixRedundantDependsOn(sol *solution.Solution, raw []byte, f *Finding, registry *provider.Registry) ([]refactor.TextEdit, bool) {
	name := resolverNameFromDependsOnLoc(f.Location)
	if name == "" {
		return nil, false
	}
	res, ok := sol.Spec.Resolvers[name]
	if !ok || res == nil || len(res.DependsOn) == 0 {
		return nil, false
	}

	// Recompute the redundant set with the SAME provider-aware lookup the lint
	// rule used to produce the finding (mirroring lintRedundantDependsOn). Using
	// the identical lookup -- rather than a nil/generic recompute -- guarantees
	// the redundant set matches exactly what the finding flagged, even when a
	// provider's custom ExtractDependencies diverges from generic extraction. The
	// guards below still decline the fix for anything not confirmed redundant, so
	// a dependency a provider considers required is never removed.
	var lookup resolver.DescriptorLookup
	if registry != nil {
		lookup = registry.DescriptorLookup()
	}
	inferred := resolver.ExtractInferredDependencies(res, lookup)
	inferredSet := make(map[string]bool, len(inferred))
	for _, dep := range inferred {
		inferredSet[dep] = true
	}

	redundantIdx := make([]int, 0, len(res.DependsOn))
	for i, dep := range res.DependsOn {
		if inferredSet[dep] {
			redundantIdx = append(redundantIdx, i)
		}
	}
	if len(redundantIdx) == 0 {
		return nil, false
	}

	nodes, err := refindex.NodeMap(raw)
	if err != nil {
		return nil, false
	}
	dependsOnPath, ok := nodeMapPath(nodes, f.Location)
	if !ok {
		return nil, false
	}

	// All redundant -> drop the whole dependsOn mapping entry (removing every
	// element would otherwise leave an empty list).
	if len(redundantIdx) == len(res.DependsOn) {
		edit, err := refactor.RemoveMappingEntry(raw, dependsOnPath)
		if err != nil {
			return nil, false
		}
		return []refactor.TextEdit{edit}, true
	}

	// Partial -> remove each redundant element by its index.
	edits := make([]refactor.TextEdit, 0, len(redundantIdx))
	for _, i := range redundantIdx {
		elemPath := fmt.Sprintf("%s[%d]", dependsOnPath, i)
		edit, err := refactor.RemoveSequenceElement(raw, elemPath)
		if err != nil {
			return nil, false
		}
		edits = append(edits, edit)
	}
	return edits, true
}

// resolverNameFromDependsOnLoc extracts the resolver name from a
// "resolvers.<name>.dependsOn" finding location (with or without a spec.
// prefix). Resolver names cannot contain dots, so the fixed prefix/suffix strip
// is unambiguous. It returns "" when loc is not that shape.
func resolverNameFromDependsOnLoc(loc string) string {
	loc = strings.TrimPrefix(loc, "spec.")
	if !strings.HasPrefix(loc, "resolvers.") || !strings.HasSuffix(loc, ".dependsOn") {
		return ""
	}
	name := strings.TrimSuffix(strings.TrimPrefix(loc, "resolvers."), ".dependsOn")
	if name == "" || strings.Contains(name, ".") {
		return ""
	}
	return name
}

// quickFixUnusedResolver removes the entire unused resolver mapping entry at the
// finding's "resolvers.<name>" location. When the target is the SOLE resolver,
// removing just its entry would leave the parent `resolvers:` key with a null
// value -- which the JSON-schema lint reports as a fresh error (want object, got
// null), turning a warning into an error. In that case the whole `resolvers:`
// mapping entry is removed instead, mirroring the all-vs-partial handling in the
// redundant-dependsOn fix.
func quickFixUnusedResolver(sol *solution.Solution, raw []byte, f *Finding) ([]refactor.TextEdit, bool) {
	nodes, err := refindex.NodeMap(raw)
	if err != nil {
		return nil, false
	}
	path, ok := nodeMapPath(nodes, f.Location)
	if !ok {
		return nil, false
	}

	// Removing the last remaining resolver would empty the parent map, so drop
	// the parent `resolvers:` entry instead of leaving a null value behind.
	if len(sol.Spec.Resolvers) <= 1 {
		if parent, ok := parentPath(path); ok {
			if edit, err := refactor.RemoveMappingEntry(raw, parent); err == nil {
				return []refactor.TextEdit{edit}, true
			}
		}
	}

	edit, err := refactor.RemoveMappingEntry(raw, path)
	if err != nil {
		return nil, false
	}
	return []refactor.TextEdit{edit}, true
}

// parentPath returns the path of the mapping that contains the entry at path by
// stripping the final ".<key>" segment (e.g. "spec.resolvers.foo" ->
// "spec.resolvers"). It returns ok=false when path has no parent segment.
func parentPath(path string) (string, bool) {
	i := strings.LastIndex(path, ".")
	if i <= 0 {
		return "", false
	}
	return path[:i], true
}
