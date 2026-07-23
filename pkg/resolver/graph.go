// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// IsTransitiveDependency checks if candidateName is a direct or transitive dependency
// of the targetResolver within the solution's resolver map.
// This is useful for filtering resolver graphs to show only relevant dependencies.
func IsTransitiveDependency(resolvers map[string]*Resolver, targetResolver, candidateName string) bool {
	return isTransitiveDep(resolvers, targetResolver, candidateName, make(map[string]bool))
}

// isTransitiveDep is the recursive implementation with cycle protection.
func isTransitiveDep(resolvers map[string]*Resolver, targetResolver, candidateName string, visited map[string]bool) bool {
	if visited[targetResolver] {
		return false
	}
	visited[targetResolver] = true

	target, ok := resolvers[targetResolver]
	if !ok {
		return false
	}

	for _, dep := range target.DependsOn {
		if dep == candidateName {
			return true
		}
		if isTransitiveDep(resolvers, dep, candidateName, visited) {
			return true
		}
	}
	return false
}

// DescriptorLookup is a function that retrieves a provider descriptor by name.
// Used during dependency extraction to allow providers to participate in
// extracting dependencies from their inputs.
type DescriptorLookup func(providerName string) *provider.Descriptor

// ExtractDependencies extracts all resolver references from a resolver definition.
// If lookup is provided, it will use provider-specific ExtractDependencies functions
// when available. If lookup is nil, only generic extraction is performed.
// Explicit dependencies from DependsOn are always included and merged with auto-extracted dependencies.
func ExtractDependencies(r *Resolver, lookup DescriptorLookup) []string {
	return extractDependencies(r, lookup)
}

// ExtractInferredDependencies extracts only auto-inferred dependencies from value
// references (expr:, rslvr:, tmpl:) and when clauses, excluding explicit DependsOn entries.
// This is useful for detecting redundant dependsOn declarations.
func ExtractInferredDependencies(r *Resolver, lookup DescriptorLookup) []string {
	return extractInferredDependencies(r, lookup)
}

