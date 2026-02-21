// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/google/cel-go/cel"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"sigs.k8s.io/yaml"
)

// registerTemplateTools registers Go template and expression validation tools.
func (s *Server) registerTemplateTools() {
	// evaluate_go_template
	evaluateGoTemplateTool := mcp.NewTool("evaluate_go_template",
		mcp.WithDescription("Evaluate a Go template against provided data. Go templates use {{ .FieldName }} syntax and support pipelines, conditionals (if/else), ranges, and custom functions. Data is accessible via dot notation (e.g., {{ .Name }}, {{ .Items }}). Use this to test tmpl: fields in solution YAML before committing them."),
		mcp.WithTitleAnnotation("Evaluate Go Template"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("template",
			mcp.Required(),
			mcp.Description("Go template content to evaluate (e.g., 'Hello {{ .Name }}', '{{ range .Items }}{{ . }}{{ end }}')"),
		),
		mcp.WithObject("data",
			mcp.Description("Data object accessible via dot notation in the template (e.g., {\"Name\": \"world\", \"Items\": [\"a\", \"b\"]})"),
		),
		mcp.WithString("data_file",
			mcp.Description("Path to a YAML/JSON file to load as template data (alternative to 'data')"),
		),
		mcp.WithString("left_delim",
			mcp.Description("Left delimiter for the template (default: '{{')"),
		),
		mcp.WithString("right_delim",
			mcp.Description("Right delimiter for the template (default: '}}')"),
		),
		mcp.WithString("missing_key",
			mcp.Description("Behavior when a map key is missing: 'default' (prints '<no value>'), 'zero' (uses zero value), 'error' (returns error). Default: 'default'"),
			mcp.Enum("default", "zero", "error"),
		),
	)
	s.mcpServer.AddTool(evaluateGoTemplateTool, s.handleEvaluateGoTemplate)

	// validate_expression
	validateExpressionTool := mcp.NewTool("validate_expression",
		mcp.WithDescription("Validate a CEL expression or Go template for syntax errors WITHOUT executing it. Returns whether the expression/template is valid and any parse errors found. Use this for quick syntax checking of when clauses, expr fields, and tmpl fields in solution YAML."),
		mcp.WithTitleAnnotation("Validate Expression"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("expression",
			mcp.Required(),
			mcp.Description("The expression or template content to validate"),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description("Expression type: 'cel' for CEL expressions, 'go-template' for Go templates"),
			mcp.Enum("cel", "go-template"),
		),
		mcp.WithString("left_delim",
			mcp.Description("Left delimiter for Go templates (default: '{{')"),
		),
		mcp.WithString("right_delim",
			mcp.Description("Right delimiter for Go templates (default: '}}')"),
		),
	)
	s.mcpServer.AddTool(validateExpressionTool, s.handleValidateExpression)
}

// handleEvaluateGoTemplate evaluates a Go template against provided data.
func (s *Server) handleEvaluateGoTemplate(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tmplContent, err := request.RequireString("template")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	args := request.GetArguments()

	// Get data from inline or file
	var data any
	inlineData, hasInlineData := args["data"]
	dataFile := request.GetString("data_file", "")

	if hasInlineData && inlineData != nil && dataFile != "" {
		return mcp.NewToolResultError("cannot specify both 'data' and 'data_file' — use one or the other"), nil
	}

	if dataFile != "" {
		fileBytes, err := os.ReadFile(dataFile)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read data file %q: %v", dataFile, err)), nil
		}
		var fileData any
		if err := yaml.Unmarshal(fileBytes, &fileData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse data file %q: %v", dataFile, err)), nil
		}
		data = fileData
	} else if hasInlineData && inlineData != nil {
		data = inlineData
	}

	// Build template options
	opts := gotmpl.TemplateOptions{
		Content: tmplContent,
		Name:    "mcp-evaluate",
		Data:    data,
	}

	if leftDelim := request.GetString("left_delim", ""); leftDelim != "" {
		opts.LeftDelim = leftDelim
	}
	if rightDelim := request.GetString("right_delim", ""); rightDelim != "" {
		opts.RightDelim = rightDelim
	}
	if missingKey := request.GetString("missing_key", ""); missingKey != "" {
		opts.MissingKey = gotmpl.MissingKeyOption(missingKey)
	}

	// Execute the template
	svc := gotmpl.NewService(nil)
	result, err := svc.Execute(s.ctx, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("template execution failed: %v", err)), nil
	}

	// Also extract references for debugging help
	refs, _ := svc.GetReferences(s.ctx, opts)

	response := map[string]any{
		"template": tmplContent,
		"output":   result.Output,
	}
	if len(refs) > 0 {
		response["referencedFields"] = refs
	}

	return mcp.NewToolResultJSON(response)
}

