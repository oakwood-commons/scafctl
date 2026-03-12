// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// registerRefsTools registers resolver reference extraction tools.
func (s *Server) registerRefsTools() {
	extractRefssTool := mcp.NewTool("extract_resolver_refs",
		mcp.WithDescription("Extract resolver references (_.resolverName patterns) from Go templates or CEL expressions. Returns a list of referenced resolver names, which should be used to populate the 'dependsOn' field. Accepts inline text or a file path."),
		mcp.WithTitleAnnotation("Extract Resolver References"),
		mcp.WithToolIcons(toolIcons["refs"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("text",
			mcp.Description("Inline Go template or CEL expression text to analyze"),
		),
		mcp.WithString("file",
			mcp.Description("Path to a template file to analyze"),
		),
		mcp.WithString("type",
			mcp.Description("Expression type: 'go-template' (default) or 'cel'"),
		),
	)
	s.mcpServer.AddTool(extractRefssTool, s.handleExtractResolverRefs)
}

// handleExtractResolverRefs extracts resolver references from expressions.
func (s *Server) handleExtractResolverRefs(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text := request.GetString("text", "")
	filePath := request.GetString("file", "")
	exprType := request.GetString("type", "go-template")

	if text == "" && filePath == "" {
		return newStructuredError(ErrCodeInvalidInput, "either 'text' or 'file' must be provided",
			WithSuggestion("Provide inline expression text via 'text' or a file path via 'file'"),
		), nil
	}

	source := "inline"
	content := text
	if filePath != "" {
		source = "file"
		data, err := os.ReadFile(filePath)
		if err != nil {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("failed to read file %q: %v", filePath, err),
				WithField("file"),
				WithSuggestion("Check that the file path exists and is accessible"),
			), nil
		}
		content = string(data)
	}

	var resolverNames []string
	var err error

	switch exprType {
	case "go-template":
		resolverNames, err = extractGoTemplateRefs(content)
	case "cel":
		resolverNames, err = extractCELRefs(content)
	default:
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("unsupported type %q", exprType),
			WithField("type"),
			WithSuggestion("Use 'go-template' or 'cel'"),
		), nil
	}

	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to extract references: %v", err),
			WithSuggestion("Check the expression syntax is valid"),
			WithRelatedTools("evaluate_cel", "render_go_template"),
		), nil
	}

	// Build detailed reference info (resolver → fields)
	details := buildRefDetails(resolverNames)

	return mcp.NewToolResultJSON(map[string]any{
		"source":     source,
		"sourceType": exprType,
		"references": uniqueResolverNames(details),
		"count":      len(uniqueResolverNames(details)),
		"details":    details,
	})
}

// refDetail represents a resolver and its referenced fields.
type refDetail struct {
	Resolver string   `json:"resolver"`
	Fields   []string `json:"fields"`
}

// extractGoTemplateRefs extracts resolver references from a Go template.
func extractGoTemplateRefs(content string) ([]string, error) {
	refs, err := gotmpl.GetGoTemplateReferences(content, "", "")
	if err != nil {
		return nil, err
	}

	var resolverPaths []string
	for _, ref := range refs {
		// Go template references start with "." — look for _.resolverName patterns
		path := ref.Path
		path = strings.TrimPrefix(path, ".")

		// Resolver references are _.resolverName or ._.resolverName
		if strings.HasPrefix(path, "_.") {
			resolverPaths = append(resolverPaths, strings.TrimPrefix(path, "_."))
		}
	}

	return resolverPaths, nil
}

// extractCELRefs extracts resolver references from a CEL expression.
func extractCELRefs(content string) ([]string, error) {
	expr := celexp.Expression(content)
	vars, err := expr.GetUnderscoreVariables(context.TODO())
	if err != nil {
		return nil, err
	}
	return vars, nil
}

// buildRefDetails groups raw resolver paths into resolver → fields mapping.
func buildRefDetails(paths []string) []refDetail {
	resolverFields := make(map[string][]string)
	for _, p := range paths {
		parts := strings.SplitN(p, ".", 2)
		resolverName := parts[0]
		if len(parts) > 1 {
			field := parts[1]
			// Avoid duplicate fields
			found := false
			for _, f := range resolverFields[resolverName] {
				if f == field {
					found = true
					break
				}
			}
			if !found {
				resolverFields[resolverName] = append(resolverFields[resolverName], field)
			}
		} else {
			if _, exists := resolverFields[resolverName]; !exists {
				resolverFields[resolverName] = []string{}
			}
		}
	}

	details := make([]refDetail, 0, len(resolverFields))
	for name, fields := range resolverFields {
		sort.Strings(fields)
		details = append(details, refDetail{Resolver: name, Fields: fields})
	}
	sort.Slice(details, func(i, j int) bool {
		return details[i].Resolver < details[j].Resolver
	})
	return details
}

// uniqueResolverNames extracts unique resolver names from details.
func uniqueResolverNames(details []refDetail) []string {
	names := make([]string, len(details))
	for i, d := range details {
		names[i] = d.Resolver
	}
	return names
}
