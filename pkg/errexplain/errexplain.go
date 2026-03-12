// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package errexplain provides structured error explanation and pattern matching
// for scafctl errors. It converts raw error messages into categorized explanations
// with root cause analysis and actionable suggestions.
//
// This is a shared domain package used by CLI, MCP, and future API consumers.
package errexplain

import (
	"fmt"
	"regexp"
	"strings"
)

// Explanation is the structured response for an error explanation.
type Explanation struct {
	Category    string   `json:"category" yaml:"category" doc:"Error classification category" maxLength:"64" example:"resolver_execution"`
	Summary     string   `json:"summary" yaml:"summary" doc:"Short description of what went wrong" maxLength:"512" example:"Resolver myApi failed during the resolve phase"`
	RootCause   string   `json:"rootCause" yaml:"rootCause" doc:"Detailed root cause analysis" maxLength:"2048" example:"connection refused"`
	Suggestions []string `json:"suggestions" yaml:"suggestions" doc:"Actionable steps to fix the error" maxItems:"20"`
	RelatedDocs []string `json:"relatedDocs,omitempty" yaml:"relatedDocs,omitempty" doc:"Related documentation links" maxItems:"10"`
	Example     string   `json:"example,omitempty" yaml:"example,omitempty" doc:"Example fix or command" maxLength:"2048" example:"Add a transform step to convert the type"`
}

// pattern defines a pattern matcher for a known error type.
type pattern struct {
	name  string
	regex *regexp.Regexp
	build func(matches []string, fullError string) Explanation
}

// Explain parses an error message and returns a structured explanation if a
// known pattern matches. If no pattern matches, it returns a generic explanation.
func Explain(errText string) *Explanation {
	for _, p := range knownPatterns() {
		matches := p.regex.FindStringSubmatch(errText)
		if matches != nil {
			exp := p.build(matches, errText)
			return &exp
		}
	}

	generic := Explanation{
		Category:  "unknown",
		Summary:   "Could not categorize this error automatically",
		RootCause: errText,
		Suggestions: []string{
			"Use lint_solution to check for configuration issues",
			"Use inspect_solution to review the solution structure",
			"Use preview_resolvers with verbose=true to see detailed execution output",
			"Check that all referenced providers and resolvers exist",
		},
	}
	return &generic
}

