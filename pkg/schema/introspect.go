// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package schema provides reflection-based struct introspection for generating
// kubectl explain-style documentation from Go struct tags.
package schema

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// FieldInfo represents introspected information about a struct field,
// extracted from Go struct tags.
type FieldInfo struct {
	// Name is the field name as it appears in JSON/YAML (from json tag)
	Name string `json:"name" yaml:"name"`

	// GoName is the original Go field name
	GoName string `json:"goName" yaml:"goName"`

	// Type is the Go type name
	Type string `json:"type" yaml:"type"`

	// Kind is the reflect.Kind (struct, slice, map, etc.)
	Kind reflect.Kind `json:"kind" yaml:"kind"`

	// Description from the "doc" tag
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Required from the "required" tag
	Required bool `json:"required,omitempty" yaml:"required,omitempty"`

	// Example from the "example" tag
	Example string `json:"example,omitempty" yaml:"example,omitempty"`

	// Pattern from the "pattern" tag (regex)
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`

	// PatternDescription from the "patternDescription" tag
	PatternDescription string `json:"patternDescription,omitempty" yaml:"patternDescription,omitempty"`

	// Format from the "format" tag (uri, email, date, etc.)
	Format string `json:"format,omitempty" yaml:"format,omitempty"`

	// MinLength from the "minLength" tag
	MinLength *int `json:"minLength,omitempty" yaml:"minLength,omitempty"`

	// MaxLength from the "maxLength" tag
	MaxLength *int `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`

	// Minimum from the "minimum" tag
	Minimum *float64 `json:"minimum,omitempty" yaml:"minimum,omitempty"`

	// Maximum from the "maximum" tag
	Maximum *float64 `json:"maximum,omitempty" yaml:"maximum,omitempty"`

	// MinItems from the "minItems" tag
	MinItems *int `json:"minItems,omitempty" yaml:"minItems,omitempty"`

	// MaxItems from the "maxItems" tag
	MaxItems *int `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`

	// Default from the "default" tag
	Default string `json:"default,omitempty" yaml:"default,omitempty"`

	// Enum values (if field is an enum type with known values)
	Enum []string `json:"enum,omitempty" yaml:"enum,omitempty"`

	// Deprecated from the "deprecated" tag
	Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`

	// DeprecatedReplacement from the "deprecatedReplacement" tag names the
	// field that supersedes this one.
	DeprecatedReplacement string `json:"deprecatedReplacement,omitempty" yaml:"deprecatedReplacement,omitempty"`

	// DeprecatedMessage from the "deprecatedMessage" tag provides additional
	// migration guidance.
	DeprecatedMessage string `json:"deprecatedMessage,omitempty" yaml:"deprecatedMessage,omitempty"`

	// Omitempty indicates if the field has omitempty in json tag
	Omitempty bool `json:"omitempty,omitempty" yaml:"omitempty,omitempty"`

	// ElemType is the element type for slices/arrays/maps
	ElemType string `json:"elemType,omitempty" yaml:"elemType,omitempty"`

	// ElemKind is the element's reflect.Kind for slices/arrays/maps
	ElemKind reflect.Kind `json:"elemKind,omitempty" yaml:"elemKind,omitempty"`

	// KeyType is the key type for maps
	KeyType string `json:"keyType,omitempty" yaml:"keyType,omitempty"`

	// NestedFields contains fields if this is a struct type
	NestedFields []FieldInfo `json:"nestedFields,omitempty" yaml:"nestedFields,omitempty"`

	// IsPointer indicates if the field is a pointer type
	IsPointer bool `json:"isPointer,omitempty" yaml:"isPointer,omitempty"`

	// IsExported indicates if the field is exported
	IsExported bool `json:"isExported,omitempty" yaml:"isExported,omitempty"`
}

