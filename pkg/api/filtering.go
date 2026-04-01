// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// FilterItems applies a CEL filter expression to a slice of items.
// Each item is passed as the activation variable "item".
// Struct items are normalized to map[string]any (keyed by JSON tag names)
// so that CEL expressions like item.name work consistently.
// A cost limit is enforced to prevent DoS via expensive user-supplied expressions.
// Returns the filtered slice and any error from expression evaluation.
func FilterItems[T any](ctx context.Context, items []T, filter string) ([]T, error) {
	if filter == "" {
		return items, nil
	}

	// Determine once whether T is a struct type that needs normalization.
	// CEL's default type adapter cannot access Go struct fields — structs must
	// be converted to map[string]any keyed by JSON field names.
	var needsNormalize bool
	if len(items) > 0 {
		rt := reflect.TypeOf(items[0])
		if rt.Kind() == reflect.Struct {
			needsNormalize = true
		} else if rt.Kind() == reflect.Ptr && rt.Elem().Kind() == reflect.Struct {
			needsNormalize = true
		}
	}

	result := make([]T, 0, len(items))
	for _, item := range items {
		var celItem any = item
		if needsNormalize {
			m, err := structToMap(item)
			if err != nil {
				return nil, fmt.Errorf("normalizing item for CEL: %w", err)
			}
			celItem = m
		}

		additionalVars := map[string]any{
			"item": celItem,
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

// structToMap converts a struct to map[string]any using JSON round-trip
// so that field names match JSON tags (lowercase, omitted, etc.).
func structToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling struct: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling to map: %w", err)
	}
	return m, nil
}
