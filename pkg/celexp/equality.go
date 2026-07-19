// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/cel-go/cel"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// operatorEquals is the CEL AST function name for the `==` operator.
const operatorEquals = "_==_"

// operatorIn is the CEL AST function name for the `in` operator.
const operatorIn = "@in"

// operatorLogicalAnd is the CEL AST function name for the `&&` operator.
const operatorLogicalAnd = "_&&_"

// operatorLogicalOr is the CEL AST function name for the `||` operator.
const operatorLogicalOr = "_||_"

// ParamEqualities performs a best-effort static analysis of a CEL expression to
// discover the literal values a prefixed variable is compared against. It walks
// the parsed AST looking for:
//
//   - equality checks: `_.action == "refresh"` -> {"action": ["refresh"]}
//   - membership checks: `_.action in ["a", "b"]` -> {"action": ["a", "b"]}
//
// Comparisons joined by `&&` / `||` are traversed recursively so a condition
// like `_.env == "prod" || _.env == "staging"` yields {"env": ["prod",
// "staging"]}. The variable name is returned WITHOUT the prefix (default "_.").
//
// This is deliberately best-effort and never returns an error: it is used to
// enrich a user-facing usage view with discovered allowed values. Any shape it
// cannot statically reduce (negations, inequalities, function calls, non-literal
// operands, etc.) simply contributes nothing. The bool result reports whether
// ANY equality/membership literals were discovered, so callers can distinguish
// "no constraints found" from "found some" and fall back gracefully.
//
// Only string, int, uint, double, and bool literals are collected; other
// constant kinds are ignored.
func (e Expression) ParamEqualities(ctx context.Context) (map[string][]any, bool) {
	return e.paramEqualitiesWithPrefix(ctx, "_.")
}

// paramEqualitiesWithPrefix is ParamEqualities with a configurable variable
// prefix (e.g. "_." for resolver values). If prefix is empty it defaults to "_.".
func (e Expression) paramEqualitiesWithPrefix(ctx context.Context, prefix string) (map[string][]any, bool) {
	if prefix == "" {
		prefix = "_."
	}

	env, err := newParseEnv(ctx)
	if err != nil {
		return nil, false
	}

	parsed, issues := env.Parse(string(e))
	if issues != nil && issues.Err() != nil {
		return nil, false
	}

	parsedExpr, err := cel.AstToParsedExpr(parsed)
	if err != nil {
		return nil, false
	}

	found := make(map[string][]any)
	collectEqualities(parsedExpr.GetExpr(), prefix, found)
	if len(found) == 0 {
		return nil, false
	}

	// Deduplicate and sort each value list for deterministic output.
	for name, vals := range found {
		found[name] = dedupeSortValues(vals)
	}
	return found, true
}

// newParseEnv builds a CEL environment for parse-only AST inspection, reusing
// the registered extension factory when available so custom functions parse.
func newParseEnv(ctx context.Context) (*cel.Env, error) {
	if factory := getEnvFactory(); factory != nil {
		return factory(ctx)
	}
	return cel.NewEnv(cel.OptionalTypes())
}

// collectEqualities recursively walks call nodes, accumulating discovered
// param -> literal-values into found.
func collectEqualities(expr *exprpb.Expr, prefix string, found map[string][]any) {
	call := expr.GetCallExpr()
	if call == nil {
		return
	}

	switch call.GetFunction() {
	case operatorLogicalAnd, operatorLogicalOr:
		for _, arg := range call.GetArgs() {
			collectEqualities(arg, prefix, found)
		}
	case operatorEquals:
		if name, val, ok := equalityPair(call.GetArgs(), prefix); ok {
			found[name] = append(found[name], val)
		}
	case operatorIn:
		if name, vals, ok := membershipValues(call.GetArgs(), prefix); ok {
			found[name] = append(found[name], vals...)
		}
	default:
		// Some other call (e.g. negation, function). Still traverse args in case
		// a boolean sub-expression contains equality checks.
		for _, arg := range call.GetArgs() {
			collectEqualities(arg, prefix, found)
		}
	}
}