func extractInferredDependencies(r *Resolver, lookup DescriptorLookup) []string {
	deps := make(map[string]bool)

	// Extract from when condition
	if r.When != nil && r.When.Expr != nil {
		extractDepsFromExpression(string(*r.When.Expr), deps)
	}

	// Extract from resolve phase
	if r.Resolve != nil {
		extractDepsFromResolvePhase(r.Resolve, deps, lookup)
	}

	// Extract from transform phase (excluding self-refs)
	if r.Transform != nil {
		transformDeps := make(map[string]bool)
		extractDepsFromTransformPhase(r.Transform, transformDeps, lookup)
		for dep := range transformDeps {
			if dep != r.Name {
				deps[dep] = true
			}
		}
	}

	// Validate-phase references are intentionally NOT part of the resolution
	// dependency graph. Validation runs in a separate post-resolution phase
	// (two-phase execution): a cross-resolver validation rule is deferred and its
	// references must not create resolve-ordering edges (or cycles). Self-only
	// (inline) rules reference nothing but __self, so they contribute no foreign
	// deps either. See partitionValidatePhase for the deferred-validation machinery.

	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

func extractDependencies(r *Resolver, lookup DescriptorLookup) []string {
	deps := make(map[string]bool) // Use map to deduplicate

	// Include explicit dependencies from DependsOn field
	for _, dep := range r.DependsOn {
		if dep != "" {
			deps[dep] = true
		}
	}

	// Extract from when condition
	if r.When != nil && r.When.Expr != nil {
		extractDepsFromExpression(string(*r.When.Expr), deps)
	}

	// Extract from resolve phase
	if r.Resolve != nil {
		extractDepsFromResolvePhase(r.Resolve, deps, lookup)
	}

	// Extract from transform phase (self-refs collected separately).
	//
	// Using _.resolverName in transform/validate is always wrong — the resolver's
	// own value is NOT stored in the context map (_.resolverName) until after ALL
	// phases complete (see executor.go SetResult in the defer). The correct way to
	// reference the current value is __self, which is injected via
	// executeProviderWithSelf. This filter prevents a confusing "cycle detected"
	// error from the DAG builder; the real issue is caught by the
	// resolver-self-reference lint rule which gives an actionable suggestion.
	if r.Transform != nil {
		transformDeps := make(map[string]bool)
		extractDepsFromTransformPhase(r.Transform, transformDeps, lookup)
		for dep := range transformDeps {
			if dep != r.Name {
				deps[dep] = true
			}
		}
	}

	// Validate-phase references are intentionally NOT part of the resolution
	// dependency graph. Validation runs in a separate post-resolution phase
	// (two-phase execution): a cross-resolver validation rule is deferred and its
	// references (including refs that appear only inside a rule's message
	// template) must not create resolve-ordering edges or cycles. Self-only
	// (inline) rules reference nothing but __self. The deferred rules and their
	// reporting-graph edges are computed by partitionValidatePhase.

	// Convert to slice
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	return result
}

// extractStrictDependencies extracts only the "strict" resolver references from a
// resolver -- those a user stated directly and expects to fail fast on a typo:
// explicit dependsOn entries, CEL `_.name` references, and direct rslvr: ValueRefs
// (including rslvr/expr forms nested inside literal maps and arrays).
//
// Go-template forms are deliberately excluded: a bare {{ .field }} accessor, a
// tmpl: ValueRef, or the go-template "template" input are inferred on a
// best-effort basis and may legitimately reference local template context (a
// data-input key or a forEach alias) rather than a resolver. BuildPhases uses this
// set to keep unknown strict references (so genuine typos fail during graph
// construction) while dropping unknown template-inferred edges.
//
// The result is always a subset of ExtractDependencies for the same resolver.
func extractStrictDependencies(r *Resolver) map[string]bool {
	deps := make(map[string]bool)

	for _, dep := range r.DependsOn {
		if dep != "" {
			deps[dep] = true
		}
	}

	if r.When != nil && r.When.Expr != nil {
		extractHardDepsFromExpression(string(*r.When.Expr), deps)
	}

	extractStrictFromResolvePhase(r.Resolve, deps)
	extractStrictFromTransformPhase(r.Transform, deps)
	// The validate phase contributes no strict resolution edges: deferred
	// (cross-resolver) rules are excluded from the resolution DAG by design, and
	// inline (self-only) rules reference nothing but __self. A typo in a deferred
	// validation reference surfaces at the deferred-validation phase (fail-closed),
	// not during graph construction.

	// A resolver is never a strict dependency of itself.
	delete(deps, r.Name)
	return deps
}

// extractStrictFromResolvePhase collects strict references from a resolve phase.
func extractStrictFromResolvePhase(phase *ResolvePhase, deps map[string]bool) {
	if phase == nil {
		return
	}
	if phase.When != nil && phase.When.Expr != nil {
		extractHardDepsFromExpression(string(*phase.When.Expr), deps)
	}
	if phase.Until != nil && phase.Until.Expr != nil {
		extractHardDepsFromExpression(string(*phase.Until.Expr), deps)
	}
	for _, source := range phase.With {
		for _, arg := range source.Args {
			extractStrictFromValueRef(arg, deps)
		}
		if source.When != nil && source.When.Expr != nil {
			extractHardDepsFromExpression(string(*source.When.Expr), deps)
		}
		if source.ContinueOnError != nil && source.ContinueOnError.Expr != nil {
			extractHardDepsFromExpression(string(*source.ContinueOnError.Expr), deps)
		}
		if source.ForEach != nil && source.ForEach.In != nil {
			extractStrictFromValueRef(source.ForEach.In, deps)
		}
		extractStrictFromInputs(source.Inputs, deps)
	}
}

// extractStrictFromTransformPhase collects strict references from a transform phase.
func extractStrictFromTransformPhase(phase *TransformPhase, deps map[string]bool) {
	if phase == nil {
		return
	}
	if phase.When != nil && phase.When.Expr != nil {
		extractHardDepsFromExpression(string(*phase.When.Expr), deps)
	}
	for _, transform := range phase.With {
		for _, arg := range transform.Args {
			extractStrictFromValueRef(arg, deps)
		}
		if transform.When != nil && transform.When.Expr != nil {
			extractHardDepsFromExpression(string(*transform.When.Expr), deps)
		}
		if transform.ContinueOnError != nil && transform.ContinueOnError.Expr != nil {
			extractHardDepsFromExpression(string(*transform.ContinueOnError.Expr), deps)
		}
		if transform.ForEach != nil && transform.ForEach.In != nil {
			extractStrictFromValueRef(transform.ForEach.In, deps)
		}
		extractStrictFromInputs(transform.Inputs, deps)
	}
}

// extractStrictFromInputs collects strict references from a step's inputs map.
// The go-template "template" input is skipped because its {{ .field }} accessors
// are inferred best-effort, not strict references.
func extractStrictFromInputs(inputs map[string]*ValueRef, deps map[string]bool) {
	for key, input := range inputs {
		if key == "template" {
			continue
		}
		extractStrictFromValueRef(input, deps)
	}
}

// extractStrictFromValueRef collects only strict references from a ValueRef:
// direct rslvr references, hard CEL expressions, and rslvr/expr forms nested
// inside literal values. tmpl: ValueRefs, {{ .field }} accessors in literal
// strings, and optional CEL access (_.?name, _[?"name"]) are intentionally
// skipped -- optional access signals the value may be absent, so it must not
// create a load-blocking strict edge.
func extractStrictFromValueRef(ref *ValueRef, deps map[string]bool) {
	if ref == nil {
		return
	}
	switch {
	case ref.Resolver != nil:
		deps[*ref.Resolver] = true
	case ref.Expr != nil:
		extractHardDepsFromExpression(string(*ref.Expr), deps)
	case ref.Tmpl != nil:
		// Template ValueRef: inferred best-effort, not a strict reference.
	case ref.Literal != nil:
		extractStrictFromLiteral(ref.Literal, deps)
	}
}

// extractStrictFromLiteral recursively collects strict references from a literal
// value: rslvr keys, expr (CEL) keys, and hard CEL patterns in strings. tmpl
// keys, {{ .field }} template accessors, and optional CEL access are skipped.
func extractStrictFromLiteral(literal any, deps map[string]bool) {
	switch v := literal.(type) {
	case string:
		if strings.Contains(v, "_.") || strings.Contains(v, "_[") {
			extractHardDepsFromExpression(v, deps)
		}
	case map[string]any:
		isValueRef := false
		if rslvr, ok := v["rslvr"].(string); ok {
			deps[rslvr] = true
			isValueRef = true
		}
		if expr, ok := v["expr"].(string); ok {
			extractHardDepsFromExpression(expr, deps)
			isValueRef = true
		}
		if _, ok := v["tmpl"].(string); ok {
			// Template ValueRef: inferred best-effort, not strict.
			isValueRef = true
		}
		if isValueRef {
			return
		}
		for _, mapVal := range v {
			extractStrictFromLiteral(mapVal, deps)
		}
	case []any:
		for _, arrVal := range v {
			extractStrictFromLiteral(arrVal, deps)
		}
	}
}

// extractDepsFromExpression extracts resolver references from CEL expressions
// Uses the existing GetUnderscoreVariables() method from pkg/celexp/refs.go
func extractDepsFromExpression(expr string, deps map[string]bool) {
	// Use existing CEL expression parsing functionality
	celExpr := celexp.Expression(expr)

	// Extract all _.resolverName and _["resolverName"] references
	vars, err := celExpr.GetUnderscoreVariables(context.TODO())
	if err != nil {
		// If parsing fails, skip dependency extraction for this expression
		// This is a non-fatal error - the resolver may still be valid
		return
	}

	// Add all found variables to the deps map
	for _, v := range vars {
		deps[v] = true
	}
}

// extractHardDepsFromExpression extracts only HARD resolver references
// (_.name, _["name"]) from a CEL expression, ignoring optional access
// (_.?name, _[?"name"]). Optional access signals the author tolerates the
// referenced resolver being absent (typically paired with .orValue(...)), so it
// must not contribute a strict, load-blocking dependency edge. A resolver
// referenced with hard syntax anywhere in the expression is still returned even
// if it also appears with optional syntax.
func extractHardDepsFromExpression(expr string, deps map[string]bool) {
	celExpr := celexp.Expression(expr)
	hard, _, err := celExpr.GetUnderscoreVariablesByOptionality(context.TODO())
	if err != nil {
		// If parsing fails, skip dependency extraction for this expression.
		// This is a non-fatal error - the resolver may still be valid.
		return
	}
	for _, v := range hard {
		deps[v] = true
	}
}

// extractDepsFromValueRef extracts dependencies from a ValueRef
func extractDepsFromValueRef(ref *ValueRef, deps map[string]bool) {
	if ref == nil {
		return
	}

	// Direct resolver reference
	if ref.Resolver != nil {
		deps[*ref.Resolver] = true
		return
	}

	// Expression
	if ref.Expr != nil {
		extractDepsFromExpression(string(*ref.Expr), deps)
		return
	}

	// Template
	if ref.Tmpl != nil {
		extractDepsFromTemplate(string(*ref.Tmpl), deps)
		return
	}

	// Literal string - check if it contains CEL-like expressions (_.resolverName patterns)
	// This handles cases where provider inputs contain expressions as literal strings
	if ref.Literal != nil {
		extractDepsFromLiteral(ref.Literal, deps)
	}
}

// ExtractRefsFromValueRefs extracts all resolver names referenced by a set of
// ValueRef values. This is useful for determining which resolvers an action
// depends on based on its inputs map. The result is sorted for deterministic output.
func ExtractRefsFromValueRefs(inputs map[string]*ValueRef) []string {
	deps := make(map[string]bool)
	for _, ref := range inputs {
		extractDepsFromValueRef(ref, deps)
	}
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	sort.Strings(result)
	return result
}

// ExtractRefsFromValueRef extracts all resolver names referenced by a single
// ValueRef, accumulating them into the provided deps set. It handles direct
// resolver references, CEL expressions, Go templates, and nested literal values
// using the same AST-based logic the dependency graph uses. Passing a nil ref or
// deps map is a no-op (deps must be non-nil to collect results).
func ExtractRefsFromValueRef(ref *ValueRef, deps map[string]bool) {
	if deps == nil {
		return
	}
	extractDepsFromValueRef(ref, deps)
}

// ExtractOptionalRefsFromValueRef collects resolver names that a ValueRef
// references via optional CEL access (_.?name, _[?"name"]), accumulating them
// into the provided optional set. Optional access is a CEL-only concept, so
// direct rslvr: references and Go-template (tmpl:) accessors -- which are always
// hard -- contribute nothing. Within a single CEL expression, a name also
// accessed with hard syntax is excluded (hard access dominates). Passing a nil
// ref or optional map is a no-op (optional must be non-nil to collect results).
func ExtractOptionalRefsFromValueRef(ref *ValueRef, optional map[string]bool) {
	if optional == nil {
		return
	}
	extractOptionalFromValueRef(ref, optional)
}

func extractOptionalFromValueRef(ref *ValueRef, optional map[string]bool) {
	if ref == nil {
		return
	}
	switch {
	case ref.Expr != nil:
		addOptionalFromExpression(string(*ref.Expr), optional)
	case ref.Literal != nil:
		extractOptionalFromLiteral(ref.Literal, optional)
	}
}

// extractOptionalFromLiteral recursively collects optional CEL references from a
// literal value: expr (CEL) keys and CEL patterns in strings. rslvr keys, tmpl
// keys, and Go-template accessors carry no optional access and are skipped.
func extractOptionalFromLiteral(literal any, optional map[string]bool) {
	switch v := literal.(type) {
	case string:
		if strings.Contains(v, "_.") || strings.Contains(v, "_[") {
			addOptionalFromExpression(v, optional)
		}
	case map[string]any:
		isValueRef := false
		if expr, ok := v["expr"].(string); ok {
			addOptionalFromExpression(expr, optional)
			isValueRef = true
		}
		if _, ok := v["rslvr"].(string); ok {
			isValueRef = true
		}
		if _, ok := v["tmpl"].(string); ok {
			isValueRef = true
		}
		if isValueRef {
			return
		}
		for _, mapVal := range v {
			extractOptionalFromLiteral(mapVal, optional)
		}
	case []any:
		for _, arrVal := range v {
			extractOptionalFromLiteral(arrVal, optional)
		}
	}
}

// addOptionalFromExpression parses a CEL expression and adds its optional-only
// resolver references to the optional set. Parse failures are non-fatal and
// yield no references.
func addOptionalFromExpression(expr string, optional map[string]bool) {
	celExpr := celexp.Expression(expr)
	_, opt, err := celExpr.GetUnderscoreVariablesByOptionality(context.TODO())
	if err != nil {
		return
	}
	for _, v := range opt {
		optional[v] = true
	}
}

// extractDepsFromLiteral recursively extracts dependencies from literal values
// that may contain CEL expression strings or Go template syntax
func extractDepsFromLiteral(literal any, deps map[string]bool) {
	extractDepsFromLiteralWithExclusions(literal, deps, nil)
}

// extractDepsFromLiteralWithExclusions works like extractDepsFromLiteral but
// allows excluding certain names from being treated as resolver dependencies.
// This is used when a sibling "data" map provides template context variables
// that should not be interpreted as resolver references.
func extractDepsFromLiteralWithExclusions(literal any, deps, exclude map[string]bool) {
	switch v := literal.(type) {
	case string:
		// Check if the string contains CEL-like expressions (_.something or _["something"] patterns)
		if strings.Contains(v, "_.") || strings.Contains(v, "_[") {
			extractDepsFromExpression(v, deps)
		}
		// Check if the string contains Go template syntax ({{ and }})
		// This handles cases like go-template provider inputs with {{.resolverName}} patterns
		if strings.Contains(v, "{{") && strings.Contains(v, "}}") {
			extractDepsFromTemplateCtx(v, deps, scanCtxFromExclude(exclude))
		}
	case map[string]any:
		// Check if this map represents a nested ValueRef ({rslvr: x}, {expr: "..."}, {tmpl: "..."}).
		// This handles inputs like: env: {APP: {rslvr: appName}}
		// We check for ValueRef-like keys and extract deps, but do NOT
		// early-return — sibling fields may also contain references.
		isValueRef := false
		if rslvr, ok := v["rslvr"].(string); ok {
			if !exclude[rslvr] {
				deps[rslvr] = true
			}
			isValueRef = true
		}
		if expr, ok := v["expr"].(string); ok {
			extractDepsFromExpression(expr, deps)
			isValueRef = true
		}
		if tmpl, ok := v["tmpl"].(string); ok {
			extractDepsFromTemplateCtx(tmpl, deps, scanCtxFromExclude(exclude))
			isValueRef = true
		}
		if isValueRef {
			return
		}

		// Check for go-template provider pattern: if this map has a "template"
		// string and a "data" map, exclude data keys from template references.
		// This prevents false-positive resolver dependencies when the template
		// accesses variables provided by the data input (e.g., {{.config}}).
		var dataKeys map[string]bool
		if dataVal, ok := v["data"]; ok {
			dataKeys = literalDataKeys(dataVal)
		}
		// Recursively check map values, passing data keys as exclusions
		// for template strings in the same map
		for key, mapVal := range v {
			if key == "template" && dataKeys != nil {
				extractDepsFromLiteralWithExclusions(mapVal, deps, dataKeys)
			} else {
				extractDepsFromLiteralWithExclusions(mapVal, deps, exclude)
			}
		}
	case []any:
		// Recursively check array elements
		for _, arrVal := range v {
			extractDepsFromLiteralWithExclusions(arrVal, deps, exclude)
		}
	}
}

// tmplScanCtx carries the context needed to correctly disambiguate a
// {{ .field }} template accessor between a resolver reference and a local
// template-context binding (a data-input key or a forEach loop alias).
type tmplScanCtx struct {
	// hasData indicates the step supplies an explicit "data" input.
	hasData bool
	// dataKeys is the set of top-level keys the data input provides. Only
	// meaningful when dataKeysComplete is true.
	dataKeys map[string]bool
	// dataKeysComplete indicates dataKeys is an authoritative, complete key set.
	dataKeysComplete bool
	// aliases is the set of forEach item/index alias names bound locally.
	aliases map[string]bool
}

// depsInput converts the scan context into the input for the shared
// gotmpl.ExtractResolverDeps helper.
func (c tmplScanCtx) depsInput(tmplContent string) gotmpl.ResolverDepsInput {
	return gotmpl.ResolverDepsInput{
		Template:         tmplContent,
		HasDataInput:     c.hasData,
		DataKeys:         c.dataKeys,
		DataKeysComplete: c.dataKeysComplete,
		Aliases:          c.aliases,
	}
}

// scanCtxFromExclude bridges the legacy exclude-set model used by the literal
// extraction path into the richer scan context. A non-empty exclude set implies
// a known, complete data-key set.
func scanCtxFromExclude(exclude map[string]bool) tmplScanCtx {
	if len(exclude) == 0 {
		return tmplScanCtx{}
	}
	return tmplScanCtx{hasData: true, dataKeys: exclude, dataKeysComplete: true}
}

// extractDepsFromTemplate extracts resolver references from Go templates with no
// surrounding data context (bare {{ .field }} accessors are resolver deps).
func extractDepsFromTemplate(tmplContent string, deps map[string]bool) {
	extractDepsFromTemplateCtx(tmplContent, deps, tmplScanCtx{})
}

// extractDepsFromTemplateCtx extracts resolver references from a Go template,
// applying the context-aware disambiguation rules in gotmpl.ExtractResolverDeps.
func extractDepsFromTemplateCtx(tmplContent string, deps map[string]bool, scan tmplScanCtx) {
	for _, name := range gotmpl.ExtractResolverDeps(scan.depsInput(tmplContent)) {
		deps[name] = true
	}
}

// buildTmplScanCtx computes the template scan context for a set of provider
// inputs and an optional forEach clause. The data input's key set is derived
// from a literal map or a statically analysable CEL map-literal expression;
// dynamic data inputs (rslvr/tmpl or non-map-literal expressions) yield an
// incomplete key set, which causes bare {{ .field }} accessors to be treated as
// local data context rather than resolver dependencies.
func buildTmplScanCtx(inputs map[string]*ValueRef, forEach *ForEachClause) tmplScanCtx {
	var scan tmplScanCtx
	if forEach != nil {
		aliases := make(map[string]bool, 2)
		if forEach.Item != "" {
			aliases[forEach.Item] = true
		}
		if forEach.Index != "" {
			aliases[forEach.Index] = true
		}
		if len(aliases) > 0 {
			scan.aliases = aliases
		}
	}
	if dataRef, ok := inputs["data"]; ok && dataRef != nil {
		scan.hasData = true
		scan.dataKeys, scan.dataKeysComplete = valueRefDataScanKeys(dataRef)
	}
	return scan
}

// valueRefDataScanKeys returns the statically-known top-level key set of a data
// ValueRef and whether that set is complete. Literal maps and CEL map-literal
// expressions yield a complete key set; all other forms are dynamic (complete is
// false, keys nil).
func valueRefDataScanKeys(dataRef *ValueRef) (map[string]bool, bool) {
	switch {
	case dataRef.Literal != nil:
		m, ok := dataRef.Literal.(map[string]any)
		if !ok {
			return nil, false
		}
		keys := make(map[string]bool, len(m))
		for k := range m {
			keys[k] = true
		}
		return keys, true
	case dataRef.Expr != nil:
		keys, known := celexp.Expression(string(*dataRef.Expr)).MapLiteralKeys(context.TODO())
		if !known {
			return nil, false
		}
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		return set, true
	default:
		// rslvr / tmpl -> dynamic, keys not statically known.
		return nil, false
	}
}

// literalDataKeys returns the statically-known top-level key set of a data value
// encountered on the raw-literal extraction path, or nil when the keys cannot be
// determined. It recognises nested ValueRef shapes ({expr: ...}, {rslvr: ...},
// {tmpl: ...}) so their control keys are not mistaken for data keys.
func literalDataKeys(data any) map[string]bool {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	if expr, ok := m["expr"].(string); ok && len(m) == 1 {
		keys, known := celexp.Expression(expr).MapLiteralKeys(context.TODO())
		if !known {
			return nil
		}
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		return set
	}
	if _, ok := m["rslvr"]; ok {
		return nil
	}
	if _, ok := m["tmpl"]; ok {
		return nil
	}
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

// extractDepsFromProviderInputs attempts to use a provider's ExtractDependencies function
// to extract dependencies from inputs. Returns true if the provider handled the extraction,
// false if generic extraction should be used instead.
//
// The forEach clause (when present) contributes item/index alias names that must
// not be treated as resolver dependencies. Because a provider's
// ExtractDependencies function receives only the input map, the computed scan
// context (data keys + forEach aliases) is injected under the reserved
// gotmpl.DepScanContextKey so template-based providers can disambiguate
// {{ .field }} accessors.
func extractDepsFromProviderInputs(providerName string, inputs map[string]*ValueRef, forEach *ForEachClause, deps map[string]bool, lookup DescriptorLookup) bool {
	if lookup == nil {
		return false
	}

	desc := lookup(providerName)
	if desc == nil || desc.ExtractDependencies == nil {
		return false
	}

	// Convert ValueRef inputs to raw map for the provider's ExtractDependencies function
	rawInputs := make(map[string]any)
	for key, ref := range inputs {
		if ref == nil {
			continue
		}
		// Extract the actual value from the ValueRef
		switch {
		case ref.Literal != nil:
			rawInputs[key] = ref.Literal
		case ref.Resolver != nil:
			rawInputs[key] = map[string]any{"rslvr": *ref.Resolver}
		case ref.Expr != nil:
			rawInputs[key] = map[string]any{"expr": string(*ref.Expr)}
		case ref.Tmpl != nil:
			rawInputs[key] = map[string]any{"tmpl": string(*ref.Tmpl)}
		}
	}

	// Inject the template scan context so template-based providers can
	// disambiguate {{ .field }} accessors from resolver references.
	scan := buildTmplScanCtx(inputs, forEach)
	rawInputs[gotmpl.DepScanContextKey] = gotmpl.DepScanContext{
		HasDataInput:     scan.hasData,
		DataKeys:         scan.dataKeys,
		DataKeysComplete: scan.dataKeysComplete,
		Aliases:          scan.aliases,
	}

	// Call the provider's ExtractDependencies function.
	// A nil return signals that extraction failed (e.g. plugin RPC error)
	// and generic extraction should be used instead.
	providerDeps := desc.ExtractDependencies(rawInputs)
	if providerDeps == nil {
		return false
	}
	for _, dep := range providerDeps {
		deps[dep] = true
	}

	return true
}

// extractDepsFromResolvePhase extracts dependencies from a resolve phase
func extractDepsFromResolvePhase(phase *ResolvePhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from until condition
	if phase.Until != nil && phase.Until.Expr != nil {
		extractDepsFromExpression(string(*phase.Until.Expr), deps)
	}

	// Extract from each source
	for _, source := range phase.With {
		// Extract from call-site args (a call source references resolvers via args).
		for _, arg := range source.Args {
			extractDepsFromValueRef(arg, deps)
		}

		// Extract from when condition
		if source.When != nil && source.When.Expr != nil {
			extractDepsFromExpression(string(*source.When.Expr), deps)
		}

		// Extract from continueOnError condition
		if source.ContinueOnError != nil && source.ContinueOnError.Expr != nil {
			extractDepsFromExpression(string(*source.ContinueOnError.Expr), deps)
		}

		// Extract from forEach.In (if using forEach with custom source)
		if source.ForEach != nil && source.ForEach.In != nil {
			extractDepsFromValueRef(source.ForEach.In, deps)
		}

		// Try provider-specific extraction first
		if extractDepsFromProviderInputs(source.Provider, source.Inputs, source.ForEach, deps, lookup) {
			continue
		}

		// Fall back to generic extraction from inputs.
		// Compute the template scan context (data keys + forEach aliases) so that
		// template {{ .field }} accessors are correctly disambiguated from resolver
		// references.
		scan := buildTmplScanCtx(source.Inputs, source.ForEach)
		for key, input := range source.Inputs {
			if key == "template" {
				extractDepsFromValueRefTmpl(input, deps, scan)
			} else {
				extractDepsFromValueRef(input, deps)
			}
		}
	}
}

// extractDepsFromValueRefTmpl works like extractDepsFromValueRef but applies the
// template scan context (data keys + forEach aliases) when the ValueRef resolves
// to a Go template, so that {{ .field }} accessors are correctly disambiguated
// from resolver references.
func extractDepsFromValueRefTmpl(ref *ValueRef, deps map[string]bool, scan tmplScanCtx) {
	if ref == nil {
		return
	}

	// Direct resolver reference - always a dependency.
	if ref.Resolver != nil {
		deps[*ref.Resolver] = true
		return
	}

	// Expression - CEL uses _.resolverName, never data keys.
	if ref.Expr != nil {
		extractDepsFromExpression(string(*ref.Expr), deps)
		return
	}

	// Template - apply the scan context.
	if ref.Tmpl != nil {
		extractDepsFromTemplateCtx(string(*ref.Tmpl), deps, scan)
		return
	}

	// Literal string with CEL or template syntax.
	if ref.Literal != nil {
		if s, ok := ref.Literal.(string); ok {
			if strings.Contains(s, "_.") || strings.Contains(s, "_[") {
				extractDepsFromExpression(s, deps)
			}
			if strings.Contains(s, "{{") && strings.Contains(s, "}}") {
				extractDepsFromTemplateCtx(s, deps, scan)
			}
		}
	}
}

// extractDepsFromTransformPhase extracts dependencies from a transform phase
func extractDepsFromTransformPhase(phase *TransformPhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from each transform step
	for _, transform := range phase.With {
		// Extract from call-site args (a call transform references resolvers via args).
		for _, arg := range transform.Args {
			extractDepsFromValueRef(arg, deps)
		}

		// Extract from when condition
		if transform.When != nil && transform.When.Expr != nil {
			extractDepsFromExpression(string(*transform.When.Expr), deps)
		}

		// Extract from continueOnError condition
		if transform.ContinueOnError != nil && transform.ContinueOnError.Expr != nil {
			extractDepsFromExpression(string(*transform.ContinueOnError.Expr), deps)
		}

		// Extract from forEach.In (if using forEach with custom source)
		if transform.ForEach != nil && transform.ForEach.In != nil {
			extractDepsFromValueRef(transform.ForEach.In, deps)
		}

		// Try provider-specific extraction first
		if extractDepsFromProviderInputs(transform.Provider, transform.Inputs, transform.ForEach, deps, lookup) {
			continue
		}

		// Fall back to generic extraction from inputs
		scan := buildTmplScanCtx(transform.Inputs, transform.ForEach)
		for key, input := range transform.Inputs {
			if key == "template" {
				extractDepsFromValueRefTmpl(input, deps, scan)
			} else {
				extractDepsFromValueRef(input, deps)
			}
		}
	}
}

// extractDepsFromValidatePhase extracts dependencies from a validate phase.
//
// NOTE: these references are NOT resolution-graph edges. Validation runs in a
// separate post-resolution phase (see partitionValidatePhase). This helper is
// retained for callers that need the full validate-phase reference set (e.g.
// tooling and tests); the resolution-dependency extractors deliberately do not
// call it.
func extractDepsFromValidatePhase(phase *ValidatePhase, deps map[string]bool, lookup DescriptorLookup) {
	if phase == nil {
		return
	}

	// Extract from phase-level when condition
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), deps)
	}

	// Extract from each validation rule
	for i := range phase.With {
		extractDepsFromValidationRule(phase.With[i], deps, lookup)
	}
}

