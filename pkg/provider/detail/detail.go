// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package detail provides shared business logic for building structured
// provider information. This package is used by CLI, MCP, and future API consumers.
package detail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// BuildProviderDetail builds a detailed map for a single provider descriptor.
// The returned map includes schema, output schemas, examples, CLI usage, and metadata.
func BuildProviderDetail(desc provider.Descriptor) map[string]any {
	output := map[string]any{
		"name":         desc.Name,
		"displayName":  desc.DisplayName,
		"apiVersion":   desc.APIVersion,
		"version":      desc.Version.String(),
		"description":  desc.Description,
		"capabilities": CapabilitiesToStrings(desc.Capabilities),
	}

	if desc.Category != "" {
		output["category"] = desc.Category
	}
	if len(desc.Tags) > 0 {
		output["tags"] = slices.Clone(desc.Tags)
	}
	if desc.Icon != "" {
		output["icon"] = desc.Icon
	}
	if desc.IsDeprecated {
		output["deprecated"] = true
	}
	if desc.Beta {
		output["beta"] = true
	}

	// Add write-operation classification and rich operation metadata
	if writeOps := desc.EffectiveWriteOperations(); writeOps != nil {
		output["writeOperations"] = writeOps
	}
	if ops := BuildOperationsOutput(desc.Operations); len(ops) > 0 {
		output["operations"] = ops
	}

	// Add schema information
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		output["schema"] = BuildSchemaOutput(desc.Schema)
	}

	// Add output schemas
	if len(desc.OutputSchemas) > 0 {
		outputSchemas := make(map[string]any)
		for cp, schema := range desc.OutputSchemas {
			outputSchemas[string(cp)] = BuildSchemaOutput(schema)
		}
		output["outputSchemas"] = outputSchemas
	}

	// Add links
	if len(desc.Links) > 0 {
		links := make([]map[string]string, 0, len(desc.Links))
		for _, link := range desc.Links {
			links = append(links, map[string]string{
				"name": link.Name,
				"url":  link.URL,
			})
		}
		output["links"] = links
	}

	// Add examples
	if len(desc.Examples) > 0 {
		examples := make([]map[string]any, 0, len(desc.Examples))
		for _, ex := range desc.Examples {
			examples = append(examples, map[string]any{
				"name":        ex.Name,
				"description": ex.Description,
				"yaml":        ex.YAML,
			})
		}
		output["examples"] = examples
	}

	// Add maintainers
	if len(desc.Maintainers) > 0 {
		maintainers := make([]map[string]string, 0, len(desc.Maintainers))
		for _, m := range desc.Maintainers {
			maintainers = append(maintainers, map[string]string{
				"name":  m.Name,
				"email": m.Email,
			})
		}
		output["maintainers"] = maintainers
	}

	// Add CLI usage examples
	cliExamples := GenerateCLIExamples(&desc)
	if len(cliExamples) > 0 {
		output["cliUsage"] = cliExamples
	}

	return output
}