// TypeInfo represents introspected information about a Go type.
type TypeInfo struct {
	// Name is the type name
	Name string `json:"name" yaml:"name"`

	// Package is the package path
	Package string `json:"package" yaml:"package"`

	// Description from the type's doc comment (not available via reflection)
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Kind is the reflect.Kind
	Kind reflect.Kind `json:"kind" yaml:"kind"`

	// Fields contains information about struct fields (if struct type)
	Fields []FieldInfo `json:"fields,omitempty" yaml:"fields,omitempty"`
}

// IntrospectType returns detailed information about a Go type by examining
// its struct tags. Pass a pointer to the type: IntrospectType((*MyStruct)(nil))
func IntrospectType(v any) (*TypeInfo, error) {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil, fmt.Errorf("nil type provided")
	}

	// Dereference pointer
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	info := &TypeInfo{
		Name:    t.Name(),
		Package: t.PkgPath(),
		Kind:    t.Kind(),
	}

	if t.Kind() == reflect.Struct {
		// Track seen types to prevent infinite recursion on self-referential types
		// like jsonschema.Schema which has many *Schema fields
		seen := make(map[reflect.Type]bool)
		info.Fields = introspectFieldsWithSeen(t, 0, seen)
	}

	return info, nil
}

// FieldsAtPath returns the child fields valid at a logical field path, for
// schema-driven key completion. An empty path returns the top-level fields of v;
// a non-empty path returns the nested fields of the struct/map/slice-element type
// at that path. ok is false when the path does not resolve to a type with named
// child fields (e.g. a scalar leaf or an unknown path). Path navigation follows
// IntrospectField's rules: dot-separated JSON/Go field names, diving through
// pointers, slice/array elements, and map values (indices and dynamic map keys
// are not part of the path -- normalize before calling).
func FieldsAtPath(v any, path string) ([]FieldInfo, bool) {
	if path == "" {
		ti, err := IntrospectType(v)
		if err != nil || ti == nil {
			return nil, false
		}
		return ti.Fields, len(ti.Fields) > 0
	}
	fi, err := IntrospectField(v, path)
	if err != nil || fi == nil || len(fi.NestedFields) == 0 {
		return nil, false
	}
	return fi.NestedFields, true
}

// IntrospectField returns field information for a specific field path.
// The path uses dot notation: "schema.properties" or "metadata.version"
func IntrospectField(v any, path string) (*FieldInfo, error) {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil, fmt.Errorf("nil type provided")
	}

	// Dereference pointer
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type %s is not a struct", t.Name())
	}

	parts := strings.Split(path, ".")
	return navigateToField(t, parts)
}

// navigateToField traverses the type hierarchy following the path segments.
func navigateToField(t reflect.Type, path []string) (*FieldInfo, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path")
	}

	// Dereference pointers
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("cannot navigate into non-struct type %s", t.Name())
	}

	targetName := strings.ToLower(path[0])

	// Find the field by JSON name or Go name
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonName := getJSONName(f)
		if strings.ToLower(jsonName) == targetName || strings.ToLower(f.Name) == targetName {
			fieldInfo := introspectFieldWithSeen(f, 0, make(map[reflect.Type]bool))

			// If there are more path segments, navigate deeper
			if len(path) > 1 {
				// Get the underlying type for navigation
				fieldType := f.Type
				for fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
				}

				// Handle slices/arrays - navigate into element type
				if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
					fieldType = fieldType.Elem()
					for fieldType.Kind() == reflect.Pointer {
						fieldType = fieldType.Elem()
					}
				}

				// Handle maps - navigate into value type
				if fieldType.Kind() == reflect.Map {
					fieldType = fieldType.Elem()
					for fieldType.Kind() == reflect.Pointer {
						fieldType = fieldType.Elem()
					}
				}

				if fieldType.Kind() == reflect.Struct {
					return navigateToField(fieldType, path[1:])
				}
				return nil, fmt.Errorf("cannot navigate into %s (type %s)", path[0], fieldType.Kind())
			}

			return &fieldInfo, nil
		}
	}

	return nil, fmt.Errorf("field %q not found in %s", path[0], t.Name())
}

