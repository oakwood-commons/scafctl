// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// maxActionLiteralScanDepth bounds recursion into nested map/slice literals
// when scanning action inputs and call arguments for inline ValueRef maps.
const maxActionLiteralScanDepth = 32

// actionRefScanCtx holds solution-wide symbol sets needed to disambiguate
// resolver references from workflow action aliases, author-defined template
// functions, and undefined optional CEL references.
type actionRefScanCtx struct {
	hasSolution       bool
	definedResolvers  map[string]bool
	declaredFunctions map[string]bool
	actionAliases     map[string]bool
	lookup            resolver.DescriptorLookup
}

func newActionRefScanCtx(sol *solution.Solution, workflow *action.Workflow, lookup resolver.DescriptorLookup) actionRefScanCtx {
	ctx := actionRefScanCtx{
		definedResolvers:  make(map[string]bool),
		declaredFunctions: make(map[string]bool),
		actionAliases:     make(map[string]bool),
		lookup:            lookup,
	}
	if sol != nil {
		ctx.hasSolution = true
		for name := range sol.Spec.Resolvers {
			ctx.definedResolvers[name] = true
		}
		for name := range sol.Spec.Functions {
			ctx.declaredFunctions[name] = true
		}
	}
	// Collect aliases only from the supplied (potentially filtered) workflow so
	// aliases on unselected actions never shadow same-named resolvers, matching
	// action.BuildGraph(workflow).
	collectWorkflowAliases(workflow, ctx.actionAliases)
	return ctx
}

func collectWorkflowAliases(w *action.Workflow, aliases map[string]bool) {
	if w == nil {
		return
	}
	for _, act := range w.Actions {
		if act != nil && act.Alias != "" {
			aliases[act.Alias] = true
		}
	}
	for _, act := range w.Finally {
		if act != nil && act.Alias != "" {
			aliases[act.Alias] = true
		}
	}
}

// ResolversForWorkflow returns the ordered slice of resolvers required to
// execute workflow (its main actions plus finally actions) and effective state
// configuration, closed over transitive resolution and (unless disabled via
// opts) deferred-validation dependencies.
//
// When no resolver references are found (len(roots) == 0), an empty non-nil
// slice is returned so a zero-reference action executes zero resolvers rather
// than triggering FilterResolversWithDependencies' bare-invocation behavior.
func ResolversForWorkflow(
	sol *solution.Solution,
	workflow *action.Workflow,
	stateCfg *state.Config,
	lookup resolver.DescriptorLookup,
	opts ...FilterOption,
) []*resolver.Resolver {
	if sol == nil {
		return []*resolver.Resolver{}
	}

	roots := WorkflowResolverRefs(sol, workflow, stateCfg, lookup)
	if len(roots) == 0 {
		return []*resolver.Resolver{}
	}

	filtered := FilterResolversWithDependencies(sol.Spec.ResolversToSlice(), roots, lookup, opts...)
	if len(filtered) == 0 {
		return []*resolver.Resolver{}
	}
	return filtered
}

type actionRefCollector struct {
	hard           map[string]bool
	opt            map[string]bool
	wholeNamespace bool
}

func newActionRefCollector() *actionRefCollector {
	return &actionRefCollector{
		hard: make(map[string]bool),
		opt:  make(map[string]bool),
	}
}

// WorkflowResolverRefs returns the sorted, deduplicated root resolver names
// referenced across workflow.Actions, workflow.Finally, and stateCfg. When any
// expression or template accesses the resolver namespace as a whole, all
// resolvers defined on sol are returned conservatively.
//
// Invalid CEL syntax in action conditions or inputs is ignored here (matching
// resolver.ExtractDependencies) so expression errors continue to surface at
// their canonical workflow graph-build or execution stage.
func WorkflowResolverRefs(
	sol *solution.Solution,
	workflow *action.Workflow,
	stateCfg *state.Config,
	lookup resolver.DescriptorLookup,
) []string {
	scanCtx := newActionRefScanCtx(sol, workflow, lookup)
	col := newActionRefCollector()

	if workflow != nil {
		for name, act := range workflow.Actions {
			if act == nil {
				continue
			}
			_ = extractActionRefs(name, act, scanCtx, false, col)
		}
		for name, act := range workflow.Finally {
			if act == nil {
				continue
			}
			_ = extractActionRefs(name, act, scanCtx, false, col)
		}
	}

	for _, ref := range state.ExtractConfigResolverRefs(stateCfg) {
		col.hard[ref] = true
	}
	if state.ConfigReferencesWholeNamespace(stateCfg) {
		col.wholeNamespace = true
	}

	return finalizeActionRefs(scanCtx, col)
}

