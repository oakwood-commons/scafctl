// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerScaffoldTools registers solution scaffolding MCP tools.
func (s *Server) registerScaffoldTools() {
	// scaffold_solution
	scaffoldSolutionTool := mcp.NewTool("scaffold_solution",
		mcp.WithDescription("Generate a valid skeleton solution YAML from parameters. Produces a guaranteed-valid starting point with the correct structure, including metadata, resolvers, workflow, and tests based on selected features. The generated YAML can be immediately linted and customized."),
		mcp.WithTitleAnnotation("Scaffold Solution"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Solution name (lowercase with hyphens, 3-60 chars, e.g., 'my-solution')"),
		),
		mcp.WithString("description",
			mcp.Required(),
			mcp.Description("Brief description of what the solution does"),
		),
		mcp.WithString("version",
			mcp.Description("Semantic version (default: '1.0.0')"),
		),
		mcp.WithArray("features",
			mcp.Description("Features to include in the scaffold. Options: parameters, resolvers, actions, transforms, validation, tests, composition"),
		),
		mcp.WithArray("providers",
			mcp.Description("Specific providers to include examples for (e.g., ['http', 'exec', 'cel']). Use list_providers to see available providers."),
		),
	)
	s.mcpServer.AddTool(scaffoldSolutionTool, s.handleScaffoldSolution)
}

// handleScaffoldSolution generates a skeleton solution YAML.
func (s *Server) handleScaffoldSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	description, err := request.RequireString("description")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	version := request.GetString("version", "1.0.0")

	// Parse features
	features := make(map[string]bool)
	args := request.GetArguments()
	if featuresRaw, ok := args["features"]; ok && featuresRaw != nil {
		if featureSlice, ok := featuresRaw.([]any); ok {
			for _, f := range featureSlice {
				if fs, ok := f.(string); ok {
					features[fs] = true
				}
			}
		}
	}

	// Parse providers
	var providerNames []string
	if providersRaw, ok := args["providers"]; ok && providersRaw != nil {
		if providerSlice, ok := providersRaw.([]any); ok {
			for _, p := range providerSlice {
				if ps, ok := p.(string); ok {
					providerNames = append(providerNames, ps)
				}
			}
		}
	}

	// Default features if none specified
	if len(features) == 0 {
		features["parameters"] = true
		features["resolvers"] = true
	}

	yaml := buildScaffoldYAML(name, description, version, features, providerNames)

	return mcp.NewToolResultJSON(map[string]any{
		"yaml":     yaml,
		"filename": fmt.Sprintf("%s.yaml", name),
		"features": featureKeys(features),
		"nextSteps": []string{
			"Save the YAML to a file",
			"Customize the resolver and action values for your use case",
			"Run lint_solution to validate the structure",
			"Run preview_resolvers to test resolver outputs",
			"Run get_run_command to see how to execute it",
		},
	})
}

