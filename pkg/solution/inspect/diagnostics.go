// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ExecutionDiagnostic holds a structured diagnosis of a resolver execution error.
// This is the domain-level representation that can be used by CLI, MCP,
// and future API consumers to build user-facing error messages.
type ExecutionDiagnostic struct {
	// Details is the human-readable description of the error.
	Details string `json:"details" yaml:"details" doc:"Detailed error description" maxLength:"4096" example:"2 resolver(s) failed, 1 succeeded"`

	// Suggestions are actionable steps to fix the error.
	Suggestions []string `json:"suggestions" yaml:"suggestions" doc:"Actionable fix suggestions" maxItems:"20"`
}

// DiagnoseExecutionError converts a resolver execution error into a structured
// diagnostic with typed analysis and actionable suggestions.
// The solution is used to inspect resolver configurations for provider-specific hints.
func DiagnoseExecutionError(err error, sol *solution.Solution) *ExecutionDiagnostic {
	var suggestions []string
	var details strings.Builder

	switch {
	case errors.As(err, new(*resolver.AggregatedExecutionError)):
		var aggErr *resolver.AggregatedExecutionError
		errors.As(err, &aggErr)

		fmt.Fprintf(&details, "%d resolver(s) failed", len(aggErr.Errors))
		if aggErr.SucceededCount > 0 {
			fmt.Fprintf(&details, ", %d succeeded", aggErr.SucceededCount)
		}
		if aggErr.SkippedCount > 0 {
			fmt.Fprintf(&details, ", %d skipped (failed dependencies: %s)",
				aggErr.SkippedCount, strings.Join(aggErr.SkippedNames, ", "))
		}
		details.WriteString("\n\nFailures:\n")

		for i, fr := range aggErr.Errors {
			fmt.Fprintf(&details, "  %d. resolver %q (phase %d): %s\n", i+1, fr.ResolverName, fr.Phase, fr.ErrMessage)
			AppendResolverHints(&suggestions, fr.Err, fr.ResolverName, sol)
		}

	case errors.As(err, new(*resolver.ExecutionError)):
		var execErr *resolver.ExecutionError
		errors.As(err, &execErr)

		fmt.Fprintf(&details, "Resolver %q failed in %s phase (step %d", execErr.ResolverName, execErr.Phase, execErr.Step)
		if execErr.Provider != "" {
			fmt.Fprintf(&details, ", provider: %s", execErr.Provider)
		}
		details.WriteString(")\n")
		fmt.Fprintf(&details, "Error: %v", execErr.Cause)

		AppendResolverHints(&suggestions, execErr.Cause, execErr.ResolverName, sol)
		if execErr.Provider == "http" {
			suggestions = append(suggestions, "HTTP provider returns {statusCode, body, headers} — use body.field, not field directly")
		}

	case errors.As(err, new(*resolver.TypeCoercionError)):
		var coercionErr *resolver.TypeCoercionError
		errors.As(err, &coercionErr)

		fmt.Fprintf(&details, "Resolver %q: cannot coerce %s → %s (after %s phase)\n",
			coercionErr.ResolverName, coercionErr.SourceType, coercionErr.TargetType, coercionErr.Phase)
		fmt.Fprintf(&details, "Error: %v", coercionErr.Cause)
		suggestions = append(suggestions,
			fmt.Sprintf("Add a transform step to convert %s to %s before the type coercion", coercionErr.SourceType, coercionErr.TargetType),
			"Check if the provider output shape matches the expected resolver type",
		)

	case errors.As(err, new(*resolver.AggregatedValidationError)):
		var valErr *resolver.AggregatedValidationError
		errors.As(err, &valErr)

		fmt.Fprintf(&details, "Resolver %q failed validation with %d error(s):\n", valErr.ResolverName, len(valErr.Failures))
		for i, f := range valErr.Failures {
			fmt.Fprintf(&details, "  %d. [rule %d] %s\n", i+1, f.Rule, f.Error())
		}
		suggestions = append(suggestions, "Review the validation rules in the resolver's validate section")

	case errors.As(err, new(*resolver.CircularDependencyError)):
		var circErr *resolver.CircularDependencyError
		errors.As(err, &circErr)

		fmt.Fprintf(&details, "Circular dependency detected: %s", strings.Join(circErr.Cycle, " → "))
		suggestions = append(suggestions, "Break the cycle by restructuring resolver dependencies or using a transform to combine data")

	default:
		details.WriteString(err.Error())
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Check resolver configuration and dependencies")
	}

	return &ExecutionDiagnostic{
		Details:     details.String(),
		Suggestions: suggestions,
	}
}

// AppendResolverHints inspects an error cause and adds provider-specific hints
// to the suggestions slice. This is exported so callers can build custom
// diagnostic flows while reusing the hint logic.
func AppendResolverHints(suggestions *[]string, cause error, resolverName string, sol *solution.Solution) {
	if cause == nil {
		return
	}
	msg := cause.Error()

	// HTTP provider: hint about envelope structure
	if strings.Contains(msg, "no such key") {
		if res, ok := sol.Spec.Resolvers[resolverName]; ok && res.Resolve != nil {
			for _, step := range res.Resolve.With {
				if step.Provider == "http" {
					*suggestions = append(*suggestions, fmt.Sprintf("Resolver %q uses the http provider which returns {statusCode, body, headers} — reference nested fields as body.<field>", resolverName))
					break
				}
			}
		}
	}

	// CEL expression errors
	if strings.Contains(msg, "undeclared reference") || strings.Contains(msg, "found no matching overload") {
		*suggestions = append(*suggestions, "Use list_cel_functions to see available CEL functions and their signatures")
	}
}