// ActionResolverRefs returns the sorted, deduplicated resolver names
// referenced directly by act across inputs, spec.calls args, spec.functions
// call sites, forEach, when, continueOnError, and retry.retryIf. Condition
// expressions are parsed strictly and return an error when malformed.
func ActionResolverRefs(
	sol *solution.Solution,
	actionName string,
	act *action.Action,
	lookup resolver.DescriptorLookup,
) ([]string, error) {
	if act == nil {
		return nil, nil
	}
	var wf *action.Workflow
	if sol != nil && sol.Spec.Workflow != nil {
		wf = sol.Spec.Workflow
		isFinallyTarget := false
		if _, ok := wf.Finally[actionName]; ok {
			isFinallyTarget = true
		} else {
			for _, fa := range wf.Finally {
				if fa != nil && fa.Alias == actionName {
					isFinallyTarget = true
					break
				}
			}
		}
		if isFinallyTarget {
			wf = &action.Workflow{Finally: wf.Finally}
		} else if filtered, err := action.FilterWorkflowActions(wf, []string{actionName}); err == nil {
			wf = filtered
		}
	}
	scanCtx := newActionRefScanCtx(sol, wf, lookup)
	col := newActionRefCollector()
	if err := extractActionRefs(actionName, act, scanCtx, true, col); err != nil {
		return nil, err
	}
	return finalizeActionRefs(scanCtx, col), nil
}

// NamedActionsResolverRefs looks up each action in actionNames across the
// solution's workflow.actions and workflow.finally sections (matching action
// names or aliases), validates condition syntax, and returns the sorted union
// of referenced resolver names.
//
// Returns an error when the solution defines no workflow, when an action name
// is not found, or when an action condition fails CEL parsing. When the named
// actions exist and reference zero resolvers, an empty slice and nil error are
// returned so callers can choose whether zero references is valid or an error.
func NamedActionsResolverRefs(
	sol *solution.Solution,
	actionNames []string,
	lookup resolver.DescriptorLookup,
) ([]string, error) {
	if sol == nil || !sol.Spec.HasWorkflow() {
		var solName string
		if sol != nil {
			solName = sol.Metadata.Name
		}
		return nil, fmt.Errorf("--action requires a solution with a workflow section, but %q has none", solName)
	}

	workflow := sol.Spec.Workflow
	aliasMap := make(map[string]string)
	for name, a := range workflow.Actions {
		if a != nil && a.Alias != "" {
			aliasMap[a.Alias] = name
		}
	}
	for name, a := range workflow.Finally {
		if a != nil && a.Alias != "" {
			aliasMap[a.Alias] = name
		}
	}

	col := newActionRefCollector()
	type resolvedTarget struct {
		canonical string
		act       *action.Action
		isMain    bool
	}
	targets := make([]resolvedTarget, 0, len(actionNames))
	var mainTargets []string

	for _, requested := range actionNames {
		canonical := requested
		if resolved, ok := aliasMap[requested]; ok {
			canonical = resolved
		}
		act, found := workflow.Actions[canonical]
		isMain := found
		if !found {
			act, found = workflow.Finally[canonical]
		}
		if !found || act == nil {
			var available []string
			for name := range workflow.Actions {
				available = append(available, name)
			}
			for name := range workflow.Finally {
				available = append(available, name)
			}
			sort.Strings(available)
			return nil, fmt.Errorf("action %q not found (available: %s)", requested, strings.Join(available, ", "))
		}
		targets = append(targets, resolvedTarget{canonical: canonical, act: act, isMain: isMain})
		if isMain {
			mainTargets = append(mainTargets, canonical)
		}
	}

	aliasWf := &action.Workflow{Finally: workflow.Finally}
	if len(mainTargets) > 0 {
		if filtered, err := action.FilterWorkflowActions(workflow, mainTargets); err == nil {
			aliasWf = filtered
		} else {
			aliasWf = workflow
		}
	}
	scanCtx := newActionRefScanCtx(sol, aliasWf, lookup)

	for _, t := range targets {
		if err := extractActionRefs(t.canonical, t.act, scanCtx, true, col); err != nil {
			return nil, err
		}
	}

	return finalizeActionRefs(scanCtx, col), nil
}