// extractDepsFromValidationRule extracts every resolver reference a single
// validation rule makes: call-site args, the rule-level when condition, provider
// inputs, and the custom message template. It uses the same provider-aware
// extraction the full-phase helper uses so a rule is classified identically
// regardless of provider.
func extractDepsFromValidationRule(v ProviderValidation, deps map[string]bool, lookup DescriptorLookup) {
	// Extract from call-site args (a call validation references resolvers via args).
	for _, arg := range v.Args {
		extractDepsFromValueRef(arg, deps)
	}

	// Extract from the rule-level when condition
	if v.When != nil && v.When.Expr != nil {
		extractDepsFromExpression(string(*v.When.Expr), deps)
	}

	// Try provider-specific extraction first
	if extractDepsFromProviderInputs(v.Provider, v.Inputs, nil, deps, lookup) {
		// Still extract from message even if the provider handled inputs
		extractDepsFromValueRef(v.Message, deps)
		return
	}

	// Fall back to generic extraction from inputs
	for _, input := range v.Inputs {
		extractDepsFromValueRef(input, deps)
	}

	// Extract from message
	extractDepsFromValueRef(v.Message, deps)
}

// ValidatePartition classifies a resolver's validation rules into those that run
// inline (during resolution, preserving fail-fast) and those deferred to the
// post-resolution validation phase.
//
// A rule is inline iff it references nothing but the owning resolver itself
// (self / __self). Any rule that references another resolver -- in its args,
// inputs, rule-level when, or message template -- is deferred and excluded from
// the resolution DAG. A phase-level validate.when that references a foreign
// resolver forces every rule in the block to defer (PhaseWhenDeferred).
type ValidatePartition struct {
	// InlineRules holds indices into Validate.With for rules that stay inline.
	InlineRules []int
	// DeferredRules holds indices into Validate.With for rules that defer.
	DeferredRules []int
	// DeferredRefs is the union of foreign resolver references contributed by the
	// deferred rules (and the phase-level when, when it forces deferral). These
	// are used only to order and report deferred validation results; they are
	// never resolution-graph edges.
	DeferredRefs map[string]bool
	// PhaseWhenDeferred indicates the phase-level validate.when referenced a
	// foreign resolver, forcing the whole block to defer.
	PhaseWhenDeferred bool
}

