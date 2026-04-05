// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// resolvePathArg detects whether a prompt argument contains file content
// (as happens when MCP clients resolve file references to their contents)
// rather than a file system path. Returns a path reference suitable for
// embedding in tool call instructions and an optional inline note explaining
// that the file content was provided instead of a path.
func resolvePathArg(argValue string) (pathRef, inlineNote string) {
	trimmed := strings.TrimSpace(argValue)
	if !strings.Contains(argValue, "\n") &&
		len(trimmed) < 500 &&
		!strings.HasPrefix(trimmed, "apiVersion:") &&
		!strings.HasPrefix(trimmed, "kind:") {
		// Looks like a normal file path
		return argValue, ""
	}

	// The argument contains file content, not a path.
	// Include the content for context and instruct the LLM to determine the real path.
	inlineNote = fmt.Sprintf(`

IMPORTANT: The path argument contains the solution's YAML content instead of a file path.
This happens when MCP clients resolve file references to their contents.
You MUST determine the actual file system path before calling any tools
(inspect_solution, lint_solution, run_solution_tests, etc.).
Ask the user for the file path if it cannot be determined from context.

Solution content provided inline:
---
%s
---
`, argValue)
	return "<solution_file_path>", inlineNote
}

// replaceName substitutes the configured binary name for "scafctl" in prompt text.
func (s *Server) replaceName(text string) string {
	if s.name == settings.CliBinaryName {
		return text
	}

	return strings.ReplaceAll(text, settings.CliBinaryName, s.name)
}

// registerPrompts registers all MCP prompts on the server.
func (s *Server) registerPrompts() {
	replaceName := s.replaceName

	// create_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("create_solution",
			mcp.WithPromptDescription(replaceName("Guide for creating a new scafctl solution YAML file from scratch. Provides the solution schema, examples, and step-by-step instructions.")),
			mcp.WithPromptIcons(promptIcons["create"]),
			mcp.WithArgument("name",
				mcp.ArgumentDescription("Name for the new solution (lowercase, hyphens allowed, e.g., 'my-solution')"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("description",
				mcp.ArgumentDescription("Brief description of what the solution does"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("features",
				mcp.ArgumentDescription("Comma-separated features to include: resolvers, actions, transforms, validation, parameters, composition, tests"),
			),
		),
		s.handleCreateSolutionPrompt,
	)

	// debug_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("debug_solution",
			mcp.WithPromptDescription(replaceName("Help debug a scafctl solution that isn't working as expected. Inspects the solution, lints it, and suggests fixes.")),
			mcp.WithPromptIcons(promptIcons["debug"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to debug"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("problem",
				mcp.ArgumentDescription("Description of the problem or error you're seeing"),
			),
		),
		s.handleDebugSolutionPrompt,
	)

	// add_resolver prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("add_resolver",
			mcp.WithPromptDescription("Guide for adding a new resolver to an existing solution. Shows available providers, schema, and patterns."),
			mcp.WithPromptIcons(promptIcons["create"]),
			mcp.WithArgument("provider",
				mcp.ArgumentDescription("Provider to use for the resolver (e.g., parameter, static, env, http, cel, file, exec)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("purpose",
				mcp.ArgumentDescription("What the resolver should do (e.g., 'get the deployment environment from user input')"),
			),
		),
		s.handleAddResolverPrompt,
	)

	// add_action prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("add_action",
			mcp.WithPromptDescription("Guide for adding a new action to an existing solution's workflow. Shows available action providers, schema, and patterns."),
			mcp.WithPromptIcons(promptIcons["create"]),
			mcp.WithArgument("provider",
				mcp.ArgumentDescription("Provider to use for the action (e.g., exec, http, directory, go-template)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("purpose",
				mcp.ArgumentDescription("What the action should do"),
			),
		),
		s.handleAddActionPrompt,
	)

	// update_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("update_solution",
			mcp.WithPromptDescription(replaceName("Guide for modifying an existing scafctl solution. Inspects the current state, makes targeted changes, then validates with lint and preview.")),
			mcp.WithPromptIcons(promptIcons["guide"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the existing solution file to modify"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("change",
				mcp.ArgumentDescription("Description of what to change (e.g., 'add a retry to the deploy action', 'add an HTTP resolver for the API endpoint')"),
				mcp.RequiredArgument(),
			),
		),
		s.handleUpdateSolutionPrompt,
	)

	// add_tests prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("add_tests",
			mcp.WithPromptDescription(replaceName("Guide for writing functional tests for a scafctl solution. Walks through test schema, assertions, snapshots, and test patterns.")),
			mcp.WithPromptIcons(promptIcons["create"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to add tests to"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("scope",
				mcp.ArgumentDescription("What to test: 'resolvers' (test resolver outputs), 'actions' (test workflow execution), 'all' (comprehensive test suite). Default: all"),
			),
		),
		s.handleAddTestsPrompt,
	)

	// compose_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("compose_solution",
			mcp.WithPromptDescription(replaceName("Guide for designing a multi-file composed solution using scafctl's composition system. Breaks a solution into reusable partial YAML files that get merged at build time.")),
			mcp.WithPromptIcons(promptIcons["guide"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the root solution file (or directory for a new composed solution)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("goal",
				mcp.ArgumentDescription("Description of what the composed solution should accomplish (e.g., 'modular deploy pipeline with separate resolver and action bundles')"),
				mcp.RequiredArgument(),
			),
		),
		s.handleComposeSolutionPrompt,
	)

	// fix_lint prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("fix_lint",
			mcp.WithPromptDescription(replaceName("Guide for fixing lint findings in a scafctl solution. Lints the solution, explains each finding, and applies targeted fixes in priority order (errors first, then warnings).")),
			mcp.WithPromptIcons(promptIcons["debug"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to fix"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("severity",
				mcp.ArgumentDescription("Minimum severity to fix: 'error' (errors only), 'warning' (errors + warnings), 'info' (all). Default: warning"),
			),
		),
		s.handleFixLintPrompt,
	)

	// prepare_execution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("prepare_execution",
			mcp.WithPromptDescription(replaceName("Prepare a scafctl solution for execution. Validates, previews, and generates the exact CLI command — without actually running it. Use this when you're ready to run a solution but want to verify everything first.")),
			mcp.WithPromptIcons(promptIcons["guide"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to prepare for execution"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("params",
				mcp.ArgumentDescription("Comma-separated key=value input parameters (e.g., 'env=prod,region=us-east1')"),
			),
		),
		s.handlePrepareExecutionPrompt,
	)

	// analyze_execution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("analyze_execution",
			mcp.WithPromptDescription(replaceName("Analyze a completed scafctl execution by inspecting the snapshot. Identifies failures, regressions, and suggests fixes. Optionally compares against a known-good snapshot.")),
			mcp.WithPromptIcons(promptIcons["analyze"]),
			mcp.WithArgument("snapshot_path",
				mcp.ArgumentDescription("Path to the snapshot JSON file from the failed or unexpected run"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("previous_snapshot",
				mcp.ArgumentDescription("Path to a known-good snapshot for comparison (optional — enables regression detection)"),
			),
			mcp.WithArgument("problem",
				mcp.ArgumentDescription("Description of what went wrong or what was unexpected"),
			),
		),
		s.handleAnalyzeExecutionPrompt,
	)

	// migrate_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("migrate_solution",
			mcp.WithPromptDescription(replaceName("Guide for structurally refactoring a scafctl solution. Supports adding composition, extracting templates, splitting into multiple files, adding tests, and upgrading to newer patterns.")),
			mcp.WithPromptIcons(promptIcons["guide"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to migrate"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("migration",
				mcp.ArgumentDescription("Migration type: 'add-composition' (split into composed files), 'extract-templates' (move inline templates to files), 'split-solution' (break into smaller solutions), 'add-tests' (add comprehensive functional tests), 'upgrade-patterns' (modernize to latest best practices)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("target_dir",
				mcp.ArgumentDescription("Target directory for split/extracted files (optional, defaults to solution directory)"),
			),
		),
		s.handleMigrateSolutionPrompt,
	)

	// optimize_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("optimize_solution",
			mcp.WithPromptDescription(replaceName("Analyze a scafctl solution for performance, readability, and quality improvements. Identifies parallelization opportunities, unnecessary dependencies, naming issues, and missing test coverage.")),
			mcp.WithPromptIcons(promptIcons["analyze"]),
			mcp.WithArgument("path",
				mcp.ArgumentDescription("Path to the solution file to optimize"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("focus",
				mcp.ArgumentDescription("Optimization focus: 'performance' (parallelization, dependency optimization), 'readability' (naming, structure, documentation), 'testing' (coverage gaps, missing edge cases), 'all' (comprehensive analysis). Default: all"),
			),
		),
		s.handleOptimizeSolutionPrompt,
	)
}