func finalizeActionRefs(scanCtx actionRefScanCtx, col *actionRefCollector) []string {
	if col.wholeNamespace && scanCtx.hasSolution {
		for name := range scanCtx.definedResolvers {
			col.hard[name] = true
		}
	}

	merged := make(map[string]bool)

	accept := func(name string, optionalOnly bool) {
		if name == "" || strings.HasPrefix(name, "__") {
			return
		}
		if scanCtx.declaredFunctions[name] && !scanCtx.definedResolvers[name] {
			return
		}
		if optionalOnly && scanCtx.hasSolution && !scanCtx.definedResolvers[name] {
			return
		}
		merged[name] = true
	}

	for name := range col.hard {
		accept(name, false)
	}
	for name := range col.opt {
		if !col.hard[name] {
			accept(name, true)
		}
	}

	if len(merged) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(merged))
	for name := range merged {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func extractActionRefs(
	actionName string,
	act *action.Action,
	scanCtx actionRefScanCtx,
	strictConditions bool,
	col *actionRefCollector,
) error {
	outerAliases := copyBoolMap(scanCtx.actionAliases)
	bodyAliases := copyBoolMap(scanCtx.actionAliases)

	if act.ForEach != nil {
		// forEach.in is evaluated before iteration begins, so item/index loop
		// aliases are not yet bound in its scope.
		if act.ForEach.In != nil {
			scanActionValueRef(act.ForEach.In, gotmpl.ResolverDepsInput{Aliases: outerAliases}, col)
		}
		if act.ForEach.Item != "" {
			bodyAliases[act.ForEach.Item] = true
		}
		if act.ForEach.Index != "" {
			bodyAliases[act.ForEach.Index] = true
		}
		if act.ForEach.ContinueOnError != nil && act.ForEach.ContinueOnError.Expr != nil {
			if err := scanActionCondition(string(*act.ForEach.ContinueOnError.Expr), "forEach.continueOnError", actionName, strictConditions, col); err != nil {
				return err
			}
		}
	}

	if act.When != nil && act.When.Expr != nil {
		if err := scanActionCondition(string(*act.When.Expr), "when", actionName, strictConditions, col); err != nil {
			return err
		}
	}

	if act.ContinueOnError != nil && act.ContinueOnError.Expr != nil {
		if err := scanActionCondition(string(*act.ContinueOnError.Expr), "continueOnError", actionName, strictConditions, col); err != nil {
			return err
		}
	}

	if act.Retry != nil && act.Retry.RetryIf != nil {
		if err := scanActionCondition(string(*act.Retry.RetryIf), "retryIf", actionName, strictConditions, col); err != nil {
			return err
		}
	}

	// spec.calls call-site arguments (call definitions themselves are isolated
	// to _.args.* and cannot reference solution resolvers directly).
	if len(act.Args) > 0 {
		argScan := gotmpl.ResolverDepsInput{Aliases: bodyAliases}
		for _, argRef := range act.Args {
			scanActionValueRef(argRef, argScan, col)
		}
	}

	if len(act.Inputs) > 0 {
		extractActionInputRefs(act.Provider, act.Inputs, bodyAliases, scanCtx.lookup, col)
	}

	return nil
}

func scanActionCondition(
	exprStr, fieldName, actionName string,
	strict bool,
	col *actionRefCollector,
) error {
	err := scanActionCELExpr(exprStr, col)
	if err != nil && strict {
		return fmt.Errorf("failed to parse %s condition for action %q: %w", fieldName, actionName, err)
	}
	return nil
}

func scanActionCELExpr(exprStr string, col *actionRefCollector) error {
	celExpr := celexp.Expression(exprStr)
	hardVars, optVars, err := celExpr.GetUnderscoreVariablesByOptionality(context.Background())
	if err != nil {
		return err
	}
	for _, v := range hardVars {
		col.hard[v] = true
	}
	for _, v := range optVars {
		col.opt[v] = true
	}
	if resolver.ExpressionReferencesWholeNamespace(exprStr) {
		col.wholeNamespace = true
	}
	return nil
}

func extractActionInputRefs(
	providerName string,
	inputs map[string]*spec.ValueRef,
	aliases map[string]bool,
	lookup resolver.DescriptorLookup,
	col *actionRefCollector,
) {
	// 1. Scan explicit ValueRef wrappers (rslvr, expr, tmpl, and nested
	// single-key ValueRef maps) across all inputs in resolver + alias scope.
	// Explicit tmpl: wrappers are evaluated by the spec layer before the
	// provider runs, so sibling data-key exclusions never apply to them, and
	// inert literal strings (e.g. message: "{{ .foo }}") are ignored here.
	plainScan := gotmpl.ResolverDepsInput{Aliases: aliases}
	for _, vr := range inputs {
		scanActionValueRef(vr, plainScan, col)
	}

	// 2. Scan raw literal string inputs for providers that evaluate them
	// directly at execution time.
	switch providerName {
	case "cel", "debug":
		if vr := inputs["expression"]; vr != nil {
			if exprStr, ok := vr.Literal.(string); ok {
				if exprStr != "" {
					_ = scanActionCELExpr(exprStr, col)
				}
			} else if vr.Resolver != nil || vr.Tmpl != nil || vr.Expr != nil {
				// Dynamically supplied CEL expression string cannot be parsed statically.
				col.wholeNamespace = true
			}
		}
		return
	case "solution":
		// Sub-solution inputs reference parent resolvers only through explicit
		// ValueRef wrappers (scanned in step 1); literal strings are inert paths
		// or child parameter values.
		return
	case gotmplprovider.ProviderName:
		// Step 1 already scanned all explicit ValueRef wrappers with hard/optional
		// CEL partitioning; scan only raw provider template strings here so
		// optional-only CEL refs on data/entries/forEach are not promoted to hard.
		// When data keys are dynamic, bare {{ .field }} accessors still fall back
		// to the base resolver context in GoTemplateProvider.buildTemplateData
		// whenever the dynamic data map omits that key, so retain matching defined
		// resolvers via col.opt.
		tmplScan := buildActionTmplScan(inputs, aliases)
		rawInputs := buildRawProviderInputs(inputs, tmplScan)
		hardDeps, candidateDeps := gotmplprovider.ExtractRawTemplateDependenciesWithCandidates(rawInputs)
		for _, dep := range hardDeps {
			col.hard[dep] = true
		}
		for _, dep := range candidateDeps {
			col.opt[dep] = true
		}
		if gotmplprovider.ReferencesWholeNamespace(rawInputs) {
			col.wholeNamespace = true
		}
		return
	case "file":
		if shouldScanFileOutputPath(inputs) {
			if vr := inputs["outputPath"]; vr != nil {
				if outTmpl, ok := vr.Literal.(string); ok {
					if outTmpl != "" {
						scanActionTemplate(outTmpl, plainScan, col)
					}
				} else if vr.Resolver != nil || vr.Expr != nil || vr.Tmpl != nil {
					col.wholeNamespace = true
				}
			}
		}
	}

	tmplScan := buildActionTmplScan(inputs, aliases)
	extractActionProviderDeps(providerName, inputs, tmplScan, lookup, col.hard)
}

func shouldScanFileOutputPath(inputs map[string]*spec.ValueRef) bool {
	opRef, exists := inputs["operation"]
	if !exists || opRef == nil {
		return false
	}
	if opRef.Resolver != nil || opRef.Expr != nil || opRef.Tmpl != nil || opRef.Secret != nil || opRef.Token != nil {
		return true
	}
	if opStr, ok := opRef.Literal.(string); ok {
		return opStr == "write-tree"
	}
	if m, ok := opRef.Literal.(map[string]any); ok && spec.IsValueRefMap(m) {
		return true
	}
	return false
}

func extractActionProviderDeps(
	providerName string,
	inputs map[string]*spec.ValueRef,
	tmplScan gotmpl.ResolverDepsInput,
	lookup resolver.DescriptorLookup,
	hard map[string]bool,
) bool {
	if providerName == "" || lookup == nil {
		return false
	}
	desc := lookup(providerName)
	if desc == nil || desc.ExtractDependencies == nil {
		return false
	}

	rawInputs := buildRawProviderInputs(inputs, tmplScan)
	providerDeps := desc.ExtractDependencies(rawInputs)
	if providerDeps == nil {
		return false
	}
	for _, dep := range providerDeps {
		hard[dep] = true
	}
	return true
}

func buildRawProviderInputs(inputs map[string]*spec.ValueRef, tmplScan gotmpl.ResolverDepsInput) map[string]any {
	rawInputs := make(map[string]any, len(inputs))
	for key, ref := range inputs {
		if ref == nil {
			continue
		}
		switch {
		case ref.Literal != nil:
			rawInputs[key] = ref.Literal
		case ref.Resolver != nil:
			rawInputs[key] = map[string]any{"rslvr": *ref.Resolver}
		case ref.Expr != nil:
			rawInputs[key] = map[string]any{"expr": string(*ref.Expr)}
		case ref.Tmpl != nil:
			rawInputs[key] = map[string]any{"tmpl": string(*ref.Tmpl)}
		case ref.Secret != nil:
			rawInputs[key] = map[string]any{"scrt": *ref.Secret}
		case ref.Token != nil:
			rawInputs[key] = map[string]any{"tkn": ref.Token.Handler}
		}
	}

	rawInputs[gotmpl.DepScanContextKey] = gotmpl.DepScanContext{
		HasDataInput:     tmplScan.HasDataInput,
		DataKeys:         tmplScan.DataKeys,
		DataKeysComplete: tmplScan.DataKeysComplete,
		Aliases:          tmplScan.Aliases,
	}
	return rawInputs
}

func buildActionTmplScan(inputs map[string]*spec.ValueRef, aliases map[string]bool) gotmpl.ResolverDepsInput {
	scan := gotmpl.ResolverDepsInput{
		Aliases: aliases,
	}
	if dataRef, ok := inputs["data"]; ok && dataRef != nil {
		scan.HasDataInput = true
		scan.DataKeys, scan.DataKeysComplete = actionDataScanKeys(dataRef)
	}
	return scan
}

func actionDataScanKeys(dataRef *spec.ValueRef) (map[string]bool, bool) {
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
		keys, known := celexp.Expression(string(*dataRef.Expr)).MapLiteralKeys(context.Background())
		if !known {
			return nil, false
		}
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		return set, true
	default:
		return nil, false
	}
}

