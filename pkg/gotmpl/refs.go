// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"fmt"
	"strings"
	"text/template/parse"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// TemplateReference represents a reference to data in a template
type TemplateReference struct {
	// Path is the dot-notation path to the data (e.g., ".User.Name", ".Items")
	Path string

	// Position is the line:column position in the template (if available)
	Position string
}

// goTemplateBuiltins lists the function names that text/template provides
// out of the box. parse.Parse needs these declared explicitly so it can
// recognise them during parsing.
var goTemplateBuiltins = []string{
	"and", "call", "html", "index", "slice", "js", "len", "not", "or",
	"print", "printf", "println", "urlquery",
	"eq", "ne", "lt", "le", "gt", "ge",
}

// GetReferences extracts all references to Data from a Go template
// This method parses the template and extracts variable references (e.g., .User, .Items)
// It excludes function calls and Go template control variables (like $var)
func (s *Service) GetReferences(ctx context.Context, opts TemplateOptions) ([]TemplateReference, error) {
	lgr := logger.FromContext(ctx)

	// Validate required fields
	if opts.Content == "" {
		return nil, fmt.Errorf("template content cannot be empty")
	}
	if opts.Name == "" {
		opts.Name = "unnamed-template"
		lgr.V(2).Info("template name not provided, using default", "name", opts.Name)
	}

	lgr.V(2).Info("extracting template references",
		"name", opts.Name,
		"contentLength", len(opts.Content))

	// Use default delimiters if not specified
	leftDelim := opts.LeftDelim
	rightDelim := opts.RightDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	// Parse the template using text/template/parse.
	// Pass the service's function map so the parser recognises built-in and
	// extension functions (e.g. index, printf, sprig helpers). Without this,
	// templates that call these functions fail to parse and their variable
	// references are silently lost.
	funcNames := make(map[string]any, len(s.defaultFuncs)+len(goTemplateBuiltins))
	for _, name := range goTemplateBuiltins {
		funcNames[name] = true
	}
	for k := range s.defaultFuncs {
		funcNames[k] = true // parse.Parse only checks existence, not the value
	}
	trees, err := parse.Parse(opts.Name, opts.Content, leftDelim, rightDelim, funcNames)
	if err != nil {
		lgr.Error(err, "failed to parse template for reference extraction",
			"name", opts.Name)
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	references := make([]TemplateReference, 0)
	visited := make(map[string]bool)

	// Walk the parse tree to extract data references
	// parse.Parse returns a map of tree names to trees
	for _, tree := range trees {
		if tree.Root != nil {
			walkNodes(tree.Root, &references, visited)
		}
	}

	lgr.V(2).Info("extracted template references",
		"name", opts.Name,
		"referenceCount", len(references))

	return references, nil
}

// GetGoTemplateReferences is a convenience function that creates a service and extracts references
// For one-off reference extraction without needing to create a service
func GetGoTemplateReferences(templateContent, leftDelim, rightDelim string) ([]TemplateReference, error) {
	svc := NewService(nil)
	return svc.GetReferences(context.Background(), TemplateOptions{
		Content:    templateContent,
		LeftDelim:  leftDelim,
		RightDelim: rightDelim,
	})
}

// walkNodes recursively walks the template parse tree to find data references
func walkNodes(node parse.Node, references *[]TemplateReference, visited map[string]bool) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				walkNodes(child, references, visited)
			}
		}

	case *parse.ActionNode:
		if n.Pipe != nil {
			walkPipe(n.Pipe, references, visited, n.Pos)
		}

	case *parse.IfNode:
		if n.Pipe != nil {
			walkPipe(n.Pipe, references, visited, n.Pos)
		}
		walkNodes(n.List, references, visited)
		if n.ElseList != nil {
			walkNodes(n.ElseList, references, visited)
		}

	case *parse.RangeNode:
		if n.Pipe != nil {
			walkPipe(n.Pipe, references, visited, n.Pos)
		}
		walkNodes(n.List, references, visited)
		if n.ElseList != nil {
			walkNodes(n.ElseList, references, visited)
		}

	case *parse.WithNode:
		if n.Pipe != nil {
			walkPipe(n.Pipe, references, visited, n.Pos)
		}
		walkNodes(n.List, references, visited)
		if n.ElseList != nil {
			walkNodes(n.ElseList, references, visited)
		}

	case *parse.TemplateNode:
		if n.Pipe != nil {
			walkPipe(n.Pipe, references, visited, n.Pos)
		}
	}
}

// walkPipe walks a pipe node to extract field references
func walkPipe(pipe *parse.PipeNode, references *[]TemplateReference, visited map[string]bool, pos parse.Pos) {
	if pipe == nil {
		return
	}

	for _, cmd := range pipe.Cmds {
		walkCommand(cmd, references, visited, pos)
	}
}

// walkCommand walks a command node to extract field references
func walkCommand(cmd *parse.CommandNode, references *[]TemplateReference, visited map[string]bool, pos parse.Pos) {
	if cmd == nil {
		return
	}

	for _, arg := range cmd.Args {
		walkArg(arg, references, visited, pos)
	}
}

// walkArg walks an argument node to extract field references
func walkArg(arg parse.Node, references *[]TemplateReference, visited map[string]bool, pos parse.Pos) {
	switch n := arg.(type) {
	case *parse.FieldNode:
		// This is a field reference like .User.Name or .Items
		path := buildFieldPath(n.Ident)
		if !visited[path] && !isTemplateVariable(path) {
			visited[path] = true
			*references = append(*references, TemplateReference{
				Path:     path,
				Position: fmt.Sprintf("pos:%d", pos),
			})
		}

	case *parse.ChainNode:
		// Handle chained field access
		if n.Node != nil {
			walkArg(n.Node, references, visited, pos)
		}
		if len(n.Field) > 0 {
			// Extract the base path from the chain's base node
			basePath := extractBasePath(n.Node)
			path := basePath + "." + strings.Join(n.Field, ".")
			if !visited[path] && !isTemplateVariable(path) {
				visited[path] = true
				*references = append(*references, TemplateReference{
					Path:     path,
					Position: fmt.Sprintf("pos:%d", pos),
				})
			}
		}

	case *parse.PipeNode:
		// Handle nested pipes
		walkPipe(n, references, visited, pos)
	}
}

// buildFieldPath constructs a dot-notation path from field identifiers
func buildFieldPath(idents []string) string {
	if len(idents) == 0 {
		return ""
	}
	return "." + strings.Join(idents, ".")
}

// extractBasePath extracts the base path from a node (for chained fields)
func extractBasePath(node parse.Node) string {
	switch n := node.(type) {
	case *parse.FieldNode:
		return buildFieldPath(n.Ident)
	case *parse.VariableNode:
		// Variable nodes start with $ - not data references
		return ""
	case *parse.DotNode:
		return "."
	default:
		return ""
	}
}

// isTemplateVariable checks if a path is a template variable (starts with $)
func isTemplateVariable(path string) bool {
	return strings.HasPrefix(path, "$")
}