// handleCreateSolutionPrompt returns a prompt guiding the AI to create a solution.
func (s *Server) handleCreateSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	name := request.Params.Arguments["name"]
	description := request.Params.Arguments["description"]
	features := request.Params.Arguments["features"]

	featureList := "resolvers, actions"
	if features != "" {
		featureList = features
	}

	prompt := strings.NewReplacer(
		"{name}", name,
		"{description}", description,
		"{features}", featureList,
	).Replace(`You are creating a new scafctl solution named "{name}".
Description: {description}

IMPORTANT: Before writing any YAML, you MUST use these tools to understand the correct schema:

1. Call get_solution_schema to get the full JSON Schema for solution YAML files
2. Call list_providers to see available providers  
3. Call get_example with path "solutions/email-notifier/solution.yaml" for a practical reference
4. Call get_example with path "solutions/comprehensive/solution.yaml" for a comprehensive reference

Requested features: {features}

Follow these rules when creating the solution:
- apiVersion MUST be "scafctl.io/v1"
- kind MUST be "Solution"
- metadata.name must be lowercase with hyphens only (3-60 chars)
- metadata.version must be a valid semver (e.g., "1.0.0")
- Resolvers go under spec.resolvers.<name> with resolve/transform/validate phases
- The resolver "type" field is OPTIONAL. Only set it when the resolved value is a known scalar type.
  NEVER set type: string on resolvers using providers that return objects (e.g., http returns {statusCode, body, headers}).
  When in doubt, omit the type field entirely.
- Actions go under spec.workflow.actions.<name> with provider and inputs
- Use ValueRef format for inputs: literal (raw value), rslvr (resolver reference), expr (CEL expression), or tmpl (Go template)
- Always validate user inputs using the validate phase with the validation provider
- Use dependsOn for ordering (both resolvers and actions)
- Use when clauses (CEL expressions) for conditional execution
- For exec provider actions: the input field is "command" (string), NOT "cmd". Always use get_provider_schema to verify required fields.

NEVER invent fields that don't exist in the schema. Only use fields documented in the JSON Schema.
When using any provider in an action, ALWAYS call get_provider_schema first to verify the exact field names and types.

After creating the solution, tell the user how to run it.
IMPORTANT: When referencing filenames in your response, ALWAYS prefix relative paths
with "./" (e.g., "./test-solution.yaml", NOT "test-solution.yaml"). Bare filenames
without "./" get auto-linkified by VS Code Chat into broken content-reference URLs.
IMPORTANT: Choose the correct run command based on whether the solution has a workflow:
  • If the solution has spec.workflow.actions (i.e. it performs side-effects/actions):
      scafctl run solution -f ./<filename>.yaml -r key=value
  • If the solution has ONLY resolvers and NO spec.workflow section:
      scafctl run resolver -f ./<filename>.yaml key=value
    'scafctl run solution' will FAIL if there is no workflow defined.
Parameters are passed with -r/--resolver or positional key=value (NOT -p).
Parameters can also be loaded from files (-r @file.yaml), piped from stdin (-r @-),
or read as raw content into a single key (key=@- for stdin, key=@file for files).
Example:
  scafctl run resolver -f ./{name}.yaml param1=value1 param2=value2
  scafctl run solution -f ./{name}.yaml -r param1=value1 -r param2=value2
  echo '{"param1": "value1"}' | scafctl run resolver -f ./{name}.yaml -r @-
  echo hello | scafctl run resolver -f ./{name}.yaml -r message=@-
  scafctl run resolver -f ./{name}.yaml body=@content.txt

After creating the solution file:
1. Call lint_solution to validate the YAML structure
2. Call preview_resolvers to verify the resolver chain produces expected values
3. Call get_run_command to show the user the exact command to run it
4. If the solution has tests, call run_solution_tests to verify they pass`)

	return &mcp.GetPromptResult{
		Description: s.replaceName(fmt.Sprintf("Create a new scafctl solution: %s", name)),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handleDebugSolutionPrompt returns a prompt guiding the AI to debug a solution.
func (s *Server) handleDebugSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	problem := request.Params.Arguments["problem"]

	pathRef, inlineNote := resolvePathArg(path)

	problemDesc := "The solution is not working as expected."
	if problem != "" {
		problemDesc = problem
	}

	prompt := fmt.Sprintf(`Debug the scafctl solution at %q.
%s
Problem: %s

Follow this debugging process:

1. Call inspect_solution with path %q to understand the solution structure
2. Call lint_solution with file %q to find validation issues
3. Call get_solution_schema to verify the YAML structure matches the expected schema
4. If there are resolvers, call render_solution with graph_type "resolver" to see the dependency graph
5. If there are actions, call render_solution with graph_type "action-deps" to see the action dependency graph
6. For any providers used, call get_provider_schema to verify the inputs are correct

Check for common issues:
- Misspelled field names (compare against the schema)
- Invalid provider names (use list_providers to see available ones)
- Missing required fields (check schema required fields)
- Circular dependencies in resolvers or actions
- Invalid CEL expressions in when/expr fields
- Type mismatches (resolver type vs actual value)
- Missing dependsOn when referencing other resolvers (same-section action dependencies are auto-inferred from __actions references)
- Using dependsOn in workflow.finally to reference a workflow.actions action — this is a validation error; use __actions.<name> in inputs or when instead (the ref appears in crossSectionRefs on the rendered graph)

7. Call preview_resolvers with path %q to check if resolvers execute successfully and produce expected values
8. If the solution has tests, call run_solution_tests with path %q to verify test results
9. Call get_run_command with path %q to verify the correct run command`, pathRef, inlineNote, problemDesc, pathRef, pathRef, pathRef, pathRef, pathRef)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Debug solution: %s", path),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handleAddResolverPrompt returns a prompt for adding a resolver.
func (s *Server) handleAddResolverPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	providerName := request.Params.Arguments["provider"]
	purpose := request.Params.Arguments["purpose"]

	purposeDesc := "resolve a value"
	if purpose != "" {
		purposeDesc = purpose
	}

	prompt := fmt.Sprintf(`Add a new resolver using the %q provider to an existing solution.
Purpose: %s

Before writing YAML:

1. Call get_provider_schema with name %q to get the provider's full input schema
2. Call explain_kind with kind "resolver" to see all resolver fields
3. Call get_example with a relevant resolver example from list_examples (category: "resolvers")

A resolver has these phases:
- resolve: Fetches the initial value (required). Has a "with" array of provider sources (fallback chain).
- transform: Transforms the resolved value (optional). Can use cel, go-template, etc.
- validate: Validates the final value (optional). Uses the validation provider.

Resolver field structure:
  <resolver-name>:
    description: "what this resolves"
    type: string|int|float|bool|array|object|time|duration|any  # OPTIONAL — see type rules below
    resolve:
      with:
        - provider: %q
          inputs:
            <provider-specific inputs>
    transform:  # optional
      with:
        - provider: cel
          inputs:
            expression: "<CEL expression using __self>"
    validate:  # optional
      with:
        - provider: validation
          inputs:
            expression: "<CEL validation expression using __self>"
          message: "Error message if validation fails"

IMPORTANT rules for resolver phases:
- Each phase (resolve, transform, validate) MUST use the "with" key containing an array. Never use a bare array.
- In transform and validate phases, use __self to reference the resolver's own value, NOT _.resolverName.
  Using _.resolverName creates a circular dependency. __self is the correct way to access the current value.
  Example: expression: "__self.statusCode == 200"  (correct)
           expression: "_.myResolver.statusCode == 200"  (WRONG - causes cycle)
- In resolve phase, _.otherResolver references other resolvers' values (not self).

IMPORTANT rules for the resolver "type" field:
- The type field is OPTIONAL. When omitted, the value passes through as-is (equivalent to type: any).
- Only set type when you KNOW the resolved value is a simple scalar (string, int, float, bool).
- NEVER set type: string on resolvers that use providers returning objects/maps (e.g., http, exec).
  The http provider returns an object with statusCode, body, headers — setting type: string would
  cause a coercion error. Omit the type field or use type: object for these providers.
- When in doubt, OMIT the type field entirely — it is safer than guessing wrong.
- Call get_provider_schema to check what a provider returns before deciding on a type.

Input values use ValueRef format:
- Literal: just the raw value
- Resolver reference: rslvr: <resolver-name>
- CEL expression: expr: "<expression>"
- Go template: tmpl: "<template>"`, providerName, purposeDesc, providerName, providerName)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Add resolver using %s provider", providerName),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: prompt,
				},
			},
		},
	}, nil
}