// introspectFieldsWithSeen recursively introspects all fields of a struct type,
// tracking seen types to prevent infinite recursion on self-referential types.
func introspectFieldsWithSeen(t reflect.Type, depth int, seen map[reflect.Type]bool) []FieldInfo {
	if depth > 10 {
		// Prevent infinite recursion
		return nil
	}

	// Check if we've already seen this type (self-referential prevention)
	if seen[t] {
		return nil
	}
	seen[t] = true
	// Don't mark as unseen after - we want to skip subsequent references

	var fields []FieldInfo

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// Skip unexported fields
		if !f.IsExported() {
			continue
		}

		// Skip fields with json:"-"
		jsonTag := f.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		// Promote the fields of an anonymous embedded struct that serializes
		// inline (no explicit key name; e.g. spec.CallRef with yaml:",inline").
		// json/yaml both flatten such embeds into the parent object, so the
		// schema must too -- otherwise the embed surfaces as a phantom field
		// named after its Go type and its real keys are hidden a level down.
		if embeddedType, ok := inlineEmbeddedStruct(f); ok {
			if depth < 10 && !seen[embeddedType] {
				fields = append(fields, introspectFieldsWithSeen(embeddedType, depth+1, seen)...)
			}
			continue
		}

		fields = append(fields, introspectFieldWithSeen(f, depth, seen))
	}

	return fields
}

// inlineEmbeddedStruct reports whether f is an anonymous embedded struct that
// serializes inline (its fields promoted into the parent), returning the
// embedded struct type. An embed is inline when it has no explicit serialized
// key name -- no json name and either no yaml tag or an explicit ",inline". A
// renamed embed (e.g. `json:"base"`) is a normal named nested field, not inline.
func inlineEmbeddedStruct(f reflect.StructField) (reflect.Type, bool) {
	if !f.Anonymous {
		return nil, false
	}
	ft := f.Type
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct {
		return nil, false
	}
	if tagName(f.Tag.Get("json")) != "" {
		return nil, false
	}
	yamlTag := f.Tag.Get("yaml")
	// A renamed embed (e.g. `yaml:"base"`) is a normal named nested field, not
	// inline -- even if the name happens to contain the substring "inline".
	if tagName(yamlTag) != "" {
		return nil, false
	}
	// With no yaml tag, a Go embed serializes inline by default. When a yaml
	// tag is present (but unnamed), require an explicit ",inline" option rather
	// than substring-matching the raw tag.
	if yamlTag != "" && !hasTagOption(yamlTag, "inline") {
		return nil, false
	}
	return ft, true
}

// hasTagOption reports whether a struct tag value carries the given comma-
// separated option (e.g. "inline" in ",inline" or "foo,omitempty,inline").
// The name portion (before the first comma) is not treated as an option.
func hasTagOption(tag, opt string) bool {
	parts := strings.Split(tag, ",")
	for _, p := range parts[1:] {
		if p == opt {
			return true
		}
	}
	return false
}

// tagName returns the name portion of a struct tag value (the text before the
// first comma), e.g. "foo,omitempty" -> "foo", ",inline" -> "".
func tagName(tag string) string {
	if i := strings.IndexByte(tag, ','); i >= 0 {
		return tag[:i]
	}
	return tag
}

