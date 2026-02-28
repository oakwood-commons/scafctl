// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package scaffold provides solution scaffolding logic for generating
// skeleton solution YAML files from parameters.
package scaffold

import (
	"fmt"
	"sort"
	"strings"
)

// Options configures the scaffolding operation.
type Options struct {
	// Name is the solution name (lowercase with hyphens, 3-60 chars).
	Name string `json:"name" doc:"Solution name" maxLength:"60" example:"my-solution"`
	// Description is a brief description of what the solution does.
	Description string `json:"description" doc:"Solution description" maxLength:"200" example:"Deploy to Kubernetes"`
	// Version is the semver version string (default: "1.0.0").
	Version string `json:"version" doc:"Semantic version" example:"1.0.0"`
	// Features is a map of features to include in the scaffold.
	// Valid keys: parameters, resolvers, actions, transforms, validation, tests, composition.
	Features map[string]bool `json:"features" doc:"Features to include"`
	// Providers lists specific providers to include examples for.
	Providers []string `json:"providers" doc:"Provider-specific examples to include"`
}

// Result contains the output of a scaffolding operation.
type Result struct {
	// YAML is the generated solution YAML content.
	YAML string `json:"yaml" doc:"Generated YAML content"`
	// Filename is the suggested filename for the solution.
	Filename string `json:"filename" doc:"Suggested filename" example:"./my-solution.yaml"`
	// Features lists the features included in the scaffold.
	Features []string `json:"features" doc:"Included features"`
	// NextSteps provides guidance for the user.
	NextSteps []string `json:"nextSteps" doc:"Recommended next steps"`
}

// ValidFeatures are the recognized feature names.
var ValidFeatures = []string{
	"parameters", "resolvers", "actions", "transforms",
	"validation", "tests", "composition",
}

// Solution generates a skeleton solution YAML from the given options.
// If no features are specified, defaults to parameters and resolvers.
// If version is empty, defaults to "1.0.0".
func Solution(opts Options) *Result {
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}

	// Default features if none specified
	if len(opts.Features) == 0 {
		opts.Features = map[string]bool{
			"parameters": true,
			"resolvers":  true,
			"actions":    true,
			"transforms": true,
			"validation": true,
			"tests":      true,
		}
	}

	yaml := BuildYAML(opts.Name, opts.Description, opts.Version, opts.Features, opts.Providers)

	return &Result{
		YAML:     yaml,
		Filename: fmt.Sprintf("./%s.yaml", opts.Name),
		Features: FeatureKeys(opts.Features),
		NextSteps: []string{
			"Save the YAML to a file",
			"Customize the resolver and action values for your use case",
			"Run lint_solution to validate the structure",
			"Run preview_resolvers to test resolver outputs",
			"Run get_run_command to see how to execute it",
		},
	}
}

// BuildYAML generates solution YAML from scaffold parameters.
func BuildYAML(name, description, version string, features map[string]bool, providers []string) string {
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

	// Resolvers
	b.WriteString("\nspec:\n")
	if features["resolvers"] || features["parameters"] || features["transforms"] || features["validation"] {
		b.WriteString("  resolvers:\n")

		if features["parameters"] {
			b.WriteString("    # User-provided input parameter\n")
			b.WriteString("    inputName:\n")
			b.WriteString("      type: string\n")
			b.WriteString("      description: \"A user-provided input value\"\n")
			b.WriteString("      example: \"my-value\"\n")
			b.WriteString("      resolve:\n")
			b.WriteString("        with:\n")
			b.WriteString("          - provider: parameter\n")
			b.WriteString("            inputs:\n")
			b.WriteString("              key: inputName\n")
			b.WriteString("          - provider: static\n")
			b.WriteString("            inputs:\n")
			b.WriteString("              value: my-value\n")

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
				b.WriteString("        - inputName\n")
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
				b.WriteString("              url: \"https://httpbin.org/get\"\n")
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
				b.WriteString("              operation: get\n")
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
				b.WriteString("        provider: exec\n")
				b.WriteString("        description: \"Execute a shell command\"\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          command: \"echo 'Hello from scaffold'\"\n")
			case "directory":
				b.WriteString("      # Create a directory structure\n")
				b.WriteString("      create-output:\n")
				b.WriteString("        provider: directory\n")
				b.WriteString("        description: \"Create output directory\"\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          operation: create\n")
				b.WriteString("          path: \"./output\"\n")
			case "go-template":
				b.WriteString("      # Render a template\n")
				b.WriteString("      render-template:\n")
				b.WriteString("        provider: go-template\n")
				b.WriteString("        description: \"Render a Go template\"\n")
				b.WriteString("        inputs:\n")
				b.WriteString("          template: \"Hello {{ .Name }}\"\n")
				b.WriteString("          output: \"./output/greeting.txt\"\n")
			}
		}

		// Default action if no provider-specific ones were added
		if !hasExec && len(providers) == 0 {
			b.WriteString("      # Example action\n")
			b.WriteString("      hello:\n")
			b.WriteString("        provider: exec\n")
			b.WriteString("        description: \"A simple action\"\n")
			b.WriteString("        inputs:\n")
			b.WriteString("          command:\n")
			if features["transforms"] {
				b.WriteString("            expr: '\"echo Hello, \" + _.inputName + \" - processed: \" + _.processed'\n")
			} else {
				b.WriteString("            expr: '\"echo Hello, \" + _.inputName'\n")
			}
		}
	}

	// Tests
	if features["tests"] {
		b.WriteString("\n  testing:\n")
		b.WriteString("    cases:\n")
		b.WriteString("      # Verify resolvers produce expected values\n")
		b.WriteString("      basic-resolve:\n")
		b.WriteString("        description: \"Verify resolvers resolve successfully\"\n")
		b.WriteString("        command: [run, resolver]\n")
		if features["parameters"] {
			b.WriteString("        args: [\"-o\", \"json\", \"-r\", \"inputName=test-value\"]\n")
		} else {
			b.WriteString("        args: [\"-o\", \"json\"]\n")
		}
		b.WriteString("        assertions:\n")
		b.WriteString("          - expression: __exitCode == 0\n")
		if features["parameters"] {
			b.WriteString("          - expression: __output.inputName == \"test-value\"\n")
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

// FeatureKeys returns sorted keys from a features map.
func FeatureKeys(features map[string]bool) []string {
	keys := make([]string, 0, len(features))
	for k := range features {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
