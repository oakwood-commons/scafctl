// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import "encoding/json"

// Output schemas for tools that return well-defined JSON structures.
// These help MCP clients understand and validate tool output shapes.

// outputSchemaListSolutions is the output schema for the list_solutions tool.
var outputSchemaListSolutions = json.RawMessage(`{
	"type": "object",
	"properties": {
		"solutions": {
			"type": "array",
			"description": "List of discovered solutions",
			"items": {
				"type": "object",
				"properties": {
					"name": { "type": "string" },
					"version": { "type": "string" },
					"description": { "type": "string" },
					"path": { "type": "string" },
					"source": { "type": "string" },
					"resolver_count": { "type": "integer" },
					"action_count": { "type": "integer" },
					"has_tests": { "type": "boolean" }
				}
			}
		},
		"total": { "type": "integer", "description": "Total number of solutions found" }
	}
}`)

// outputSchemaInspectSolution is the output schema for the inspect_solution tool.
var outputSchemaInspectSolution = json.RawMessage(`{
	"type": "object",
	"properties": {
		"metadata": {
			"type": "object",
			"properties": {
				"name": { "type": "string" },
				"version": { "type": "string" },
				"description": { "type": "string" }
			}
		},
		"resolvers": {
			"type": "object",
			"description": "Map of resolver name to resolver details",
			"additionalProperties": {
				"type": "object",
				"properties": {
					"provider": { "type": "string" },
					"depends_on": { "type": "array", "items": { "type": "string" } }
				}
			}
		},
		"actions": {
			"type": "object",
			"description": "Map of action name to action details",
			"additionalProperties": {
				"type": "object",
				"properties": {
					"provider": { "type": "string" },
					"depends_on": { "type": "array", "items": { "type": "string" } }
				}
			}
		},
		"dependency_graph": { "type": "string" }
	}
}`)

// outputSchemaListProviders is the output schema for the list_providers tool.
var outputSchemaListProviders = json.RawMessage(`{
	"type": "object",
	"properties": {
		"providers": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"name": { "type": "string" },
					"description": { "type": "string" },
					"capabilities": {
						"type": "array",
						"items": { "type": "string" }
					}
				}
			}
		},
		"total": { "type": "integer" }
	}
}`)

// outputSchemaVersion is the output schema for the get_version tool.
var outputSchemaVersion = json.RawMessage(`{
	"type": "object",
	"properties": {
		"version": { "type": "string" },
		"commit": { "type": "string" },
		"build_time": { "type": "string" },
		"go_version": { "type": "string" },
		"os": { "type": "string" },
		"arch": { "type": "string" }
	}
}`)

// outputSchemaLintResult is the output schema for the lint_solution tool.
var outputSchemaLintResult = json.RawMessage(`{
	"type": "object",
	"properties": {
		"findings": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"rule": { "type": "string" },
					"severity": { "type": "string", "enum": ["error", "warning", "info"] },
					"message": { "type": "string" },
					"location": { "type": "string" }
				}
			}
		},
		"summary": {
			"type": "object",
			"properties": {
				"errors": { "type": "integer" },
				"warnings": { "type": "integer" },
				"info": { "type": "integer" },
				"total": { "type": "integer" }
			}
		}
	}
}`)

// outputSchemaEvaluateCEL is the output schema for the evaluate_cel tool.
var outputSchemaEvaluateCEL = json.RawMessage(`{
	"type": "object",
	"properties": {
		"result": { "description": "The evaluation result (any JSON type)" },
		"type": { "type": "string", "description": "The Go type of the result" }
	}
}`)

// outputSchemaRenderSolution is the output schema for the render_solution tool.
var outputSchemaRenderSolution = json.RawMessage(`{
	"type": "object",
	"properties": {
		"rendered_yaml": { "type": "string", "description": "The rendered solution YAML" },
		"resolver_graph": { "type": "string", "description": "ASCII dependency graph" },
		"mermaid_graph": { "type": "string", "description": "Mermaid diagram syntax" }
	}
}`)

// outputSchemaAuthStatus is the output schema for the auth_status tool.
var outputSchemaAuthStatus = json.RawMessage(`{
	"type": "object",
	"properties": {
		"authenticated": { "type": "boolean" },
		"handler": { "type": "string" },
		"details": { "type": "object" }
	}
}`)

// outputSchemaGetConfig is the output schema for the get_config tool.
var outputSchemaGetConfig = json.RawMessage(`{
	"type": "object",
	"properties": {
		"config": { "type": "object", "description": "The application configuration" }
	}
}`)

// outputSchemaGetConfigPaths is the output schema for the get_config_paths tool.
var outputSchemaGetConfigPaths = json.RawMessage(`{
	"type": "object",
	"properties": {
		"config_dir": { "type": "string" },
		"data_dir": { "type": "string" },
		"cache_dir": { "type": "string" },
		"state_dir": { "type": "string" }
	}
}`)

// outputSchemaPreviewResolvers is the output schema for the preview_resolvers tool.
var outputSchemaPreviewResolvers = json.RawMessage(`{
	"type": "object",
	"properties": {
		"resolvers": {
			"type": "object",
			"description": "Map of resolver name to resolved value or error",
			"additionalProperties": {}
		},
		"summary": {
			"type": "object",
			"properties": {
				"total": { "type": "integer" },
				"resolved": { "type": "integer" },
				"failed": { "type": "integer" },
				"skipped": { "type": "integer" }
			}
		}
	}
}`)

// outputSchemaDryRun is the output schema for the dry_run_solution tool.
var outputSchemaDryRun = json.RawMessage(`{
	"type": "object",
	"properties": {
		"dryRun": { "type": "boolean", "description": "Always true for dry-run reports" },
		"solution": { "type": "string", "description": "Solution name" },
		"version": { "type": "string", "description": "Solution version" },
		"hasWorkflow": { "type": "boolean", "description": "Whether the solution has a workflow" },
		"actionPlan": {
			"type": "array",
			"description": "Planned action execution with WhatIf descriptions",
			"items": {
				"type": "object",
				"properties": {
					"name": { "type": "string" },
					"provider": { "type": "string" },
					"description": { "type": "string" },
					"wouldDo": { "type": "string", "description": "Provider-generated description of what this action would do" },
					"phase": { "type": "integer" },
					"section": { "type": "string" },
					"dependencies": { "type": "array", "items": { "type": "string" } },
					"when": { "type": "string" },
					"materializedInputs": { "type": "object" },
					"deferredInputs": { "type": "object" }
				}
			}
		},
		"totalActions": { "type": "integer" },
		"totalPhases": { "type": "integer" },
		"warnings": { "type": "array", "items": { "type": "string" } }
	}
}`)
