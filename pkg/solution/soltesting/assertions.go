// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// EvaluateAssertions evaluates all assertions against the command output.
// It never short-circuits — all assertions are evaluated even if some fail.
func EvaluateAssertions(ctx context.Context, assertions []Assertion, cmdOutput *CommandOutput) []AssertionResult {
	assertionCtx := BuildAssertionContext(cmdOutput)
	results := make([]AssertionResult, 0, len(assertions))

	for _, a := range assertions {
		result := evaluateOne(ctx, a, cmdOutput, assertionCtx)
		results = append(results, result)
	}

	return results
}

// evaluateOne evaluates a single assertion.
func evaluateOne(ctx context.Context, a Assertion, cmdOutput *CommandOutput, celCtx map[string]any) AssertionResult {
	switch {
	case a.Expression != "":
		return evaluateExpression(ctx, a, celCtx)
	case a.Regex != "":
		return evaluateRegex(a, cmdOutput)
	case a.Contains != "":
		return evaluateContains(a, cmdOutput)
	case a.NotRegex != "":
		return evaluateNotRegex(a, cmdOutput)
	case a.NotContains != "":
		return evaluateNotContains(a, cmdOutput)
	default:
		return AssertionResult{
			Type:    "unknown",
			Passed:  false,
			Message: "no assertion type set",
		}
	}
}

// evaluateExpression evaluates a CEL expression assertion.
func evaluateExpression(ctx context.Context, a Assertion, celCtx map[string]any) AssertionResult {
	result := AssertionResult{
		Type:  "expression",
		Input: string(a.Expression),
	}

	// Check if __output is nil and expression references it
	if celCtx["__output"] == nil && strings.Contains(string(a.Expression), "__output") {
		result.Passed = false
		result.Message = formatMessage(a.Message,
			"variable '__output' is nil — this command does not support structured output or -o json was not specified")
		return result
	}

	evalResult, err := celexp.EvaluateExpression(ctx, string(a.Expression), nil, celCtx)
	if err != nil {
		result.Passed = false
		result.Message = formatMessage(a.Message, fmt.Sprintf("CEL evaluation error: %s", err))
		return result
	}

	boolResult, ok := evalResult.(bool)
	if !ok {
		result.Passed = false
		result.Message = formatMessage(a.Message,
			fmt.Sprintf("CEL expression must evaluate to bool, got %T: %v", evalResult, evalResult))
		result.Actual = evalResult
		return result
	}

	result.Passed = boolResult
	if !boolResult {
		diagnostic := DiagnoseExpression(ctx, string(a.Expression), celCtx)
		result.Message = formatMessage(a.Message, diagnostic)
	}

	return result
}

// evaluateRegex evaluates a regex assertion.
func evaluateRegex(a Assertion, cmdOutput *CommandOutput) AssertionResult {
	result := AssertionResult{
		Type:  "regex",
		Input: a.Regex,
	}

	target := resolveTarget(cmdOutput, a.Target)
	re, err := regexp.Compile(a.Regex)
	if err != nil {
		result.Passed = false
		result.Message = formatMessage(a.Message, fmt.Sprintf("invalid regex: %s", err))
		return result
	}

	result.Passed = re.MatchString(target)
	if !result.Passed {
		result.Message = formatMessage(a.Message,
			fmt.Sprintf("regex %q did not match %s output", a.Regex, targetName(a.Target)))
		result.Actual = truncate(target, 200)
	}

	return result
}

// evaluateContains evaluates a contains assertion.
func evaluateContains(a Assertion, cmdOutput *CommandOutput) AssertionResult {
	result := AssertionResult{
		Type:  "contains",
		Input: a.Contains,
	}

	target := resolveTarget(cmdOutput, a.Target)
	result.Passed = strings.Contains(target, a.Contains)
	if !result.Passed {
		result.Message = formatMessage(a.Message,
			fmt.Sprintf("substring %q not found in %s output", a.Contains, targetName(a.Target)))
		result.Actual = truncate(target, 200)
	}

	return result
}

// evaluateNotRegex evaluates a notRegex assertion.
func evaluateNotRegex(a Assertion, cmdOutput *CommandOutput) AssertionResult {
	result := AssertionResult{
		Type:  "notRegex",
		Input: a.NotRegex,
	}

	target := resolveTarget(cmdOutput, a.Target)
	re, err := regexp.Compile(a.NotRegex)
	if err != nil {
		result.Passed = false
		result.Message = formatMessage(a.Message, fmt.Sprintf("invalid regex: %s", err))
		return result
	}

	match := re.FindString(target)
	result.Passed = match == ""
	if !result.Passed {
		result.Message = formatMessage(a.Message,
			fmt.Sprintf("regex %q unexpectedly matched in %s output: %q", a.NotRegex, targetName(a.Target), match))
	}

	return result
}

// evaluateNotContains evaluates a notContains assertion.
func evaluateNotContains(a Assertion, cmdOutput *CommandOutput) AssertionResult {
	result := AssertionResult{
		Type:  "notContains",
		Input: a.NotContains,
	}

	target := resolveTarget(cmdOutput, a.Target)
	result.Passed = !strings.Contains(target, a.NotContains)
	if !result.Passed {
		result.Message = formatMessage(a.Message,
			fmt.Sprintf("substring %q unexpectedly found in %s output", a.NotContains, targetName(a.Target)))
	}

	return result
}

// resolveTarget returns the appropriate output text based on the target field.
func resolveTarget(cmdOutput *CommandOutput, target string) string {
	if cmdOutput == nil {
		return ""
	}

	switch target {
	case "stderr":
		return cmdOutput.Stderr
	case "combined":
		return cmdOutput.Stdout + "\n" + cmdOutput.Stderr
	default: // "", "stdout"
		return cmdOutput.Stdout
	}
}

// targetName returns a human-readable name for the target.
func targetName(target string) string {
	if target == "" {
		return "stdout"
	}
	return target
}

// formatMessage combines a custom message with the diagnostic message.
func formatMessage(custom, diagnostic string) string {
	if custom != "" {
		return custom + ": " + diagnostic
	}
	return diagnostic
}

// truncate shortens a string to maxLen characters with an ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
