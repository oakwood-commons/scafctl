// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// FormatOptions controls how schema information is displayed.
type FormatOptions struct {
	// ShowNestedFields controls whether to expand nested struct fields
	ShowNestedFields bool

	// MaxDepth limits how deep to recurse into nested types
	MaxDepth int

	// ShowValidation controls whether to show validation rules
	ShowValidation bool

	// Compact uses a more compact output format
	Compact bool
}

// DefaultFormatOptions returns sensible defaults for formatting.
func DefaultFormatOptions() FormatOptions {
	return FormatOptions{
		ShowNestedFields: true,
		MaxDepth:         2,
		ShowValidation:   true,
		Compact:          false,
	}
}

// Formatter handles rendering schema information to the terminal.
type Formatter struct {
	w    *writer.Writer
	opts FormatOptions
}

// NewFormatterWithWriter creates a formatter with an existing writer.
func NewFormatterWithWriter(w *writer.Writer) *Formatter {
	return &Formatter{
		w:    w,
		opts: DefaultFormatOptions(),
	}
}

// WithOptions sets formatting options.
func (f *Formatter) WithOptions(opts FormatOptions) *Formatter {
	f.opts = opts
	return f
}

// FormatType renders a TypeInfo in kubectl explain style.
func (f *Formatter) FormatType(info *TypeInfo) {
	// Header
	f.w.Infof("KIND: %s", info.Name)
	if info.Package != "" {
		f.w.Plainlnf("PACKAGE: %s", info.Package)
	}
	f.w.Plainln("")

	if info.Description != "" {
		f.w.Plainlnf("DESCRIPTION:")
		f.w.Plainlnf("    %s", info.Description)
		f.w.Plainln("")
	}

	if len(info.Fields) > 0 {
		f.w.Infof("FIELDS:")
		f.formatFields(info.Fields, 0)
	}
}

// FormatField renders a single FieldInfo in kubectl explain style.
func (f *Formatter) FormatField(info *FieldInfo) {
	// Header showing field name and type
	typeStr := f.formatTypeString(info)
	reqStr := ""
	if info.Required {
		reqStr = " -required-"
	}

	f.w.Infof("FIELD: %s <%s>%s", info.Name, typeStr, reqStr)
	f.w.Plainln("")

	if info.Description != "" {
		f.w.Plainlnf("DESCRIPTION:")
		f.w.Plainlnf("    %s", info.Description)
		f.w.Plainln("")
	}

	// Validation rules
	if f.opts.ShowValidation && f.hasValidation(info) {
		f.w.Infof("VALIDATION:")
		f.formatValidation(info, "    ")
		f.w.Plainln("")
	}

	// Example
	if info.Example != "" {
		f.w.Plainlnf("EXAMPLE: %s", info.Example)
		f.w.Plainln("")
	}

	// Default
	if info.Default != "" {
		f.w.Plainlnf("DEFAULT: %s", info.Default)
		f.w.Plainln("")
	}

	// Nested fields for complex types
	if len(info.NestedFields) > 0 && f.opts.ShowNestedFields {
		f.w.Infof("FIELDS:")
		f.formatFields(info.NestedFields, 0)
	}
}

// formatFields renders a list of fields with indentation.
func (f *Formatter) formatFields(fields []FieldInfo, depth int) {
	if depth >= f.opts.MaxDepth {
		return
	}

	indent := strings.Repeat("    ", depth+1)

	for _, field := range fields {
		typeStr := f.formatTypeString(&field)
		reqStr := ""
		if field.Required {
			reqStr = " -required-"
		}
		deprecatedStr := ""
		if field.Deprecated {
			deprecatedStr = " [DEPRECATED]"
		}

		// Field line
		f.w.Plainlnf("%s%s <%s>%s%s", indent, field.Name, typeStr, reqStr, deprecatedStr)

		// Description (indented further)
		if field.Description != "" {
			f.w.Plainlnf("%s    %s", indent, field.Description)
		}

		// Show validation rules in compact form
		if f.opts.ShowValidation && f.hasValidation(&field) {
			f.formatValidationCompact(&field, indent+"    ")
		}

		// Show example if available
		if field.Example != "" {
			f.w.Plainlnf("%s    Example: %s", indent, field.Example)
		}

		// Recurse into nested fields (if struct/slice of struct/map of struct)
		if len(field.NestedFields) > 0 && f.opts.ShowNestedFields && depth+1 < f.opts.MaxDepth {
			f.formatFields(field.NestedFields, depth+1)
		}

		f.w.Plainln("")
	}
}