// GenerateCLIExamples auto-generates CLI usage examples from the provider's schema.
// It builds "<binary> run provider <name>" commands using required fields and
// type-appropriate placeholder values.
func GenerateCLIExamples(desc *provider.Descriptor) []string {
	if desc.Schema == nil || len(desc.Schema.Properties) == 0 {
		return nil
	}

	// Build required set
	requiredSet := make(map[string]bool, len(desc.Schema.Required))
	for _, name := range desc.Schema.Required {
		requiredSet[name] = true
	}

	// Collect required fields with placeholder values, sorted for deterministic output
	requiredNames := make([]string, 0)
	for name := range desc.Schema.Properties {
		if requiredSet[name] {
			requiredNames = append(requiredNames, name)
		}
	}
	slices.Sort(requiredNames)

	// Build input flags for required fields
	inputFlags := make([]string, 0, len(requiredNames))
	for _, name := range requiredNames {
		prop := desc.Schema.Properties[name]
		placeholder := SchemaPlaceholder(name, prop)
		inputFlags = append(inputFlags, fmt.Sprintf("%s=%s", name, placeholder))
	}

	var examples []string

	binName := paths.AppName()

	// Example with required fields only
	if len(inputFlags) > 0 {
		cmd := fmt.Sprintf("%s run provider %s %s", binName, desc.Name, strings.Join(inputFlags, " "))
		examples = append(examples, cmd)
	} else {
		// No required fields - show a minimal example
		examples = append(examples, fmt.Sprintf("%s run provider %s", binName, desc.Name))
	}

	// If multiple capabilities, show an example with --capability for non-first capabilities
	if len(desc.Capabilities) > 1 {
		for _, cp := range desc.Capabilities[1:] {
			baseCmdParts := []string{fmt.Sprintf("%s run provider %s", binName, desc.Name)}
			if len(inputFlags) > 0 {
				baseCmdParts = append(baseCmdParts, inputFlags...)
			}
			baseCmdParts = append(baseCmdParts, fmt.Sprintf("--capability %s", cp))
			examples = append(examples, strings.Join(baseCmdParts, " "))
		}
	}

	// Add file-based input example
	examples = append(examples, fmt.Sprintf("%s run provider %s @inputs.yaml", binName, desc.Name))

	return examples
}

// SchemaPlaceholder returns a reasonable placeholder value for a schema property.
func SchemaPlaceholder(name string, prop *jsonschema.Schema) string {
	if prop == nil {
		return "<value>"
	}

	// Use the first example if available
	if len(prop.Examples) > 0 {
		return fmt.Sprintf("%v", prop.Examples[0])
	}

	// Use the first enum value if available
	if len(prop.Enum) > 0 {
		return fmt.Sprintf("%v", prop.Enum[0])
	}

	// Type-based placeholder
	switch prop.Type {
	case "string":
		return fmt.Sprintf("<%s>", name)
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	case "array":
		return fmt.Sprintf("<%s1>,<%s2>", name, name)
	case "object":
		return fmt.Sprintf("@%s.yaml", name)
	default:
		return fmt.Sprintf("<%s>", name)
	}
}

// BuildSchemaOutput converts a JSON Schema to a map for structured output.
func BuildSchemaOutput(schema *jsonschema.Schema) map[string]any {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}

	// Build required set
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	properties := make(map[string]any)
	for name, prop := range schema.Properties {
		if prop == nil {
			propMap := map[string]any{
				"type": "any",
			}
			if requiredSet[name] {
				propMap["required"] = true
			}
			properties[name] = propMap
			continue
		}
		propMap := map[string]any{
			"type": prop.Type,
		}
		if prop.Description != "" {
			propMap["description"] = prop.Description
		}
		if requiredSet[name] {
			propMap["required"] = true
		}
		if prop.Default != nil {
			var def any
			_ = json.Unmarshal(prop.Default, &def)
			propMap["default"] = def
		}
		if len(prop.Examples) > 0 {
			propMap["example"] = prop.Examples[0]
		}
		if len(prop.Enum) > 0 {
			propMap["enum"] = slices.Clone(prop.Enum)
		}
		properties[name] = propMap
	}

	return map[string]any{"properties": properties}
}

// CapabilitiesToStrings converts []Capability to []string.
func CapabilitiesToStrings(caps []provider.Capability) []string {
	result := make([]string, 0, len(caps))
	for _, cp := range caps {
		result = append(result, string(cp))
	}
	return result
}

// BuildOperationsOutput converts a slice of OperationDescriptor entries into
// structured maps suitable for JSON/YAML/kvx output. Returns nil when ops is empty.
func BuildOperationsOutput(ops []provider.OperationDescriptor) []map[string]any {
	if len(ops) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		result = append(result, BuildOperationDetail(op))
	}
	return result
}