// HasDeferred reports whether the partition contains any deferred rules.
func (p ValidatePartition) HasDeferred() bool {
	return len(p.DeferredRules) > 0
}

// PartitionValidatePhase splits a resolver's validate phase into inline and
// deferred (cross-resolver) rule sets. It is the exported entry point used by
// tooling such as linters and embedders to reason about which validation rules
// run in the deferred phase. A nil resolver or nil validate phase yields an
// empty partition.
func PartitionValidatePhase(r *Resolver, lookup DescriptorLookup) ValidatePartition {
	return partitionValidatePhase(r, lookup)
}

// classifyValidationRule returns the set of foreign resolver references a single
// validation rule makes, excluding the owning resolver's own name (self) and the
// injected __self accessor (which is never a resolver reference). An empty result
// means the rule is inline-eligible.
func classifyValidationRule(v ProviderValidation, selfName string, lookup DescriptorLookup) map[string]bool {
	deps := make(map[string]bool)
	extractDepsFromValidationRule(v, deps, lookup)
	delete(deps, selfName)
	return deps
}

// partitionValidatePhase splits a resolver's validate phase into inline and
// deferred rule sets and collects the foreign references contributed by the
// deferred rules. A nil resolver or nil validate phase yields an empty partition.
func partitionValidatePhase(r *Resolver, lookup DescriptorLookup) ValidatePartition {
	if r == nil {
		return ValidatePartition{DeferredRefs: make(map[string]bool)}
	}
	return partitionValidatePhaseFor(r.Validate, r.Name, lookup)
}