// handleAddActionPrompt returns a prompt for adding an action.
func (s *Server) handleAddActionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	providerName := request.Params.Arguments["provider"]
	purpose := request.Params.Arguments["purpose"]

	purposeDesc := "perform an operation"
	if purpose != "" {
		purposeDesc = purpose
	}

	featureHints := make([]string, 0, 6)
	featureHints = append(featureHints, "dependsOn: explicit ordering for same-section actions. If inputs or when already reference __actions.<name> within the same section, that dependency is auto-inferred and dependsOn is optional (still required for ordering without a data dependency)")
	featureHints = append(featureHints, "dependsOn in workflow.finally: can only reference other finally actions—never actions in workflow.actions. To read results from a main action in finally, use __actions.<name> in inputs or when; the reference appears in crossSectionRefs on the rendered graph (informational only—cross-section ordering is guaranteed structurally)")
	featureHints = append(featureHints, "when: CEL expression for conditional execution")
	featureHints = append(featureHints, "onError: 'fail' (default) or 'continue'")
	featureHints = append(featureHints, "retry: { maxAttempts, backoff: fixed|linear|exponential, initialDelay, maxDelay }")
	featureHints = append(featureHints, "forEach: { in: <ValueRef>, item: 'item', index: 'index', concurrency: N }")
	featureHints = append(featureHints, "timeout: duration string (e.g., '30s', '5m')")

	prompt := fmt.Sprintf(`Add a new action using the %q provider to an existing solution's workflow.
Purpose: %s

Before writing YAML:

1. Call get_provider_schema with name %q to get the provider's full input schema and capabilities
2. Call explain_kind with kind "action" to see all action fields
3. Call get_example with a relevant action example from list_examples (category: "actions")

Action field structure (goes under spec.workflow.actions.<name>):
  <action-name>:
    description: "what this action does"
    provider: %q
    inputs:
      <provider-specific inputs using ValueRef format>
    dependsOn:
      - other-action-name
    when:
      expr: "resolvers.some_flag == true"

CRITICAL: Action input field names MUST match the provider's schema exactly.
For example, the exec provider requires "command" (string), NOT "cmd".
Always verify field names with get_provider_schema before writing inputs.

Available action features:
%s

Input values use ValueRef format:
- Literal: just the raw value
- Resolver reference: rslvr: <resolver-name>
- CEL expression: expr: "<expression>"
- Go template: tmpl: "<template>"

Cleanup actions go under spec.workflow.finally.<name> (same structure, always run).`, providerName, purposeDesc, providerName, providerName, strings.Join(featureHints, "\n"))

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Add action using %s provider", providerName),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: prompt,
				},
			},
		},
	}, nil
}

