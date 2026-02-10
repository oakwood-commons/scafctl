// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"gopkg.in/yaml.v3"
)

// ValueRef represents a value that can be literal, resolver reference, expression, or template.
// It is used throughout the resolver and action systems to provide flexible value specification.
type ValueRef struct {
	Literal  any                         `json:"-" yaml:"-"`
	Resolver *string                     `json:"rslvr,omitempty" yaml:"rslvr,omitempty" doc:"Reference to another resolver by name" example:"environment" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must be a valid resolver name"`
	Expr     *celexp.Expression          `json:"expr,omitempty" yaml:"expr,omitempty" doc:"CEL expression to evaluate"`
	Tmpl     *gotmpl.GoTemplatingContent `json:"tmpl,omitempty" yaml:"tmpl,omitempty" doc:"Go template to execute"`
}

// UnmarshalYAML implements custom YAML unmarshalling for ValueRef.
// It handles scalar values, sequences, and mappings appropriately.
func (v *ValueRef) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		var raw struct {
			Resolver *string                     `yaml:"rslvr"`
			Expr     *celexp.Expression          `yaml:"expr"`
			Tmpl     *gotmpl.GoTemplatingContent `yaml:"tmpl"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}

		count := 0
		if raw.Resolver != nil {
			count++
		}
		if raw.Expr != nil {
			count++
		}
		if raw.Tmpl != nil {
			count++
		}

		// If no special keys found, treat the entire map as a literal value
		if count == 0 {
			var anyVal any
			if err := node.Decode(&anyVal); err != nil {
				return err
			}
			v.Literal = anyVal
			return nil
		}

		if count != 1 {
			return fmt.Errorf("invalid value ref: expected exactly one of rslvr, expr, or tmpl")
		}

		v.Resolver = raw.Resolver
		v.Expr = raw.Expr
		v.Tmpl = raw.Tmpl
		return nil

	case yaml.ScalarNode, yaml.SequenceNode, yaml.DocumentNode, yaml.AliasNode:
		// Handle scalar values, sequences, documents, and aliases as literals
		var anyVal any
		if err := node.Decode(&anyVal); err != nil {
			return err
		}
		v.Literal = anyVal
		return nil

	default:
		return fmt.Errorf("unsupported YAML node kind: %v", node.Kind)
	}
}

// IterationContext holds the context for forEach iteration variables.
// It provides access to the current item and index during iteration.
type IterationContext struct {
	Item       any    `json:"-" yaml:"-" doc:"Current array element (__item)"`
	Index      int    `json:"-" yaml:"-" doc:"Current index (__index)"`
	ItemAlias  string `json:"-" yaml:"-" doc:"Custom name for item (if specified)"`
	IndexAlias string `json:"-" yaml:"-" doc:"Custom name for index (if specified)"`
}

// Resolve resolves the ValueRef to a concrete value.
// This is a convenience method that calls ResolveWithIterationContext with nil iteration context.
func (v *ValueRef) Resolve(ctx context.Context, resolverData map[string]any, self any) (any, error) {
	return v.ResolveWithIterationContext(ctx, resolverData, self, nil)
}

// ResolveWithIterationContext resolves the ValueRef with optional forEach iteration context.
// It handles literal values, resolver references, CEL expressions, and Go templates.
func (v *ValueRef) ResolveWithIterationContext(ctx context.Context, resolverData map[string]any, self any, iterCtx *IterationContext) (any, error) {
	// Handle nil ValueRef - return nil value
	if v == nil {
		return nil, nil
	}

	// Literal value
	if v.Literal != nil {
		return v.Literal, nil
	}

	// Resolver reference
	if v.Resolver != nil {
		val, ok := resolverData[*v.Resolver]
		if !ok {
			return nil, fmt.Errorf("resolver %q not found", *v.Resolver)
		}
		return val, nil
	}

	// Build additional variables map for iteration context
	// All iteration variables go in additionalVars
	var additionalVars map[string]any
	if iterCtx != nil {
		additionalVars = make(map[string]any, 5)
		additionalVars[celexp.VarSelf] = self
		additionalVars[celexp.VarItem] = iterCtx.Item
		additionalVars[celexp.VarIndex] = iterCtx.Index
		if iterCtx.ItemAlias != "" {
			additionalVars[iterCtx.ItemAlias] = iterCtx.Item
		}
		if iterCtx.IndexAlias != "" {
			additionalVars[iterCtx.IndexAlias] = iterCtx.Index
		}
	} else if self != nil {
		additionalVars = map[string]any{celexp.VarSelf: self}
	}

	// CEL expression
	if v.Expr != nil {
		result, err := celexp.EvaluateExpression(ctx, string(*v.Expr), resolverData, additionalVars)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate expression: %w", err)
		}
		return result, nil
	}

	// Go template
	if v.Tmpl != nil {
		templateData := map[string]any{
			"_":      resolverData,
			"__self": self,
		}
		// Also add iteration variables at top level for template convenience
		if iterCtx != nil {
			templateData["__item"] = iterCtx.Item
			templateData["__index"] = iterCtx.Index
			if iterCtx.ItemAlias != "" {
				templateData[iterCtx.ItemAlias] = iterCtx.Item
			}
			if iterCtx.IndexAlias != "" {
				templateData[iterCtx.IndexAlias] = iterCtx.Index
			}
		}
		result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Content: string(*v.Tmpl),
			Data:    templateData,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to execute template: %w", err)
		}
		return result.Output, nil
	}

	return nil, fmt.Errorf("empty value reference")
}

// ReferencesVariable checks if the ValueRef references a specific variable name.
// This is useful for detecting references to __actions, __self, __item, etc.
// For expressions, it checks both top-level variables (like __actions, __self)
// and underscore-prefixed variables (like _.environment).
func (v *ValueRef) ReferencesVariable(varName string) bool {
	if v == nil {
		return false
	}

	if v.Expr != nil {
		// Check top-level variables (for __actions, __self, __item, __index)
		topLevelVars, err := v.Expr.RequiredVariables()
		if err == nil {
			for _, vn := range topLevelVars {
				if vn == varName {
					return true
				}
			}
		}

		// Also check underscore-prefixed variables (for _.resolver references)
		underscoreVars, err := v.Expr.GetUnderscoreVariables()
		if err == nil {
			for _, vn := range underscoreVars {
				if vn == varName {
					return true
				}
			}
		}
	}

	if v.Tmpl != nil {
		refs, err := gotmpl.GetGoTemplateReferences(string(*v.Tmpl), "", "")
		if err == nil {
			for _, ref := range refs {
				// Template paths start with "." (e.g., ".__actions.build.results")
				// Strip the leading dot for comparison
				path := strings.TrimPrefix(ref.Path, ".")
				// Check if the path equals the variable name or starts with it followed by a dot
				// e.g., for varName "__actions", match paths like "__actions" or "__actions.build.results"
				if path == varName || strings.HasPrefix(path, varName+".") {
					return true
				}
			}
		}
	}

	return false
}
