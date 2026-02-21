// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
)

// registerLintTools registers lint-related MCP tools.
func (s *Server) registerLintTools() {
	// explain_lint_rule
	explainLintRuleTool := mcp.NewTool("explain_lint_rule",
		mcp.WithDescription("Get a detailed explanation and fix suggestions for a specific lint rule. When lint_solution returns findings with a ruleName, use this tool to understand what the rule checks for, why it matters, and how to fix it."),
		mcp.WithTitleAnnotation("Explain Lint Rule"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("rule",
			mcp.Required(),
			mcp.Description("The lint rule name to explain (e.g., 'unused-resolver', 'invalid-expression', 'missing-provider')"),
		),
	)
	s.mcpServer.AddTool(explainLintRuleTool, s.handleExplainLintRule)
}

// lintRuleExplanation contains a detailed explanation of a lint rule.
type lintRuleExplanation struct {
	Rule        string   `json:"rule"`
	Severity    string   `json:"severity"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Why         string   `json:"why"`
	Fix         string   `json:"fix"`
	Examples    []string `json:"examples,omitempty"`
}

// handleExplainLintRule returns a detailed explanation for a specific lint rule.
func (s *Server) handleExplainLintRule(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rule, err := request.RequireString("rule")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	explanation, ok := lintRuleExplanations[rule]
	if !ok {
		// List all known rules
		known := make([]string, 0, len(lintRuleExplanations))
		for k := range lintRuleExplanations {
			known = append(known, k)
		}
		return mcp.NewToolResultError(fmt.Sprintf("unknown lint rule %q. Known rules: %s", rule, strings.Join(known, ", "))), nil
	}

	return mcp.NewToolResultJSON(explanation)
}

// lintRuleExplanations maps rule names to detailed explanations.
// These match the rules defined in pkg/cmd/scafctl/lint/lint.go.
var lintRuleExplanations = map[string]lintRuleExplanation{
	"empty-solution": {
		Rule:        "empty-solution",
		Severity:    string(lint.SeverityError),
		Category:    "structure",
		Description: "The solution has no resolvers defined under spec.resolvers and no workflow defined under spec.workflow.",
		Why:         "A solution must have at least resolvers or a workflow to be useful. Without either, there's nothing to execute.",
		Fix:         "Add at least one resolver under spec.resolvers or define a workflow with actions under spec.workflow.actions.",
		Examples: []string{
			"spec:\n  resolvers:\n    greeting:\n      type: string\n      resolve:\n        with:\n          - provider: static\n            inputs:\n              value: Hello World",
		},
	},
	"reserved-name": {
		Rule:        "reserved-name",
		Severity:    string(lint.SeverityError),
		Category:    "naming",
		Description: "A resolver uses a reserved name that conflicts with built-in variables.",
		Why:         "Names like '__actions', '__error', '__item', '__index', and '_' are reserved for internal use in CEL expressions and forEach iterations.",
		Fix:         "Rename the resolver to avoid reserved names. Use descriptive names like 'user-input' or 'api-response' instead.",
		Examples:    []string{"Reserved names: __actions, __error, __item, __index, _"},
	},
	"unused-resolver": {
		Rule:        "unused-resolver",
		Severity:    string(lint.SeverityWarning),
		Category:    "usage",
		Description: "A resolver is defined but never referenced by any other resolver, action input, when clause, or expression.",
		Why:         "Unused resolvers add complexity without benefit. They may indicate a typo in a reference or leftover code from refactoring.",
		Fix:         "Either remove the unused resolver, reference it in an action input (rslvr: resolver-name), use it in a CEL expression (_.resolver_name), or add it as a dependency (dependsOn).",
	},
	"missing-provider": {
		Rule:        "missing-provider",
		Severity:    string(lint.SeverityError),
		Category:    "provider",
		Description: "A resolver step or action references a provider name that is not registered in the provider registry.",
		Why:         "The solution cannot execute if it references providers that don't exist. This usually indicates a typo in the provider name.",
		Fix:         "Use list_providers to see all available provider names. Common providers: static, parameter, env, http, cel, file, exec, directory, go-template, validation.",
	},
	"invalid-expression": {
		Rule:        "invalid-expression",
		Severity:    string(lint.SeverityError),
		Category:    "expression",
		Description: "A CEL expression (in a 'when' clause or 'expr' input) has a syntax error and cannot be parsed.",
		Why:         "Invalid CEL expressions will cause runtime failures. Common issues include missing quotes around strings, unbalanced parentheses, and using unknown functions.",
		Fix:         "Use validate_expression with type 'cel' to check the expression syntax. Use list_cel_functions to see available functions. Use evaluate_cel to test the expression with sample data.",
	},
	"invalid-template": {
		Rule:        "invalid-template",
		Severity:    string(lint.SeverityError),
		Category:    "template",
		Description: "A Go template (in a 'tmpl' input) has a syntax error and cannot be parsed.",
		Why:         "Invalid Go templates will cause runtime failures. Common issues include unclosed actions (missing '}}'), unclosed control blocks (if/range/with without end), and unknown functions.",
		Fix:         "Use validate_expression with type 'go-template' to check the template syntax. Use evaluate_go_template to test the template with sample data.",
	},
	"invalid-dependency": {
		Rule:        "invalid-dependency",
		Severity:    string(lint.SeverityError),
		Category:    "dependency",
		Description: "An action's dependsOn references an action name that does not exist in the workflow.",
		Why:         "Actions with invalid dependencies cannot be scheduled for execution. This usually indicates a typo in the action name.",
		Fix:         "Check the action names in spec.workflow.actions and ensure all dependsOn references match exactly. Names are case-sensitive.",
	},
	"empty-workflow": {
		Rule:        "empty-workflow",
		Severity:    string(lint.SeverityWarning),
		Category:    "structure",
		Description: "A workflow section is defined (spec.workflow) but contains no actions.",
		Why:         "An empty workflow serves no purpose. If you only need resolvers, remove the workflow section entirely.",
		Fix:         "Either add actions under spec.workflow.actions or remove spec.workflow entirely if you only need resolvers.",
	},
	"finally-with-foreach": {
		Rule:        "finally-with-foreach",
		Severity:    string(lint.SeverityError),
		Category:    "validation",
		Description: "An action in the spec.workflow.finally section uses forEach, which is not supported.",
		Why:         "Finally actions are cleanup/teardown steps that must run reliably. forEach adds complexity and potential failure points that could prevent cleanup from completing.",
		Fix:         "Remove the forEach from the finally action. If you need to iterate during cleanup, move the action to spec.workflow.actions and handle cleanup differently.",
	},
	"workflow-validation": {
		Rule:        "workflow-validation",
		Severity:    string(lint.SeverityError),
		Category:    "validation",
		Description: "The workflow has structural validation errors such as circular dependencies between actions.",
		Why:         "Circular dependencies create infinite loops that prevent the workflow from completing. The dependency graph must be a DAG (directed acyclic graph).",
		Fix:         "Use render_solution with graph_type 'action-deps' to visualize the dependency graph. Break cycles by reordering dependencies or removing unnecessary dependsOn references.",
	},
	"schema-violation": {
		Rule:        "schema-violation",
		Severity:    string(lint.SeverityError),
		Category:    "schema",
		Description: "The solution YAML violates the JSON Schema definition. This includes unknown fields, type mismatches, pattern violations, and constraint violations.",
		Why:         "Schema violations indicate the YAML structure doesn't match what scafctl expects. This will cause parsing errors or unexpected behavior.",
		Fix:         "Use get_solution_schema to see the expected schema. Common issues: unknown field names (typos), wrong types (string vs int), invalid metadata.name pattern (must be lowercase with hyphens, 3-60 chars).",
	},
	"unknown-provider-input": {
		Rule:        "unknown-provider-input",
		Severity:    string(lint.SeverityError),
		Category:    "provider",
		Description: "An input key passed to a provider is not declared in that provider's input schema.",
		Why:         "Unknown inputs are silently ignored, which usually indicates a typo. The intended field won't receive its value.",
		Fix:         "Use get_provider_schema with the provider name to see all valid input field names. For example, the exec provider requires 'command' (not 'cmd').",
	},
	"invalid-provider-input-type": {
		Rule:        "invalid-provider-input-type",
		Severity:    string(lint.SeverityError),
		Category:    "provider",
		Description: "A literal input value doesn't match the type expected by the provider's schema (e.g., passing a string where an integer is required).",
		Why:         "Type mismatches cause runtime errors. The provider expects a specific type and will fail to process the wrong type.",
		Fix:         "Use get_provider_schema to check the expected type for each input. Ensure literal values match (e.g., use '42' not 'forty-two' for integer fields).",
	},
	"invalid-test-name": {
		Rule:        "invalid-test-name",
		Severity:    string(lint.SeverityError),
		Category:    "naming",
		Description: "A test case name doesn't match the required naming pattern (lowercase with hyphens).",
		Why:         "Test names must be lowercase with hyphens (e.g., 'my-test-case') for consistency and to work correctly with filtering.",
		Fix:         "Rename the test to use only lowercase letters, numbers, and hyphens. Avoid spaces, underscores, and uppercase letters.",
	},
	"unbundled-test-file": {
		Rule:        "unbundled-test-file",
		Severity:    string(lint.SeverityError),
		Category:    "bundling",
		Description: "A test case references a file that is not covered by the solution's bundle.include patterns.",
		Why:         "When the solution is published to the catalog, unbundled files won't be included, causing test failures.",
		Fix:         "Add the file path or a glob pattern to bundle.include that covers the test file. Example: bundle:\n  include:\n    - 'tests/**'",
	},
	"unused-template": {
		Rule:        "unused-template",
		Severity:    string(lint.SeverityWarning),
		Category:    "usage",
		Description: "A test template (name starting with '_') is defined but not referenced by any test case via 'extends'.",
		Why:         "Unused test templates add unnecessary complexity. They may indicate a typo in an extends reference.",
		Fix:         "Either remove the unused template or reference it from a test case using 'extends: _template-name'.",
	},
	"missing-description": {
		Rule:        "missing-description",
		Severity:    string(lint.SeverityInfo),
		Category:    "documentation",
		Description: "A resolver or action does not have a description field.",
		Why:         "Descriptions help others understand what each resolver/action does. They appear in inspect_solution output and documentation.",
		Fix:         "Add a description field: description: \"Brief explanation of what this does\"",
	},
	"long-timeout": {
		Rule:        "long-timeout",
		Severity:    string(lint.SeverityInfo),
		Category:    "performance",
		Description: "An action has a timeout exceeding 10 minutes.",
		Why:         "Very long timeouts may indicate a misconfiguration or an action that should be broken into smaller steps. They also delay failure detection.",
		Fix:         "Consider reducing the timeout or breaking the action into smaller steps. If the long timeout is intentional, this is just an informational notice.",
	},
	"unused-finally": {
		Rule:        "unused-finally",
		Severity:    string(lint.SeverityInfo),
		Category:    "structure",
		Description: "A finally section is defined but there are no regular actions in the workflow.",
		Why:         "Finally actions are cleanup steps that run after regular actions. Without regular actions, there's nothing to clean up after.",
		Fix:         "Either add regular actions under spec.workflow.actions or remove the spec.workflow.finally section.",
	},
	"permissive-result-schema": {
		Rule:        "permissive-result-schema",
		Severity:    string(lint.SeverityInfo),
		Category:    "schema",
		Description: "An action's resultSchema is defined but doesn't specify a type, making it accept any value.",
		Why:         "A result schema without a type constraint doesn't provide meaningful validation of action output.",
		Fix:         "Add a 'type' field to the resultSchema: resultSchema:\n  type: object\n  properties:\n    status:\n      type: string",
	},
	"invalid-result-schema": {
		Rule:        "invalid-result-schema",
		Severity:    string(lint.SeverityError),
		Category:    "schema",
		Description: "An action's resultSchema is not a valid JSON Schema definition.",
		Why:         "Invalid result schemas cannot be used to validate action outputs, causing runtime errors.",
		Fix:         "Ensure the resultSchema follows JSON Schema syntax. Use get_solution_schema to see the expected format.",
	},
	"undefined-required-property": {
		Rule:        "undefined-required-property",
		Severity:    string(lint.SeverityError),
		Category:    "schema",
		Description: "A resultSchema lists a property in 'required' that is not defined in 'properties'.",
		Why:         "Requiring a property that isn't defined means the validation will always fail for that property.",
		Fix:         "Either add the missing property to 'properties' or remove it from the 'required' list.",
	},
	"schema-error": {
		Rule:        "schema-error",
		Severity:    string(lint.SeverityWarning),
		Category:    "schema",
		Description: "Schema validation could not be performed (schema generation failed).",
		Why:         "This is an internal issue that prevents full validation. The solution may still work but hasn't been fully checked.",
		Fix:         "This usually resolves itself. If persistent, check that the solution YAML is well-formed (valid YAML syntax).",
	},
}