func scanActionValueRef(
	vr *spec.ValueRef,
	tmplScan gotmpl.ResolverDepsInput,
	col *actionRefCollector,
) {
	if vr == nil {
		return
	}
	switch {
	case vr.Resolver != nil:
		addResolverName(*vr.Resolver, col.hard)
	case vr.Expr != nil:
		_ = scanActionCELExpr(string(*vr.Expr), col)
	case vr.Tmpl != nil:
		scanActionTemplate(string(*vr.Tmpl), tmplScan, col)
	case vr.Literal != nil:
		scanActionLiteralWrappersOnly(vr.Literal, tmplScan, col, 0)
	}
}

func scanActionLiteralWrappersOnly(
	val any,
	tmplScan gotmpl.ResolverDepsInput,
	col *actionRefCollector,
	depth int,
) {
	if depth > maxActionLiteralScanDepth || val == nil {
		return
	}
	switch v := val.(type) {
	case map[string]any:
		if scanInlineValueRefMap(v, tmplScan, col) {
			return
		}
		for _, child := range v {
			scanActionLiteralWrappersOnly(child, tmplScan, col, depth+1)
		}
	case []any:
		for _, item := range v {
			scanActionLiteralWrappersOnly(item, tmplScan, col, depth+1)
		}
	}
}

