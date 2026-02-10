// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// Condition represents a conditional execution clause.
// It wraps a CEL expression that must evaluate to a boolean value.
type Condition struct {
	Expr *celexp.Expression `json:"expr" yaml:"expr" doc:"CEL expression that must evaluate to boolean" example:"_.environment == 'prod'"`
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
