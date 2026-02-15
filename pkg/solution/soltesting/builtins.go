// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import "strings"

// Builtin test name prefix.
const builtinPrefix = "builtin:"

// Builtin test names (without prefix, for skipBuiltins matching).
const (
	BuiltinParse          = "parse"
	BuiltinLint           = "lint"
	BuiltinResolveDefault = "resolve-defaults"
	BuiltinRenderDefault  = "render-defaults"
)

// BuiltinTests returns the builtin test cases, filtered by testConfig.skipBuiltins.
// If testConfig is nil, all builtins are returned.
func BuiltinTests(testConfig *TestConfig) []*TestCase {
	all := allBuiltins()

	if testConfig == nil {
		return all
	}

	if testConfig.SkipBuiltins.All {
		return nil
	}

	if len(testConfig.SkipBuiltins.Names) == 0 {
		return all
	}

	var filtered []*TestCase
	for _, tc := range all {
		shortName := strings.TrimPrefix(tc.Name, builtinPrefix)
		if !shouldSkipBuiltin(shortName, testConfig.SkipBuiltins) {
			filtered = append(filtered, tc)
		}
	}

	return filtered
}

// shouldSkipBuiltin checks if a specific builtin (by short name) is skipped.
func shouldSkipBuiltin(name string, skipValue SkipBuiltinsValue) bool {
	if skipValue.All {
		return true
	}
	for _, n := range skipValue.Names {
		if n == name {
			return true
		}
	}
	return false
}

// BuiltinName returns the full builtin name with the "builtin:" prefix.
func BuiltinName(shortName string) string {
	return builtinPrefix + shortName
}

// IsBuiltin returns true if the test name starts with "builtin:".
func IsBuiltin(name string) bool {
	return strings.HasPrefix(name, builtinPrefix)
}

// allBuiltins returns all four builtin test cases in alphabetical order.
func allBuiltins() []*TestCase {
	exitCodeZero := 0
	return []*TestCase{
		{
			Name:        BuiltinName(BuiltinLint),
			Description: "Verify solution has no lint errors",
			Command:     []string{"lint"},
			Assertions: []Assertion{
				{Expression: `exitCode == 0`},
			},
		},
		{
			Name:        BuiltinName(BuiltinParse),
			Description: "Verify solution YAML parses without errors",
			// No command — parse is validated internally by the runner
			// by checking if the solution loaded successfully.
			// The runner sets pass/error based on the parse result.
			ExitCode: &exitCodeZero,
		},
		{
			Name:        BuiltinName(BuiltinRenderDefault),
			Description: "Verify solution renders with default values",
			Command:     []string{"render", "solution"},
			ExitCode:    &exitCodeZero,
		},
		{
			Name:        BuiltinName(BuiltinResolveDefault),
			Description: "Verify all resolvers resolve with default values",
			Command:     []string{"run", "resolver"},
			ExitCode:    &exitCodeZero,
		},
	}
}