// knownPatterns returns all recognized error patterns.
func knownPatterns() []pattern {
	return []pattern{
		{
			name:  "execution-error",
			regex: regexp.MustCompile(`resolver "([^"]+)" failed in (\w+) phase \(step (\d+)(?:, provider (\w+))?\): (.+)`),
			build: func(m []string, _ string) Explanation {
				resolverName := m[1]
				phase := m[2]
				providerName := m[4]
				cause := m[5]
				exp := Explanation{
					Category:  "resolver_execution",
					Summary:   fmt.Sprintf("Resolver %q failed during the %s phase", resolverName, phase),
					RootCause: cause,
					Suggestions: []string{
						fmt.Sprintf("Check the %s phase configuration for resolver %q", phase, resolverName),
						"Use inspect_solution to view the full resolver definition",
						"Use lint_solution to check for configuration issues",
					},
				}
				if providerName == "http" {
					exp.Suggestions = append(exp.Suggestions,
						"HTTP provider returns {statusCode, body, headers} - access response data via body.<field>",
						"Check that the URL is reachable and returns the expected format",
					)
				}
				if providerName == "cel" {
					exp.Suggestions = append(exp.Suggestions,
						"Use evaluate_cel to test the expression independently",
						"Use list_cel_functions to discover available functions",
					)
				}
				return exp
			},
		},
		{
			name:  "type-coercion",
			regex: regexp.MustCompile(`resolver "([^"]+)": type coercion from (\w+) to (\w+) failed after (\w+) phase: (.+)`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "type_coercion",
					Summary:   fmt.Sprintf("Cannot convert %s to %s for resolver %q", m[2], m[3], m[1]),
					RootCause: m[5],
					Suggestions: []string{
						fmt.Sprintf("Add a transform step to convert %s to %s before the resolver returns", m[2], m[3]),
						"Check that the provider output matches the declared resolver type",
						"If using the CEL provider, ensure the expression returns the correct type",
						fmt.Sprintf("Example transform: {\"provider\": \"cel\", \"inputs\": {\"expression\": \"%s(value)\"}}", m[3]),
					},
				}
			},
		},
		{
			name:  "validation-failed",
			regex: regexp.MustCompile(`resolver "([^"]+)" validation failed(?:: (.+)| with (\d+) errors?)`),
			build: func(m []string, _ string) Explanation {
				resolverName := m[1]
				exp := Explanation{
					Category:  "validation",
					Summary:   fmt.Sprintf("Resolver %q produced a value that failed validation", resolverName),
					RootCause: "The resolved value did not satisfy the validation rules defined in the validate phase",
					Suggestions: []string{
						fmt.Sprintf("Use inspect_solution to view the validation rules for resolver %q", resolverName),
						"Use preview_resolvers to see the actual resolved values",
						"Add or adjust validation rules to match the expected value format",
					},
				}
				if m[2] != "" {
					exp.RootCause = m[2]
				}
				return exp
			},
		},
		{
			name:  "circular-dependency",
			regex: regexp.MustCompile(`circular dependency detected: (.+)`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "dependency",
					Summary:   "Circular dependency in resolver graph",
					RootCause: fmt.Sprintf("The following resolvers form a cycle: %s", m[1]),
					Suggestions: []string{
						"Break the cycle by removing one of the dependsOn entries",
						"Consider merging the interdependent resolvers into one",
						"Use a transform step to combine data instead of circular references",
						"Use lint_solution to visualize the dependency graph",
					},
				}
			},
		},
		{
			name:  "cel-undeclared-ref",
			regex: regexp.MustCompile(`undeclared reference to '([^']+)'`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "cel_expression",
					Summary:   fmt.Sprintf("CEL expression references undefined variable %q", m[1]),
					RootCause: fmt.Sprintf("The variable %q is not available in the current CEL evaluation context", m[1]),
					Suggestions: []string{
						"Check that the referenced resolver exists and is declared as a dependency",
						"Available variables in CEL expressions: resolvers (resolved values), params (input parameters), metadata (solution metadata)",
						"Use evaluate_cel with the data parameter to test the expression with sample data",
						"Use list_cel_functions to see available functions",
					},
				}
			},
		},
		{
			name:  "cel-no-overload",
			regex: regexp.MustCompile(`found no matching overload for '([^']+)'`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "cel_expression",
					Summary:   fmt.Sprintf("CEL function %q called with wrong argument types", m[1]),
					RootCause: "The function exists but the argument types don't match any known signature",
					Suggestions: []string{
						fmt.Sprintf("Use list_cel_functions to check the signature of %q", m[1]),
						"Ensure input types match - e.g., string functions need string arguments",
						"Use type() to inspect the actual type of a value in CEL",
					},
				}
			},
		},
		{
			name:  "no-such-key",
			regex: regexp.MustCompile(`no such key: (\w+)`),
			build: func(m []string, full string) Explanation {
				exp := Explanation{
					Category:  "data_access",
					Summary:   fmt.Sprintf("Attempted to access non-existent key %q", m[1]),
					RootCause: "The data structure does not contain the referenced key",
					Suggestions: []string{
						"Use preview_resolvers to inspect the actual data shape returned by the provider",
						"Check for typos in the key name",
						"Use has() in CEL to safely check if a key exists before accessing it",
					},
				}
				if strings.Contains(full, "http") || strings.Contains(full, "statusCode") {
					exp.Suggestions = append(exp.Suggestions,
						"HTTP provider returns {statusCode, body, headers} - access response fields via body.<field>",
					)
				}
				return exp
			},
		},
		{
			name:  "phase-timeout",
			regex: regexp.MustCompile(`phase (\d+) timed out(?: with (\d+) resolvers? still waiting)?`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "timeout",
					Summary:   fmt.Sprintf("Phase %s timed out", m[1]),
					RootCause: "One or more resolvers in this execution phase took too long to complete",
					Suggestions: []string{
						"Increase the resolver timeout from the default 30s",
						"Check if the provider target (API, command) is responsive",
						"For HTTP providers, verify the endpoint is reachable",
						"Consider breaking slow resolvers into smaller steps",
					},
				}
			},
		},
		{
			name:  "value-size",
			regex: regexp.MustCompile(`resolver "([^"]+)" value size (\d+) bytes exceeds maximum (\d+) bytes`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "value_size",
					Summary:   fmt.Sprintf("Resolver %q produced a value exceeding the size limit", m[1]),
					RootCause: fmt.Sprintf("Value is %s bytes but the maximum is %s bytes", m[2], m[3]),
					Suggestions: []string{
						"Add a transform step to filter or reduce the data before returning",
						"Use CEL expressions like size(), filter(), or map() to trim results",
						"Consider fetching only the fields you need from the source",
					},
				}
			},
		},
		{
			name:  "foreach-type",
			regex: regexp.MustCompile(`resolver "([^"]+)" transform step (\d+): forEach requires array input, got (\w+)`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "type_mismatch",
					Summary:   fmt.Sprintf("forEach in resolver %q expected an array but got %s", m[1], m[3]),
					RootCause: fmt.Sprintf("Transform step %s uses forEach which requires an array input, but the current value is type %s", m[2], m[3]),
					Suggestions: []string{
						"Add a transform step before forEach to convert the value to an array",
						"Use [value] to wrap a single value into an array if needed",
						"Check that the resolve phase returns an array for this resolver",
					},
				}
			},
		},
		{
			name:  "aggregated-execution",
			regex: regexp.MustCompile(`(\d+) resolver\(s\) failed`),
			build: func(m []string, _ string) Explanation {
				return Explanation{
					Category:  "multiple_failures",
					Summary:   fmt.Sprintf("%s resolvers failed during execution", m[1]),
					RootCause: "Multiple resolvers encountered errors. Failures may cascade if resolvers depend on each other.",
					Suggestions: []string{
						"Fix the resolver errors starting from those with no dependencies (earliest phase)",
						"Use preview_resolvers to see which resolvers succeeded and which failed",
						"Skipped resolvers are typically caused by their dependencies failing - fix their dependencies first",
						"Use lint_solution to check for configuration issues across all resolvers",
					},
				}
			},
		},
	}
}