// handleUpdateSolutionPrompt returns a prompt for modifying an existing solution.
func (s *Server) handleUpdateSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	change := request.Params.Arguments["change"]

	pathRef, inlineNote := resolvePathArg(path)

	prompt := fmt.Sprintf(`Modify the existing scafctl solution at %q.
%s
Requested change: %s

Follow this workflow to make a safe, validated change:

STEP 1 — UNDERSTAND the current solution:
1. Call inspect_solution with path %q to see the full structure (resolvers, actions, metadata)
2. Read the solution file to understand the current YAML

STEP 2 — PLAN the change:
3. Call get_solution_schema to verify what fields are available
4. If adding/modifying providers, call get_provider_schema for each provider used
5. Call list_providers if you need to find the right provider for the change

STEP 3 — IMPLEMENT the change:
6. Make targeted edits to the solution file — change ONLY what was requested
7. Preserve existing structure, formatting, and comments where possible
8. Use correct ValueRef format for inputs (literal, rslvr:, expr:, tmpl:)

STEP 4 — VALIDATE the change:
9. Call lint_solution with file %q to check for errors
10. Call preview_resolvers with path %q to verify resolvers still produce expected values
11. If the solution has tests, call run_solution_tests with path %q to ensure nothing broke
12. Call get_run_command with path %q to remind the user how to run it

IMPORTANT:
- Do NOT restructure the entire solution — make the minimal change needed
- If lint_solution reports errors after your change, fix them before finishing
- If preview_resolvers shows unexpected values, investigate and fix
- Always verify provider field names with get_provider_schema before writing inputs`, pathRef, inlineNote, change, pathRef, pathRef, pathRef, pathRef, pathRef)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Update solution: %s", path),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handleAddTestsPrompt returns a prompt for writing functional tests.
func (s *Server) handleAddTestsPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	scope := request.Params.Arguments["scope"]

	pathRef, inlineNote := resolvePathArg(path)

	if scope == "" {
		scope = "all"
	}

	prompt := fmt.Sprintf(`Add functional tests to the scafctl solution at %q.
%s
Test scope: %s

Follow these steps:

STEP 1 — UNDERSTAND the solution:
1. Call inspect_solution with path %q to see resolvers, actions, and structure
2. Identify what needs testing based on the scope

STEP 2 — LEARN the test format:
3. Call explain_kind with kind "solution" and field "spec.testing" to see test schema
4. Call get_example with path "solutions/tested-solution/solution.yaml" for a practical reference
5. Review the test case structure

STEP 3 — WRITE the tests:

Tests go under spec.testing in the solution YAML:

  testing:
    config:                          # optional global config
      timeout: 30s
      tags: [smoke]
    cases:
      <test-name>:                   # lowercase with hyphens
        description: "what this tests"
        command: [render, solution]   # or [run, resolver], [run, solution], [lint]
        args: ["-o", "json"]         # extra CLI args (do NOT include -f)
        resolvers:                   # input parameters for test
          param-name: "test-value"
        env:                         # environment variables
          MY_VAR: "value"
        assertions:
          - expression: __exitCode == 0
          - contains: "expected substring"
          - regex: "pattern.*match"
          - notContains: "unexpected"
          - notRegex: "error.*pattern"
        tags: [smoke, validation]

Available commands for testing:
- [render, solution]  — render the solution (outputs resolver data), most common
- [run, resolver]     — execute resolvers only
- [run, solution]     — execute full workflow (resolvers + actions)
- [lint]              — lint validation check

Assertion variables available in expressions:
- __exitCode: CLI exit code (0 = success)
- __stdout: standard output as string
- __stderr: standard error as string
- __output: parsed JSON output (when -o json is used)
- __sandbox: sandbox directory path

%s

STEP 4 — VALIDATE the tests:
6. Call lint_solution with file %q to verify the solution with tests is valid YAML
7. Call run_solution_tests with path %q to execute the tests and verify they pass

After adding tests, tell the user how to run them from the CLI:
  scafctl test functional -f ./<filename>.yaml
  scafctl test functional -f ./<filename>.yaml -v   # verbose output
IMPORTANT: The CLI command is 'scafctl test functional -f <file>', NOT 'scafctl test -f <file>'.
The 'functional' subcommand is REQUIRED. 'scafctl test -f' will FAIL.

IMPORTANT:
- Test names must be lowercase with hyphens (no spaces, underscores, or uppercase)
- Built-in tests (lint, parse) run automatically unless skipped
- Use tags to organize tests (smoke, validation, integration, etc.)
- The -f flag is auto-injected; do NOT include it in args
- Use 'render solution' with '-o json' for most resolver output testing`, pathRef, inlineNote, scope, pathRef, scopeHints(scope), pathRef, pathRef)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Add tests to solution: %s", pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handleComposeSolutionPrompt returns a prompt for designing a multi-file composed solution.
func (s *Server) handleComposeSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	goal := request.Params.Arguments["goal"]

	pathRef, inlineNote := resolvePathArg(path)

	prompt := fmt.Sprintf(`Design a multi-file composed scafctl solution.

Root path: %s
%s
Goal: %s

Composition allows splitting a large solution into multiple partial YAML files that get merged at build time. This promotes reusability and maintainability.

STEP 1 — UNDERSTAND composition:
1. Call get_solution_schema and look at the "compose" field in the spec
2. Call get_example with path "solutions/composition/parent.yaml" or similar composition examples from list_examples (category: "solutions")

Composition works by declaring partial YAML files in the root solution:

  spec:
    compose:
      - path: resolvers/common.yaml       # relative to root solution
      - path: resolvers/api.yaml
      - path: actions/deploy.yaml
      - path: tests/smoke.yaml

Each partial file is a YAML document containing fragments that get deep-merged into the root solution.

Partial file example (resolvers/common.yaml):
  spec:
    resolvers:
      environment:
        description: "Deployment environment"
        type: string
        resolve:
          with:
            - provider: parameter
              inputs:
                name: environment

STEP 2 — PLAN the file structure:
3. Identify natural groupings:
   - Common resolvers (parameters, shared config) → resolvers/common.yaml
   - Feature-specific resolvers → resolvers/<feature>.yaml
   - Action groups → actions/<phase>.yaml (e.g., actions/build.yaml, actions/deploy.yaml)
   - Tests → tests/<scope>.yaml
4. Keep the root solution minimal — metadata, compose list, and maybe global config

STEP 3 — CREATE the files:
5. Create the root solution.yaml with metadata and compose references
6. Create each partial file with its slice of the solution
7. Ensure resolver/action names are unique across all files
8. Handle cross-file dependencies via dependsOn (dependencies can reference names from other files)

STEP 4 — VALIDATE:
9. Call lint_solution with the root solution file path — linting resolves composition automatically
10. Call preview_resolvers with the root path to verify resolver chain works
11. Call render_solution with the root path to see the fully merged structure

IMPORTANT RULES:
- Partial files contain ONLY the fields they contribute (spec.resolvers, spec.workflow, etc.)
- The root solution MUST have apiVersion, kind, and metadata
- Partial files do NOT need apiVersion/kind/metadata
- Names must be globally unique — two files cannot define the same resolver or action name
- Use relative paths in the compose list (relative to the root solution file)
- Deep merge means maps are merged recursively; arrays are replaced, not appended
- Order matters: later compose entries override earlier ones for conflicting keys

SUB-SOLUTION COMPOSITION (alternative to compose partials):
- Use the 'solution' provider to delegate to a child solution (full encapsulation)
- Child solutions are separate .yaml files with their own apiVersion, kind, metadata, and resolvers
- Reference them with: provider: solution, inputs: { source: "./sub/child.yaml" }
- When building for the catalog, child solution files and their dependencies are discovered recursively
- Circular sub-solution references are detected and reported at build time
- See get_example with path "solutions/nested-bundle/parent.yaml" for a nested sub-solution example`, pathRef, inlineNote, goal)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Compose solution: %s", pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// scopeHints returns scope-specific testing guidance.
func scopeHints(scope string) string {
	switch scope {
	case "resolvers":
		return `RESOLVER TESTING TIPS:
- Use command [render, solution] with args ["-o", "json"] to test resolver outputs
- Access resolver values via __output.resolvers.<name> in expressions
- Test each resolver independently with focused assertions
- Test edge cases: missing params, invalid inputs, type coercion
- Test conditional resolvers (when clauses) by varying inputs`
	case "actions":
		return `ACTION TESTING TIPS:
- Use command [run, solution] to test the full workflow
- Test success cases and expected error cases separately
- Use 'expectFailure: true' for tests that should exit non-zero
- Test conditional actions (when clauses) by providing different resolver inputs
- Check file creation/modification in the sandbox directory
- Use forEach scenarios if actions iterate over collections`
	default:
		return `COMPREHENSIVE TESTING TIPS:
- Start with resolver tests (command: [render, solution], args: ["-o", "json"])
- Then add action/workflow tests (command: [run, solution])
- Include edge cases: missing params, invalid inputs
- Tag tests appropriately: smoke (fast, essential), validation, integration
- Test both success and expected failure paths`
	}
}

