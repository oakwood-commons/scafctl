// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package strings

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	cleanReplacer = strings.NewReplacer("-", "", "_", "", " ", "")
	titleCaser    = cases.Title(language.English)
)

// cleanString removes hyphens, underscores, and spaces from a lowercased string.
func cleanString(s string) string {
	return cleanReplacer.Replace(strings.ToLower(s))
}

// titleString converts a string to title case using English language rules.
func titleString(s string) string {
	return titleCaser.String(s)
}

func CleanFunc() celexp.ExtFunction {
	funcName := "strings.clean"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Cleans a string by converting it to lowercase and removing hyphens, underscores, and spaces",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Clean a string with mixed separators",
				Expression:  `strings.clean("My-String_Name Test")`,
			},
			{
				Description: "Clean an uppercase string with hyphens",
				Expression:  `strings.clean("HELLO-WORLD")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(dirtyStringRef ref.Val) ref.Val {
						str, ok := dirtyStringRef.Value().(string)
						if !ok {
							return types.NewErr("strings.clean: expected string argument, got %s", dirtyStringRef.Type())
						}
						return types.String(cleanString(str))
					}),
				),
			),
		},
	}
}

func TitleFunc() celexp.ExtFunction {
	funcName := "strings.title"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Converts a string to title case using English language rules",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Convert a lowercase string to title case",
				Expression:  `strings.title("hello world")`,
			},
			{
				Description: "Convert uppercase to proper title case",
				Expression:  `strings.title("HELLO WORLD")`,
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
							return types.NewErr("strings.title: expected string argument, got %s", inputStringRef.Type())
						}
						return types.String(titleString(str))
					}),
				),
			),
		},
	}
}

// maxRepeatLen is the maximum total length allowed for strings.repeat output
// to prevent excessive memory allocation.
const maxRepeatLen = 1 << 20 // 1 MiB

func RepeatFunc() celexp.ExtFunction {
	funcName := "strings.repeat"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Repeats a string a given number of times",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Repeat a string three times",
				Expression:  `strings.repeat("ab", 3)`,
			},
			{
				Description: "Create a separator line",
				Expression:  `strings.repeat("-", 40)`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType, cel.IntType},
					cel.StringType,
					cel.BinaryBinding(func(strRef, countRef ref.Val) ref.Val {
						str, ok := strRef.Value().(string)
						if !ok {
							return types.NewErr("strings.repeat: expected string as first argument, got %s", strRef.Type())
						}
						count, ok := countRef.Value().(int64)
						if !ok {
							return types.NewErr("strings.repeat: expected int as second argument, got %s", countRef.Type())
						}
						if count < 0 {
							return types.NewErr("strings.repeat: count must be non-negative, got %d", count)
						}
						if len(str) == 0 {
							return types.String("")
						}
						if count > int64(maxRepeatLen)/int64(len(str)) {
							return types.NewErr("strings.repeat: result would exceed maximum length (%d bytes)", maxRepeatLen)
						}
						return types.String(strings.Repeat(str, int(count)))
					}),
				),
			),
		},
	}
}
