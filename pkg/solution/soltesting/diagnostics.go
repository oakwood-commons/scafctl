// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// DiagnoseExpression inspects a failing CEL expression and returns a diagnostic
// string showing sub-expression values. For comparison expressions (==, !=, <, >, >=, <=),
// it evaluates both sides independently and shows the mismatch.
// For non-comparison expressions, it returns the expression and its result.
// Falls back to "expected true, got false" for expressions too complex to decompose.
func DiagnoseExpression(ctx context.Context, expr string, celCtx map[string]any) string {
	// Try splitting on comparison operators (order matters: >= before >, etc.)
	for _, op := range []string{" == ", " != ", " >= ", " <= ", " > ", " < "} {
		parts := strings.SplitN(expr, op, 2)
		if len(parts) == 2 {
			lhs := strings.TrimSpace(parts[0])
			rhs := strings.TrimSpace(parts[1])

			lhsVal := evalSubExpr(ctx, lhs, celCtx)
			rhsVal := evalSubExpr(ctx, rhs, celCtx)

			opClean := strings.TrimSpace(op)
			return fmt.Sprintf("%s = %v\n  %s = %v\n  Comparison %q failed", lhs, lhsVal, rhs, rhsVal, opClean)
		}
	}

	// Try to evaluate the whole expression and show its result
	result, err := celexp.EvaluateExpression(ctx, expr, nil, celCtx)
	if err == nil {
		return fmt.Sprintf("expression %q = %v, expected true", expr, result)
	}

	return fallbackDiagnostic(expr)
}

// evalSubExpr evaluates a sub-expression in the given context.
func evalSubExpr(ctx context.Context, expr string, celCtx map[string]any) any {
	result, err := celexp.EvaluateExpression(ctx, expr, nil, celCtx)
	if err != nil {
		return fmt.Sprintf("<error: %s>", err)
	}
	return result
}

// fallbackDiagnostic returns a simple diagnostic for complex expressions.
func fallbackDiagnostic(expr string) string {
	return fmt.Sprintf("expression %q: expected true, got false", expr)
}
