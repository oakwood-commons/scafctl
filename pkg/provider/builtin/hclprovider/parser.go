// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ParseHCL parses raw HCL content and extracts structured block information.
// The filename parameter is used for error reporting and defaults to "input.tf".
func ParseHCL(src []byte, filename string) (map[string]any, error) {
	if filename == "" {
		filename = "input.tf"
	}

	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse HCL: %s", diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("unexpected body type: %T", file.Body)
	}

	variables := []any{}
	resources := []any{}
	data := []any{}
	modules := []any{}
	outputs := []any{}
	locals := map[string]any{}
	providers := []any{}
	terraform := map[string]any{}
	moved := []any{}
	imports := []any{}
	check := []any{}

	for _, block := range body.Blocks {
		switch block.Type {
		case "variable":
			variables = append(variables, extractVariable(block, src))
		case "resource":
			resources = append(resources, extractResource(block, src))
		case "data":
			data = append(data, extractDataSource(block, src))
		case "module":
			modules = append(modules, extractModule(block, src))
		case "output":
			outputs = append(outputs, extractOutput(block, src))
		case "locals":
			mergeLocals(locals, block, src)
		case "provider":
			providers = append(providers, extractProvider(block, src))
		case "terraform":
			terraform = extractTerraform(block, src)
		case "moved":
			moved = append(moved, extractMoved(block, src))
		case "import":
			imports = append(imports, extractImport(block, src))
		case "check":
			check = append(check, extractCheck(block, src))
		}
	}

	result := map[string]any{
		"variables": variables,
		"resources": resources,
		"data":      data,
		"modules":   modules,
		"outputs":   outputs,
		"locals":    locals,
		"providers": providers,
		"terraform": terraform,
		"moved":     moved,
		"import":    imports,
		"check":     check,
	}

	// Also extract top-level attributes (rare but valid in HCL)
	topAttrs := bodyAttributes(body, src)
	if len(topAttrs) > 0 {
		result["attributes"] = topAttrs
	}

	return result, nil
}

// extractVariable extracts a variable block: variable "name" { ... }
func extractVariable(block *hclsyntax.Block, src []byte) map[string]any {
	v := map[string]any{
		"name": labelOrEmpty(block.Labels, 0),
	}

	attrs := bodyAttributes(block.Body, src)

	if typ, ok := attrs["type"]; ok {
		v["type"] = typ
		delete(attrs, "type")
	}
	if def, ok := attrs["default"]; ok {
		v["default"] = def
		delete(attrs, "default")
	}
	if desc, ok := attrs["description"]; ok {
		v["description"] = desc
		delete(attrs, "description")
	}
	if sens, ok := attrs["sensitive"]; ok {
		v["sensitive"] = sens
		delete(attrs, "sensitive")
	}
	if nul, ok := attrs["nullable"]; ok {
		v["nullable"] = nul
		delete(attrs, "nullable")
	}

	// Extract validation sub-blocks
	var validations []any
	for _, sub := range block.Body.Blocks {
		if sub.Type == "validation" {
			valBlock := bodyAttributes(sub.Body, src)
			validations = append(validations, valBlock)
		}
	}
	if len(validations) > 0 {
		v["validation"] = validations
	}

	// Include any remaining attributes
	if len(attrs) > 0 {
		v["attributes"] = attrs
	}

	return v
}

// extractResource extracts: resource "type" "name" { ... }
func extractResource(block *hclsyntax.Block, src []byte) map[string]any {
	r := map[string]any{
		"type": labelOrEmpty(block.Labels, 0),
		"name": labelOrEmpty(block.Labels, 1),
	}

	attrs := bodyAttributes(block.Body, src)
	if len(attrs) > 0 {
		r["attributes"] = attrs
	}

	blocks := extractSubBlocks(block.Body, src)
	if len(blocks) > 0 {
		r["blocks"] = blocks
	}

	return r
}

// extractDataSource extracts: data "type" "name" { ... }
func extractDataSource(block *hclsyntax.Block, src []byte) map[string]any {
	d := map[string]any{
		"type": labelOrEmpty(block.Labels, 0),
		"name": labelOrEmpty(block.Labels, 1),
	}

	attrs := bodyAttributes(block.Body, src)
	if len(attrs) > 0 {
		d["attributes"] = attrs
	}

	blocks := extractSubBlocks(block.Body, src)
	if len(blocks) > 0 {
		d["blocks"] = blocks
	}

	return d
}