// partitionValidatePhaseFor is the phase-based core of partitionValidatePhase. It
// operates directly on a validate phase and the owning resolver's name, so the
// executor can partition without holding the full *Resolver.
func partitionValidatePhaseFor(phase *ValidatePhase, selfName string, lookup DescriptorLookup) ValidatePartition {
	part := ValidatePartition{DeferredRefs: make(map[string]bool)}
	if phase == nil {
		return part
	}

	// A phase-level validate.when that references a foreign resolver forces the
	// entire block to defer (the guard cannot be evaluated until that resolver
	// has resolved).
	phaseWhen := make(map[string]bool)
	if phase.When != nil && phase.When.Expr != nil {
		extractDepsFromExpression(string(*phase.When.Expr), phaseWhen)
	}
	delete(phaseWhen, selfName)
	if len(phaseWhen) > 0 {
		part.PhaseWhenDeferred = true
		for ref := range phaseWhen {
			part.DeferredRefs[ref] = true
		}
	}

	for i := range phase.With {
		foreign := classifyValidationRule(phase.With[i], selfName, lookup)
		if part.PhaseWhenDeferred || len(foreign) > 0 {
			part.DeferredRules = append(part.DeferredRules, i)
			for ref := range foreign {
				part.DeferredRefs[ref] = true
			}
			continue
		}
		part.InlineRules = append(part.InlineRules, i)
	}
	return part
}