// formatTypeString creates a human-readable type representation.
func (f *Formatter) formatTypeString(info *FieldInfo) string {
	//exhaustive:ignore
	switch info.Kind {
	case reflect.Slice, reflect.Array:
		if info.ElemType != "" {
			return fmt.Sprintf("[]%s", info.ElemType)
		}
		return "[]" + info.Type
	case reflect.Map:
		if info.KeyType != "" && info.ElemType != "" {
			return fmt.Sprintf("map[%s]%s", info.KeyType, info.ElemType)
		}
		return info.Type
	case reflect.Ptr:
		return "*" + info.Type
	case reflect.Interface:
		if info.Type == "" {
			return "any"
		}
		return info.Type
	default:
		if info.Type == "" {
			return info.Kind.String()
		}
		return info.Type
	}
}

// hasValidation returns true if the field has any validation constraints.
func (f *Formatter) hasValidation(info *FieldInfo) bool {
	return info.MinLength != nil ||
		info.MaxLength != nil ||
		info.Minimum != nil ||
		info.Maximum != nil ||
		info.MinItems != nil ||
		info.MaxItems != nil ||
		info.Pattern != "" ||
		info.Format != "" ||
		len(info.Enum) > 0
}

// formatValidation renders validation rules.
func (f *Formatter) formatValidation(info *FieldInfo, indent string) {
	if info.MinLength != nil {
		f.w.Plainlnf("%sminLength: %d", indent, *info.MinLength)
	}
	if info.MaxLength != nil {
		f.w.Plainlnf("%smaxLength: %d", indent, *info.MaxLength)
	}
	if info.Minimum != nil {
		f.w.Plainlnf("%sminimum: %v", indent, *info.Minimum)
	}
	if info.Maximum != nil {
		f.w.Plainlnf("%smaximum: %v", indent, *info.Maximum)
	}
	if info.MinItems != nil {
		f.w.Plainlnf("%sminItems: %d", indent, *info.MinItems)
	}
	if info.MaxItems != nil {
		f.w.Plainlnf("%smaxItems: %d", indent, *info.MaxItems)
	}
	if info.Pattern != "" {
		f.w.Plainlnf("%spattern: %s", indent, info.Pattern)
		if info.PatternDescription != "" {
			f.w.Plainlnf("%s  (%s)", indent, info.PatternDescription)
		}
	}
	if info.Format != "" {
		f.w.Plainlnf("%sformat: %s", indent, info.Format)
	}
	if len(info.Enum) > 0 {
		f.w.Plainlnf("%senum: [%s]", indent, strings.Join(info.Enum, ", "))
	}
}

// formatValidationCompact renders validation in a single line.
func (f *Formatter) formatValidationCompact(info *FieldInfo, indent string) {
	var parts []string

	parts = appendLengthValidation(parts, info.MinLength, info.MaxLength)
	parts = appendRangeValidation(parts, info.Minimum, info.Maximum)
	parts = appendItemsValidation(parts, info.MinItems, info.MaxItems)

	if info.Pattern != "" {
		patternStr := info.Pattern
		if info.PatternDescription != "" {
			patternStr = info.PatternDescription
		}
		parts = append(parts, fmt.Sprintf("pattern: %s", patternStr))
	}

	if info.Format != "" {
		parts = append(parts, fmt.Sprintf("format: %s", info.Format))
	}

	if len(info.Enum) > 0 {
		parts = append(parts, fmt.Sprintf("enum: [%s]", strings.Join(info.Enum, "|")))
	}

	if len(parts) > 0 {
		f.w.Plainlnf("%sValidation: %s", indent, strings.Join(parts, ", "))
	}
}

func appendLengthValidation(parts []string, minLen, maxLen *int) []string {
	switch {
	case minLen != nil && maxLen != nil:
		return append(parts, fmt.Sprintf("length: %d-%d", *minLen, *maxLen))
	case minLen != nil:
		return append(parts, fmt.Sprintf("minLength: %d", *minLen))
	case maxLen != nil:
		return append(parts, fmt.Sprintf("maxLength: %d", *maxLen))
	default:
		return parts
	}
}

func appendRangeValidation(parts []string, minVal, maxVal *float64) []string {
	switch {
	case minVal != nil && maxVal != nil:
		return append(parts, fmt.Sprintf("range: %v-%v", *minVal, *maxVal))
	case minVal != nil:
		return append(parts, fmt.Sprintf("min: %v", *minVal))
	case maxVal != nil:
		return append(parts, fmt.Sprintf("max: %v", *maxVal))
	default:
		return parts
	}
}

func appendItemsValidation(parts []string, minItems, maxItems *int) []string {
	switch {
	case minItems != nil && maxItems != nil:
		return append(parts, fmt.Sprintf("items: %d-%d", *minItems, *maxItems))
	case minItems != nil:
		return append(parts, fmt.Sprintf("minItems: %d", *minItems))
	case maxItems != nil:
		return append(parts, fmt.Sprintf("maxItems: %d", *maxItems))
	default:
		return parts
	}
}
