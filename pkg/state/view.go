// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"reflect"
	"sort"
	"strings"
	"time"
)

// displayTimeFormat is the timestamp layout used when projecting state entries
// for display. It is a UTC RFC3339-style layout without sub-second precision.
const displayTimeFormat = "2006-01-02T15:04:05Z"

// Section kinds. Key/value sections describe a single object (e.g. metadata);
// row sections describe a collection rendered as a table (e.g. resolvers).
const (
	SectionKindKeyValue = "keyValue"
	SectionKindRows     = "rows"
)

// SectionNameOverview is the name of the leading section that collects
// top-level scalar fields (e.g. schemaVersion). It exists so no schema field is
// silently dropped from the projection; presentation layers may choose to omit
// it.
const SectionNameOverview = "overview"

// Section is one display group within a ListView. Key/value sections populate
// Fields; row sections populate Rows. The Name matches the json-tag name of the
// originating Data field so the display mirrors the on-disk layout.
type Section struct {
	// Name is the section identifier (json-tag name of the source field).
	Name string `json:"name"`

	// Kind is either SectionKindKeyValue or SectionKindRows.
	Kind string `json:"kind"`

	// Fields holds the key/value pairs for a SectionKindKeyValue section.
	Fields map[string]any `json:"fields,omitempty"`

	// Rows holds the ordered rows for a SectionKindRows section.
	Rows []map[string]any `json:"rows,omitempty"`
}

// Title returns the section name capitalized for use as a display heading.
func (s Section) Title() string {
	if s.Name == "" {
		return s.Name
	}
	return strings.ToUpper(s.Name[:1]) + s.Name[1:]
}

// ListView is a schema-driven, display-oriented projection of state Data.
// It is built by reflecting over Data, so new top-level fields (and new fields
// on persisted-entry types) automatically surface as sections/columns without
// any change to this view. Struct fields become key/value sections, map fields
// become key-sorted row sections, and top-level scalars are grouped into a
// leading "overview" section so no schema field is silently dropped.
type ListView struct {
	// Sections are the ordered display groups derived from Data.
	Sections []Section `json:"sections"`
}

// EntryCount returns the total number of rows across all collection (row)
// sections. Key/value identity sections (overview, metadata, command) are not
// counted, mirroring the notion of "how many stored entries exist".
func (v *ListView) EntryCount() int {
	n := 0
	for i := range v.Sections {
		n += len(v.Sections[i].Rows)
	}
	return n
}

// SectionByName returns a pointer to the section with the given name, or nil if
// no such section exists.
func (v *ListView) SectionByName(name string) *Section {
	for i := range v.Sections {
		if v.Sections[i].Name == name {
			return &v.Sections[i]
		}
	}
	return nil
}

// Summary is a compact, at-a-glance projection of a state document: its identity
// (solution, version), when it was last written, and the size of each stored
// collection. It backs the one-line header shown above the detailed sections in
// human output so a user can grasp "what is this state" without scanning tables.
type Summary struct {
	// Solution is the solution name the state belongs to (may be empty).
	Solution string `json:"solution"`

	// Version is the solution semver the state belongs to (may be empty).
	Version string `json:"version"`

	// SchemaVersion is the state file format version.
	SchemaVersion int `json:"schemaVersion"`

	// LastUpdated is when the state was most recently saved (zero if never).
	LastUpdated time.Time `json:"lastUpdated"`

	// ParameterCount is the number of stored replay parameters.
	ParameterCount int `json:"parameterCount"`

	// ResolverCount is the number of persisted resolver entries.
	ResolverCount int `json:"resolverCount"`

	// FingerprintCount is the number of stored action fingerprints.
	FingerprintCount int `json:"fingerprintCount"`
}

// Summarize projects state Data into a Summary for the header line. It is
// side-effect free and safe to call on a freshly created (empty) Data.
func (sd *Data) Summarize() Summary {
	return Summary{
		Solution:         sd.Metadata.Solution,
		Version:          sd.Metadata.Version,
		SchemaVersion:    sd.SchemaVersion,
		LastUpdated:      sd.Metadata.LastUpdatedAt,
		ParameterCount:   len(sd.Parameters),
		ResolverCount:    len(sd.Resolvers),
		FingerprintCount: len(sd.Fingerprints),
	}
}