// handleValidateExpression validates a CEL expression or Go template for syntax errors.
func (s *Server) handleValidateExpression(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	expression, err := request.RequireString("expression")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	exprType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	switch exprType {
	case "cel":
		return s.validateCELExpression(expression)
	case "go-template":
		leftDelim := request.GetString("left_delim", "")
		rightDelim := request.GetString("right_delim", "")
		return s.validateGoTemplate(expression, leftDelim, rightDelim)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown expression type %q — use 'cel' or 'go-template'", exprType)), nil
	}
}

// validateCELExpression checks a CEL expression for syntax errors without executing it.
func (s *Server) validateCELExpression(expression string) (*mcp.CallToolResult, error) {
	env, err := cel.NewEnv()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create CEL environment: %v", err)), nil
	}

	_, issues := env.Parse(expression)
	if issues != nil && issues.Err() != nil {
		return mcp.NewToolResultJSON(map[string]any{
			"valid":      false,
			"type":       "cel",
			"expression": expression,
			"error":      issues.Err().Error(),
			"suggestion": "Check CEL syntax. Common issues: missing quotes around strings, using == instead of =, unbalanced parentheses. Use list_cel_functions to see available functions.",
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"valid":      true,
		"type":       "cel",
		"expression": expression,
	})
}

// validateGoTemplate checks a Go template for parse errors without executing it.
func (s *Server) validateGoTemplate(content, leftDelim, rightDelim string) (*mcp.CallToolResult, error) {
	// Use text/template to parse-only (no execution)
	tmpl := template.New("mcp-validate")

	switch {
	case leftDelim != "" && rightDelim != "":
		tmpl = tmpl.Delims(leftDelim, rightDelim)
	case leftDelim != "":
		tmpl = tmpl.Delims(leftDelim, "}}")
	case rightDelim != "":
		tmpl = tmpl.Delims("{{", rightDelim)
	}

	_, err := tmpl.Parse(content)
	if err != nil {
		errMsg := err.Error()
		suggestion := "Check Go template syntax. Common issues: missing closing braces '}}', unclosed {{ if }}/{{ range }}/{{ with }} blocks, undefined functions."
		if strings.Contains(errMsg, "function") {
			suggestion = "The template references an unknown function. Use standard Go template functions or check available custom functions."
		}

		return mcp.NewToolResultJSON(map[string]any{
			"valid":      false,
			"type":       "go-template",
			"template":   content,
			"error":      errMsg,
			"suggestion": suggestion,
		})
	}

	// Also extract field references via gotmpl for extra help
	opts := gotmpl.TemplateOptions{
		Content: content,
		Name:    "mcp-validate",
	}
	if leftDelim != "" {
		opts.LeftDelim = leftDelim
	}
	if rightDelim != "" {
		opts.RightDelim = rightDelim
	}

	svc := gotmpl.NewService(nil)
	refs, _ := svc.GetReferences(s.ctx, opts)

	response := map[string]any{
		"valid":    true,
		"type":     "go-template",
		"template": content,
	}
	if len(refs) > 0 {
		response["referencedFields"] = refs
	}

	return mcp.NewToolResultJSON(response)
}
