// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"gopkg.in/yaml.v3"
)

// ValueRef represents a value that can be literal, resolver reference, expression,
// template, host secret reference, or auth-handler token reference.
// It is used throughout the resolver and action systems to provide flexible value specification.
type ValueRef struct {
	Literal  any                         `json:"-" yaml:"-"`
	Resolver *string                     `json:"rslvr,omitempty" yaml:"rslvr,omitempty" doc:"Reference to another resolver by name" example:"environment" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must be a valid resolver name"`
	Expr     *celexp.Expression          `json:"expr,omitempty" yaml:"expr,omitempty" doc:"CEL expression to evaluate"`
	Tmpl     *gotmpl.GoTemplatingContent `json:"tmpl,omitempty" yaml:"tmpl,omitempty" doc:"Go template to execute"`
	Secret   *string                     `json:"scrt,omitempty" yaml:"scrt,omitempty" doc:"Secret name from the host secret store" maxLength:"255" example:"npm-token"`
	Token    *TokenRef                   `json:"tkn,omitempty" yaml:"tkn,omitempty" doc:"Auth token reference minted by an auth handler"`
}

// UnmarshalYAML implements custom YAML unmarshalling for ValueRef.
// It handles scalar values, sequences, and mappings appropriately.
func (v *ValueRef) UnmarshalYAML(node *yaml.Node) error {
	*v = ValueRef{}

	switch node.Kind {
	case yaml.MappingNode:
		var found []string
		var scrtNode, tknNode *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i].Value
			valNode := node.Content[i+1]
			switch k {
			case "scrt":
				scrtNode = valNode
				found = append(found, "scrt")
			case "tkn":
				tknNode = valNode
				found = append(found, "tkn")
			case "rslvr", "expr", "tmpl":
				// Legacy behavior for rslvr/expr/tmpl: null value on rslvr/expr/tmpl
				// was only rejected when typed, whereas scrt/tkn explicitly reject null.
				if valNode.Tag != "!!null" {
					found = append(found, k)
				}
			}
		}

		// If a map has extra non-ValueRef keys alongside a single ValueRef key
		// (e.g. {scrt: "x", extra: "y"} when len > 2 and scrt/tkn is present),
		// check count of recognized ValueRef keys first.
		if len(found) == 0 {
			var anyVal any
			if err := node.Decode(&anyVal); err != nil {
				return err
			}
			v.Literal = anyVal
			return nil
		}

		if len(found) != 1 {
			return fmt.Errorf("invalid value ref at line %d, column %d: expected exactly one of rslvr, expr, tmpl, scrt, or tkn, but found [%s]", node.Line, node.Column, strings.Join(found, ", "))
		}

		// Reject maps that mix scrt/tkn with arbitrary literal keys unless it is a pure single-key ValueRef.
		if (scrtNode != nil || tknNode != nil) && len(node.Content) != 2 {
			return fmt.Errorf("invalid value ref at line %d, column %d: %s cannot be combined with additional map keys", node.Line, node.Column, found[0])
		}

		if scrtNode != nil {
			if scrtNode.Kind != yaml.ScalarNode || scrtNode.Tag != "!!str" {
				return fmt.Errorf("invalid value ref at line %d, column %d: scrt must be a non-empty string", scrtNode.Line, scrtNode.Column)
			}
			if strings.TrimSpace(scrtNode.Value) == "" {
				return fmt.Errorf("invalid value ref at line %d, column %d: scrt must not be empty", scrtNode.Line, scrtNode.Column)
			}
			s := scrtNode.Value
			v.Secret = &s
			return nil
		}

		if tknNode != nil {
			var tr TokenRef
			if err := tr.UnmarshalYAML(tknNode); err != nil {
				return err
			}
			v.Token = &tr
			return nil
		}

		var raw struct {
			Resolver *string                     `yaml:"rslvr"`
			Expr     *celexp.Expression          `yaml:"expr"`
			Tmpl     *gotmpl.GoTemplatingContent `yaml:"tmpl"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}

		v.Resolver = raw.Resolver
		v.Expr = raw.Expr
		v.Tmpl = raw.Tmpl
		return nil

	case yaml.ScalarNode, yaml.SequenceNode, yaml.DocumentNode, yaml.AliasNode:
		// Handle scalar values, sequences, documents, and aliases as literals
		var anyVal any
		if err := node.Decode(&anyVal); err != nil {
			return err
		}
		v.Literal = anyVal
		return nil

	default:
		return fmt.Errorf("unsupported YAML node kind: %v", node.Kind)
	}
}

// MarshalYAML implements custom YAML marshalling for ValueRef.
// This is required to survive the deepCopySolution YAML round-trip used in compose/bundling.
// Without it, the Literal field (tagged yaml:"-") would be silently dropped during marshaling.
func (v ValueRef) MarshalYAML() (any, error) {
	if v.Literal != nil {
		return v.Literal, nil
	}
	if v.Resolver != nil {
		return map[string]any{"rslvr": *v.Resolver}, nil
	}
	if v.Expr != nil {
		return map[string]any{"expr": string(*v.Expr)}, nil
	}
	if v.Tmpl != nil {
		return map[string]any{"tmpl": string(*v.Tmpl)}, nil
	}
	if v.Secret != nil {
		return map[string]any{"scrt": *v.Secret}, nil
	}
	if v.Token != nil {
		encoded, err := v.Token.MarshalYAML()
		if err != nil {
			return nil, err
		}
		return map[string]any{"tkn": encoded}, nil
	}
	return nil, nil
}

// MarshalJSON implements custom JSON marshalling for ValueRef.
// Mirrors MarshalYAML to ensure consistent serialization across formats.
func (v ValueRef) MarshalJSON() ([]byte, error) {
	if v.Literal != nil {
		return json.Marshal(v.Literal)
	}
	if v.Resolver != nil {
		return json.Marshal(map[string]any{"rslvr": *v.Resolver})
	}
	if v.Expr != nil {
		return json.Marshal(map[string]any{"expr": string(*v.Expr)})
	}
	if v.Tmpl != nil {
		return json.Marshal(map[string]any{"tmpl": string(*v.Tmpl)})
	}
	if v.Secret != nil {
		return json.Marshal(map[string]any{"scrt": *v.Secret})
	}
	if v.Token != nil {
		return json.Marshal(map[string]any{"tkn": v.Token})
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements custom JSON unmarshalling for ValueRef.
// Mirrors UnmarshalYAML to ensure consistent deserialization across formats.
func (v *ValueRef) UnmarshalJSON(data []byte) error {
	// Clear all fields to avoid stale state when reusing a ValueRef instance.
	*v = ValueRef{}

	// Check if the data is a JSON object containing known keys.
	// We use json.RawMessage to detect key presence before typed decoding,
	// so that malformed refs like {"expr": {}} error instead of silently
	// becoming literals.
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil {
		var found []string
		for _, k := range []string{"rslvr", "expr", "tmpl", "scrt", "tkn"} {
			if _, ok := obj[k]; ok {
				found = append(found, k)
			}
		}

		if len(found) > 1 {
			return fmt.Errorf("invalid value ref: expected exactly one of rslvr, expr, tmpl, scrt, or tkn, but found [%s]", strings.Join(found, ", "))
		}

		if len(found) == 1 {
			key := found[0]
			if (key == "scrt" || key == "tkn") && len(obj) != 1 {
				return fmt.Errorf("invalid value ref: %s cannot be combined with additional object keys", key)
			}

			var raw struct {
				Resolver *string                     `json:"rslvr"`
				Expr     *celexp.Expression          `json:"expr"`
				Tmpl     *gotmpl.GoTemplatingContent `json:"tmpl"`
				Secret   *string                     `json:"scrt"`
				Token    *TokenRef                   `json:"tkn"`
			}
			if err := json.Unmarshal(data, &raw); err != nil {
				return fmt.Errorf("invalid value ref: %w", err)
			}
			// Reject null values for known keys (e.g. {"rslvr": null}).
			if raw.Resolver == nil && raw.Expr == nil && raw.Tmpl == nil && raw.Secret == nil && raw.Token == nil {
				return fmt.Errorf("invalid value ref: known key has null value")
			}
			if raw.Secret != nil && strings.TrimSpace(*raw.Secret) == "" {
				return fmt.Errorf("invalid value ref: scrt must not be empty")
			}
			v.Resolver = raw.Resolver
			v.Expr = raw.Expr
			v.Tmpl = raw.Tmpl
			v.Secret = raw.Secret
			v.Token = raw.Token
			return nil
		}

		// Object with no known keys — fall through to literal.
	}

	// Not an object, or object without known keys — treat as literal.
	var literal any
	if err := json.Unmarshal(data, &literal); err != nil {
		return err
	}
	v.Literal = literal
	return nil
}

// IterationContext holds the context for forEach iteration variables.
// It provides access to the current item and index during iteration.
type IterationContext struct {
	Item       any    `json:"-" yaml:"-" doc:"Current array element (__item)"`
	Index      int    `json:"-" yaml:"-" doc:"Current index (__index)"`
	ItemAlias  string `json:"-" yaml:"-" doc:"Custom name for item (if specified)"`
	IndexAlias string `json:"-" yaml:"-" doc:"Custom name for index (if specified)"`
}

// Resolve resolves the ValueRef to a concrete value.
// This is a convenience method that calls ResolveWithIterationContext with nil iteration context.
func (v *ValueRef) Resolve(ctx context.Context, resolverData map[string]any, self any) (any, error) {
	return v.ResolveWithIterationContext(ctx, resolverData, self, nil)
}

// ResolveWithIterationContext resolves the ValueRef with optional forEach iteration context.
// It handles literal values, resolver references, CEL expressions, Go templates,
// host secrets (scrt:), and auth-handler tokens (tkn:).
func (v *ValueRef) ResolveWithIterationContext(ctx context.Context, resolverData map[string]any, self any, iterCtx *IterationContext) (any, error) {
	// Handle nil ValueRef - return nil value
	if v == nil {
		return nil, nil
	}

	// Literal value — resolve any nested ValueRef patterns in maps/arrays
	if v.Literal != nil {
		return resolveNestedValueRefs(ctx, v.Literal, resolverData, self, iterCtx)
	}

	// Resolver reference
	if v.Resolver != nil {
		val, ok := resolverData[*v.Resolver]
		if !ok {
			return nil, fmt.Errorf("resolver %q not found", *v.Resolver)
		}
		return val, nil
	}

	// Host secret reference
	if v.Secret != nil {
		if strings.TrimSpace(*v.Secret) == "" {
			return nil, fmt.Errorf("secret name must not be empty")
		}
		credResolver := CredentialResolverFromContext(ctx)
		if credResolver == nil {
			return nil, fmt.Errorf("credential resolver not available in context for secret %q", *v.Secret)
		}
		val, err := credResolver.ResolveSecret(ctx, *v.Secret)
		if err != nil {
			return nil, err
		}
		if tracker := redact.FromContext(ctx); tracker != nil {
			tracker.Track(val)
		}
		return val, nil
	}

	// Auth handler token reference
	if v.Token != nil {
		if strings.TrimSpace(v.Token.Handler) == "" {
			return nil, fmt.Errorf("token handler must not be empty")
		}
		credResolver := CredentialResolverFromContext(ctx)
		if credResolver == nil {
			return nil, fmt.Errorf("credential resolver not available in context for token handler %q", v.Token.Handler)
		}
		val, err := credResolver.ResolveToken(ctx, *v.Token)
		if err != nil {
			return nil, err
		}
		if tracker := redact.FromContext(ctx); tracker != nil {
			tracker.Track(val)
		}
		return val, nil
	}

	// Build additional variables map for iteration context
	// All iteration variables go in additionalVars
	var additionalVars map[string]any
	if iterCtx != nil {
		additionalVars = make(map[string]any, 5)
		additionalVars[celexp.VarSelf] = self
		additionalVars[celexp.VarItem] = iterCtx.Item
		additionalVars[celexp.VarIndex] = iterCtx.Index
		if iterCtx.ItemAlias != "" {
			additionalVars[iterCtx.ItemAlias] = iterCtx.Item
		}
		if iterCtx.IndexAlias != "" {
			additionalVars[iterCtx.IndexAlias] = iterCtx.Index
		}
	} else if self != nil {
		additionalVars = map[string]any{celexp.VarSelf: self}
	}

	// CEL expression
	if v.Expr != nil {
		result, err := celexp.EvaluateExpression(ctx, string(*v.Expr), resolverData, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression: %w", err)
		}
		return result, nil
	}

	// Go template
	if v.Tmpl != nil {
		// Spread resolver data at the top level so templates can use
		// {{ .resolverName }} directly. The "._" alias is injected by
		// gotmpl.Execute to unify CEL (_.foo) and template (.foo) access.
		templateData := make(map[string]any, len(resolverData)+4)
		for k, val := range resolverData {
			templateData[k] = val
		}
		templateData["__self"] = self
		// Also add iteration variables at top level for template convenience
		if iterCtx != nil {
			templateData["__item"] = iterCtx.Item
			templateData["__index"] = iterCtx.Index
			if iterCtx.ItemAlias != "" {
				templateData[iterCtx.ItemAlias] = iterCtx.Item
			}
			if iterCtx.IndexAlias != "" {
				templateData[iterCtx.IndexAlias] = iterCtx.Index
			}
		}
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content:    string(*v.Tmpl),
			Data:       templateData,
			MissingKey: gotmpl.MissingKeyError,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute template: %w", err)
		}
		return result.Output, nil
	}

	return nil, fmt.Errorf("empty value reference")
}

// ContainsCredentialRef reports whether v is or contains any scrt: or tkn:
// ValueRef (including inline {scrt: ...} / {tkn: ...} maps nested in Literal).
func (v *ValueRef) ContainsCredentialRef() bool {
	if v == nil {
		return false
	}
	if v.Secret != nil || v.Token != nil {
		return true
	}
	if v.Literal != nil {
		return literalContainsCredentialRef(v.Literal, 0)
	}
	return false
}

func literalContainsCredentialRef(val any, depth int) bool {
	if depth > maxNestedValueRefDepth || val == nil {
		return false
	}
	switch typed := val.(type) {
	case map[string]any:
		if isValueRefMap(typed) {
			_, isScrt := typed["scrt"]
			_, isTkn := typed["tkn"]
			return isScrt || isTkn
		}
		for _, elem := range typed {
			if literalContainsCredentialRef(elem, depth+1) {
				return true
			}
		}
	case []any:
		for _, elem := range typed {
			if literalContainsCredentialRef(elem, depth+1) {
				return true
			}
		}
	}
	return false
}

// CredentialSummary returns a symbolic summary (e.g. "scrt:npm-token" or
// "tkn:github") suitable for dry-run and preview output without exposing secret values.
func (v *ValueRef) CredentialSummary() string {
	if v == nil {
		return ""
	}
	if v.Secret != nil {
		return "scrt:" + *v.Secret
	}
	if v.Token != nil {
		return v.Token.Summary()
	}
	if v.Expr != nil {
		return string(*v.Expr)
	}
	if v.Tmpl != nil {
		return string(*v.Tmpl)
	}
	if v.Literal != nil {
		var labels []string
		v.VisitCredentialRefs("", func(_ string, scrt *string, tkn *TokenRef, _ error) {
			if scrt != nil {
				labels = append(labels, "scrt:"+*scrt)
			}
			if tkn != nil && tkn.Handler != "" {
				labels = append(labels, "tkn:"+tkn.Handler)
			}
		})
		if len(labels) > 0 {
			sort.Strings(labels)
			return strings.Join(labels, ", ")
		}
	}
	return "(deferred)"
}

// ReferencedAuthHandlers returns all auth handler names referenced via tkn:
// on this ValueRef or nested inside Literal maps/arrays.
func (v *ValueRef) ReferencedAuthHandlers() []string {
	if v == nil {
		return nil
	}
	var handlers []string
	seen := make(map[string]struct{})
	v.VisitCredentialRefs("", func(_ string, _ *string, tkn *TokenRef, _ error) {
		if tkn != nil && strings.TrimSpace(tkn.Handler) != "" {
			if _, ok := seen[tkn.Handler]; !ok {
				seen[tkn.Handler] = struct{}{}
				handlers = append(handlers, tkn.Handler)
			}
		}
	})
	return handlers
}

// VisitCredentialRefs traverses top-level and nested scrt:/tkn: references on v,
// invoking fn with the logical path, parsed Secret/Token pointers, or parseErr
// if a nested {scrt: ...} or {tkn: ...} map is structurally invalid.
func (v *ValueRef) VisitCredentialRefs(basePath string, fn func(path string, scrt *string, tkn *TokenRef, parseErr error)) {
	if v == nil || fn == nil {
		return
	}
	if v.Secret != nil {
		fn(basePath, v.Secret, nil, nil)
		return
	}
	if v.Token != nil {
		fn(basePath, nil, v.Token, nil)
		return
	}
	if v.Literal != nil {
		visitLiteralCredentialRefs(v.Literal, basePath, 0, fn)
	}
}

func visitLiteralCredentialRefs(val any, curPath string, depth int, fn func(path string, scrt *string, tkn *TokenRef, parseErr error)) {
	if depth > maxNestedValueRefDepth || val == nil {
		return
	}
	switch typed := val.(type) {
	case map[string]any:
		if isValueRefMap(typed) {
			if _, isScrt := typed["scrt"]; isScrt {
				vr, err := parseNestedValueRef(typed)
				if err != nil {
					fn(curPath, nil, nil, err)
					return
				}
				fn(curPath, vr.Secret, nil, nil)
				return
			}
			if _, isTkn := typed["tkn"]; isTkn {
				vr, err := parseNestedValueRef(typed)
				if err != nil {
					fn(curPath, nil, nil, err)
					return
				}
				fn(curPath, nil, vr.Token, nil)
				return
			}
			return
		}
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			nextPath := k
			if curPath != "" {
				nextPath = curPath + "." + k
			}
			visitLiteralCredentialRefs(typed[k], nextPath, depth+1, fn)
		}
	case []any:
		for i, elem := range typed {
			nextPath := fmt.Sprintf("%s[%d]", curPath, i)
			visitLiteralCredentialRefs(elem, nextPath, depth+1, fn)
		}
	}
}

// ReferencedVariables returns the set of all variable names referenced by this ValueRef.
// It collects top-level variables, underscore-prefixed variables, and template references
// in a single pass. Use this instead of calling ReferencesVariable multiple times to
// avoid redundant expression parsing.
func (v *ValueRef) ReferencedVariables() map[string]struct{} {
	vars := make(map[string]struct{})
	if v == nil {
		return vars
	}

	if v.Expr != nil {
		if topLevel, err := v.Expr.RequiredVariables(context.TODO()); err == nil {
			for _, vn := range topLevel {
				vars[vn] = struct{}{}
			}
		}
		if underscore, err := v.Expr.GetUnderscoreVariables(context.TODO()); err == nil {
			for _, vn := range underscore {
				vars[vn] = struct{}{}
			}
		}
	}

	if v.Tmpl != nil {
		if refs, err := gotmpl.GetGoTemplateReferences(string(*v.Tmpl), "", ""); err == nil {
			for _, ref := range refs {
				// Skip scoped references inside {{ with }}/{{ range }} bodies
				if ref.Scoped {
					continue
				}

				path := strings.TrimPrefix(ref.Path, ".")
				// Extract the root variable name (before the first dot)
				if idx := strings.Index(path, "."); idx >= 0 {
					vars[path[:idx]] = struct{}{}
				} else {
					vars[path] = struct{}{}
				}
			}
		}
	}

	// Scan Literal maps/arrays for nested ValueRef patterns (expr/tmpl)
	if v.Literal != nil {
		collectNestedReferencedVariables(v.Literal, vars)
	}

	return vars
}

// ReferencesVariable checks if the ValueRef references a specific variable name.
// This is useful for detecting references to __actions, __self, __item, etc.
// For expressions, it checks both top-level variables (like __actions, __self)
// and underscore-prefixed variables (like _.environment).
func (v *ValueRef) ReferencesVariable(varName string) bool {
	vars := v.ReferencedVariables()
	_, ok := vars[varName]
	return ok
}

// valueRefKeys are the recognized keys for a ValueRef map pattern.
var valueRefKeys = map[string]struct{}{
	"rslvr": {},
	"expr":  {},
	"tmpl":  {},
	"scrt":  {},
	"tkn":   {},
}

// maxNestedValueRefDepth is the maximum recursion depth for resolving nested
// ValueRef patterns in literal maps/arrays. This prevents stack overflow from
// pathologically deep structures.
const maxNestedValueRefDepth = 32

// collectNestedReferencedVariables recursively scans a literal value for
// nested ValueRef patterns and collects their referenced variable names.
func collectNestedReferencedVariables(val any, vars map[string]struct{}) {
	collectNestedReferencedVariablesDepth(val, vars, 0)
}

func collectNestedReferencedVariablesDepth(val any, vars map[string]struct{}, depth int) {
	if depth > maxNestedValueRefDepth {
		return
	}
	switch v := val.(type) {
	case map[string]any:
		if isValueRefMap(v) {
			vr, err := parseNestedValueRef(v)
			if err != nil {
				return
			}
			for name := range vr.ReferencedVariables() {
				vars[name] = struct{}{}
			}
			return
		}
		for _, elem := range v {
			collectNestedReferencedVariablesDepth(elem, vars, depth+1)
		}
	case []any:
		for _, elem := range v {
			collectNestedReferencedVariablesDepth(elem, vars, depth+1)
		}
	}
}

// IsValueRefMap reports whether m is a map with exactly one key from
// {rslvr, expr, tmpl, scrt, tkn} and no other keys.
func IsValueRefMap(m map[string]any) bool {
	return isValueRefMap(m)
}

// ParseNestedValueRef converts a map matching IsValueRefMap into a ValueRef.
func ParseNestedValueRef(m map[string]any) (*ValueRef, error) {
	return parseNestedValueRef(m)
}

// isValueRefMap checks whether m is a map with exactly one key from
// {rslvr, expr, tmpl, scrt, tkn} and no other keys.
func isValueRefMap(m map[string]any) bool {
	if len(m) != 1 {
		return false
	}
	// Single-element map: extract the only key directly.
	for k := range m {
		_, ok := valueRefKeys[k]
		return ok
	}
	return false // unreachable, but satisfies the compiler
}

// parseNestedValueRef converts a map that passes isValueRefMap into a ValueRef.
func parseNestedValueRef(m map[string]any) (*ValueRef, error) {
	if v, ok := m["rslvr"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("rslvr value must be a string, got %T", v)
		}
		return &ValueRef{Resolver: &s}, nil
	}
	if v, ok := m["expr"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expr value must be a string, got %T", v)
		}
		e := celexp.Expression(s)
		return &ValueRef{Expr: &e}, nil
	}
	if v, ok := m["tmpl"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("tmpl value must be a string, got %T", v)
		}
		t := gotmpl.GoTemplatingContent(s)
		return &ValueRef{Tmpl: &t}, nil
	}
	if v, ok := m["scrt"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("scrt value must be a string, got %T", v)
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("scrt value must not be empty")
		}
		return &ValueRef{Secret: &s}, nil
	}
	if v, ok := m["tkn"]; ok {
		tr, err := parseTokenRefFromAny(v)
		if err != nil {
			return nil, err
		}
		return &ValueRef{Token: tr}, nil
	}
	return nil, fmt.Errorf("no recognized ValueRef key in map")
}

// resolveNestedValueRefs recursively walks a value and resolves any inline
// ValueRef patterns ({rslvr: X}, {expr: X}, {tmpl: X}, {scrt: X}, {tkn: X})
// found in maps or arrays. Non-ValueRef values are returned unchanged.
func resolveNestedValueRefs(ctx context.Context, val any, resolverData map[string]any, self any, iterCtx *IterationContext) (any, error) {
	return resolveNestedValueRefsDepth(ctx, val, resolverData, self, iterCtx, 0)
}

func resolveNestedValueRefsDepth(ctx context.Context, val any, resolverData map[string]any, self any, iterCtx *IterationContext, depth int) (any, error) {
	if depth > maxNestedValueRefDepth {
		return nil, fmt.Errorf("nested value ref exceeds maximum depth of %d", maxNestedValueRefDepth)
	}
	switch v := val.(type) {
	case map[string]any:
		// Check if this map itself is a ValueRef pattern
		if isValueRefMap(v) {
			vr, err := parseNestedValueRef(v)
			if err != nil {
				return nil, fmt.Errorf("invalid nested value ref: %w", err)
			}
			return vr.ResolveWithIterationContext(ctx, resolverData, self, iterCtx)
		}
		// Otherwise walk the map values
		result := make(map[string]any, len(v))
		for key, elem := range v {
			resolved, err := resolveNestedValueRefsDepth(ctx, elem, resolverData, self, iterCtx, depth+1)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			result[key] = resolved
		}
		return result, nil

	case []any:
		result := make([]any, len(v))
		for i, elem := range v {
			resolved, err := resolveNestedValueRefsDepth(ctx, elem, resolverData, self, iterCtx, depth+1)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			result[i] = resolved
		}
		return result, nil

	default:
		return val, nil
	}
}