// TemplateAccessor identifies a root-level Go template accessor in a resolver's
// resolve or transform step that does not resolve to any known resolver after
// accounting for data-input keys and forEach aliases. These are likely typos:
// at runtime a bare {{ .field }} accessor to an unknown root renders empty
// rather than failing, so they are surfaced as advisory findings rather than
// hard errors.
type TemplateAccessor struct {
	// Resolver is the name of the resolver containing the accessor.
	Resolver string
	// Step identifies where the accessor was found ("resolve" or "transform").
	Step string
	// Name is the unresolved accessor root name.
	Name string
}

// UnresolvedTemplateAccessors scans the go-template "template" input of every
// resolve and transform step for root-level accessors that cannot resolve to any
// known resolver, data-input key, or forEach alias. A step whose data input has
// a dynamically-determined key set is skipped for bare {{ .field }} accessors
// (its root may be populated at runtime); {{ ._.name }} explicit resolver
// references are always checked. The returned accessors are likely typos and are
// intended to be surfaced as lint findings.
func UnresolvedTemplateAccessors(resolvers []*Resolver) []TemplateAccessor {
	known := make(map[string]bool, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			known[r.Name] = true
		}
	}

	var out []TemplateAccessor
	for _, r := range resolvers {
		if r == nil {
			continue
		}
		if r.Resolve != nil {
			for _, src := range r.Resolve.With {
				collectUnresolvedAccessors(r.Name, "resolve", src.Inputs, src.ForEach, known, &out)
			}
		}
		if r.Transform != nil {
			for _, step := range r.Transform.With {
				collectUnresolvedAccessors(r.Name, "transform", step.Inputs, step.ForEach, known, &out)
			}
		}
	}
	return out
}

// collectUnresolvedAccessors appends unresolved template accessors found in a
// single step's "template" input to out.
func collectUnresolvedAccessors(resolverName, step string, inputs map[string]*ValueRef, forEach *ForEachClause, known map[string]bool, out *[]TemplateAccessor) {
	tmpl := templateInputString(inputs)
	if tmpl == "" {
		return
	}

	scan := buildTmplScanCtx(inputs, forEach)
	in := scan.depsInput(tmpl)
	in.LeftDelim = literalStringInput(inputs, "leftDelim")
	in.RightDelim = literalStringInput(inputs, "rightDelim")

	seen := make(map[string]bool)
	for _, name := range gotmpl.ExtractResolverDeps(in) {
		if known[name] || seen[name] {
			continue
		}
		seen[name] = true
		*out = append(*out, TemplateAccessor{Resolver: resolverName, Step: step, Name: name})
	}
}

// templateInputString returns the inline template string from a step's
// "template" input (whether provided as a tmpl ValueRef or a literal string), or
// "" when the step has no inline template.
func templateInputString(inputs map[string]*ValueRef) string {
	ref, ok := inputs["template"]
	if !ok || ref == nil {
		return ""
	}
	if ref.Tmpl != nil {
		return string(*ref.Tmpl)
	}
	if ref.Literal != nil {
		if s, ok := ref.Literal.(string); ok {
			return s
		}
	}
	return ""
}

// literalStringInput returns the literal string value of the named input, or ""
// when the input is absent or not a literal string.
func literalStringInput(inputs map[string]*ValueRef, key string) string {
	ref, ok := inputs[key]
	if !ok || ref == nil || ref.Literal == nil {
		return ""
	}
	if s, ok := ref.Literal.(string); ok {
		return s
	}
	return ""
}

// GraphNode represents a resolver node in the dependency graph
type GraphNode struct {
	Name         string            `json:"id" yaml:"id" doc:"Resolver name" maxLength:"256" example:"api-data"`
	Type         Type              `json:"type" yaml:"type" doc:"Resolver type" maxLength:"64" example:"standard"`
	Phase        int               `json:"phase" yaml:"phase" doc:"Execution phase (1-based)" maximum:"100" example:"1"`
	Conditional  bool              `json:"conditional" yaml:"conditional" doc:"Whether resolver has conditional execution"`
	Dependencies []GraphDependency `json:"dependencies" yaml:"dependencies" doc:"List of dependencies" maxItems:"100"`
}