func scanInlineValueRefMap(
	v map[string]any,
	tmplScan gotmpl.ResolverDepsInput,
	col *actionRefCollector,
) bool {
	if !spec.IsValueRefMap(v) {
		return false
	}
	if rslvr, ok := v["rslvr"].(string); ok {
		addResolverName(rslvr, col.hard)
		return true
	}
	if expr, ok := v["expr"].(string); ok {
		_ = scanActionCELExpr(expr, col)
		return true
	}
	if tmpl, ok := v["tmpl"].(string); ok {
		scanActionTemplate(tmpl, tmplScan, col)
		return true
	}
	return true
}

func scanActionTemplate(tmplStr string, scan gotmpl.ResolverDepsInput, col *actionRefCollector) {
	scan.Template = tmplStr
	hardDeps, candidateDeps := gotmpl.ExtractResolverDepsWithCandidates(scan)
	for _, name := range hardDeps {
		col.hard[name] = true
	}
	for _, name := range candidateDeps {
		col.opt[name] = true
	}
	if gotmpl.ReferencesWholeResolverNamespace(scan) {
		col.wholeNamespace = true
	}
}

func addResolverName(raw string, hard map[string]bool) {
	if raw == "" {
		return
	}
	hard[raw] = true
}

func copyBoolMap(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return make(map[string]bool)
	}
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
