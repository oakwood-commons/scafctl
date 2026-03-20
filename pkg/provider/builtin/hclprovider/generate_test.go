// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHCL_Variables(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `variable "region"`)
	assert.Contains(t, hcl, `"us-east-1"`)
	assert.Contains(t, hcl, `"AWS region"`)
}

func TestGenerateHCL_Resources(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `resource "aws_instance" "web"`)
	assert.Contains(t, hcl, `"ami-12345"`)
	assert.Contains(t, hcl, `"t3.micro"`)
}

func TestGenerateHCL_Outputs(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"outputs": []any{
			map[string]any{
				"name":        "result",
				"value":       "var.region",
				"description": "The region",
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `output "result"`)
	assert.Contains(t, hcl, "var.region")
	assert.Contains(t, hcl, `"The region"`)
}

func TestGenerateHCL_Locals(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"locals": map[string]any{
			"env":    "prod",
			"region": "us-east-1",
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "locals")
	assert.Contains(t, hcl, `"prod"`)
	assert.Contains(t, hcl, `"us-east-1"`)
}

func TestGenerateHCL_Terraform(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"terraform": map[string]any{
			"required_version": ">= 1.0",
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "terraform")
	assert.Contains(t, hcl, `">= 1.0"`)
}

func TestGenerateHCL_Modules(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `module "vpc"`)
	assert.Contains(t, hcl, `"./modules/vpc"`)
	assert.Contains(t, hcl, `"10.0.0.0/16"`)
}

func TestGenerateHCL_DataSources(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `data "aws_ami" "latest"`)
	assert.Contains(t, hcl, "most_recent")
}

func TestGenerateHCL_EmptyInput(t *testing.T) {
	t.Parallel()
	hcl, err := GenerateHCL(map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(hcl))
}

func TestGenerateHCL_BoolAndNumericValues(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":      "enabled",
				"default":   true,
				"sensitive": false,
			},
			map[string]any{
				"name":    "count",
				"default": float64(3),
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "true")
	assert.Contains(t, hcl, `variable "enabled"`)
	assert.Contains(t, hcl, `variable "count"`)
}

func TestGenerateHCL_Providers(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, `provider "aws"`)
	assert.Contains(t, hcl, "west")
}

func TestGenerateHCL_Moved(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"moved": []any{
			map[string]any{
				"from": "aws_instance.old",
				"to":   "aws_instance.new",
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "moved")
	assert.Contains(t, hcl, "aws_instance.old")
	assert.Contains(t, hcl, "aws_instance.new")
}

func TestGenerateHCL_Import(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"import": []any{
			map[string]any{
				"to": "aws_instance.web",
				"id": "i-12345",
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "import")
	assert.Contains(t, hcl, "aws_instance.web")
	assert.Contains(t, hcl, `"i-12345"`)
}

func TestGenerateHCL_MultipleBlockTypes(t *testing.T) {
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
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)

	// Check ordering: variables before resources before outputs.
	varIdx := strings.Index(hcl, `variable "env"`)
	resIdx := strings.Index(hcl, `resource "aws_instance" "web"`)
	outIdx := strings.Index(hcl, `output "id"`)
	assert.Greater(t, resIdx, varIdx, "resources should come after variables")
	assert.Greater(t, outIdx, resIdx, "outputs should come after resources")
}

func TestHCLProvider_Execute_Generate(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "generate",
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
	hcl := data["hcl"].(string)
	assert.Contains(t, hcl, `variable "region"`)
	assert.Contains(t, hcl, `"us-east-1"`)
	assert.Equal(t, "generate", output.Metadata["operation"])
	assert.NotZero(t, output.Metadata["bytes"])
}

func TestHCLProvider_Execute_Generate_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := provider.WithDryRun(context.Background(), true)

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
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "", data["hcl"])
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}

func TestHCLProvider_Execute_Generate_MissingBlocks(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "generate",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocks")
}

