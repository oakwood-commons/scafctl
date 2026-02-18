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
  scafctl run solution -f ./%s.yaml -r param1=value1 -r param2=value2`, name, description, featureList, name, name)

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
- Missing dependsOn when referencing other resolvers/actions`, path, problemDesc, path, path)

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