// equalityPair matches `<prefixedVar> == <literal>` (in either operand order)
// and returns the unprefixed variable name and the literal value.
func equalityPair(args []*exprpb.Expr, prefix string) (string, any, bool) {
	if len(args) != 2 {
		return "", nil, false
	}
	// Try both orders: var == literal, literal == var.
	if name, ok := prefixedVarName(args[0], prefix); ok {
		if val, ok := constValue(args[1]); ok {
			return name, val, true
		}
	}
	if name, ok := prefixedVarName(args[1], prefix); ok {
		if val, ok := constValue(args[0]); ok {
			return name, val, true
		}
	}
	return "", nil, false
}

// membershipValues matches `<prefixedVar> in [<literals>...]` and returns the
// unprefixed variable name and the list literal's constant values.
func membershipValues(args []*exprpb.Expr, prefix string) (string, []any, bool) {
	if len(args) != 2 {
		return "", nil, false
	}
	name, ok := prefixedVarName(args[0], prefix)
	if !ok {
		return "", nil, false
	}
	listExpr := args[1].GetListExpr()
	if listExpr == nil {
		return "", nil, false
	}
	vals := make([]any, 0, len(listExpr.GetElements()))
	for _, el := range listExpr.GetElements() {
		val, ok := constValue(el)
		if !ok {
			// A non-literal element means the set is not statically known.
			return "", nil, false
		}
		vals = append(vals, val)
	}
	if len(vals) == 0 {
		return "", nil, false
	}
	return name, vals, true
}

// prefixedVarName returns the variable name (without prefix) when expr is a
// direct single-segment access on the prefix root. For prefix "_." it matches
// `_.<name>` and returns "<name>". Multi-segment chains (`_.a.b`) return false:
// a value compared against `_.a.b` is a value of the nested field, not of the
// top-level parameter `a`, so attributing it to `a` would be misleading. Since
// this feeds a best-effort allowed-values view, we prefer to omit rather than
// mislead.
func prefixedVarName(expr *exprpb.Expr, prefix string) (string, bool) {
	// The root identifier is the prefix without its trailing dot ("_." -> "_").
	root := prefix
	if len(root) > 0 && root[len(root)-1] == '.' {
		root = root[:len(root)-1]
	}

	sel := expr.GetSelectExpr()
	if sel == nil {
		return "", false
	}
	// The operand must be exactly the root identifier (single segment).
	id := sel.GetOperand().GetIdentExpr()
	if id == nil || id.GetName() != root {
		return "", false
	}
	return sel.GetField(), true
}

// constValue extracts a Go value from a CEL constant expression. Supports
// string, int, uint, double, and bool. Returns false for non-constants or
// unsupported kinds.
func constValue(expr *exprpb.Expr) (any, bool) {
	c := expr.GetConstExpr()
	if c == nil {
		return nil, false
	}
	switch k := c.GetConstantKind().(type) {
	case *exprpb.Constant_StringValue:
		return k.StringValue, true
	case *exprpb.Constant_Int64Value:
		return k.Int64Value, true
	case *exprpb.Constant_Uint64Value:
		return k.Uint64Value, true
	case *exprpb.Constant_DoubleValue:
		return k.DoubleValue, true
	case *exprpb.Constant_BoolValue:
		return k.BoolValue, true
	default:
		return nil, false
	}
}

// dedupeSortValues removes duplicate values (by formatted key) and sorts the
// result deterministically for stable output.
func dedupeSortValues(vals []any) []any {
	seen := make(map[string]struct{}, len(vals))
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		key := valueKey(v)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return valueKey(out[i]) < valueKey(out[j])
	})
	return out
}

// valueKey produces a stable string key for a literal value for dedupe/sort.
func valueKey(v any) string {
	if s, ok := v.(string); ok {
		return "s:" + s
	}
	return "x:" + fmt.Sprintf("%v", v)
}
