// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// defaultTransform runs the org-owned CEL expression against the fetched
// inventory body and validates that the result matches the canonical
// list<{name, url}> contract. Optional per-cluster OIDC fields (audience,
// authType, caData, consoleUrl, insecureSkipTls) flow through untouched when
// the transform emits them. The fetched JSON is available to the expression
// as the root variable "_".
func defaultTransform(ctx context.Context, cel string, body []byte) ([]Entry, error) {
	if cel == "" {
		return nil, fmt.Errorf("%w: no transform expression configured", ErrTransformShape)
	}

	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parsing inventory JSON: %w", err)
	}

	result, err := celexp.EvaluateExpression(ctx, cel, root, nil)
	if err != nil {
		return nil, fmt.Errorf("evaluating hostname transform: %w", err)
	}

	return coerceEntries(result)
}

// coerceEntries converts the CEL result into a validated []Entry. It uses a
// JSON round-trip so any CEL list-of-maps shape is accepted, then enforces the
// {name, url} contract structurally (the host stays shape-blind beyond this).
// Optional OIDC fields on Entry are preserved by the round-trip.
func coerceEntries(result any) ([]Entry, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("%w: result is not serializable", ErrTransformShape)
	}

	var entries []Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("%w: expected a list, got %s", ErrTransformShape, string(raw))
	}

	for i, e := range entries {
		if e.Name == "" || e.URL == "" {
			return nil, fmt.Errorf("%w: entry %d missing name or url", ErrTransformShape, i)
		}
	}
	return entries, nil
}
