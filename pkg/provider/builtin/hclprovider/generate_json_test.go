// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHCLJSON_Variables(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":        "region",
				"type":        "string",
				"default":     "us-east-1",
				"description": "AWS region",
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	vars, ok := parsed["variable"].(map[string]any)
	require.True(t, ok)
	region, ok := vars["region"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", region["type"])
	assert.Equal(t, "us-east-1", region["default"])
	assert.Equal(t, "AWS region", region["description"])
}

func TestGenerateHCLJSON_Resources(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_instance",
				"name": "web",
				"attributes": map[string]any{
					"ami":           "ami-12345",
					"instance_type": "t3.micro",
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	resources := parsed["resource"].(map[string]any)
	awsInstance := resources["aws_instance"].(map[string]any)
	web := awsInstance["web"].(map[string]any)
	assert.Equal(t, "ami-12345", web["ami"])
	assert.Equal(t, "t3.micro", web["instance_type"])
}

func TestGenerateHCLJSON_DataSources(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"data": []any{
			map[string]any{
				"type": "aws_ami",
				"name": "latest",
				"attributes": map[string]any{
					"most_recent": true,
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	data := parsed["data"].(map[string]any)
	ami := data["aws_ami"].(map[string]any)
	latest := ami["latest"].(map[string]any)
	assert.Equal(t, true, latest["most_recent"])
}

func TestGenerateHCLJSON_Outputs(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"outputs": []any{
			map[string]any{
				"name":        "id",
				"value":       "aws_instance.web.id",
				"description": "Instance ID",
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	outputs := parsed["output"].(map[string]any)
	id := outputs["id"].(map[string]any)
	assert.Equal(t, "aws_instance.web.id", id["value"])
	assert.Equal(t, "Instance ID", id["description"])
}

func TestGenerateHCLJSON_Locals(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"locals": map[string]any{
			"env":    "prod",
			"region": "us-east-1",
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	locals := parsed["locals"].(map[string]any)
	assert.Equal(t, "prod", locals["env"])
	assert.Equal(t, "us-east-1", locals["region"])
}

func TestGenerateHCLJSON_Terraform(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"terraform": map[string]any{
			"required_version": ">= 1.0",
			"required_providers": map[string]any{
				"aws": map[string]any{
					"source":  "hashicorp/aws",
					"version": "~> 5.0",
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	tf := parsed["terraform"].(map[string]any)
	assert.Equal(t, ">= 1.0", tf["required_version"])
	rp := tf["required_providers"].(map[string]any)
	aws := rp["aws"].(map[string]any)
	assert.Equal(t, "hashicorp/aws", aws["source"])
}

func TestGenerateHCLJSON_Modules(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"modules": []any{
			map[string]any{
				"name":   "vpc",
				"source": "./modules/vpc",
				"attributes": map[string]any{
					"cidr": "10.0.0.0/16",
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	modules := parsed["module"].(map[string]any)
	vpc := modules["vpc"].(map[string]any)
	assert.Equal(t, "./modules/vpc", vpc["source"])
	assert.Equal(t, "10.0.0.0/16", vpc["cidr"])
}

func TestGenerateHCLJSON_Providers(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"providers": []any{
			map[string]any{
				"name":  "aws",
				"alias": "west",
				"attributes": map[string]any{
					"region": "us-west-2",
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	providers := parsed["provider"].(map[string]any)
	aws := providers["aws"].(map[string]any)
	assert.Equal(t, "west", aws["alias"])
	assert.Equal(t, "us-west-2", aws["region"])
}

func TestGenerateHCLJSON_MultipleProvidersAliased(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"providers": []any{
			map[string]any{
				"name":  "aws",
				"alias": "east",
				"attributes": map[string]any{
					"region": "us-east-1",
				},
			},
			map[string]any{
				"name":  "aws",
				"alias": "west",
				"attributes": map[string]any{
					"region": "us-west-2",
				},
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	providers := parsed["provider"].(map[string]any)
	awsArr := providers["aws"].([]any)
	require.Len(t, awsArr, 2)
}

func TestGenerateHCLJSON_Moved(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"moved": []any{
			map[string]any{
				"from": "aws_instance.old",
				"to":   "aws_instance.new",
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	moved := parsed["moved"].([]any)
	require.Len(t, moved, 1)
	first := moved[0].(map[string]any)
	assert.Equal(t, "aws_instance.old", first["from"])
	assert.Equal(t, "aws_instance.new", first["to"])
}

func TestGenerateHCLJSON_Import(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"import": []any{
			map[string]any{
				"to": "aws_instance.web",
				"id": "i-12345",
			},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	imports := parsed["import"].([]any)
	require.Len(t, imports, 1)
	first := imports[0].(map[string]any)
	assert.Equal(t, "aws_instance.web", first["to"])
	assert.Equal(t, "i-12345", first["id"])
}

func TestGenerateHCLJSON_EmptyInput(t *testing.T) {
	t.Parallel()
	result, err := GenerateHCLJSON(map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "{}\n", result)
}

func TestGenerateHCLJSON_MultipleBlockTypes(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{"name": "env", "type": "string"},
		},
		"resources": []any{
			map[string]any{"type": "aws_instance", "name": "web", "attributes": map[string]any{"ami": "ami-123"}},
		},
		"outputs": []any{
			map[string]any{"name": "id", "value": "aws_instance.web.id"},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	assert.Contains(t, parsed, "variable")
	assert.Contains(t, parsed, "resource")
	assert.Contains(t, parsed, "output")
}

func TestGenerateHCLJSON_ValidJSON(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{"name": "region", "type": "string", "default": "us-east-1"},
		},
		"resources": []any{
			map[string]any{"type": "aws_instance", "name": "web", "attributes": map[string]any{
				"ami":           "ami-12345",
				"instance_type": "t3.micro",
				"tags":          map[string]any{"Name": "web-server"},
			}},
		},
	}

	result, err := GenerateHCLJSON(input)
	require.NoError(t, err)

	// Must be valid JSON
	assert.True(t, json.Valid([]byte(result)))
}

// --- Provider-level integration tests ---

func TestHCLProvider_Execute_Generate_JSON(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation":     "generate",
		"output_format": "json",
		"blocks": map[string]any{
			"variables": []any{
				map[string]any{
					"name":    "region",
					"type":    "string",
					"default": "us-east-1",
				},
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	generated := data["hcl"].(string)
	assert.True(t, json.Valid([]byte(generated)), "output should be valid JSON")
	assert.Contains(t, generated, "variable")
	assert.Contains(t, generated, "region")
	assert.Equal(t, "generate", output.Metadata["operation"])
	assert.Equal(t, "json", output.Metadata["output_format"])
}

func TestHCLProvider_Execute_Generate_JSON_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation":     "generate",
		"output_format": "json",
		"blocks": map[string]any{
			"variables": []any{
				map[string]any{"name": "x", "type": "string"},
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.Equal(t, "", data["hcl"])
	assert.Equal(t, "dry-run", output.Metadata["mode"])
	assert.Equal(t, "json", output.Metadata["output_format"])
}

func TestHCLProvider_Execute_Generate_DefaultFormat(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "generate",
		"blocks": map[string]any{
			"variables": []any{
				map[string]any{"name": "x", "type": "string"},
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	// Default format is HCL, not JSON
	data := output.Data.(map[string]any)
	generated := data["hcl"].(string)
	assert.Contains(t, generated, `variable "x"`)
	assert.False(t, json.Valid([]byte(generated)), "default output should be HCL, not JSON")
	assert.Equal(t, "hcl", output.Metadata["output_format"])
}