// introspectFieldWithSeen extracts all tag information from a struct field,
// tracking seen types to prevent infinite recursion.
func introspectFieldWithSeen(f reflect.StructField, depth int, seen map[reflect.Type]bool) FieldInfo {
	info := FieldInfo{
		GoName:     f.Name,
		Name:       getJSONName(f),
		IsExported: f.IsExported(),
	}

	// Get type information
	fieldType := f.Type
	if fieldType.Kind() == reflect.Pointer {
		info.IsPointer = true
		fieldType = fieldType.Elem()
	}

	info.Kind = fieldType.Kind()
	info.Type = formatTypeName(fieldType)

	// Handle complex types
	//exhaustive:ignore
	switch fieldType.Kind() {
	case reflect.Slice, reflect.Array:
		elemType := fieldType.Elem()
		for elemType.Kind() == reflect.Pointer {
			elemType = elemType.Elem()
		}
		info.ElemType = formatTypeName(elemType)
		info.ElemKind = elemType.Kind()

		// Introspect nested struct elements (only if not already seen)
		if elemType.Kind() == reflect.Struct && depth < 10 && !seen[elemType] {
			info.NestedFields = introspectFieldsWithSeen(elemType, depth+1, seen)
		}

	case reflect.Map:
		info.KeyType = formatTypeName(fieldType.Key())
		elemType := fieldType.Elem()
		for elemType.Kind() == reflect.Pointer {
			elemType = elemType.Elem()
		}
		info.ElemType = formatTypeName(elemType)
		info.ElemKind = elemType.Kind()

		// Introspect nested struct values (only if not already seen)
		if elemType.Kind() == reflect.Struct && depth < 10 && !seen[elemType] {
			info.NestedFields = introspectFieldsWithSeen(elemType, depth+1, seen)
		}

	case reflect.Struct:
		// Introspect nested struct fields (only if not already seen)
		if depth < 10 && !seen[fieldType] {
			info.NestedFields = introspectFieldsWithSeen(fieldType, depth+1, seen)
		}
	}

	// Parse struct tags
	info.Description = f.Tag.Get("doc")
	info.Example = f.Tag.Get("example")
	info.Pattern = f.Tag.Get("pattern")
	info.PatternDescription = f.Tag.Get("patternDescription")
	info.Format = f.Tag.Get("format")
	info.Default = f.Tag.Get("default")

	// Required tag
	if req := f.Tag.Get("required"); req == "true" {
		info.Required = true
	}

	// Deprecated tag
	if dep := f.Tag.Get("deprecated"); dep == "true" {
		info.Deprecated = true
	}
	info.DeprecatedReplacement = f.Tag.Get("deprecatedReplacement")
	info.DeprecatedMessage = f.Tag.Get("deprecatedMessage")

	// Omitempty from json tag
	jsonTag := f.Tag.Get("json")
	if strings.Contains(jsonTag, "omitempty") {
		info.Omitempty = true
	}

	// Numeric tags
	if v := f.Tag.Get("minLength"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.MinLength = &n
		}
	}
	if v := f.Tag.Get("maxLength"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.MaxLength = &n
		}
	}
	if v := f.Tag.Get("minItems"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.MinItems = &n
		}
	}
	if v := f.Tag.Get("maxItems"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.MaxItems = &n
		}
	}
	if v := f.Tag.Get("minimum"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			info.Minimum = &n
		}
	}
	if v := f.Tag.Get("maximum"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			info.Maximum = &n
		}
	}

	return info
}

// getJSONName extracts the JSON field name from struct tags.
func getJSONName(f reflect.StructField) string {
	jsonTag := f.Tag.Get("json")
	if jsonTag == "" {
		return f.Name
	}

	parts := strings.Split(jsonTag, ",")
	if parts[0] == "" {
		return f.Name
	}
	return parts[0]
}

// formatTypeName returns a human-readable type name.
func formatTypeName(t reflect.Type) string {
	//exhaustive:ignore
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + formatTypeName(t.Elem())
	case reflect.Slice:
		return "[]" + formatTypeName(t.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), formatTypeName(t.Elem()))
	case reflect.Map:
		return fmt.Sprintf("map[%s]%s", formatTypeName(t.Key()), formatTypeName(t.Elem()))
	default:
		if t.PkgPath() != "" && t.Name() != "" {
			// For types from other packages, just use the type name
			return t.Name()
		}
		return t.Name()
	}
}
