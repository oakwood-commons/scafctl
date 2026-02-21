// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerPrompts registers all MCP prompts on the server.
func (s *Server) registerPrompts() {
	// create_solution prompt
	s.mcpServer.AddPrompt(
		mcp.NewPrompt("create_solution",
			mcp.WithPromptDescription("Guide for creating a new scafctl solution YAML file from scratch. Provides the solution schema, examples, and step-by-step instructions."),
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
			mcp.WithPromptDescription("Help debug a scafctl solution that isn't working as expected. Inspects the solution, lints it, and suggests fixes."),
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
			mcp.WithPromptDescription("Guide for modifying an existing scafctl solution. Inspects the current state, makes targeted changes, then validates with lint and preview."),
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
			mcp.WithPromptDescription("Guide for writing functional tests for a scafctl solution. Walks through test schema, assertions, snapshots, and test patterns."),
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
			mcp.WithPromptDescription("Guide for designing a multi-file composed solution using scafctl's composition system. Breaks a solution into reusable partial YAML files that get merged at build time."),
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

	prompt := fmt.Sprintf(`You are creating a new scafctl solution named %q.
Description: %s

IMPORTANT: Before writing any YAML, you MUST use these tools to understand the correct schema:

1. Call get_solution_schema to get the full JSON Schema for solution YAML files
2. Call list_providers to see available providers  
3. Call get_example with path "solutions/email-notifier/solution.yaml" for a practical reference
4. Call get_example with path "solutions/comprehensive/solution.yaml" for a comprehensive reference

Requested features: %s

Follow these rules when creating the solution:
- apiVersion MUST be "scafctl.io/v1"
- kind MUST be "Solution"
- metadata.name must be lowercase with hyphens only (3-60 chars)
- metadata.version must be a valid semver (e.g., "1.0.0")
- Resolvers go under spec.resolvers.<name> with resolve/transform/validate phases
- Actions go under spec.workflow.actions.<name> with provider and inputs
- Use ValueRef format for inputs: literal (raw value), rslvr (resolver reference), expr (CEL expression), or tmpl (Go template)
- Always validate user inputs using the validate phase with the validation provider
- Use dependsOn for ordering (both resolvers and actions)
- Use when clauses (CEL expressions) for conditional execution
- For exec provider actions: the input field is "command" (string), NOT "cmd". Always use get_provider_schema to verify required fields.

NEVER invent fields that don't exist in the schema. Only use fields documented in the JSON Schema.
When using any provider in an action, ALWAYS call get_provider_schema first to verify the exact field names and types.

After creating the solution, tell the user how to run it.
IMPORTANT: Choose the correct run command based on whether the solution has a workflow:
  • If the solution has spec.workflow.actions (i.e. it performs side-effects/actions):
      scafctl run solution -f ./<filename>.yaml -r key=value
  • If the solution has ONLY resolvers and NO spec.workflow section:
      scafctl run resolver -f ./<filename>.yaml -r key=value
    'scafctl run solution' will FAIL if there is no workflow defined.
Parameters are passed with -r/--resolver (NOT -p). Example:
  scafctl run resolver -f ./%s.yaml -r param1=value1 -r param2=value2
  scafctl run solution -f ./%s.yaml -r param1=value1 -r param2=value2

After creating the solution file:
1. Call lint_solution to validate the YAML structure
2. Call preview_resolvers to verify the resolver chain produces expected values
3. Call get_run_command to show the user the exact command to run it
4. If the solution has tests, call run_solution_tests to verify they pass`, name, description, featureList, name, name)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Create a new scafctl solution: %s", name),
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

// handleDebugSolutionPrompt returns a prompt guiding the AI to debug a solution.
func (s *Server) handleDebugSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	problem := request.Params.Arguments["problem"]

	problemDesc := "The solution is not working as expected."
	if problem != "" {
		problemDesc = problem
	}

	prompt := fmt.Sprintf(`Debug the scafctl solution at %q.

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
- Missing dependsOn when referencing other resolvers/actions

7. Call preview_resolvers with path %q to check if resolvers execute successfully and produce expected values
8. If the solution has tests, call run_solution_tests with path %q to verify test results
9. Call get_run_command with path %q to verify the correct run command`, path, problemDesc, path, path, path, path, path)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Debug solution: %s", path),
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
    type: string|int|float|bool|array|object|time|duration|any
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
            expression: "<CEL validation expression>"
          message: "Error message if validation fails"

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
	featureHints = append(featureHints, "dependsOn: list of action names this depends on")
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

	prompt := fmt.Sprintf(`Modify the existing scafctl solution at %q.

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
- Always verify provider field names with get_provider_schema before writing inputs`, path, change, path, path, path, path, path)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Update solution: %s", path),
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

// handleAddTestsPrompt returns a prompt for writing functional tests.
func (s *Server) handleAddTestsPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	scope := request.Params.Arguments["scope"]

	if scope == "" {
		scope = "all"
	}

	prompt := fmt.Sprintf(`Add functional tests to the scafctl solution at %q.

Test scope: %s

Follow these steps:

STEP 1 — UNDERSTAND the solution:
1. Call inspect_solution with path %q to see resolvers, actions, and structure
2. Identify what needs testing based on the scope

STEP 2 — LEARN the test format:
3. Call explain_kind with kind "solution" and field "testing" to see test schema
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

IMPORTANT:
- Test names must be lowercase with hyphens (no spaces, underscores, or uppercase)
- Built-in tests (lint, parse) run automatically unless skipped
- Use tags to organize tests (smoke, validation, integration, etc.)
- The -f flag is auto-injected; do NOT include it in args
- Use 'render solution' with '-o json' for most resolver output testing`, path, scope, path, scopeHints(scope), path, path)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Add tests to solution: %s", path),
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

// handleComposeSolutionPrompt returns a prompt for designing a multi-file composed solution.
func (s *Server) handleComposeSolutionPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	path := request.Params.Arguments["path"]
	goal := request.Params.Arguments["goal"]

	prompt := fmt.Sprintf(`Design a multi-file composed scafctl solution.

Root path: %s
Goal: %s

Composition allows splitting a large solution into multiple partial YAML files that get merged at build time. This promotes reusability and maintainability.

STEP 1 — UNDERSTAND composition:
1. Call get_solution_schema and look at the "compose" field in the spec
2. Call get_example with path "solutions/composed/solution.yaml" or similar composition examples from list_examples (category: "solutions")

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
- Order matters: later compose entries override earlier ones for conflicting keys`, path, goal)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Compose solution: %s", path),
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