// handleFixLintPrompt returns a prompt guiding the AI to fix lint findings.
func (s *Server) handleFixLintPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	severity := request.Params.Arguments["severity"]

	pathRef, inlineNote := resolvePathArg(path)

	if severity == "" {
		severity = "warning"
	}

	prompt := fmt.Sprintf(`Fix lint findings in the scafctl solution at %q.
%s
Minimum severity to fix: %s

Follow this exact workflow:

STEP 1 — LINT the solution:
1. Call lint_solution with file %q and severity %q to get current findings
2. If there are no findings, report "Solution is clean" and stop

STEP 2 — UNDERSTAND each finding:
3. For each finding, call explain_lint_rule with the finding's ruleName to get:
   - What the rule checks for
   - Why it matters
   - How to fix it
   - Examples of correct usage

STEP 3 — FIX findings in priority order:
Fix errors first, then warnings, then info:

For each finding:
4. Read the solution file and locate the problem area (the finding's "location" field tells you where)
5. Apply the fix suggested by explain_lint_rule
6. If the fix requires verifying a provider's schema (e.g., fixing unknown-provider-input), call get_provider_schema first
7. If the fix involves CEL expressions, call validate_expression to verify the corrected expression
8. If the fix involves Go templates, call evaluate_go_template to verify

STEP 4 — VALIDATE the fixes:
9. Call lint_solution again with the same severity to confirm all findings are resolved
10. If new findings appeared from the fixes, repeat from STEP 3
11. Call preview_resolvers with path %q to verify the solution still works correctly

IMPORTANT:
- Fix ONE finding at a time to avoid cascading errors
- Always re-lint after each fix to catch regressions
- Do NOT change anything unrelated to the lint findings
- If a finding is intentional (e.g., long-timeout for a known long operation), explain and skip it
- Some findings may be related (e.g., unused-resolver + missing reference) — fix them together`, pathRef, inlineNote, severity, pathRef, severity, pathRef)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Fix lint findings in: %s", pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handlePrepareExecutionPrompt returns a prompt that validates and prepares a solution for execution.
func (s *Server) handlePrepareExecutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	params := request.Params.Arguments["params"]

	pathRef, inlineNote := resolvePathArg(path)

	paramsSection := ""
	if params != "" {
		paramsSection = fmt.Sprintf("\nUser-provided parameters: %s\n", params)
	}

	prompt := fmt.Sprintf(`Prepare the scafctl solution at %q for execution.
%s%s
This prompt validates the solution, previews its behavior, and provides the exact CLI 
command to run it — without actually executing it. The user makes the final decision.

STEP 1 — INSPECT the solution:
1. Call inspect_solution with path %q to understand its structure
2. Identify:
   - Does it have a spec.workflow.actions section? → needs 'run solution'
   - Does it have only resolvers (no workflow)? → needs 'run resolver'
   - What resolver parameters does it expect? (look for parameter provider usage)

STEP 2 — VALIDATE the solution:
3. Call lint_solution with file %q to check for errors
4. If there are error-severity findings, STOP and tell the user to fix them first
   (suggest using the fix_lint prompt)

STEP 3 — CHECK authentication:
5. Call auth_status to see configured auth providers
6. Cross-reference with the solution's providers — if the solution uses HTTP providers 
   that need auth, or cloud providers (GCP, Azure, GitHub), warn if auth is not configured

STEP 4 — PREVIEW the solution:
7. Call preview_resolvers with path %q to see what values the resolvers produce
   - Verify resolver outputs look correct for the given parameters
   - Flag any resolvers that returned errors or unexpected values
8. If the solution has a workflow, call preview_action with path %q to see the action graph
   - Review which actions would execute and in what order
   - Check for any actions with conditions (when clauses) that might skip

STEP 5 — GENERATE the run command:
9. Call get_run_command with path %q to get the exact CLI command

STEP 6 — PRESENT the execution plan:
Present a clear summary to the user:

**Solution:** <name> (v<version>)
**Command type:** run solution / run resolver
**Parameters:** <list of parameters and their values>
**Resolvers:** <count> resolvers will execute
**Actions:** <count> actions will execute (if applicable)
**Auth:** <status of required auth providers>
**Warnings:** <any issues from preview>

**Command to run:**
`+"```"+`
<exact command from get_run_command>
`+"```"+`

DO NOT run the command yourself. Present it to the user and let them execute it.
If there are any concerns from the preview (unexpected values, missing auth, lint warnings), 
highlight them clearly so the user can make an informed decision.`, pathRef, inlineNote, paramsSection, pathRef, pathRef, pathRef, pathRef, pathRef)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Prepare execution: %s", pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// handleAnalyzeExecutionPrompt returns a prompt guiding post-execution analysis.
func (s *Server) handleAnalyzeExecutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	snapshotPath := request.Params.Arguments["snapshot_path"]
	previousSnapshot := request.Params.Arguments["previous_snapshot"]
	problem := request.Params.Arguments["problem"]

	problemDesc := "The execution did not produce the expected results."
	if problem != "" {
		problemDesc = problem
	}

	var diffSection string
	if previousSnapshot != "" {
		diffSection = fmt.Sprintf(`
STEP 2 — COMPARE with the previous snapshot:
3. Call diff_snapshots with before %q and after %q
4. Review the diff output:
   - Look for status changes (success → failed indicates a regression)
   - Look for value changes (unexpected output changes)
   - Look for added/removed resolvers (structural changes)
5. For any regressions (success → failed), note the resolver name and error message`, previousSnapshot, snapshotPath)
	} else {
		diffSection = `
STEP 2 — No previous snapshot provided:
- If the user has a known-good snapshot from a prior run, suggest they provide it for comparison
- Without a baseline, focus on the error messages and resolver statuses in the current snapshot`
	}

	prompt := fmt.Sprintf(`Analyze the execution results from snapshot at %q.

Problem reported: %s

Follow this analysis process:

STEP 1 — INSPECT the snapshot:
1. Call show_snapshot with path %q and format "full" to see complete results
2. Review the snapshot:
   - Overall status (success/failed)
   - Duration (was it unusually slow?)
   - Phase execution order
   - Failed resolvers (status != "success")
   - Resolver errors (error messages)
%s

STEP 3 — DIAGNOSE failed resolvers:
For each failed resolver:
6. Note the provider used and the error message
7. Call get_provider_schema for that provider to verify the configuration is correct
8. If the provider is an HTTP/cloud provider (http, gcp, azure, github), call auth_status to check authentication
9. Common failure patterns:
   - "connection refused" or "timeout" → service endpoint issue, check URL/host configuration
   - "401 Unauthorized" or "403 Forbidden" → auth issue, verify credentials/tokens
   - "no such key" or "not found" → configuration key changed or missing
   - "template execution failed" → Go template syntax error, use eval_template to test
   - "CEL evaluation failed" → CEL expression error, use eval_cel to test
   - "dependency failed" → upstream resolver failed, fix that first

STEP 4 — SUGGEST fixes:
10. For each issue found, provide a specific fix:
    - If it's a configuration issue: show the exact YAML change needed
    - If it's an auth issue: show the auth setup command
    - If it's an expression error: show the corrected expression
    - If it's a dependency issue: identify the root cause (upstream) resolver

STEP 5 — VERIFY fixes:
11. After suggesting fixes, recommend verification steps:
    - Re-run the solution with --snapshot flag to capture new results
    - Compare new snapshot with the problematic one using diff_snapshots
    - Run lint_solution to check for any structural issues

Present findings as:

**Execution Summary:**
- Status: <overall status>
- Duration: <duration>
- Resolvers: <success>/<total> succeeded

**Failed Resolvers:**
For each failure:
- **<name>** (<provider>): <error summary>
  - Root cause: <analysis>
  - Fix: <specific fix>

**Regressions:** (if comparison snapshot provided)
- <list of resolvers that changed from success to failed>

**Recommended Actions:**
1. <prioritized list of fixes>`, snapshotPath, problemDesc, snapshotPath, diffSection)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Analyze execution: %s", snapshotPath),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: prompt,
				},
			},
		},
	}, nil
}