// buildScaffoldYAML generates solution YAML from scaffold parameters.
func buildScaffoldYAML(name, description, version string, features map[string]bool, providers []string) string {
	var b strings.Builder

	// Header
	b.WriteString("apiVersion: scafctl.io/v1\n")
	b.WriteString("kind: Solution\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", name)
	fmt.Fprintf(&b, "  version: \"%s\"\n", version)
	fmt.Fprintf(&b, "  description: %s\n", description)
	b.WriteString("  tags:\n")
	b.WriteString("    - scaffold\n")
	b.WriteString("\nspec:\n")

	// Resolvers
	if features["resolvers"] || features["parameters"] || features["transforms"] || features["validation"] {
		b.WriteString("  resolvers:\n")

		if features["parameters"] {
			b.WriteString("    # User-provided input parameter\n")
			b.WriteString("    input-name:\n")
			b.WriteString("      type: string\n")
			b.WriteString("      description: \"A user-provided input value\"\n")
			b.WriteString("      example: \"my-value\"\n")
			b.WriteString("      resolve:\n")
			b.WriteString("        with:\n")
			b.WriteString("          - provider: parameter\n")

			if features["validation"] {
				b.WriteString("      validate:\n")
				b.WriteString("        with:\n")
				b.WriteString("          - provider: validation\n")
				b.WriteString("            inputs:\n")
				b.WriteString("              expression: 'size(__self) >= 1 && size(__self) <= 100'\n")
				b.WriteString("            message: \"Input must be between 1 and 100 characters\"\n")
			}
		}

		if features["resolvers"] && !features["parameters"] {
			b.WriteString("    # Static value resolver\n")
			b.WriteString("    config-value:\n")
			b.WriteString("      type: string\n")
			b.WriteString("      description: \"A static configuration value\"\n")
			b.WriteString("      resolve:\n")
			b.WriteString("        with:\n")
			b.WriteString("          - provider: static\n")
			b.WriteString("            inputs:\n")
			b.WriteString("              value: \"default-value\"\n")
		}

		if features["transforms"] {
			b.WriteString("    # Transformed value\n")
			b.WriteString("    processed:\n")
			b.WriteString("      type: string\n")
			b.WriteString("      description: \"A value processed through a transform\"\n")
			if features["parameters"] {
				b.WriteString("      dependsOn:\n")
				b.WriteString("        - input-name\n")
			}
			b.WriteString("      resolve:\n")
			b.WriteString("        with:\n")
			b.WriteString("          - provider: static\n")
			b.WriteString("            inputs:\n")
			b.WriteString("              value: \"raw-data\"\n")
			b.WriteString("      transform:\n")
			b.WriteString("        with:\n")
			b.WriteString("          - provider: cel\n")
			b.WriteString("            inputs:\n")
			b.WriteString("              expression: '__self.upperAscii()'\n")
		}

		// Provider-specific resolver examples
		for _, p := range providers {
			switch p {
			case "http":
				b.WriteString("    # HTTP API call\n")
				b.WriteString("    api-data:\n")
				b.WriteString("      type: object\n")
				b.WriteString("      description: \"Data fetched from an API\"\n")
				b.WriteString("      resolve:\n")
				b.WriteString("        with:\n")
				b.WriteString("          - provider: http\n")
				b.WriteString("            inputs:\n")
				b.WriteString("              url: \"https://api.example.com/data\"\n")
				b.WriteString("              method: GET\n")
			case "env":
				b.WriteString("    # Environment variable\n")
				b.WriteString("    env-value:\n")
				b.WriteString("      type: string\n")
				b.WriteString("      description: \"Value from environment variable\"\n")
				b.WriteString("      resolve:\n")
				b.WriteString("        with:\n")
				b.WriteString("          - provider: env\n")
				b.WriteString("            inputs:\n")
				b.WriteString("              name: MY_ENV_VAR\n")
			case "file":
				b.WriteString("    # File content\n")
				b.WriteString("    file-content:\n")
				b.WriteString("      type: string\n")
				b.WriteString("      description: \"Content read from a file\"\n")
				b.WriteString("      resolve:\n")
				b.WriteString("        with:\n")
				b.WriteString("          - provider: file\n")
				b.WriteString("            inputs:\n")
				b.WriteString("              path: \"./config.json\"\n")
			}
		}
	}

	// Workflow / Actions
	if features["actions"] {
		b.WriteString("\n  workflow:\n")
		b.WriteString("    actions:\n")

		hasExec := false
		for _, p := range providers {
			switch p {
			case "exec":
				hasExec = true
				b.WriteString("      # Execute a command\n")
				b.WriteString("      run-command:\n")
				b.WriteString("        description: \"Execute a shell command\"\n")
				b.WriteString("        provider: exec\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          command: \"echo 'Hello from scaffold'\"\n")
			case "directory":
				b.WriteString("      # Create a directory structure\n")
				b.WriteString("      create-output:\n")
				b.WriteString("        description: \"Create output directory\"\n")
				b.WriteString("        provider: directory\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          operation: create\n")
				b.WriteString("          path: \"./output\"\n")
			case "go-template":
				b.WriteString("      # Render a template\n")
				b.WriteString("      render-template:\n")
				b.WriteString("        description: \"Render a Go template\"\n")
				b.WriteString("        provider: go-template\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          template: \"Hello {{ .Name }}\"\n")
				b.WriteString("          output: \"./output/greeting.txt\"\n")
			}
		}

		// Default action if no provider-specific ones were added
		if !hasExec && len(providers) == 0 {
			b.WriteString("      # Example action\n")
			b.WriteString("      hello:\n")
			b.WriteString("        description: \"A simple action\"\n")
			b.WriteString("        provider: exec\n")
			b.WriteString("        inputs:\n")
			b.WriteString("          command:\n")
			b.WriteString("            expr: '\"echo Hello, \" + resolvers.input_name'\n")
		}
	}

	// Tests
	if features["tests"] {
		b.WriteString("\n  testing:\n")
		b.WriteString("    cases:\n")
		b.WriteString("      # Basic lint validation\n")
		b.WriteString("      basic-render:\n")
		b.WriteString("        description: \"Verify solution renders successfully\"\n")
		b.WriteString("        command: [render, solution]\n")
		b.WriteString("        args: [\"-o\", \"json\"]\n")

		if features["parameters"] {
			b.WriteString("        resolvers:\n")
			b.WriteString("          input-name: \"test-value\"\n")
		}

		b.WriteString("        assertions:\n")
		b.WriteString("          - expression: __exitCode == 0\n")

		if features["parameters"] {
			b.WriteString("          - expression: __output.resolvers.input_name == \"test-value\"\n")
		}
	}

	// Composition
	if features["composition"] {
		b.WriteString("\n# Uncomment to compose with partial solutions:\n")
		b.WriteString("# compose:\n")
		b.WriteString("#   - ./common-resolvers.yaml\n")
		b.WriteString("#   - ./shared-actions.yaml\n")
	}

	return b.String()
}

// featureKeys returns sorted keys from a features map.
func featureKeys(features map[string]bool) []string {
	keys := make([]string, 0, len(features))
	for k := range features {
		keys = append(keys, k)
	}
	return keys
}
