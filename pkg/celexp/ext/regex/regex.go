// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package regex

import (
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// MatchFunc returns a CEL extension function that tests if a string matches a regular expression.
// The function takes two string arguments: a regex pattern and an input string.
// Returns true if the pattern matches anywhere in the input string.
//
// CEL usage:
//
//	regex.match("^test", "testing")    // true
//	regex.match("[0-9]+", "abc")       // false
func MatchFunc() celexp.ExtFunction {
	funcName := "regex.match"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Tests if a string matches a regular expression pattern (RE2 syntax). Returns true if the pattern matches anywhere in the input string",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Match a pattern at the start of a string",
				Expression:  `regex.match("^Hello", "Hello World")`,
			},
			{
				Description: "Match digits in a string",
				Expression:  `regex.match("[0-9]+", "abc123")`,
			},
			{
				Description: "No match returns false",
				Expression:  `regex.match("^xyz", "Hello World")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType, cel.StringType},
					cel.BoolType,
					cel.BinaryBinding(func(patternRef, inputRef ref.Val) ref.Val {
						pattern, ok := patternRef.Value().(string)
						if !ok {
							return types.NewErr("regex.match: expected string pattern, got %s", patternRef.Type())
						}
						input, ok := inputRef.Value().(string)
						if !ok {
							return types.NewErr("regex.match: expected string input, got %s", inputRef.Type())
						}
						re, err := regexp.Compile(pattern)
						if err != nil {
							return types.NewErr("regex.match: invalid regex pattern %q: %v", pattern, err)
						}
						return types.Bool(re.MatchString(input))
					}),
				),
			),
		},
	}
}

// ReplaceFunc returns a CEL extension function that replaces all occurrences of a regex
// pattern in a string with a replacement string.
//
// CEL usage:
//
//	regex.replace("Hello World", "[Ww]orld", "Go")     // "Hello Go"
//	regex.replace("abc123def456", "[0-9]+", "#")        // "abc#def#"
func ReplaceFunc() celexp.ExtFunction {
	funcName := "regex.replace"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Replaces all occurrences of a regular expression pattern (RE2 syntax) in a string with a replacement string",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Replace all digits with a hash",
				Expression:  `regex.replace("abc123def456", "[0-9]+", "#")`,
			},
			{
				Description: "Replace whitespace with dashes",
				Expression:  `regex.replace("hello world foo", "\\s+", "-")`,
			},
			{
				Description: "Remove all non-alphanumeric characters",
				Expression:  `regex.replace("hello! @world#", "[^a-zA-Z0-9]", "")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						if len(args) != 3 {
							return types.NewErr("regex.replace: expected 3 arguments (input, pattern, replacement), got %d", len(args))
						}
						input, ok := args[0].Value().(string)
						if !ok {
							return types.NewErr("regex.replace: expected string input, got %s", args[0].Type())
						}
						pattern, ok := args[1].Value().(string)
						if !ok {
							return types.NewErr("regex.replace: expected string pattern, got %s", args[1].Type())
						}
						replacement, ok := args[2].Value().(string)
						if !ok {
							return types.NewErr("regex.replace: expected string replacement, got %s", args[2].Type())
						}
						re, err := regexp.Compile(pattern)
						if err != nil {
							return types.NewErr("regex.replace: invalid regex pattern %q: %v", pattern, err)
						}
						return types.String(re.ReplaceAllString(input, replacement))
					}),
				),
			),
		},
	}
}

// FindAllFunc returns a CEL extension function that finds all matches of a regex pattern
// in a string and returns them as a list of strings.
//
// CEL usage:
//
//	regex.findAll("[0-9]+", "abc123def456")    // ["123", "456"]
//	regex.findAll("[a-z]+", "ABC")             // []
func FindAllFunc() celexp.ExtFunction {
	funcName := "regex.findAll"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Finds all matches of a regular expression pattern (RE2 syntax) in a string and returns them as a list of strings",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Find all digit sequences",
				Expression:  `regex.findAll("[0-9]+", "abc123def456")`,
			},
			{
				Description: "Find all words",
				Expression:  `regex.findAll("[a-zA-Z]+", "hello 123 world 456")`,
			},
			{
				Description: "No matches returns empty list",
				Expression:  `regex.findAll("[0-9]+", "no digits here")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType, cel.StringType},
					cel.ListType(cel.StringType),
					cel.BinaryBinding(func(patternRef, inputRef ref.Val) ref.Val {
						pattern, ok := patternRef.Value().(string)
						if !ok {
							return types.NewErr("regex.findAll: expected string pattern, got %s", patternRef.Type())
						}
						input, ok := inputRef.Value().(string)
						if !ok {
							return types.NewErr("regex.findAll: expected string input, got %s", inputRef.Type())
						}
						re, err := regexp.Compile(pattern)
						if err != nil {
							return types.NewErr("regex.findAll: invalid regex pattern %q: %v", pattern, err)
						}
						matches := re.FindAllString(input, -1)
						if matches == nil {
							matches = []string{}
						}
						celList := make([]ref.Val, len(matches))
						for i, m := range matches {
							celList[i] = types.String(m)
						}
						return types.DefaultTypeAdapter.NativeToValue(celList)
					}),
				),
			),
		},
	}
}

// SplitFunc returns a CEL extension function that splits a string by a regex pattern
// and returns a list of strings.
//
// CEL usage:
//
//	regex.split("\\s+", "hello   world")   // ["hello", "world"]
//	regex.split("[,;]+", "a,b;c,,d")       // ["a", "b", "c", "d"]
func SplitFunc() celexp.ExtFunction {
	funcName := "regex.split"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Splits a string by a regular expression pattern (RE2 syntax) and returns a list of strings",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Split by whitespace",
				Expression:  `regex.split("\\s+", "hello   world   foo")`,
			},
			{
				Description: "Split by multiple delimiters",
				Expression:  `regex.split("[,;]+", "a,b;c,,d")`,
			},
			{
				Description: "Split by digits",
				Expression:  `regex.split("[0-9]+", "abc123def456ghi")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType, cel.StringType},
					cel.ListType(cel.StringType),
					cel.BinaryBinding(func(patternRef, inputRef ref.Val) ref.Val {
						pattern, ok := patternRef.Value().(string)
						if !ok {
							return types.NewErr("regex.split: expected string pattern, got %s", patternRef.Type())
						}
						input, ok := inputRef.Value().(string)
						if !ok {
							return types.NewErr("regex.split: expected string input, got %s", inputRef.Type())
						}
						re, err := regexp.Compile(pattern)
						if err != nil {
							return types.NewErr("regex.split: invalid regex pattern %q: %v", pattern, err)
						}
						parts := re.Split(input, -1)
						celList := make([]ref.Val, len(parts))
						for i, p := range parts {
							celList[i] = types.String(p)
						}
						return types.DefaultTypeAdapter.NativeToValue(celList)
					}),
				),
			),
		},
	}
}
