// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package strings

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/dnslabel"
)

// SlugifyFunc returns a CEL extension function that converts a string into a
// DNS-safe label (RFC 1123). It shares its implementation with the go-template
// slugify/toDnsString functions via pkg/dnslabel so both engines produce
// identical output.
func SlugifyFunc() celexp.ExtFunction {
	funcName := "strings.slugify"
	return celexp.ExtFunction{
		Name:      funcName,
		Signature: "strings.slugify(string) -> string",
		Description: "Converts a string into a DNS-safe label (RFC 1123): lowercases the input, " +
			"replaces non-alphanumeric characters with hyphens, collapses consecutive hyphens, " +
			"strips leading/trailing hyphens, and truncates to 63 characters",
		FunctionNames: []string{funcName},
		Custom:        true,
		Links:         []string{"https://tools.ietf.org/html/rfc1123"},
		Examples: []celexp.Example{
			{
				Description: "Sanitize a name into a DNS label",
				Expression:  `strings.slugify("My_Org--Name! (test)")`,
			},
			{
				Description: "Handle special characters and truncation",
				Expression:  `strings.slugify("a]very/long+string...")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(inputStringRef ref.Val) ref.Val {
						str, ok := inputStringRef.Value().(string)
						if !ok {
							return types.NewErr("strings.slugify: expected string argument, got %s", inputStringRef.Type())
						}
						return types.String(dnslabel.Slugify(str))
					}),
				),
			),
		},
	}
}
