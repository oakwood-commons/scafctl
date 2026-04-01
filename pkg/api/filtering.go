// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// FilterItems applies a CEL filter expression to a slice of items.
// Each item is passed as the activation variable "item".
// A cost limit is enforced to prevent DoS via expensive user-supplied expressions.
// Returns the filtered slice and any error from expression evaluation.
func FilterItems[T any](ctx context.Context, items []T, filter string) ([]T, error) {
	if filter == "" {
		return items, nil
	}

	result := make([]T, 0, len(items))
	for _, item := range items {
		additionalVars := map[string]any{
			"item": item,
		}

		val, err := celexp.EvaluateExpression(ctx, filter, nil, additionalVars, celexp.WithCostLimit(settings.DefaultAPIFilterCostLimit))
		if err != nil {
			return nil, fmt.Errorf("evaluating filter expression: %w", err)
		}

		boolVal, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("filter expression must return a boolean, got %T", val)
		}
		if boolVal {
			result = append(result, item)
		}
	}
	return result, nil
}
