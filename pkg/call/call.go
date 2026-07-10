// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package call implements the bind and expand engine for parameterized provider
// calls (spec.calls). It is a pure utility over pkg/spec: it binds already-resolved
// call-site argument values against a Call definition's declared arguments and
// builds the enriched evaluation data that exposes those arguments under the
// "args" namespace. It deliberately imports only pkg/spec so that provider
// dispatch (which lives in the resolver and action executors) can depend on it
// without creating an import cycle.
package call

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// ArgsNamespace is the key under which bound arguments are exposed in the
// evaluation data. Definition inputs reference arguments as _.args.x (CEL) and
// {{ .args.x }} (Go templates).
const ArgsNamespace = "args"

// BindArgs binds already-resolved call-site argument values against a Call
// definition's ArgDefs. It rejects unknown arguments, enforces required
// arguments, applies defaults for omitted optional arguments, and coerces every
// value to its declared type. The returned map contains exactly the declared
// arguments. callName is included in all error messages so diagnostics are
// call-site aware.
func BindArgs(callName string, def *spec.Call, resolved map[string]any) (map[string]any, error) {
	if def == nil {
		return nil, fmt.Errorf("call %q: nil definition", callName)
	}

	// Reject arguments that are not declared by the definition.
	if err := checkUnknownArgs(callName, def, resolved); err != nil {
		return nil, err
	}

	bound := make(map[string]any, len(def.Args))
	// Iterate declared arguments in sorted order so that, when multiple
	// arguments are missing or invalid, the first error returned is
	// deterministic (map iteration order is randomized in Go).
	for _, name := range declaredArgNames(def) {
		argDef := def.Args[name]
		if argDef == nil {
			argDef = &spec.ArgDef{}
		}

		value, supplied := resolved[name]
		switch {
		case supplied:
			// value used as-is below
		case argDef.Required:
			return nil, fmt.Errorf("call %q: missing required argument %q", callName, name)
		case argDef.Default != nil:
			value = argDef.Default
		default:
			// Omitted optional argument with no default: use the type zero value
			// so downstream inputs (e.g. structured bodies) can operate on it.
			value = nil
		}

		coerced, err := spec.CoerceType(value, argDef.Type)
		if err != nil {
			return nil, fmt.Errorf("call %q: argument %q: expected %s, got %T (%w)", callName, name, argType(argDef.Type), value, err)
		}
		bound[name] = coerced
	}

	return bound, nil
}

// checkUnknownArgs returns an error naming every call-site argument that is not
// declared by the definition.
func checkUnknownArgs(callName string, def *spec.Call, resolved map[string]any) error {
	var unknown []string
	for name := range resolved {
		if _, ok := def.Args[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	declared := declaredArgNames(def)
	return fmt.Errorf("call %q: unknown argument(s) %s (declared args: [%s])",
		callName, quoteList(unknown), strings.Join(declared, ", "))
}

// declaredArgNames returns the sorted list of argument names declared by the
// definition.
func declaredArgNames(def *spec.Call) []string {
	names := make([]string, 0, len(def.Args))
	for name := range def.Args {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

// argType returns a display string for an argument type, defaulting to "any"
// when the type is unset.
func argType(t spec.Type) spec.Type {
	if t == "" {
		return spec.TypeAny
	}
	return t
}

// ExpandData returns a shallow copy of resolverData with the bound arguments
// injected under the args namespace so definition inputs and the invoked
// provider can reference them as _.args.x and {{ .args.x }}. The input map is
// not mutated.
func ExpandData(resolverData, boundArgs map[string]any) map[string]any {
	enriched := make(map[string]any, len(resolverData)+1)
	for k, v := range resolverData {
		enriched[k] = v
	}
	enriched[ArgsNamespace] = boundArgs
	return enriched
}

// DedupKey builds a stable, canonical de-duplication key for a call invocation.
// The key combines the call name with a canonical JSON encoding of the bound
// arguments so that logically-equal argument sets collide regardless of the
// order in which they were written. Bound arguments must already be coerced.
func DedupKey(callName string, boundArgs map[string]any) (string, error) {
	// encoding/json sorts map[string]any keys, so marshaling the bound args
	// yields a canonical form for equal argument sets.
	data, err := json.Marshal(boundArgs)
	if err != nil {
		return "", fmt.Errorf("call %q: failed to canonicalize args for dedup: %w", callName, err)
	}
	return callName + "\x00" + string(data), nil
}