// extractModule extracts: module "name" { ... }
func extractModule(block *hclsyntax.Block, src []byte) map[string]any {
	m := map[string]any{
		"name": labelOrEmpty(block.Labels, 0),
	}

	attrs := bodyAttributes(block.Body, src)

	// Promote well-known module fields
	for _, key := range []string{"source", "version", "count", "for_each", "depends_on", "providers"} {
		if val, ok := attrs[key]; ok {
			m[key] = val
			delete(attrs, key)
		}
	}

	if len(attrs) > 0 {
		m["attributes"] = attrs
	}

	blocks := extractSubBlocks(block.Body, src)
	if len(blocks) > 0 {
		m["blocks"] = blocks
	}

	return m
}

// extractOutput extracts: output "name" { ... }
func extractOutput(block *hclsyntax.Block, src []byte) map[string]any {
	o := map[string]any{
		"name": labelOrEmpty(block.Labels, 0),
	}

	attrs := bodyAttributes(block.Body, src)

	for _, key := range []string{"value", "description", "sensitive", "depends_on"} {
		if val, ok := attrs[key]; ok {
			o[key] = val
			delete(attrs, key)
		}
	}

	if len(attrs) > 0 {
		o["attributes"] = attrs
	}

	return o
}

// mergeLocals merges attributes from a locals block into the target map.
func mergeLocals(target map[string]any, block *hclsyntax.Block, src []byte) {
	attrs := bodyAttributes(block.Body, src)
	for k, v := range attrs {
		target[k] = v
	}
}

// extractProvider extracts: provider "name" { ... }
func extractProvider(block *hclsyntax.Block, src []byte) map[string]any {
	p := map[string]any{
		"name": labelOrEmpty(block.Labels, 0),
	}

	attrs := bodyAttributes(block.Body, src)

	if alias, ok := attrs["alias"]; ok {
		p["alias"] = alias
		delete(attrs, "alias")
	}
	if region, ok := attrs["region"]; ok {
		p["region"] = region
		delete(attrs, "region")
	}

	if len(attrs) > 0 {
		p["attributes"] = attrs
	}

	return p
}

// extractTerraform extracts: terraform { ... }
func extractTerraform(block *hclsyntax.Block, src []byte) map[string]any {
	t := map[string]any{}

	attrs := bodyAttributes(block.Body, src)

	if rv, ok := attrs["required_version"]; ok {
		t["required_version"] = rv
		delete(attrs, "required_version")
	}

	// Extract sub-blocks: required_providers, backend, cloud
	for _, sub := range block.Body.Blocks {
		switch sub.Type {
		case "required_providers":
			rp := bodyAttributes(sub.Body, src)
			t["required_providers"] = rp
		case "backend":
			b := map[string]any{
				"type": labelOrEmpty(sub.Labels, 0),
			}
			backendAttrs := bodyAttributes(sub.Body, src)
			if len(backendAttrs) > 0 {
				b["attributes"] = backendAttrs
			}
			t["backend"] = b
		case "cloud":
			t["cloud"] = bodyAttributes(sub.Body, src)
		}
	}

	if len(attrs) > 0 {
		t["attributes"] = attrs
	}

	return t
}

// extractMoved extracts: moved { from = ..., to = ... }
func extractMoved(block *hclsyntax.Block, src []byte) map[string]any {
	attrs := bodyAttributes(block.Body, src)
	m := map[string]any{}
	if from, ok := attrs["from"]; ok {
		m["from"] = from
	}
	if to, ok := attrs["to"]; ok {
		m["to"] = to
	}
	return m
}

// extractImport extracts: import { to = ..., id = ..., provider = ... }
func extractImport(block *hclsyntax.Block, src []byte) map[string]any {
	attrs := bodyAttributes(block.Body, src)
	i := map[string]any{}
	for _, key := range []string{"to", "id", "provider", "for_each"} {
		if val, ok := attrs[key]; ok {
			i[key] = val
		}
	}
	return i
}

