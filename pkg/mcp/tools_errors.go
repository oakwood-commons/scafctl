// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/errexplain"
)

// registerErrorTools registers the explain_error MCP tool.
func (s *Server) registerErrorTools() {
	explainErrorTool := mcp.NewTool("explain_error",
		mcp.WithDescription("Explain a scafctl error message. Parses the error text, identifies the error category, and returns a structured explanation with root cause analysis and actionable fix suggestions. Paste any error from resolver execution, validation, CEL evaluation, or provider failures."),
		mcp.WithTitleAnnotation("Explain Error"),
		mcp.WithToolIcons(toolIcons["lint"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("error",
			mcp.Required(),
			mcp.Description("The error message text to explain. Can be a full error string or snippet from resolver execution, validation, CEL evaluation, or any scafctl operation."),
		),
	)
	s.mcpServer.AddTool(explainErrorTool, s.handleExplainError)
}

// handleExplainError parses an error message and returns a structured explanation.
func (s *Server) handleExplainError(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	errText := request.GetString("error", "")
	if errText == "" {
		return newStructuredError(ErrCodeInvalidInput, "error parameter is required",
			WithField("error"),
			WithSuggestion("Paste the full error message from a failed operation"),
		), nil
	}

	explanation := errexplain.Explain(errText)
	return marshalExplanation(explanation)
}

func marshalExplanation(exp *errexplain.Explanation) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(exp)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal explanation: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