// timeType is the reflect.Type of time.Time, used to detect timestamp fields
// for consistent formatting.
var timeType = reflect.TypeOf(time.Time{})

// BuildListView reflects over state Data and projects it into ordered display
// sections. The transformation is schema-driven and side-effect free:
//   - each exported struct field becomes a key/value section,
//   - each exported map field becomes a section of key-sorted rows,
//   - top-level scalars are collected into a leading "overview" section,
//   - time.Time values are formatted with displayTimeFormat (zero -> empty).
//
// Because it is driven entirely by the Data type, adding a field to Data or to a
// persisted-entry type surfaces here automatically with no manual update.
func BuildListView(sd *Data) *ListView {
	view := &ListView{}
	root := reflect.ValueOf(sd).Elem()
	rootType := root.Type()

	overview := map[string]any{}

	for i := 0; i < rootType.NumField(); i++ {
		field := rootType.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonFieldName(field)
		if name == "" || name == "-" {
			continue
		}
		value := root.Field(i)

		switch {
		case value.Kind() == reflect.Struct && value.Type() != timeType:
			view.Sections = append(view.Sections, Section{
				Name:   name,
				Kind:   SectionKindKeyValue,
				Fields: structToDisplayMap(value),
			})
		case value.Kind() == reflect.Map:
			view.Sections = append(view.Sections, Section{
				Name: name,
				Kind: SectionKindRows,
				Rows: mapToDisplayRows(value),
			})
		default:
			overview[name] = displayValue(value)
		}
	}

	// Prepend an overview section for top-level scalars (e.g. schemaVersion) so
	// no schema field is silently dropped from the projection. Presentation
	// layers may omit it (see SectionNameOverview).
	if len(overview) > 0 {
		view.Sections = append([]Section{{
			Name:   SectionNameOverview,
			Kind:   SectionKindKeyValue,
			Fields: overview,
		}}, view.Sections...)
	}

	return view
}

// structToDisplayMap projects an exported struct's fields into a display map
// keyed by json-tag name, formatting time.Time values.
func structToDisplayMap(v reflect.Value) map[string]any {
	t := v.Type()
	out := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonFieldName(field)
		if name == "" || name == "-" {
			continue
		}
		out[name] = displayValue(v.Field(i))
	}
	return out
}

// mapToDisplayRows converts a string-keyed map into rows sorted by key. Each row
// carries a "key" column; struct (or pointer-to-struct) values are expanded into
// their display fields, while scalar values become a "value" column.
func mapToDisplayRows(m reflect.Value) []map[string]any {
	keys := sortedMapKeys(m)
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		row := map[string]any{"key": key}
		entry := deref(m.MapIndex(reflect.ValueOf(key)))
		switch {
		case entry.IsValid() && entry.Kind() == reflect.Struct && entry.Type() != timeType:
			for name, val := range structToDisplayMap(entry) {
				if name == "key" {
					continue // never let an entry field shadow the row key
				}
				row[name] = val
			}
		case entry.IsValid():
			row["value"] = displayValue(entry)
		default:
			row["value"] = nil
		}
		rows = append(rows, row)
	}
	return rows
}

// displayValue unwraps pointer/interface indirection and formats time.Time
// values with displayTimeFormat (zero -> empty string).
func displayValue(v reflect.Value) any {
	v = deref(v)
	if !v.IsValid() {
		return nil
	}
	if v.Type() == timeType {
		if t, ok := v.Interface().(time.Time); ok {
			return formatDisplayTime(t)
		}
		return ""
	}
	if v.CanInterface() {
		return v.Interface()
	}
	return nil
}

// deref unwraps pointer and interface indirection, returning an invalid Value
// for nil pointers/interfaces.
func deref(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// jsonFieldName returns the field's json-tag name, falling back to the Go field
// name when no json tag is present.
func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return field.Name
	}
	return name
}

// formatDisplayTime renders a timestamp in UTC using displayTimeFormat, or an
// empty string when the timestamp is zero.
func formatDisplayTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(displayTimeFormat)
}

// sortedMapKeys returns a string-keyed map's keys sorted lexicographically.
func sortedMapKeys(m reflect.Value) []string {
	keys := make([]string, 0, m.Len())
	for _, k := range m.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	return keys
}
