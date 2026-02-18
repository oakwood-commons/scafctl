// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSolutionResource(t *testing.T) {
	t.Run("returns YAML for a valid solution file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Create a minimal solution file
		dir := t.TempDir()
		solPath := filepath.Join(dir, "test-solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
  description: A test solution for resource testing
spec:
  resolvers:
    greeting:
      description: A greeting message
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Hello!"
`
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o644))

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://" + solPath

		contents, err := srv.handleSolutionResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok, "expected TextResourceContents")
		assert.Equal(t, "application/yaml", textContent.MIMEType)
		assert.Contains(t, textContent.Text, "test-solution")
		assert.Contains(t, textContent.Text, "greeting")
		assert.Equal(t, "solution://"+solPath, textContent.URI)
	})

	t.Run("returns error for nonexistent solution", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution:///nonexistent/solution.yaml"

		_, err = srv.handleSolutionResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading solution")
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://"

		_, err = srv.handleSolutionResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "solution name is required")
	})

	t.Run("handles example solution file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://../../examples/actions/hello-world.yaml"

		contents, err := srv.handleSolutionResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok, "expected TextResourceContents")
		assert.Contains(t, textContent.Text, "hello-world")
	})
}

func TestHandleSolutionSchemaResource(t *testing.T) {
	t.Run("returns schema for solution with parameter resolvers", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Create a solution with parameter resolvers
		dir := t.TempDir()
		solPath := filepath.Join(dir, "param-solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: param-solution
  version: 1.0.0
  description: Solution with parameter resolvers
spec:
  resolvers:
    environment:
      description: Target deployment environment
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
    region:
      description: Cloud region
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
          - provider: static
            inputs:
              value: us-east-1
    greeting:
      description: Static greeting
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o644))

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://" + solPath + "/schema"

		contents, err := srv.handleSolutionSchemaResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok, "expected TextResourceContents")
		assert.Equal(t, "application/json", textContent.MIMEType)

		// Parse the schema
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(textContent.Text), &schema))
		assert.Equal(t, "object", schema["type"])
		assert.Contains(t, schema["$schema"], "json-schema.org")
		assert.Contains(t, schema["title"], "param-solution")

		// Check properties
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok, "expected properties to be a map")

		// environment uses only parameter provider → should be in properties and required
		envProp, ok := properties["environment"].(map[string]any)
		require.True(t, ok, "expected environment property")
		assert.Equal(t, "string", envProp["type"])
		assert.Equal(t, "Target deployment environment", envProp["description"])

		// region uses parameter + static fallback → should be in properties but NOT required
		regionProp, ok := properties["region"].(map[string]any)
		require.True(t, ok, "expected region property")
		assert.Equal(t, "string", regionProp["type"])

		// greeting uses only static → should NOT be in properties
		_, hasGreeting := properties["greeting"]
		assert.False(t, hasGreeting, "greeting should not be in properties (not a parameter resolver)")

		// Check required
		required, ok := schema["required"].([]any)
		require.True(t, ok, "expected required to be an array")
		assert.Contains(t, required, "environment")
		// region has a fallback, so it should NOT be required
		assert.NotContains(t, required, "region")
	})

	t.Run("returns empty schema for solution without resolvers", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		dir := t.TempDir()
		solPath := filepath.Join(dir, "empty-solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-solution
  version: 1.0.0
  description: Solution with no resolvers
`
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o644))

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://" + solPath + "/schema"

		contents, err := srv.handleSolutionSchemaResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok)

		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(textContent.Text), &schema))
		assert.Equal(t, "object", schema["type"])

		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Empty(t, properties)
	})

	t.Run("returns error for nonexistent solution", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution:///nonexistent/solution.yaml/schema"

		_, err = srv.handleSolutionSchemaResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading solution")
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution:///schema"

		_, err = srv.handleSolutionSchemaResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "solution name is required")
	})

	t.Run("handles resolver type mapping", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		dir := t.TempDir()
		solPath := filepath.Join(dir, "typed-solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: typed-solution
  version: 1.0.0
spec:
  resolvers:
    count:
      description: Number of instances
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: count
    enabled:
      description: Enable feature
      type: bool
      resolve:
        with:
          - provider: parameter
            inputs:
              key: enabled
    ratio:
      description: Scaling ratio
      type: float
      resolve:
        with:
          - provider: parameter
            inputs:
              key: ratio
    tags:
      description: Resource tags
      type: array
      resolve:
        with:
          - provider: parameter
            inputs:
              key: tags
    config:
      description: Configuration object
      type: object
      resolve:
        with:
          - provider: parameter
            inputs:
              key: config
`
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o644))

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "solution://" + solPath + "/schema"

		contents, err := srv.handleSolutionSchemaResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok)

		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(textContent.Text), &schema))

		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		// Verify type mappings
		countProp := properties["count"].(map[string]any)
		assert.Equal(t, "integer", countProp["type"])

		enabledProp := properties["enabled"].(map[string]any)
		assert.Equal(t, "boolean", enabledProp["type"])

		ratioProp := properties["ratio"].(map[string]any)
		assert.Equal(t, "number", ratioProp["type"])

		tagsProp := properties["tags"].(map[string]any)
		assert.Equal(t, "array", tagsProp["type"])

		configProp := properties["config"].(map[string]any)
		assert.Equal(t, "object", configProp["type"])
	})
}

func TestExtractNameFromURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		scheme   string
		expected string
	}{
		{
			name:     "simple path",
			uri:      "solution:///path/to/solution.yaml",
			scheme:   "solution://",
			expected: "/path/to/solution.yaml",
		},
		{
			name:     "with schema suffix",
			uri:      "solution:///path/to/solution.yaml/schema",
			scheme:   "solution://",
			expected: "/path/to/solution.yaml/schema",
		},
		{
			name:     "catalog name",
			uri:      "solution://my-solution",
			scheme:   "solution://",
			expected: "my-solution",
		},
		{
			name:     "empty after scheme",
			uri:      "solution://",
			scheme:   "solution://",
			expected: "",
		},
		{
			name:     "wrong scheme",
			uri:      "other://something",
			scheme:   "solution://",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractNameFromURI(tt.uri, tt.scheme)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateSolutionInputSchema(t *testing.T) {
	t.Run("nil solution spec", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "empty"

		schema := generateSolutionInputSchema(sol)
		assert.Equal(t, "object", schema["type"])
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Empty(t, properties)
	})
}

func TestResolverTypeToJSONSchemaType(t *testing.T) {
	tests := []struct {
		resolverType string
		expected     string
	}{
		{"string", "string"},
		{"int", "integer"},
		{"float", "number"},
		{"bool", "boolean"},
		{"array", "array"},
		{"object", "object"},
		{"time", "string"},
		{"duration", "string"},
		{"any", ""},
		{"", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.resolverType, func(t *testing.T) {
			result := resolverTypeToJSONSchemaType(resolver.Type(tt.resolverType))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleProviderResource(t *testing.T) {
	t.Run("returns provider details for valid provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://exec"

		contents, err := srv.handleProviderResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok, "expected TextResourceContents")
		assert.Equal(t, "application/json", textContent.MIMEType)

		// Parse the response
		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(textContent.Text), &detail))

		// Verify key fields
		assert.Equal(t, "exec", detail["name"])
		assert.NotEmpty(t, detail["description"])
		assert.NotEmpty(t, detail["capabilities"])

		// Verify schema is included with properties
		schema, ok := detail["schema"].(map[string]any)
		require.True(t, ok, "expected schema to be a map")
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok, "expected schema to have properties")
		assert.NotEmpty(t, properties, "expected at least one schema property")

		// Verify command is a required input
		cmdProp, ok := properties["command"].(map[string]any)
		require.True(t, ok, "expected 'command' property in schema")
		assert.Equal(t, true, cmdProp["required"])
		assert.Equal(t, "string", cmdProp["type"])

		// Verify examples are included
		examples, ok := detail["examples"].([]any)
		require.True(t, ok, "expected examples to be an array")
		assert.NotEmpty(t, examples, "expected at least one example")

		// Verify CLI usage is included
		cliUsage, ok := detail["cliUsage"].([]any)
		require.True(t, ok, "expected cliUsage to be an array")
		assert.NotEmpty(t, cliUsage, "expected at least one CLI usage example")

		// Verify output schemas are included
		outputSchemas, ok := detail["outputSchemas"].(map[string]any)
		require.True(t, ok, "expected outputSchemas to be a map")
		assert.NotEmpty(t, outputSchemas, "expected at least one output schema")
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://nonexistent-provider"

		_, err = srv.handleProviderResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://"

		_, err = srv.handleProviderResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider name is required")
	})

	t.Run("returns error when registry nil", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.registry = nil

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://exec"

		_, err = srv.handleProviderResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry not available")
	})
}

func TestHandleProviderReferenceResource(t *testing.T) {
	t.Run("returns compact reference for all providers", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://reference"

		contents, err := srv.handleProviderReferenceResource(context.Background(), request)
		require.NoError(t, err)
		require.Len(t, contents, 1)

		textContent, ok := mcp.AsTextResourceContents(contents[0])
		require.True(t, ok, "expected TextResourceContents")
		assert.Equal(t, "application/json", textContent.MIMEType)

		// Parse the response
		var reference []map[string]any
		require.NoError(t, json.Unmarshal([]byte(textContent.Text), &reference))
		assert.NotEmpty(t, reference, "expected at least one provider in reference")

		// Verify each entry has required fields
		for _, entry := range reference {
			assert.NotEmpty(t, entry["name"], "provider reference entry should have name")
			assert.NotEmpty(t, entry["description"], "provider reference entry should have description")
			assert.NotEmpty(t, entry["capabilities"], "provider reference entry should have capabilities")
		}

		// Find exec provider and verify it has required/optional inputs
		var execEntry map[string]any
		for _, entry := range reference {
			if entry["name"] == "exec" {
				execEntry = entry
				break
			}
		}
		require.NotNil(t, execEntry, "expected exec provider in reference")

		requiredInputs, ok := execEntry["requiredInputs"].(map[string]any)
		require.True(t, ok, "expected exec to have requiredInputs")
		assert.Contains(t, requiredInputs, "command", "exec should require 'command' input")

		optionalInputs, ok := execEntry["optionalInputs"].(map[string]any)
		require.True(t, ok, "expected exec to have optionalInputs")
		assert.NotEmpty(t, optionalInputs, "exec should have optional inputs")
	})

	t.Run("returns error when registry nil", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.registry = nil

		request := mcp.ReadResourceRequest{}
		request.Params.URI = "provider://reference"

		_, err = srv.handleProviderReferenceResource(context.Background(), request)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry not available")
	})
}

func TestGetProviderSchemaReturnsStructuredOutput(t *testing.T) {
	t.Run("returns structured detail with per-property required annotations", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_provider_schema"
		request.Params.Arguments = map[string]any{
			"name": "exec",
		}

		result, err := srv.handleGetProviderSchema(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &detail))

		// Verify it uses the structured format (not raw descriptor)
		assert.Equal(t, "exec", detail["name"])

		// Schema should have flattened required per-property
		schema, ok := detail["schema"].(map[string]any)
		require.True(t, ok)
		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		cmdProp, ok := props["command"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, cmdProp["required"], "command should be marked as required")
		assert.Equal(t, "string", cmdProp["type"])

		// CLI usage should be included
		_, hasCLIUsage := detail["cliUsage"]
		assert.True(t, hasCLIUsage, "expected cliUsage in response")
	})
}

func TestServerRegistersProviderResources(t *testing.T) {
	reg := provider.NewRegistry()
	srv, err := NewServer(
		WithServerRegistry(reg),
		WithServerVersion("test"),
	)
	require.NoError(t, err)

	// Verify the server was created and provider resources are reachable by calling handlers
	// The provider://{name} template handler should work (returns error for empty registry, not panic)
	request := mcp.ReadResourceRequest{}
	request.Params.URI = "provider://exec"
	_, err = srv.handleProviderResource(context.Background(), request)
	// We expect an error here since the registry has no providers registered, but no panic
	assert.Error(t, err, "expected error for empty registry")

	// The provider://reference handler should return empty list for empty registry
	refRequest := mcp.ReadResourceRequest{}
	refRequest.Params.URI = "provider://reference"
	contents, err := srv.handleProviderReferenceResource(context.Background(), refRequest)
	require.NoError(t, err)
	require.Len(t, contents, 1)
	textContent, ok := mcp.AsTextResourceContents(contents[0])
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "[]")
}