func TestGenerateHCL_Expressions(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"outputs": []any{
			map[string]any{
				"name":  "id",
				"value": "var.region",
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	// Expression-like values (var.*, local.*, etc.) should not be quoted
	assert.Contains(t, hcl, "var.region")
	assert.NotContains(t, hcl, `"var.region"`)
}

func TestGenerateHCL_NilValue(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":    "optional",
				"default": nil,
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "null")
}

func TestGenerateHCL_ListAttribute(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_security_group",
				"name": "web",
				"attributes": map[string]any{
					"ingress_ports": []any{float64(80), float64(443)},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "ingress_ports")
	assert.Contains(t, hcl, "80")
	assert.Contains(t, hcl, "443")
}

func TestGenerateHCL_MapAttribute(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_instance",
				"name": "web",
				"attributes": map[string]any{
					"tags": map[string]any{
						"Name": "web-server",
						"Env":  "prod",
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "tags")
	assert.Contains(t, hcl, `"web-server"`)
	assert.Contains(t, hcl, `"prod"`)
}

func TestGenerateHCL_VariableWithValidation(t *testing.T) {
	t.Parallel()
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":        "region",
				"type":        "string",
				"description": "AWS region",
				"validation": []any{
					map[string]any{
						"condition":     `can(regex("^us-", var.region))`,
						"error_message": "Region must start with us-",
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "validation")
	assert.Contains(t, hcl, "condition")
	assert.Contains(t, hcl, "error_message")
}

func TestGenerateHCL_TerraformInvalidValue(t *testing.T) {
	t.Parallel()
	// terraform key with a non-map value should be skipped (continue path)
	input := map[string]any{
		"terraform": "not-a-map",
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(hcl))
}

func TestGenerateHCL_TerraformThenVariables_NeedNewline(t *testing.T) {
	t.Parallel()
	// Place terraform before variables to trigger needsNewline → AppendNewline path
	input := map[string]any{
		"terraform": map[string]any{"required_version": ">= 1.0"},
		"variables": []any{
			map[string]any{"name": "env", "type": "string"},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "terraform")
	assert.Contains(t, hcl, `variable "env"`)
}

func TestGenerateHCL_LocalsInvalidValue(t *testing.T) {
	t.Parallel()
	// locals key with a non-map value should be skipped (continue path)
	input := map[string]any{
		"locals": "not-a-map",
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(hcl))
}

func TestGenerateHCL_BlockTypeNonSlice(t *testing.T) {
	t.Parallel()
	// A block type (like "variables") with a non-slice value should be skipped
	input := map[string]any{
		"variables": "not-a-slice",
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(hcl))
}

func TestGenerateHCL_BlockTypeNonMapItem(t *testing.T) {
	t.Parallel()
	// Slice with a non-map item should be skipped for that item
	input := map[string]any{
		"variables": []any{
			"not-a-map-item",
			map[string]any{"name": "valid", "type": "string"},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	// Only the valid block should appear
	assert.Contains(t, hcl, `variable "valid"`)
}

func TestGenerateHCL_CheckBlockWithAssertions(t *testing.T) {
	t.Parallel()
	// check blocks have assertions sub-blocks
	input := map[string]any{
		"check": []any{
			map[string]any{
				"name": "health_check",
				"assertions": []any{
					map[string]any{
						"condition":     "true",
						"error_message": "must be true",
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "check")
	assert.Contains(t, hcl, "assert")
	assert.Contains(t, hcl, "error_message")
}

func TestGenerateHCL_GenericSubBlocks(t *testing.T) {
	t.Parallel()
	// resources with generic blocks sub-block (ingress/egress for security groups)
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_security_group",
				"name": "web",
				"blocks": []any{
					map[string]any{
						"type": "ingress",
						"attributes": map[string]any{
							"from_port": float64(80),
							"to_port":   float64(80),
							"protocol":  "tcp",
						},
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "ingress")
	assert.Contains(t, hcl, "from_port")
}

func TestGenerateHCL_NestedSubBlocks(t *testing.T) {
	t.Parallel()
	// blocks with nested blocks (to exercise the recursive generateBlock path)
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_lb",
				"name": "main",
				"blocks": []any{
					map[string]any{
						"type": "access_logs",
						"blocks": []any{
							map[string]any{
								"type": "s3_config",
								"attributes": map[string]any{
									"bucket": "my-bucket",
								},
							},
						},
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "access_logs")
}

func TestValueToHCLString_Types(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		val      any
		contains string
	}{
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", int(42), "42"},
		{"int64", int64(100), "100"},
		{"float64 whole", float64(7), "7"},
		{"float64 fractional", float64(3.14), "3.14"},
		{"nil", nil, "null"},
		{"default (other type)", []string{"a", "b"}, "["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := valueToHCLString(tt.val)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestGenerateHCL_BlocksWithLabels(t *testing.T) {
	t.Parallel()
	// blocks with labels array
	input := map[string]any{
		"resources": []any{
			map[string]any{
				"type": "aws_instance",
				"name": "web",
				"blocks": []any{
					map[string]any{
						"type":   "ephemeral_block_device",
						"labels": []any{"device"},
						"attributes": map[string]any{
							"device_name": "/dev/xvda",
						},
					},
				},
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "ephemeral_block_device")
}

func TestCtyNumberIntVal(t *testing.T) {
	t.Parallel()
	// This exercises ctyNumberIntVal via generating HCL with int (not float) values
	// int type in valueToTokens goes through ctyNumberIntVal
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":    "count",
				"default": int(5),
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "5")
}

func TestGenerateHCL_Int64Value(t *testing.T) {
	t.Parallel()
	// Exercises int64 type in valueToTokens → ctyNumberIntVal path
	input := map[string]any{
		"variables": []any{
			map[string]any{
				"name":    "port",
				"default": int64(8080),
			},
		},
	}
	hcl, err := GenerateHCL(input)
	require.NoError(t, err)
	assert.Contains(t, hcl, "8080")
}