// handleMigrateSolutionPrompt returns a prompt guiding structural migration of a solution.
func (s *Server) handleMigrateSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	migration := request.Params.Arguments["migration"]
	targetDir := request.Params.Arguments["target_dir"]

	pathRef, inlineNote := resolvePathArg(path)

	targetSection := ""
	if targetDir != "" {
		targetSection = fmt.Sprintf("\nTarget directory for extracted/split files: %s\n", targetDir)
	}

	migrationGuide := migrationTypeGuide(migration)

	prompt := fmt.Sprintf(`Migrate the scafctl solution at %q.
%s
Migration type: %s
%s
This prompt guides a STRUCTURAL refactoring of the solution. Unlike update_solution (which makes
targeted functional changes), this migration reorganizes the solution's architecture while
preserving its behavior.

STEP 1 — ESTABLISH baseline:
1. Call inspect_solution with path %q to understand the current structure
2. Call lint_solution with file %q to establish a baseline — note any existing findings
3. Call preview_resolvers with path %q to capture expected resolver outputs (these MUST NOT change)

STEP 2 — PLAN the migration:
%s

STEP 3 — EXECUTE the migration:
4. Apply the planned changes according to the migration type above
5. Preserve ALL functional behavior — resolver outputs, action execution order, and conditions must remain identical

STEP 4 — VALIDATE the migration:
6. Call lint_solution with the root solution file — ZERO new findings should appear
7. Call preview_resolvers with the root path — compare outputs against the baseline from STEP 1
8. Call diff_solution between the original and migrated solution to confirm only structural changes
9. If the solution has tests, call run_solution_tests to verify all tests still pass

CRITICAL RULES:
- Migration MUST be behavior-preserving — same inputs produce same outputs
- Do NOT add, remove, or modify resolver logic (that's what update_solution is for)
- Do NOT change action behavior or ordering
- If any validation step fails, revert the change and investigate before continuing
- Document what was migrated and why in commit messages`, pathRef, inlineNote, migration, targetSection, pathRef, pathRef, pathRef, migrationGuide)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Migrate solution (%s): %s", migration, pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// migrationTypeGuide returns migration-specific instructions.
func migrationTypeGuide(migration string) string {
	switch migration {
	case "add-composition":
		return `Migration: ADD COMPOSITION
- Identify logical groupings of resolvers and actions
- Create partial YAML files for each group (e.g., resolvers/params.yaml, actions/deploy.yaml)
- Move resolver and action definitions into the partials
- Add spec.compose entries in the root solution pointing to each partial
- The root solution keeps metadata, compose list, and any shared configuration
- Verify the merged result matches the original with lint_solution`
	case "extract-templates":
		return `Migration: EXTRACT TEMPLATES
- Find all inline Go templates (tmpl: fields with multi-line content)
- Create template files in a templates/ directory alongside the solution
- Replace inline tmpl: values with file references (tmpl: file:templates/<name>.tmpl)
- For CEL expressions, consider if complex ones should become standalone files
- Use extract_resolver_refs on each template to verify references are preserved`
	case "split-solution":
		return `Migration: SPLIT SOLUTION
- Identify independent functional units in the solution
- Create separate solution files for each unit
- Ensure each sub-solution is self-contained (has its own resolvers)
- Use the 'solution' provider to reference sub-solutions: provider: solution, inputs: { source: "./sub/child.yaml" }
- When building for the catalog, nested sub-solution files and their dependencies are bundled recursively
- Shared resolvers should be duplicated or extracted into a composition partial
- Update any documentation or run commands to reference the new files`
	case "add-tests":
		return `Migration: ADD TESTS
- Call generate_test_scaffold to get starter test cases based on the solution
- Add comprehensive tests for each resolver (use command [render, solution] -o json)
- Add workflow tests if spec.workflow exists (use command [run, solution])
- Include edge cases: missing parameters, invalid inputs, boundary values
- Tag tests appropriately (smoke, validation, integration)
- Call run_solution_tests to verify all new tests pass`
	case "upgrade-patterns":
		return `Migration: UPGRADE PATTERNS
- Check for deprecated patterns (old-style ValueRef formats, legacy fields)
- Replace string concatenation in templates with proper expressions
- Add missing validation phases to resolvers that accept user input
- Ensure all providers are using their latest input field names (call get_provider_schema)
- Add display_name and description fields where missing
- Standardize naming conventions (lowercase-with-hyphens)
- Add type annotations to resolvers that are missing them`
	default:
		return fmt.Sprintf(`Migration: %s
- Inspect the solution to understand current structure
- Plan changes that preserve behavior
- Execute incrementally, validating after each change`, migration)
	}
}

