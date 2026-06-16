// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package url

import (
	"net/url"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// EncodeFunc returns a CEL function that URL-encodes a string using
// application/x-www-form-urlencoded encoding (spaces become +, special
// characters become %XX). Suitable for query parameter values.
//
// Example usage:
//
//	url.encode("hello world") // Returns: "hello+world"
func EncodeFunc() celexp.ExtFunction {
	funcName := "url.encode"
	return celexp.ExtFunction{
		Name:          funcName,
		Signature:     "url.encode(string) -> string",
		Description:   "URL-encodes a string using application/x-www-form-urlencoded encoding (spaces become +). Use url.encode(value) to safely embed values in URL query parameters",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Encode a string with spaces",
				Expression:  `url.encode("hello world")`,
			},
			{
				Description: "Encode special characters for use in a URL query",
				Expression:  `url.encode("key=value&other=a b")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						str, ok := value.Value().(string)
						if !ok {
							return types.NewErr("url.encode: expected string argument, got %s", value.Type())
						}
						return types.String(url.QueryEscape(str))
					}),
				),
			),
		},
	}
}

// DecodeFunc returns a CEL function that decodes a URL form-encoded string
// (application/x-www-form-urlencoded). Both %XX sequences and "+" (as space)
// are decoded.
//
// Example usage:
//
//	url.decode("hello+world") // Returns: "hello world"
func DecodeFunc() celexp.ExtFunction {
	funcName := "url.decode"
	return celexp.ExtFunction{
		Name:          funcName,
		Signature:     "url.decode(string) -> string",
		Description:   "Decodes a URL form-encoded string (application/x-www-form-urlencoded) back to its original form. Both %XX sequences and + (as space) are decoded. Use url.decode(encoded) to reverse url.encode",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Decode a percent-encoded string",
				Expression:  `url.decode("hello%20world")`,
			},
			{
				Description: "Decode plus-encoded spaces",
				Expression:  `url.decode("hello+world")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(value ref.Val) ref.Val {
						str, ok := value.Value().(string)
						if !ok {
							return types.NewErr("url.decode: expected string argument, got %s", value.Type())
						}
						decoded, err := url.QueryUnescape(str)
						if err != nil {
							return types.NewErr("url.decode: %s", err.Error())
						}
						return types.String(decoded)
					}),
				),
			),
		},
	}
}