// GraphDependency represents a dependency edge
type GraphDependency struct {
	Resolver string `json:"resolver" yaml:"resolver" doc:"Target resolver name" maxLength:"256" example:"auth-token"`
	Field    string `json:"field" yaml:"field" doc:"Field name in reference" maxLength:"128" example:"value"`
}

// GraphEdge represents a directed edge
type GraphEdge struct {
	From  string `json:"from" yaml:"from" doc:"Source resolver name" maxLength:"256" example:"api-data"`
	To    string `json:"to" yaml:"to" doc:"Target resolver name" maxLength:"256" example:"auth-token"`
	Label string `json:"label" yaml:"label" doc:"Edge label" maxLength:"256" example:"depends_on"`
}

// GraphDiagrams contains pre-rendered diagram representations of the dependency graph.
type GraphDiagrams struct {
	ASCII   string `json:"ascii" yaml:"ascii" doc:"ASCII art representation of the graph" maxLength:"65536"`
	DOT     string `json:"dot" yaml:"dot" doc:"Graphviz DOT format representation" maxLength:"65536"`
	Mermaid string `json:"mermaid" yaml:"mermaid" doc:"Mermaid.js diagram representation" maxLength:"65536"`
}

// Graph represents the complete resolver dependency graph
type Graph struct {
	Nodes    []*GraphNode   `json:"nodes" yaml:"nodes" doc:"Graph nodes" maxItems:"1000"`
	Edges    []*GraphEdge   `json:"edges" yaml:"edges" doc:"Graph edges" maxItems:"10000"`
	Phases   []*PhaseInfo   `json:"phases" yaml:"phases" doc:"Phase information" maxItems:"100"`
	Stats    *GraphStats    `json:"stats" yaml:"stats" doc:"Graph statistics"`
	Diagrams *GraphDiagrams `json:"diagrams" yaml:"diagrams" doc:"Pre-rendered diagram representations"`
}

// PhaseInfo contains information about a phase
type PhaseInfo struct {
	Phase       int      `json:"phase" yaml:"phase" doc:"Phase number (1-based)" maximum:"100" example:"1"`
	Resolvers   []string `json:"resolvers" yaml:"resolvers" doc:"Resolver names in this phase" maxItems:"1000"`
	Parallelism int      `json:"parallelism" yaml:"parallelism" doc:"Number of resolvers that can execute in parallel" maximum:"1000" example:"4"`
}

// GraphStats contains graph statistics
type GraphStats struct {
	TotalResolvers  int      `json:"totalResolvers" yaml:"totalResolvers" doc:"Total number of resolvers" maximum:"10000" example:"20"`
	TotalPhases     int      `json:"totalPhases" yaml:"totalPhases" doc:"Total number of execution phases" maximum:"100" example:"3"`
	MaxParallelism  int      `json:"maxParallelism" yaml:"maxParallelism" doc:"Maximum parallelism across all phases" maximum:"1000" example:"4"`
	AvgDependencies float64  `json:"avgDependencies" yaml:"avgDependencies" doc:"Average number of dependencies per resolver"`
	CriticalPath    []string `json:"criticalPath" yaml:"criticalPath" doc:"Longest dependency chain in the graph" maxItems:"100"`
	CriticalDepth   int      `json:"criticalDepth" yaml:"criticalDepth" doc:"Length of the critical path" maximum:"100" example:"5"`
}

// BuildGraph creates a Graph from resolvers.
// If lookup is provided, provider-specific ExtractDependencies functions will be used
// when available for more accurate dependency detection.
func BuildGraph(resolvers []*Resolver, lookup DescriptorLookup) (*Graph, error) {
	// Build phases first
	buildResult, err := BuildPhases(resolvers, lookup)
	if err != nil {
		return nil, fmt.Errorf("build phases: %w", err)
	}
	phases := buildResult.Phases

	graph := &Graph{
		Nodes:  make([]*GraphNode, 0, len(resolvers)),
		Edges:  make([]*GraphEdge, 0),
		Phases: make([]*PhaseInfo, 0, len(phases)),
	}

	// Create nodes
	for _, phase := range phases {
		phaseInfo := &PhaseInfo{
			Phase:       phase.Phase,
			Resolvers:   make([]string, 0, len(phase.Resolvers)),
			Parallelism: len(phase.Resolvers),
		}

		for _, r := range phase.Resolvers {
			// Extract dependencies
			deps := extractDependencies(r, lookup)
			graphDeps := make([]GraphDependency, 0, len(deps))

			for _, dep := range deps {
				graphDeps = append(graphDeps, GraphDependency{
					Resolver: dep,
					Field:    dep,
				})

				// Create edge (from dependent to dependency)
				graph.Edges = append(graph.Edges, &GraphEdge{
					From:  r.Name,
					To:    dep,
					Label: dep,
				})
			}

			node := &GraphNode{
				Name:         r.Name,
				Type:         r.Type,
				Phase:        phase.Phase,
				Conditional:  r.When != nil,
				Dependencies: graphDeps,
			}

			graph.Nodes = append(graph.Nodes, node)
			phaseInfo.Resolvers = append(phaseInfo.Resolvers, r.Name)
		}

		graph.Phases = append(graph.Phases, phaseInfo)
	}

	// Calculate stats
	graph.Stats = calculateGraphStats(graph)

	return graph, nil
}

// calculateGraphStats computes graph statistics including the critical path
func calculateGraphStats(graph *Graph) *GraphStats {
	totalDeps := 0
	maxParallelism := 0

	for _, node := range graph.Nodes {
		totalDeps += len(node.Dependencies)
	}

	for _, phase := range graph.Phases {
		if phase.Parallelism > maxParallelism {
			maxParallelism = phase.Parallelism
		}
	}

	avgDeps := 0.0
	if len(graph.Nodes) > 0 {
		avgDeps = float64(totalDeps) / float64(len(graph.Nodes))
	}

	criticalPath := computeCriticalPath(graph)

	return &GraphStats{
		TotalResolvers:  len(graph.Nodes),
		TotalPhases:     len(graph.Phases),
		MaxParallelism:  maxParallelism,
		AvgDependencies: avgDeps,
		CriticalPath:    criticalPath,
		CriticalDepth:   len(criticalPath),
	}
}

