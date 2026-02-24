// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"sigs.k8s.io/yaml"
)

// registerCELTools registers all CEL-related MCP tools.
func (s *Server) registerCELTools() {
	// list_cel_functions
	listCELFunctionsTool := mcp.NewTool("list_cel_functions",
		mcp.WithDescription("List all available CEL (Common Expression Language) functions. Includes both scafctl custom functions (map.merge, json.unmarshal, guid.new, etc.) and standard CEL built-in functions."),
		mcp.WithTitleAnnotation("List CEL Functions"),
		mcp.WithToolIcons(toolIcons["cel"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithBoolean("custom_only",
			mcp.Description("If true, only return scafctl custom functions"),
		),
		mcp.WithBoolean("builtin_only",
			mcp.Description("If true, only return standard CEL built-in functions"),
		),
		mcp.WithString("name",
			mcp.Description("Get details for a specific function by name (substring match)"),
		),
	)
	s.mcpServer.AddTool(listCELFunctionsTool, s.handleListCELFunctions)

	// evaluate_cel
	evaluateCELTool := mcp.NewTool("evaluate_cel",
		mcp.WithDescription("Evaluate a CEL (Common Expression Language) expression against provided data. Supports both inline data and file-based context. Data is accessible as '_' in the expression (e.g., '_.name'). Additional variables are accessible as top-level names."),
		mcp.WithTitleAnnotation("Evaluate CEL"),
		mcp.WithToolIcons(toolIcons["cel"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaEvaluateCEL),
		mcp.WithString("expression",
			mcp.Required(),
			mcp.Description("CEL expression to evaluate (e.g., '_.items.map(i, i.name)', '_.count > 5')"),
		),
		mcp.WithObject("data",
			mcp.Description("Root data object accessible as '_' in the expression (e.g., {\"name\": \"test\", \"count\": 42})"),
		),
		mcp.WithObject("variables",
			mcp.Description("Additional named variables accessible as top-level names in the expression"),
		),
		mcp.WithString("data_file",
			mcp.Description("Path to a YAML/JSON file to load as root data (alternative to 'data')"),
		),
	)
	s.mcpServer.AddTool(evaluateCELTool, s.handleEvaluateCEL)
}

// handleListCELFunctions lists available CEL functions.
func (s *Server) handleListCELFunctions(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	customOnly := request.GetBool("custom_only", false)
	builtinOnly := request.GetBool("builtin_only", false)
	name := request.GetString("name", "")

	functions := ext.All()
	if customOnly {
		functions = ext.Custom()
	} else if builtinOnly {
		functions = ext.BuiltIn()
	}

	// If searching by name, filter
	if name != "" {
		filtered := functions[:0:0]
		nameLower := strings.ToLower(name)
		for _, f := range functions {
			if strings.Contains(strings.ToLower(f.Name), nameLower) {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) == 0 {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("no CEL function matching %q found", name),
				WithField("name"),
				WithSuggestion("Use list_cel_functions without a name filter to see all available functions"),
				WithRelatedTools("list_cel_functions"),
			), nil
		}
		functions = filtered
	}

	return mcp.NewToolResultJSON(functions)
}

// handleEvaluateCEL evaluates a CEL expression against provided data.
func (s *Server) handleEvaluateCEL(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	expression, err := request.RequireString("expression")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("expression"),
			WithSuggestion("Provide a CEL expression string to evaluate"),
		), nil
	}

	args := request.GetArguments()

	// Get root data from inline data or file
	var rootData any
	data, hasData := args["data"]
	dataFile := request.GetString("data_file", "")

	if hasData && data != nil && dataFile != "" {
		return newStructuredError(ErrCodeInvalidInput, "cannot specify both 'data' and 'data_file' — use one or the other",
			WithField("data"),
			WithSuggestion("Use 'data' for inline JSON data or 'data_file' for file-based data, not both"),
		), nil
	}

	if dataFile != "" {
		fileBytes, err := os.ReadFile(dataFile)
		if err != nil {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to read data file %q: %v", dataFile, err),
				WithField("data_file"),
				WithSuggestion("Check the file path exists and is readable"),
			), nil
		}

		var fileData any
		if err := yaml.Unmarshal(fileBytes, &fileData); err != nil {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to parse data file %q: %v", dataFile, err),
				WithField("data_file"),
				WithSuggestion("Ensure the file contains valid YAML or JSON"),
			), nil
		}
		rootData = fileData
	} else if hasData && data != nil {
		rootData = data
	}

	// Get additional variables
	var additionalVars map[string]any
	if vars, ok := args["variables"]; ok && vars != nil {
		if varsMap, ok := vars.(map[string]any); ok {
			additionalVars = varsMap
		} else {
			return newStructuredError(ErrCodeInvalidInput, "'variables' must be an object (key-value pairs)",
				WithField("variables"),
				WithSuggestion("Provide variables as a JSON object, e.g. {\"key\": \"value\"}"),
			), nil
		}
	}

	// Evaluate the expression
	result, err := celexp.EvaluateExpression(s.ctx, expression, rootData, additionalVars)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("CEL evaluation failed: %v", err),
			WithField("expression"),
			WithSuggestion("Use validate_expression to check CEL syntax first"),
			WithRelatedTools("validate_expression", "list_cel_functions"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"expression": expression,
		"result":     result,
	})
}
