// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"gopkg.in/yaml.v3"
)

// Condition represents a conditional execution clause.
// It wraps a CEL expression that must evaluate to a boolean value.
//
// Supported YAML forms:
//   - Boolean literal:  when: true / when: false
//   - String shorthand: when: "_.environment == 'prod'"
//   - Explicit object:  when: { expr: "_.environment == 'prod'" }
type Condition struct {
	Expr *celexp.Expression `json:"expr" yaml:"expr" doc:"CEL expression that must evaluate to boolean" example:"_.environment == 'prod'"`
}

// UnmarshalYAML implements custom YAML unmarshalling for Condition.
// It supports boolean literals, string shorthand, and the explicit {expr: ...} form.
func (c *Condition) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		return c.unmarshalScalarYAML(node)
	case yaml.MappingNode:
		// Explicit object form: {expr: "..."}
		type conditionRaw Condition
		var raw conditionRaw
		if err := node.Decode(&raw); err != nil {
			return fmt.Errorf("invalid condition at line %d, column %d: %w", node.Line, node.Column, err)
		}
		*c = Condition(raw)
		return nil
	case yaml.AliasNode:
		return c.UnmarshalYAML(node.Alias)
	case yaml.DocumentNode, yaml.SequenceNode:
		return fmt.Errorf("invalid condition at line %d, column %d: expected boolean, string, or object {expr: \"...\"}, got unsupported node kind", node.Line, node.Column)
	default:
		return fmt.Errorf("invalid condition at line %d, column %d: expected boolean, string, or object {expr: \"...\"}, got unsupported node kind", node.Line, node.Column)
	}
}

func (c *Condition) unmarshalScalarYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!bool":
		// Boolean literal: when: true / false → stored as CEL expression "true"/"false"
		expr := celexp.Expression(node.Value)
		c.Expr = &expr
		return nil
	case "!!str":
		if node.Value == "" {
			return fmt.Errorf("invalid condition at line %d, column %d: empty string is not a valid CEL expression", node.Line, node.Column)
		}
		expr := celexp.Expression(node.Value)
		c.Expr = &expr
		return nil
	case "!!null":
		return fmt.Errorf("invalid condition at line %d, column %d: null is not a valid condition; use true, false, a CEL expression string, or {expr: \"...\"}", node.Line, node.Column)
	default:
		return fmt.Errorf("invalid condition at line %d, column %d: unsupported type %s; use true, false, a CEL expression string, or {expr: \"...\"}", node.Line, node.Column, node.Tag)
	}
}

// MarshalYAML implements custom YAML marshalling for Condition.
// If the expression is a literal "true" or "false", it marshals as a boolean.
// Otherwise it uses the string shorthand form for compact output.
func (c Condition) MarshalYAML() (any, error) {
	if c.Expr == nil {
		return nil, nil
	}
	s := string(*c.Expr)
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return s, nil
	}
}

// UnmarshalJSON implements custom JSON unmarshalling for Condition.
// It supports boolean literals, string shorthand, and the explicit {"expr": "..."} form.
func (c *Condition) UnmarshalJSON(data []byte) error {
	*c = Condition{}

	// Null → zero-value Condition (nil Expr), consistent with YAML null handling.
	if string(data) == "null" {
		return nil
	}

	// Try boolean
	var b bool
	if json.Unmarshal(data, &b) == nil {
		s := fmt.Sprintf("%t", b)
		expr := celexp.Expression(s)
		c.Expr = &expr
		return nil
	}

	// Try string
	var s string
	if json.Unmarshal(data, &s) == nil {
		if s == "" {
			return fmt.Errorf("invalid condition: empty string is not a valid CEL expression")
		}
		expr := celexp.Expression(s)
		c.Expr = &expr
		return nil
	}

	// Try object
	var obj struct {
		Expr *celexp.Expression `json:"expr"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("invalid condition: expected boolean, string, or object {\"expr\": \"...\"}: %w", err)
	}
	c.Expr = obj.Expr
	return nil
}

// MarshalJSON implements custom JSON marshalling for Condition.
func (c Condition) MarshalJSON() ([]byte, error) {
	if c.Expr == nil {
		return []byte("null"), nil
	}
	s := string(*c.Expr)
	switch s {
	case "true":
		return []byte("true"), nil
	case "false":
		return []byte("false"), nil
	default:
		return json.Marshal(s)
	}
}

// Evaluate evaluates the condition with the given resolver data.
// Returns true if the condition is met, false otherwise.
// Returns an error if evaluation fails or if the result is not a boolean.
func (c *Condition) Evaluate(ctx context.Context, resolverData map[string]any) (bool, error) {
	return c.EvaluateWithAdditionalVars(ctx, resolverData, nil)
}

// EvaluateWithAdditionalVars evaluates the condition with additional variables.
// This is useful for evaluating conditions with __self set to a specific value.
func (c *Condition) EvaluateWithAdditionalVars(ctx context.Context, resolverData, additionalVars map[string]any) (bool, error) {
	if c == nil || c.Expr == nil {
		return true, nil
	}

	result, err := celexp.EvaluateExpression(ctx, string(*c.Expr), resolverData, additionalVars)
	if err != nil {
		return false, fmt.Errorf("condition evaluation failed: %w", err)
	}

	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to boolean, got %T", result)
	}

	return boolResult, nil
}

// EvaluateWithSelf evaluates the condition with __self set to the provided value.
// This is used for until: conditions where __self should be the current resolved value.
func (c *Condition) EvaluateWithSelf(ctx context.Context, resolverData map[string]any, self any) (bool, error) {
	additionalVars := map[string]any{celexp.VarSelf: self}
	return c.EvaluateWithAdditionalVars(ctx, resolverData, additionalVars)
}