// BuildOperationDetail builds a structured map for a single OperationDescriptor.
// It always emits "name" and "isWrite" (as a boolean) so CEL expressions and
// kvx -w filters never fail on missing keys when inspecting read vs write operations.
func BuildOperationDetail(op provider.OperationDescriptor) map[string]any {
	out := map[string]any{
		"name":    op.Name,
		"isWrite": op.IsWrite,
	}
	if op.DisplayName != "" {
		out["displayName"] = op.DisplayName
	}
	if op.Description != "" {
		out["description"] = op.Description
	}
	if len(op.Capabilities) > 0 {
		out["capabilities"] = CapabilitiesToStrings(op.Capabilities)
	}
	if inputSchema := buildOperationSchemaOutput(op.InputSchema); inputSchema != nil {
		out["inputSchema"] = inputSchema
	}
	if outputSchema := buildOperationSchemaOutput(op.OutputSchema); outputSchema != nil {
		out["outputSchema"] = outputSchema
	}
	if len(op.Examples) > 0 {
		examples := make([]map[string]any, 0, len(op.Examples))
		for _, ex := range op.Examples {
			examples = append(examples, map[string]any{
				"name":        ex.Name,
				"description": ex.Description,
				"yaml":        ex.YAML,
			})
		}
		out["examples"] = examples
	}
	if len(op.Tags) > 0 {
		out["tags"] = slices.Clone(op.Tags)
	}
	if op.IsDeprecated {
		out["deprecated"] = true
	}
	if op.DeprecationMessage != "" {
		out["deprecationMessage"] = op.DeprecationMessage
	}
	return out
}

// buildOperationSchemaOutput converts an operation's JSON Schema into an
// independent JSON-compatible value (typically map[string]any) while preserving
// nested properties, required lists, array items, dynamic-key
// additionalProperties, validation constraints, and exact 64-bit integer
// precision across JSON, YAML, CEL, MCP, and REST outputs without sharing
// mutable state with the descriptor.
func buildOperationSchemaOutput(schema *jsonschema.Schema) any {
	if schema == nil {
		return nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil
	}
	return normalizeJSONNumbers(out)
}

func normalizeJSONNumbers(v any) any {
	switch val := v.(type) {
	case json.Number:
		s := val.String()
		if intVal, ok := parseIntegralJSONNumber(s); ok {
			return intVal
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return val
	case map[string]any:
		for k, elem := range val {
			val[k] = normalizeJSONNumbers(elem)
		}
		return val
	case []any:
		for i, elem := range val {
			val[i] = normalizeJSONNumbers(elem)
		}
		return val
	default:
		return v
	}
}

const maxUint64DecimalDigits = 20 // len("18446744073709551615")

// parseIntegralJSONNumber parses a JSON number token into int64 or uint64 when
// its mathematical value is an exact 64-bit integer, including decimal and
// exponent representations such as "9007199254740993.0" or "9.007199254740993e15".
func parseIntegralJSONNumber(s string) (any, bool) {
	if s == "" {
		return nil, false
	}
	if !strings.ContainsAny(s, ".eE") {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, true
		}
		if u, err := strconv.ParseUint(s, 10, 64); err == nil {
			return u, true
		}
		return nil, false
	}

	rest := s
	neg := false
	if rest[0] == '-' {
		neg = true
		rest = rest[1:]
	}

	mantissa := rest
	expStr := ""
	if eIdx := strings.IndexAny(rest, "eE"); eIdx >= 0 {
		mantissa = rest[:eIdx]
		expStr = rest[eIdx+1:]
		if expStr == "" {
			return nil, false
		}
		expDigits := expStr
		if expDigits[0] == '+' || expDigits[0] == '-' {
			expDigits = expDigits[1:]
		}
		if expDigits == "" {
			return nil, false
		}
		for _, ch := range expDigits {
			if ch < '0' || ch > '9' {
				return nil, false
			}
		}
	}

	intPart := mantissa
	fracPart := ""
	if dotIdx := strings.IndexByte(mantissa, '.'); dotIdx >= 0 {
		intPart = mantissa[:dotIdx]
		fracPart = mantissa[dotIdx+1:]
		if intPart == "" || fracPart == "" {
			return nil, false
		}
	} else if intPart == "" {
		return nil, false
	}

	digits := intPart + fracPart
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return nil, false
		}
	}

	trimmedDigits := strings.TrimLeft(digits, "0")
	if trimmedDigits == "" {
		return int64(0), true
	}

	exp := 0
	if expStr != "" {
		parsedExp, err := strconv.Atoi(expStr)
		if err != nil {
			return nil, false
		}
		exp = parsedExp
	}

	if exp < 0 {
		maxNegExp := len(trimmedDigits) - len(fracPart) - 1
		if maxNegExp < 0 || exp < -maxNegExp {
			return nil, false
		}
	}

	shift := exp - len(fracPart)
	if shift < 0 {
		drop := -shift
		if drop >= len(trimmedDigits) {
			return nil, false
		}
		for _, ch := range trimmedDigits[len(trimmedDigits)-drop:] {
			if ch != '0' {
				return nil, false
			}
		}
		trimmedDigits = trimmedDigits[:len(trimmedDigits)-drop]
	} else if shift > 0 {
		if len(trimmedDigits) > maxUint64DecimalDigits || shift > maxUint64DecimalDigits-len(trimmedDigits) {
			return nil, false
		}
		trimmedDigits += strings.Repeat("0", shift)
	}

	if len(trimmedDigits) > maxUint64DecimalDigits {
		return nil, false
	}
	if neg {
		if i, err := strconv.ParseInt("-"+trimmedDigits, 10, 64); err == nil {
			return i, true
		}
		return nil, false
	}
	if i, err := strconv.ParseInt(trimmedDigits, 10, 64); err == nil {
		return i, true
	}
	if u, err := strconv.ParseUint(trimmedDigits, 10, 64); err == nil {
		return u, true
	}
	return nil, false
}

