// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// deprecatedFieldMeta describes a deprecated struct field discovered via the
// struct tags read by reflection. It is the bridge between the Go type system
// (where deprecation is declared via `deprecated:"true"` and friends) and the
// linter (which reports findings against YAML keys).
type deprecatedFieldMeta struct {
	yamlName    string
	replacement string
	message     string
}

// lookupDeprecatedField inspects the struct type of proto for the given Go field
// name and returns its deprecation metadata when the field is marked deprecated.
// proto may be a value or pointer to a struct. This keeps the deprecation rule
// generic: it derives its messaging entirely from struct tags, so deprecating a
// new field only requires tagging it (deprecated, deprecatedReplacement,
// deprecatedMessage) and adding a traversal call below.
func lookupDeprecatedField(proto any, goFieldName string) (deprecatedFieldMeta, bool) {
	t := reflect.TypeOf(proto)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return deprecatedFieldMeta{}, false
	}
	f, ok := t.FieldByName(goFieldName)
	if !ok || f.Tag.Get("deprecated") != "true" {
		return deprecatedFieldMeta{}, false
	}
	return deprecatedFieldMeta{
		yamlName:    yamlTagName(f),
		replacement: f.Tag.Get("deprecatedReplacement"),
		message:     f.Tag.Get("deprecatedMessage"),
	}, true
}

// yamlTagName extracts the YAML key name from a struct field's yaml/json tag,
// falling back to the lowercased Go field name.
func yamlTagName(f reflect.StructField) string {
	for _, key := range []string{"yaml", "json"} {
		if tag := f.Tag.Get(key); tag != "" {
			if name := strings.Split(tag, ",")[0]; name != "" && name != "-" {
				return name
			}
		}
	}
	return strings.ToLower(f.Name)
}

// emitDeprecated records a finding when a deprecated field is set. When the
// replacement field is also set on the same object it emits a conflict error
// (the replacement wins at runtime, so the deprecated key should be removed);
// otherwise it emits a deprecation warning that names the replacement.
func emitDeprecated(result *Result, proto any, goFieldName string, deprecatedSet, replacementSet bool, location string) { //nolint:unparam // goFieldName is parameterized so the helper is reusable for future deprecated fields
	if !deprecatedSet {
		return
	}
	meta, ok := lookupDeprecatedField(proto, goFieldName)
	if !ok {
		return
	}
	fieldLoc := location + "." + meta.yamlName

	if replacementSet && meta.replacement != "" {
		result.addFinding(SeverityError, "deprecation", fieldLoc,
			fmt.Sprintf("both '%s' (deprecated) and '%s' are set; '%s' takes precedence",
				meta.yamlName, meta.replacement, meta.replacement),
			fmt.Sprintf("Remove the deprecated '%s' field and keep '%s'.", meta.yamlName, meta.replacement),
			"deprecated-field-conflict")
		return
	}

	msg := fmt.Sprintf("field '%s' is deprecated", meta.yamlName)
	if meta.replacement != "" {
		msg += fmt.Sprintf("; use '%s' instead", meta.replacement)
	}
	if meta.message != "" {
		msg += ". " + meta.message
	}
	suggestion := ""
	if meta.replacement != "" {
		suggestion = fmt.Sprintf("Replace '%s' with '%s'.", meta.yamlName, meta.replacement)
	}
	result.addFinding(SeverityWarning, "deprecation", fieldLoc, msg, suggestion, "deprecated-field")
}

// lintDeprecatedFields walks the parsed solution and reports use of deprecated
// fields. Because it traverses the typed spec (not the raw YAML), each check is
// inherently scoped to the correct struct type — an unrelated map key that
// happens to share a deprecated field's name will not trigger a false positive.
func lintDeprecatedFields(sol *solution.Solution, result *Result) {
	srcProto := resolver.ProviderSource{}
	transformProto := resolver.ProviderTransform{}
	actionProto := action.Action{}
	forEachProto := spec.ForEachClause{}

	for name, res := range sol.Spec.Resolvers {
		if res == nil {
			continue
		}
		loc := fmt.Sprintf("resolvers.%s", name)

		if res.Resolve != nil {
			for i := range res.Resolve.With {
				s := &res.Resolve.With[i]
				emitDeprecated(result, srcProto, "OnError",
					s.OnError != "", s.ContinueOnError != nil,
					fmt.Sprintf("%s.resolve.with[%d]", loc, i))
			}
		}
		if res.Transform != nil {
			for i := range res.Transform.With {
				t := &res.Transform.With[i]
				emitDeprecated(result, transformProto, "OnError",
					t.OnError != "", t.ContinueOnError != nil,
					fmt.Sprintf("%s.transform.with[%d]", loc, i))
			}
		}
	}

	if sol.Spec.Workflow == nil {
		return
	}
	for name, act := range sol.Spec.Workflow.Actions {
		emitActionDeprecations(result, act, actionProto, forEachProto,
			fmt.Sprintf("workflow.actions.%s", name))
	}
	for name, act := range sol.Spec.Workflow.Finally {
		emitActionDeprecations(result, act, actionProto, forEachProto,
			fmt.Sprintf("workflow.finally.%s", name))
	}
}

func emitActionDeprecations(result *Result, act *action.Action, actionProto action.Action, forEachProto spec.ForEachClause, location string) {
	if act == nil {
		return
	}
	//nolint:staticcheck // intentionally reading the deprecated field to report its use
	emitDeprecated(result, actionProto, "OnError",
		act.OnError != "", act.ContinueOnError != nil, location)
	if act.ForEach != nil {
		//nolint:staticcheck // intentionally reading the deprecated field to report its use
		emitDeprecated(result, forEachProto, "OnError",
			act.ForEach.OnError != "", act.ForEach.ContinueOnError != nil,
			location+".forEach")
	}
}
