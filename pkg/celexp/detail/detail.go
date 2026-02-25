// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package detail provides functions for building structured output
// representations of CEL extension functions.
package detail

import (
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// BuildFunctionList builds structured output for a list of CEL extension functions.
func BuildFunctionList(funcs celexp.ExtFunctionList) []map[string]any {
	output := make([]map[string]any, 0, len(funcs))
	for _, fn := range funcs {
		output = append(output, BuildFunctionDetail(&fn))
	}
	return output
}

// BuildFunctionDetail builds structured output for a single CEL extension function.
func BuildFunctionDetail(fn *celexp.ExtFunction) map[string]any {
	m := map[string]any{
		"name":   fn.Name,
		"custom": fn.Custom,
	}

	if fn.Description != "" {
		m["description"] = fn.Description
	}
	if len(fn.FunctionNames) > 0 {
		m["functionNames"] = fn.FunctionNames
	}
	if len(fn.Links) > 0 {
		m["links"] = fn.Links
	}
	if len(fn.Examples) > 0 {
		examples := make([]map[string]any, 0, len(fn.Examples))
		for _, ex := range fn.Examples {
			exMap := map[string]any{}
			if ex.Description != "" {
				exMap["description"] = ex.Description
			}
			if ex.Expression != "" {
				exMap["expression"] = ex.Expression
			}
			if len(ex.Links) > 0 {
				exMap["links"] = ex.Links
			}
			examples = append(examples, exMap)
		}
		m["examples"] = examples
	}

	return m
}
