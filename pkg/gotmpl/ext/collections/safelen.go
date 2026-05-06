// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"fmt"
	"reflect"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// SafeLenFunc returns an ExtFunction that provides a nil-safe len override.
// Unlike the built-in Go template len, this function returns 0 for nil values
// instead of panicking.
func SafeLenFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "len",
		Description: "Returns the length of a string, slice, array, map, or channel. " +
			"Unlike the built-in Go template len, this version returns 0 for nil values " +
			"instead of panicking.",
		Custom: true,
		Examples: []gotmpl.Example{
			{
				Description: "Get length of a list",
				Template:    `{{ len .items }}`,
			},
			{
				Description: "Safe nil check",
				Template:    `{{ eq (len .maybeNil) 0 }}`,
			},
		},
		Func: template.FuncMap{
			"len": SafeLen,
		},
	}
}

// SafeLen returns the length of the value, or 0 if the value is nil.
// It supports strings, slices, arrays, maps, channels, and pointers to any of these types.
func SafeLen(v any) (int, error) {
	if v == nil {
		return 0, nil
	}

	rv := reflect.ValueOf(v)

	// Dereference pointers so *[N]T, *[]T, *map, etc. work like builtins.
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() { //nolint:exhaustive // only length-able types are relevant
	case reflect.String, reflect.Array:
		return rv.Len(), nil
	case reflect.Slice, reflect.Map, reflect.Chan:
		if rv.IsNil() {
			return 0, nil
		}
		return rv.Len(), nil
	default:
		return 0, fmt.Errorf("len: cannot determine length of type %T", v)
	}
}
