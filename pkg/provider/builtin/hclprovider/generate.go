// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

// GenerateHCL converts a structured map representation into canonical HCL text.
// The input map follows the same schema as ParseHCL output: top-level keys are
// block types (variable, resource, module, output, locals, provider, terraform,
// moved, import, data, check) with arrays of block definitions.
func GenerateHCL(input map[string]any) (string, error) {
	f := hclwrite.NewEmptyFile()
	body := f.Body()

	// blockOrder lists the input map keys in desired output order.
	// The parse output uses plural keys (variables, resources, …), so we
	// map each input key to the singular HCL block type for code generation.
	type blockEntry struct {
		inputKey  string // key in the input map (parse output schema)
		blockType string // HCL block type name (singular)
	}
	blockOrder := []blockEntry{
		{"terraform", "terraform"},
		{"variables", "variable"},
		{"locals", "locals"},
		{"data", "data"},
		{"resources", "resource"},
		{"modules", "module"},
		{"outputs", "output"},
		{"providers", "provider"},
		{"moved", "moved"},
		{"import", "import"},
		{"check", "check"},
	}

	needsNewline := false
	for _, entry := range blockOrder {
		val, ok := input[entry.inputKey]
		if !ok {
			continue
		}

		switch entry.inputKey {
		case "terraform":
			m, ok := val.(map[string]any)
			if !ok || len(m) == 0 {
				continue
			}
			if needsNewline {
				body.AppendNewline()
			}
			generateTerraformBlock(body, m)
			needsNewline = true

		case "locals":
			m, ok := val.(map[string]any)
			if !ok || len(m) == 0 {
				continue
			}
			if needsNewline {
				body.AppendNewline()
			}
			generateLocalsBlock(body, m)
			needsNewline = true

		default:
			items, ok := val.([]any)
			if !ok || len(items) == 0 {
				continue
			}
			for _, item := range items {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if needsNewline {
					body.AppendNewline()
				}
				if err := generateBlock(body, entry.blockType, itemMap); err != nil {
					return "", fmt.Errorf("generating %s block: %w", entry.blockType, err)
				}
				needsNewline = true
			}
		}
	}

	return string(f.Bytes()), nil
}