// handleOptimizeSolutionPrompt returns a prompt for performance/quality analysis.
func (s *Server) handleOptimizeSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	focus := request.Params.Arguments["focus"]

	pathRef, inlineNote := resolvePathArg(path)

	if focus == "" {
		focus = "all"
	}

	focusGuide := optimizationFocusGuide(focus)

	prompt := fmt.Sprintf(`Optimize the scafctl solution at %q.
%s
Optimization focus: %s

STEP 1 — ANALYZE structure:
1. Call inspect_solution with path %q to see the full structure
2. Call render_solution with path %q and graph_type "resolver" to see the resolver dependency graph
3. Call render_solution with path %q and graph_type "action-deps" to see action dependencies
4. Call lint_solution with file %q to identify quality issues

STEP 2 — IDENTIFY optimization opportunities:
%s

STEP 3 — PRIORITIZE recommendations:
Present findings as a prioritized list:

**High Impact:**
- Changes that improve execution speed or correctness
- Missing validations that could prevent runtime errors

**Medium Impact:**
- Readability improvements that help maintainability
- Test coverage gaps

**Low Impact:**
- Cosmetic naming/formatting improvements
- Documentation additions

For each recommendation, provide:
- What to change (specific YAML snippet)
- Why it helps
- Expected impact

STEP 4 — VALIDATE changes (if user applies them):
5. Call lint_solution to verify no regressions
6. Call preview_resolvers to confirm resolver outputs are unchanged
7. Call extract_resolver_refs on any modified templates to verify references
8. If tests exist, call run_solution_tests to ensure nothing broke

IMPORTANT:
- Optimizations MUST be behavior-preserving unless explicitly noted
- Flag any changes that alter execution order or outputs
- Performance changes (parallelization) should be safe — only parallelize truly independent items`, pathRef, inlineNote, focus, pathRef, pathRef, pathRef, pathRef, focusGuide)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Optimize solution (%s): %s", focus, pathRef),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: s.replaceName(prompt),
				},
			},
		},
	}, nil
}