// MaxListedAvailableOperations caps the inline list of valid operation names
// included in "operation not found" diagnostics so providers with dozens of
// operations produce readable error messages.
const MaxListedAvailableOperations = 15

// FormatAvailableOperations formats operation names for error diagnostics,
// sanitizing each listed name for safe single-line output and truncating lists
// longer than MaxListedAvailableOperations.
func FormatAvailableOperations(names []string) string {
	if len(names) == 0 {
		return ""
	}
	limit := min(len(names), MaxListedAvailableOperations)
	sanitized := make([]string, 0, limit)
	for _, name := range names[:limit] {
		clean := SanitizeInlineText(name)
		if clean == "" {
			clean = "<unnamed>"
		}
		sanitized = append(sanitized, clean)
	}
	if len(names) <= MaxListedAvailableOperations {
		return strings.Join(sanitized, ", ")
	}
	return fmt.Sprintf("%s, ... (%d total)",
		strings.Join(sanitized, ", "),
		len(names))
}

// SanitizeInlineText strips ANSI CSI/OSC escape sequences and C0/C1 control
// characters from s and collapses whitespace (including newlines and tabs)
// into single spaces for safe single-line terminal rendering.
func SanitizeInlineText(s string) string {
	return strings.Join(strings.Fields(stripTerminalEscapes(s, false)), " ")
}

// SanitizeMultilineText strips ANSI CSI/OSC escape sequences and C0/C1 control
// characters from s while preserving line breaks (normalizing CRLF to LF) and
// trimming outer whitespace.
func SanitizeMultilineText(s string) string {
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(stripTerminalEscapes(normalized, true))
}

func stripTerminalEscapes(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i++
			if i < len(s) {
				switch s[i] {
				case '[':
					i++
					for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
						i++
					}
					if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
						i++
					}
				case ']':
					i++
					for i < len(s) {
						if s[i] == 0x07 {
							i++
							break
						}
						if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
							i += 2
							break
						}
						i++
					}
				default:
					if s[i] >= 0x20 && s[i] <= 0x7e {
						i++
					}
				}
			}
			continue
		}
		r, width := utf8.DecodeRuneInString(s[i:])
		i += width
		switch {
		case r == '\n':
			if keepNewlines {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		case r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Drop C0/C1 control characters.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