// generateBlock creates a single HCL block from a map definition.
func generateBlock(body *hclwrite.Body, blockType string, def map[string]any) error {
	labels := labelsForBlock(blockType, def)
	block := body.AppendNewBlock(blockType, labels)
	blockBody := block.Body()

	// Get the attribute keys we should write, excluding label keys and sub-block keys.
	skipKeys := labelKeySet(blockType)
	skipKeys["blocks"] = true
	skipKeys["attributes"] = true
	skipKeys["validation"] = true
	skipKeys["assertions"] = true
	skipKeys["data"] = true // for check blocks

	// Write promoted attributes first (well-known fields).
	promoted := promotedKeysForBlock(blockType)
	for _, key := range promoted {
		if v, ok := def[key]; ok {
			writeAttribute(blockBody, key, v)
		}
	}

	// Write remaining attributes from the "attributes" map.
	if attrs, ok := def["attributes"].(map[string]any); ok {
		keys := sortedKeys(attrs)
		for _, k := range keys {
			writeAttribute(blockBody, k, attrs[k])
		}
	}

	// Write validation sub-blocks (for variable blocks).
	if validations, ok := def["validation"].([]any); ok {
		for _, v := range validations {
			if vm, ok := v.(map[string]any); ok {
				valBlock := blockBody.AppendNewBlock("validation", nil)
				valBody := valBlock.Body()
				keys := sortedKeys(vm)
				for _, k := range keys {
					writeAttribute(valBody, k, vm[k])
				}
			}
		}
	}

	// Write assertions sub-blocks (for check blocks).
	if assertions, ok := def["assertions"].([]any); ok {
		for _, a := range assertions {
			if am, ok := a.(map[string]any); ok {
				aBlock := blockBody.AppendNewBlock("assert", nil)
				aBody := aBlock.Body()
				keys := sortedKeys(am)
				for _, k := range keys {
					writeAttribute(aBody, k, am[k])
				}
			}
		}
	}

	// Write data sub-blocks (for check blocks).
	if dataBlocks, ok := def["data"].([]any); ok {
		for _, d := range dataBlocks {
			if dm, ok := d.(map[string]any); ok {
				dataLabels := []string{}
				if t, ok := dm["type"].(string); ok {
					dataLabels = append(dataLabels, t)
				}
				if n, ok := dm["name"].(string); ok {
					dataLabels = append(dataLabels, n)
				}
				dBlock := blockBody.AppendNewBlock("data", dataLabels)
				if attrs, ok := dm["attributes"].(map[string]any); ok {
					dBody := dBlock.Body()
					keys := sortedKeys(attrs)
					for _, k := range keys {
						writeAttribute(dBody, k, attrs[k])
					}
				}
			}
		}
	}

	// Write generic sub-blocks from "blocks" array.
	if blocks, ok := def["blocks"].([]any); ok {
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok {
				bType, _ := bm["type"].(string)
				var bLabels []string
				if ls, ok := bm["labels"].([]any); ok {
					for _, l := range ls {
						if s, ok := l.(string); ok {
							bLabels = append(bLabels, s)
						}
					}
				}
				sub := blockBody.AppendNewBlock(bType, bLabels)
				subBody := sub.Body()
				if attrs, ok := bm["attributes"].(map[string]any); ok {
					keys := sortedKeys(attrs)
					for _, k := range keys {
						writeAttribute(subBody, k, attrs[k])
					}
				}
				// Recurse into nested blocks.
				if nested, ok := bm["blocks"].([]any); ok {
					for _, nb := range nested {
						if nbm, ok := nb.(map[string]any); ok {
							nbType, _ := nbm["type"].(string)
							if err := generateBlock(subBody, nbType, nbm); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// generateTerraformBlock creates a terraform { ... } block.
func generateTerraformBlock(body *hclwrite.Body, def map[string]any) {
	block := body.AppendNewBlock("terraform", nil)
	blockBody := block.Body()

	if rv, ok := def["required_version"].(string); ok {
		writeAttribute(blockBody, "required_version", rv)
	}

	if rp, ok := def["required_providers"].(map[string]any); ok && len(rp) > 0 {
		rpBlock := blockBody.AppendNewBlock("required_providers", nil)
		rpBody := rpBlock.Body()
		keys := sortedKeys(rp)
		for _, k := range keys {
			writeAttribute(rpBody, k, rp[k])
		}
	}

	if backend, ok := def["backend"].(map[string]any); ok {
		bType, _ := backend["type"].(string)
		labels := []string{}
		if bType != "" {
			labels = append(labels, bType)
		}
		bBlock := blockBody.AppendNewBlock("backend", labels)
		if attrs, ok := backend["attributes"].(map[string]any); ok {
			bBody := bBlock.Body()
			keys := sortedKeys(attrs)
			for _, k := range keys {
				writeAttribute(bBody, k, attrs[k])
			}
		}
	}

	if cloud, ok := def["cloud"].(map[string]any); ok && len(cloud) > 0 {
		cBlock := blockBody.AppendNewBlock("cloud", nil)
		cBody := cBlock.Body()
		keys := sortedKeys(cloud)
		for _, k := range keys {
			writeAttribute(cBody, k, cloud[k])
		}
	}

	if attrs, ok := def["attributes"].(map[string]any); ok {
		keys := sortedKeys(attrs)
		for _, k := range keys {
			writeAttribute(blockBody, k, attrs[k])
		}
	}
}

// generateLocalsBlock creates a locals { ... } block.
func generateLocalsBlock(body *hclwrite.Body, locals map[string]any) {
	block := body.AppendNewBlock("locals", nil)
	blockBody := block.Body()
	keys := sortedKeys(locals)
	for _, k := range keys {
		writeAttribute(blockBody, k, locals[k])
	}
}

// writeAttribute writes a key-value attribute to an hclwrite body.
// Values are converted to HCL token representations.
// The "type" key is special-cased: when its value is a recognized HCL type
// constraint (e.g. string, list(string)), it is written as an unquoted
// identifier. Other values fall back to normal token rendering.
func writeAttribute(body *hclwrite.Body, key string, val any) {
	var tokens hclwrite.Tokens
	if key == "type" {
		if s, ok := val.(string); ok && isHCLTypeExpression(s) {
			tokens = hclwrite.TokensForIdentifier(s)
		} else {
			tokens = valueToTokens(val)
		}
	} else {
		tokens = valueToTokens(val)
	}
	body.SetAttributeRaw(key, tokens)
}

// valueToTokens converts a Go value into hclwrite tokens.
func valueToTokens(val any) hclwrite.Tokens {
	switch v := val.(type) {
	case string:
		// Check if the string looks like an HCL expression (references, function calls).
		if looksLikeExpression(v) {
			return hclwrite.TokensForIdentifier(v)
		}
		return hclwrite.TokensForValue(ctyStringVal(v))
	case bool:
		return hclwrite.TokensForValue(ctyBoolVal(v))
	case int:
		return hclwrite.TokensForValue(ctyNumberIntVal(int64(v)))
	case int64:
		return hclwrite.TokensForValue(ctyNumberIntVal(v))
	case float64:
		return hclwrite.TokensForValue(ctyNumberFloatVal(v))
	case []any:
		return tokensForList(v)
	case map[string]any:
		return tokensForObject(v)
	case nil:
		return hclwrite.TokensForIdentifier("null")
	default:
		return hclwrite.TokensForValue(ctyStringVal(fmt.Sprintf("%v", v)))
	}
}

// tokensForList renders a Go slice as HCL list tokens: [a, b, c].
func tokensForList(items []any) hclwrite.Tokens {
	var src strings.Builder
	src.WriteString("[")
	for i, item := range items {
		if i > 0 {
			src.WriteString(", ")
		}
		src.WriteString(valueToHCLString(item))
	}
	src.WriteString("]")
	return hclwrite.TokensForIdentifier(src.String())
}

// tokensForObject renders a Go map as HCL object tokens: {k = v, ...}.
func tokensForObject(m map[string]any) hclwrite.Tokens {
	if len(m) == 0 {
		return hclwrite.TokensForIdentifier("{}")
	}
	var src strings.Builder
	src.WriteString("{\n")
	keys := sortedKeys(m)
	for _, k := range keys {
		src.WriteString("    ")
		src.WriteString(k)
		src.WriteString(" = ")
		src.WriteString(valueToHCLString(m[k]))
		src.WriteString("\n")
	}
	src.WriteString("  }")
	return hclwrite.TokensForIdentifier(src.String())
}

// valueToHCLString renders a scalar Go value as an HCL literal string.
func valueToHCLString(val any) string {
	switch v := val.(type) {
	case string:
		if looksLikeExpression(v) {
			return v
		}
		return fmt.Sprintf("%q", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%q", fmt.Sprint(v))
	}
}

// hclTypeKeywords are bare HCL type constraint keywords that must be written
// unquoted as identifiers (e.g. type = string, not type = "string").
// Only scalar types are allowed bare; composite types (list, map, set, object,
// tuple) require parenthesised arguments and are handled by isHCLTypeExpression.
var hclTypeKeywords = map[string]bool{
	"string": true, "number": true, "bool": true, "any": true,
}

// hclCompositeTypeKeywords are type constructors that are valid only with
// parenthesised arguments (e.g. list(string), not bare "list").
var hclCompositeTypeKeywords = map[string]bool{
	"list": true, "map": true, "set": true, "object": true, "tuple": true,
}

// isHCLTypeExpression returns true if a string is an HCL type constraint expression.
// Type constraints are always identifiers, never quoted strings:
// simple types (string, number, bool, any) and complex types (list(string), map(number), etc.).
func isHCLTypeExpression(s string) bool {
	s = strings.TrimSpace(s)
	if hclTypeKeywords[s] {
		return true
	}
	// Complex types require parenthesised arguments: list(...), map(...), etc.
	if idx := strings.Index(s, "("); idx > 0 {
		keyword := s[:idx]
		if hclCompositeTypeKeywords[keyword] && strings.HasSuffix(s, ")") {
			return true
		}
	}
	return false
}

// looksLikeExpression returns true if a string appears to be an HCL expression
// rather than a plain string literal (e.g., variable references, function calls).
func looksLikeExpression(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "var.") || strings.HasPrefix(s, "local.") ||
		strings.HasPrefix(s, "module.") || strings.HasPrefix(s, "data.") ||
		strings.HasPrefix(s, "each.") || strings.HasPrefix(s, "self.") ||
		strings.HasPrefix(s, "count.") || strings.HasPrefix(s, "path.") {
		return true
	}
	// Function calls like merge(...), lookup(...)
	if strings.Contains(s, "(") && strings.HasSuffix(s, ")") {
		return true
	}
	// Ternary: condition ? a : b
	if strings.Contains(s, "?") && strings.Contains(s, ":") {
		return true
	}
	return false
}

// labelsForBlock returns the appropriate HCL block labels based on block type.
func labelsForBlock(blockType string, def map[string]any) []string {
	switch blockType {
	case "variable", "module", "output", "check":
		if name, ok := def["name"].(string); ok {
			return []string{name}
		}
	case "resource", "data":
		var labels []string
		if t, ok := def["type"].(string); ok {
			labels = append(labels, t)
		}
		if n, ok := def["name"].(string); ok {
			labels = append(labels, n)
		}
		return labels
	case "provider":
		if name, ok := def["name"].(string); ok {
			return []string{name}
		}
	}
	return nil
}

// labelKeySet returns a set of keys used as labels for a given block type.
func labelKeySet(blockType string) map[string]bool {
	switch blockType {
	case "variable", "module", "output", "check":
		return map[string]bool{"name": true}
	case "resource", "data":
		return map[string]bool{"type": true, "name": true}
	case "provider":
		return map[string]bool{"name": true}
	default:
		return map[string]bool{}
	}
}

// promotedKeysForBlock returns well-known attribute keys that should be written
// before generic attributes, in order. These are keys that ParseHCL promotes
// out of the raw attributes map.
func promotedKeysForBlock(blockType string) []string {
	switch blockType {
	case "variable":
		return []string{"type", "default", "description", "sensitive", "nullable"}
	case "output":
		return []string{"value", "description", "sensitive", "depends_on"}
	case "module":
		return []string{"source", "version", "count", "for_each", "depends_on", "providers"}
	case "provider":
		return []string{"alias", "region"}
	case "moved":
		return []string{"from", "to"}
	case "import":
		return []string{"to", "id", "provider", "for_each"}
	default:
		return nil
	}
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