// optimizationFocusGuide returns focus-specific optimization instructions.
func optimizationFocusGuide(focus string) string {
	switch focus {
	case "performance":
		return `PERFORMANCE ANALYSIS:
- Identify resolvers that run serially but could run in parallel (no shared dependencies)
- Find unnecessary dependsOn chains — remove dependencies that aren't actually needed
- Look for duplicate resolver work (two resolvers fetching the same data)
- Check action parallelization opportunities (independent actions in different phases)
- Look for expensive operations (HTTP calls, exec commands) that could be cached or avoided
- Check if resolver types are set correctly (wrong types cause unnecessary conversion overhead)`
	case "readability":
		return `READABILITY ANALYSIS:
- Check naming consistency: resolver names, action names, parameter names (should be lowercase-with-hyphens)
- Look for missing descriptions on resolvers and actions
- Find overly complex CEL expressions that could be simplified or split
- Identify undocumented parameters (parameter provider resolvers without descriptions)
- Check for magic values that should be named resolvers
- Look for duplicate template logic that could be extracted
- Verify display_name fields are set for user-facing resolvers`
	case "testing":
		return `TESTING ANALYSIS:
- Call list_tests to see existing test coverage
- Call generate_test_scaffold to see what tests are recommended
- Identify untested resolvers (especially those with complex transform/validate phases)
- Look for missing edge case tests (empty inputs, boundary values, error conditions)
- Check for untested conditional logic (when clauses)
- Verify error path testing (expected failures, validation errors)
- Look for missing integration tests if the solution has external dependencies`
	default:
		return `COMPREHENSIVE ANALYSIS (all areas):

PERFORMANCE:
- Identify serial resolvers that could run in parallel
- Find unnecessary dependency chains
- Look for duplicate work and caching opportunities

READABILITY:
- Check naming consistency and missing descriptions
- Simplify complex expressions
- Document undocumented parameters

TESTING:
- Identify untested resolvers and actions
- Look for missing edge case coverage
- Call list_tests and generate_test_scaffold for gap analysis`
	}
}