// computeCriticalPath finds the longest dependency chain in the graph.
// It uses dynamic programming on the DAG to find the path with the most nodes.
func computeCriticalPath(graph *Graph) []string {
	if len(graph.Nodes) == 0 {
		return nil
	}

	// Build adjacency list (node -> dependents that depend on it)
	dependents := make(map[string][]string)
	for _, edge := range graph.Edges {
		// edge.From depends on edge.To, so edge.To feeds into edge.From
		dependents[edge.To] = append(dependents[edge.To], edge.From)
	}

	// Build dependency set per node for quick lookup
	depCount := make(map[string]int)
	for _, node := range graph.Nodes {
		depCount[node.Name] = len(node.Dependencies)
	}

	// DP: longest path ending at each node
	longest := make(map[string]int)
	parent := make(map[string]string)
	var bestNode string
	bestLen := 0

	// Process nodes in phase order (phase 1 first = roots)
	for _, phase := range graph.Phases {
		for _, name := range phase.Resolvers {
			node := graph.findNode(name)
			if node == nil {
				continue
			}

			myLen := 1
			myParent := ""

			// Check all dependencies (predecessors in the chain)
			// Tie-break alphabetically for deterministic output when paths are equal length
			for _, dep := range node.Dependencies {
				if l, ok := longest[dep.Resolver]; ok && (l+1 > myLen || (l+1 == myLen && dep.Resolver < myParent)) {
					myLen = l + 1
					myParent = dep.Resolver
				}
			}

			longest[name] = myLen
			if myParent != "" {
				parent[name] = myParent
			}

			if myLen > bestLen {
				bestLen = myLen
				bestNode = name
			}
		}
	}

	if bestLen == 0 {
		return nil
	}

	// Reconstruct path from best node back to root
	path := make([]string, 0, bestLen)
	for node := bestNode; node != ""; node = parent[node] {
		path = append(path, node)
	}

	// Reverse to get root -> leaf order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}

// findNode finds a node by name
func (g *Graph) findNode(name string) *GraphNode {
	for _, node := range g.Nodes {
		if node.Name == name {
			return node
		}
	}
	return nil
}

// RenderDOT generates GraphViz DOT format
func (g *Graph) RenderDOT(w io.Writer) error {
	fmt.Fprintln(w, "digraph Resolvers {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box, style=rounded];")
	fmt.Fprintln(w)

	// Phase subgraphs
	for _, phase := range g.Phases {
		fmt.Fprintf(w, "  subgraph cluster_phase_%d {\n", phase.Phase)
		fmt.Fprintf(w, "    label=\"Phase %d\";\n", phase.Phase)
		fmt.Fprintln(w, "    style=filled;")
		fmt.Fprintln(w, "    color=lightgrey;")
		fmt.Fprintln(w)

		// Nodes in this phase
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			color := getPhaseColor(phase.Phase)
			style := "rounded,filled"
			if node.Conditional {
				style = "rounded,dashed"
				color = "lightpink"
			}

			fmt.Fprintf(w, "    \"%s\" [fillcolor=%s, style=\"%s\"];\n",
				node.Name, color, style)
		}

		fmt.Fprintln(w, "  }")
		fmt.Fprintln(w)
	}

	// Edges
	fmt.Fprintln(w, "  // Dependencies")
	for _, edge := range g.Edges {
		style := ""
		fromNode := g.findNode(edge.From)
		if fromNode != nil && fromNode.Conditional {
			style = " [style=dashed]"
		}

		fmt.Fprintf(w, "  \"%s\" -> \"%s\" [label=\"%s\"]%s;\n",
			edge.From, edge.To, edge.Label, style)
	}

	fmt.Fprintln(w, "}")
	return nil
}

// getPhaseColor returns a color for a phase number
func getPhaseColor(phase int) string {
	colors := []string{"lightblue", "lightgreen", "lightyellow", "lightcoral", "lightcyan"}
	return colors[phase%len(colors)]
}

// RenderMermaid generates Mermaid diagram format
func (g *Graph) RenderMermaid(w io.Writer) error {
	fmt.Fprintln(w, "graph LR")

	// Phase subgraphs
	for _, phase := range g.Phases {
		fmt.Fprintf(w, "  subgraph Phase_%d[\"Phase %d\"]\n", phase.Phase, phase.Phase)
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			nodeStyle := resolverName
			if node.Conditional {
				nodeStyle = resolverName + ":::conditional"
			}
			fmt.Fprintf(w, "    %s[%s]\n", nodeStyle, resolverName)
		}
		fmt.Fprintln(w, "  end")
	}

	// Edges
	for _, edge := range g.Edges {
		fromNode := g.findNode(edge.From)
		arrow := "-->"
		if fromNode != nil && fromNode.Conditional {
			arrow = "-.."
		}
		fmt.Fprintf(w, "  %s %s|%s| %s\n", edge.From, arrow, edge.Label, edge.To)
	}

	// Styles
	fmt.Fprintln(w, "  classDef conditional stroke-dasharray: 5 5")
	return nil
}

// RenderASCII generates ASCII art representation
func (g *Graph) RenderASCII(w io.Writer) error {
	fmt.Fprintln(w, "Resolver Dependency Graph:")
	fmt.Fprintln(w)

	for _, phase := range g.Phases {
		fmt.Fprintf(w, "Phase %d:\n", phase.Phase)
		for _, resolverName := range phase.Resolvers {
			node := g.findNode(resolverName)
			if node == nil {
				continue
			}

			conditional := ""
			if node.Conditional {
				conditional = " [conditional]"
			}

			fmt.Fprintf(w, "  - %s%s\n", node.Name, conditional)
			if len(node.Dependencies) > 0 {
				fmt.Fprintln(w, "    depends on:")
				for _, dep := range node.Dependencies {
					fmt.Fprintf(w, "      * %s\n", dep.Resolver)
				}
			}
		}
		fmt.Fprintln(w)
	}

	// Stats
	fmt.Fprintln(w, "Statistics:")
	fmt.Fprintf(w, "  Total Resolvers: %d\n", g.Stats.TotalResolvers)
	fmt.Fprintf(w, "  Total Phases: %d\n", g.Stats.TotalPhases)
	fmt.Fprintf(w, "  Max Parallelism: %d\n", g.Stats.MaxParallelism)
	fmt.Fprintf(w, "  Avg Dependencies: %.2f\n", g.Stats.AvgDependencies)

	return nil
}

// RenderDiagrams pre-renders all diagram representations and stores them in
// the Diagrams field. This allows diagram strings to appear in JSON/YAML
// output without requiring an io.Writer at consumption time.
func (g *Graph) RenderDiagrams() error {
	g.Diagrams = &GraphDiagrams{}

	var buf bytes.Buffer

	if err := g.RenderASCII(&buf); err != nil {
		return fmt.Errorf("rendering ASCII diagram: %w", err)
	}
	g.Diagrams.ASCII = buf.String()

	buf.Reset()
	if err := g.RenderDOT(&buf); err != nil {
		return fmt.Errorf("rendering DOT diagram: %w", err)
	}
	g.Diagrams.DOT = buf.String()

	buf.Reset()
	if err := g.RenderMermaid(&buf); err != nil {
		return fmt.Errorf("rendering Mermaid diagram: %w", err)
	}
	g.Diagrams.Mermaid = buf.String()

	return nil
}
