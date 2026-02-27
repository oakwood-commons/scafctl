// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"encoding/json"
	"fmt"
)

// GenerateHCLJSON converts a structured map representation into Terraform JSON
// configuration syntax (.tf.json). The input uses the same schema as ParseHCL
// output (plural keys: variables, resources, etc.) and the output follows the
// Terraform JSON Configuration Syntax specification.
//
// See: https://developer.hashicorp.com/terraform/language/syntax/json
func GenerateHCLJSON(input map[string]any) (string, error) {
	result := make(map[string]any)

	// Each block type is converted to its JSON representation.
	// The JSON syntax uses block-type keys at the top level, with label nesting.

	if tf, ok := input["terraform"].(map[string]any); ok && len(tf) > 0 {
		result["terraform"] = convertTerraformJSON(tf)
	}

	if vars, ok := input["variables"].([]any); ok && len(vars) > 0 {
		result["variable"] = convertSingleLabelBlocksJSON(vars)
	}

	if locals, ok := input["locals"].(map[string]any); ok && len(locals) > 0 {
		result["locals"] = locals
	}

	if data, ok := input["data"].([]any); ok && len(data) > 0 {
		result["data"] = convertDoubleLabelBlocksJSON(data)
	}

	if resources, ok := input["resources"].([]any); ok && len(resources) > 0 {
		result["resource"] = convertDoubleLabelBlocksJSON(resources)
	}

	if modules, ok := input["modules"].([]any); ok && len(modules) > 0 {
		result["module"] = convertSingleLabelBlocksJSON(modules)
	}

	if outputs, ok := input["outputs"].([]any); ok && len(outputs) > 0 {
		result["output"] = convertSingleLabelBlocksJSON(outputs)
	}

	if providers, ok := input["providers"].([]any); ok && len(providers) > 0 {
		result["provider"] = convertProviderBlocksJSON(providers)
	}

	if moved, ok := input["moved"].([]any); ok && len(moved) > 0 {
		result["moved"] = convertUnlabeledBlocksJSON(moved)
	}

	if imports, ok := input["import"].([]any); ok && len(imports) > 0 {
		result["import"] = convertUnlabeledBlocksJSON(imports)
	}

	if checks, ok := input["check"].([]any); ok && len(checks) > 0 {
		result["check"] = convertSingleLabelBlocksJSON(checks)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}

	return string(out) + "\n", nil
}

// convertTerraformJSON converts the terraform block to JSON syntax.
// The terraform block is special: it has no labels and contains nested blocks
// like required_providers and backend.
func convertTerraformJSON(tf map[string]any) map[string]any {
	result := make(map[string]any)

	for k, v := range tf {
		result[k] = v
	}

	return result
}

// convertSingleLabelBlocksJSON converts blocks with a single label (variable,
// module, output, check) to the JSON syntax where the label becomes a key.
// These blocks always use "name" as their label key.
//
// Input:  [{name: "region", type: "string", default: "us-east-1"}, ...]
// Output: {"region": {type: "string", default: "us-east-1"}, ...}
func convertSingleLabelBlocksJSON(blocks []any) map[string]any {
	result := make(map[string]any)

	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}

		label, _ := bm["name"].(string)
		if label == "" {
			continue
		}

		body := make(map[string]any)
		for k, v := range bm {
			if k == "name" {
				continue
			}
			// Flatten "attributes" into the body for JSON syntax.
			if k == "attributes" {
				if attrs, ok := v.(map[string]any); ok {
					for ak, av := range attrs {
						body[ak] = av
					}
					continue
				}
			}
			body[k] = v
		}

		result[label] = body
	}

	return result
}

// convertDoubleLabelBlocksJSON converts blocks with two labels (resource, data)
// to the JSON syntax with nested label keys.
//
// Input:  [{type: "aws_instance", name: "web", attributes: {ami: "ami-123"}}]
// Output: {"aws_instance": {"web": {ami: "ami-123"}}}
func convertDoubleLabelBlocksJSON(blocks []any) map[string]any {
	result := make(map[string]any)

	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}

		typeName, _ := bm["type"].(string)
		name, _ := bm["name"].(string)
		if typeName == "" || name == "" {
			continue
		}

		body := make(map[string]any)
		for k, v := range bm {
			if k == "type" || k == "name" {
				continue
			}
			if k == "attributes" {
				if attrs, ok := v.(map[string]any); ok {
					for ak, av := range attrs {
						body[ak] = av
					}
					continue
				}
			}
			body[k] = v
		}

		typeMap, ok := result[typeName].(map[string]any)
		if !ok {
			typeMap = make(map[string]any)
		}
		typeMap[name] = body
		result[typeName] = typeMap
	}

	return result
}

// convertProviderBlocksJSON converts provider blocks to JSON syntax.
// Terraform JSON syntax allows multiple provider configurations via a map
// where values can be objects or arrays of objects (for aliased providers).
//
// Input:  [{name: "aws", alias: "west", attributes: {region: "us-west-2"}}]
// Output: {"aws": {"alias": "west", "region": "us-west-2"}}
func convertProviderBlocksJSON(blocks []any) map[string]any {
	result := make(map[string]any)

	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}

		name, _ := bm["name"].(string)
		if name == "" {
			continue
		}

		body := make(map[string]any)
		for k, v := range bm {
			if k == "name" {
				continue
			}
			if k == "attributes" {
				if attrs, ok := v.(map[string]any); ok {
					for ak, av := range attrs {
						body[ak] = av
					}
					continue
				}
			}
			body[k] = v
		}

		// If there's already an entry for this provider name, convert to array.
		if existing, ok := result[name]; ok {
			switch e := existing.(type) {
			case []any:
				result[name] = append(e, body)
			default:
				result[name] = []any{e, body}
			}
		} else {
			result[name] = body
		}
	}

	return result
}

// convertUnlabeledBlocksJSON converts blocks without labels (moved, import)
// to an array of objects in JSON syntax.
//
// Input:  [{from: "aws_instance.old", to: "aws_instance.new"}]
// Output: [{from: "aws_instance.old", to: "aws_instance.new"}]
func convertUnlabeledBlocksJSON(blocks []any) []any {
	result := make([]any, 0, len(blocks))

	for _, block := range blocks {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}

		body := make(map[string]any)
		for k, v := range bm {
			if k == "attributes" {
				if attrs, ok := v.(map[string]any); ok {
					for ak, av := range attrs {
						body[ak] = av
					}
					continue
				}
			}
			body[k] = v
		}

		result = append(result, body)
	}

	return result
}
