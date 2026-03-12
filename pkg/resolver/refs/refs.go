// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package refs provides functions for extracting resolver references from
// Go templates and CEL expressions.
package refs

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// Output represents the output structure for resolver reference extraction.
type Output struct {
	Source     string   `json:"source" yaml:"source"`
	SourceType string   `json:"sourceType" yaml:"sourceType"`
	References []string `json:"references" yaml:"references"`
	Count      int      `json:"count" yaml:"count"`
}

// ReadStdin reads all content from the given reader, trimming trailing newlines.
func ReadStdin(r io.Reader) (string, error) {
	if r == nil {
		return "", fmt.Errorf("stdin is not available")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read from stdin: %w", err)
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

// ExtractFromTemplateFile reads a template file and extracts resolver references.
func ExtractFromTemplateFile(filePath, leftDelim, rightDelim string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	return ExtractFromTemplate(string(content), leftDelim, rightDelim)
}

// ExtractFromTemplate extracts resolver references from a Go template string.
func ExtractFromTemplate(content, leftDelim, rightDelim string) ([]string, error) {
	templateRefs, err := gotmpl.GetGoTemplateReferences(content, leftDelim, rightDelim)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	// Extract resolver names from paths and deduplicate
	seen := make(map[string]bool)
	var result []string

	for _, ref := range templateRefs {
		name := ExtractResolverName(ref.Path)
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	return result, nil
}

// ExtractFromCEL extracts resolver references from a CEL expression.
func ExtractFromCEL(ctx context.Context, expr string) ([]string, error) {
	celExpr := celexp.Expression(expr)
	vars, err := celExpr.GetUnderscoreVariables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CEL expression: %w", err)
	}

	return vars, nil
}

// ExtractResolverName extracts the resolver name from a template path.
// e.g., "._.config.host" -> "config", ".config" -> "config"
func ExtractResolverName(path string) string {
	// Remove leading dot
	if len(path) > 0 && path[0] == '.' {
		path = path[1:]
	}

	// Handle _.resolverName pattern
	if len(path) > 2 && path[0] == '_' && path[1] == '.' {
		path = path[2:]
	} else if len(path) > 1 && path[0] == '_' {
		path = path[1:]
	}

	// Get first segment (resolver name)
	for i, c := range path {
		if c == '.' {
			return path[:i]
		}
	}

	return path
}
