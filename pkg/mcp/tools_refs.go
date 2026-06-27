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
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver/refs"
)

// registerRefsTools registers resolver reference extraction tools.
func (s *Server) registerRefsTools() {
	extractRefssTool := mcp.NewTool("extract_resolver_refs",
		mcp.WithDescription("Extract resolver references (_.resolverName patterns) from Go templates or CEL expressions. Returns a list of referenced resolver names, which should be used to populate the 'dependsOn' field. Accepts inline text, a file path, or a directory path to scan all template files recursively."),
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
		mcp.WithString("directory",
			mcp.Description("Path to a directory to scan recursively for template files. Returns aggregated resolver references from all matched files. Template-ness is role-based, not extension-based: any rendered file is a go-template source, so use 'glob' to scan non-standard extensions (e.g. '*.tf', '*.yaml')."),
		),
		mcp.WithString("glob",
			mcp.Description("Comma-separated glob patterns matched against the base filename when scanning a directory (e.g. '*.tf,*.yaml'). Defaults to '*.tpl,*.tmpl,*.gotmpl' for convenience, but any extension can be scanned since rendering is role-based, not extension-based."),
		),
		mcp.WithString("type",
			mcp.Description("Expression type: 'go-template' (default) or 'cel'"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative file paths resolve against this directory instead of the process CWD."),
		),
	)
	s.addTool(extractRefssTool, s.handleExtractResolverRefs)
}

// handleExtractResolverRefs extracts resolver references from expressions.
func (s *Server) handleExtractResolverRefs(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text := request.GetString("text", "")
	filePath := request.GetString("file", "")
	directory := request.GetString("directory", "")
	glob := request.GetString("glob", "")
	exprType := request.GetString("type", "go-template")
	cwd := request.GetString("cwd", "")

	if text == "" && filePath == "" && directory == "" {
		return newStructuredError(ErrCodeInvalidInput, "either 'text', 'file', or 'directory' must be provided",
			WithSuggestion("Provide inline expression text via 'text', a file path via 'file', or a directory path via 'directory'"),
		), nil
	}

	// Reject ambiguous multi-input: only one of text/file/directory may be provided
	inputCount := 0
	if text != "" {
		inputCount++
	}
	if filePath != "" {
		inputCount++
	}
	if directory != "" {
		inputCount++
	}
	if inputCount > 1 {
		return newStructuredError(ErrCodeInvalidInput, "only one of 'text', 'file', or 'directory' may be provided",
			WithSuggestion("Provide exactly one input source"),
		), nil
	}

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	// Handle directory scanning mode
	if directory != "" {
		return s.handleExtractRefsFromDirectory(ctx, directory, glob, exprType)
	}

	source := "inline"
	content := text
	if filePath != "" {
		// Resolve relative file path against the working directory
		resolved, resolveErr := provider.AbsFromContext(ctx, filePath)
		if resolveErr != nil {
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("failed to resolve path: %v", resolveErr),
				WithField("file"),
			), nil
		}
		source = "file"
		data, err := os.ReadFile(resolved)
		if err != nil {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("failed to read file %q: %v", filePath, err),
				WithField("file"),
				WithSuggestion("Check that the file path exists and is accessible"),
			), nil
		}
		content = string(data)
	}

	var resolverNames []string

	switch exprType {
	case "go-template":
		resolverNames, err = extractGoTemplateRefs(content)
	case "cel":
		resolverNames, err = extractCELRefs(ctx, content)
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

// handleExtractRefsFromDirectory scans a directory recursively for template files
// and returns aggregated resolver references from all matched files.
func (s *Server) handleExtractRefsFromDirectory(ctx context.Context, directory, glob, exprType string) (*mcp.CallToolResult, error) {
	resolved, err := provider.AbsFromContext(ctx, directory)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("failed to resolve directory path: %v", err),
			WithField("directory"),
		), nil
	}

	globs := refs.ParseGlobs(glob)
	scanResult, err := refs.ScanDirectory(ctx, resolved, globs, exprType)
	if err != nil {
		// Map domain errors to structured MCP errors
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "unsupported expression type"):
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("unsupported type %q", exprType),
				WithField("type"),
				WithSuggestion("Use 'go-template' or 'cel'"),
			), nil
		case strings.Contains(errMsg, "not found"):
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("directory %q not found: %v", directory, err),
				WithField("directory"),
				WithSuggestion("Check that the directory path exists and is accessible"),
			), nil
		case strings.Contains(errMsg, "is not a directory"):
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("%q is not a directory", directory),
				WithField("directory"),
				WithSuggestion("Use 'file' parameter for single files, or provide a directory path"),
			), nil
		case strings.Contains(errMsg, "invalid glob pattern"):
			return newStructuredError(ErrCodeInvalidInput, errMsg,
				WithField("glob"),
				WithSuggestion("Check glob syntax (e.g. unclosed character class)"),
			), nil
		default:
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to scan directory: %v", err),
				WithField("directory"),
			), nil
		}
	}

	result := map[string]any{
		"source":     "directory",
		"sourceType": exprType,
		"directory":  directory,
		"references": scanResult.References,
		"count":      scanResult.Count,
		"details":    scanResult.Details,
		"files":      scanResult.Files,
		"filesCount": scanResult.FilesCount,
	}
	if len(scanResult.Warnings) > 0 {
		result["warnings"] = scanResult.Warnings
	}

	return mcp.NewToolResultJSON(result)
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
		// Skip scoped references inside {{ with }}/{{ range }} bodies
		if ref.Scoped {
			continue
		}

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
func extractCELRefs(ctx context.Context, content string) ([]string, error) {
	expr := celexp.Expression(content)
	vars, err := expr.GetUnderscoreVariables(ctx)
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