// extractCheck extracts: check "name" { ... }
func extractCheck(block *hclsyntax.Block, src []byte) map[string]any {
	c := map[string]any{
		"name": labelOrEmpty(block.Labels, 0),
	}

	// Extract data sub-blocks
	var dataBlocks []any
	var assertions []any
	for _, sub := range block.Body.Blocks {
		switch sub.Type {
		case "data":
			d := map[string]any{
				"type": labelOrEmpty(sub.Labels, 0),
				"name": labelOrEmpty(sub.Labels, 1),
			}
			dAttrs := bodyAttributes(sub.Body, src)
			if len(dAttrs) > 0 {
				d["attributes"] = dAttrs
			}
			dataBlocks = append(dataBlocks, d)
		case "assert":
			assertions = append(assertions, bodyAttributes(sub.Body, src))
		}
	}

	if len(dataBlocks) > 0 {
		c["data"] = dataBlocks
	}
	if len(assertions) > 0 {
		c["assertions"] = assertions
	}

	return c
}

// bodyAttributes extracts all attributes from an HCL body as a map[string]any.
// Expression values are evaluated to Go primitives when possible; complex
// expressions fall back to their source text.
func bodyAttributes(body *hclsyntax.Body, src []byte) map[string]any {
	result := map[string]any{}

	// Sort attributes by source position for deterministic output
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, attr := range body.Attributes {
		attrs = append(attrs, attr)
	}
	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].SrcRange.Start.Byte < attrs[j].SrcRange.Start.Byte
	})

	for _, attr := range attrs {
		result[attr.Name] = exprToValue(attr.Expr, src)
	}

	return result
}

// extractSubBlocks extracts non-well-known sub-blocks generically.
func extractSubBlocks(body *hclsyntax.Body, src []byte) []any {
	blocks := make([]any, 0, len(body.Blocks))
	for _, sub := range body.Blocks {
		b := map[string]any{
			"type": sub.Type,
		}
		if len(sub.Labels) > 0 {
			b["labels"] = toAnySlice(sub.Labels)
		}
		attrs := bodyAttributes(sub.Body, src)
		if len(attrs) > 0 {
			b["attributes"] = attrs
		}
		nested := extractSubBlocks(sub.Body, src)
		if len(nested) > 0 {
			b["blocks"] = nested
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// exprToValue converts an HCL expression to a Go value.
// Literal values (strings, numbers, bools, null) are returned as Go primitives.
// Complex expressions are returned as their source text.
func exprToValue(expr hclsyntax.Expression, src []byte) any {
	// Try to evaluate with no context — works for pure literals
	val, diags := expr.Value(nil)
	if !diags.HasErrors() {
		return ctyToGo(val)
	}

	// For non-literal expressions, return the raw source text
	rng := expr.Range()
	if rng.Start.Byte < len(src) && rng.End.Byte <= len(src) {
		raw := strings.TrimSpace(string(src[rng.Start.Byte:rng.End.Byte]))
		return raw
	}

	return nil
}

// ctyToGo converts a cty.Value to a native Go value.
func ctyToGo(val cty.Value) any {
	if val.IsNull() {
		return nil
	}
	if !val.IsKnown() {
		return "(unknown)"
	}

	ty := val.Type()
	switch {
	case ty == cty.String:
		return val.AsString()
	case ty == cty.Bool:
		return val.True()
	case ty == cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var items []any
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			items = append(items, ctyToGo(v))
		}
		return items
	case ty.IsMapType() || ty.IsObjectType():
		m := map[string]any{}
		for it := val.ElementIterator(); it.Next(); {
			k, v := it.Element()
			m[k.AsString()] = ctyToGo(v)
		}
		return m
	default:
		// Fallback: use GoBigFloat/GoString-style representation
		if ty == cty.Number {
			bf := val.AsBigFloat()
			if bf.IsInt() {
				i, _ := bf.Int(new(big.Int))
				return i.Int64()
			}
			f, _ := bf.Float64()
			return f
		}
		return val.GoString()
	}
}

// labelOrEmpty returns the label at the given index, or "" if out of range.
func labelOrEmpty(labels []string, idx int) string {
	if idx < len(labels) {
		return labels[idx]
	}
	return ""
}

// toAnySlice converts a []string to []any for JSON-compatible output.
func toAnySlice(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}
