// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import "text/template"

// ExtFunction describes a Go template function extension with metadata
// for discoverability via MCP tools and CLI commands.
type ExtFunction struct {
	// Name is the function name as used in templates (e.g., "toHcl", "upper")
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Function name as used in templates" maxLength:"128" example:"toHcl"`

	// Description is a human-readable description of the function
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the function" maxLength:"1024" example:"Converts a value to HCL format"`

	// Links contains reference URLs (documentation, source code, etc.)
	Links []string `json:"links,omitempty" yaml:"links,omitempty" doc:"Reference URLs for documentation" maxItems:"10"`

	// Examples provides usage examples for documentation and discoverability
	Examples []Example `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples for documentation" maxItems:"20"`

	// Custom indicates whether this is a scafctl-specific function (true)
	// or a third-party/built-in function (false, e.g., sprig functions)
	Custom bool `json:"custom,omitempty" yaml:"custom,omitempty" doc:"Whether this is a scafctl-specific function"`

	// Func is the template.FuncMap entry for this function.
	// Excluded from JSON/YAML serialization.
	Func template.FuncMap `json:"-" yaml:"-" doc:"Template FuncMap entry for this function"`
}

// Example describes a usage example for a Go template function.
type Example struct {
	// Description explains what the example demonstrates
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"What the example demonstrates" maxLength:"512" example:"Convert map to HCL"`

	// Template is the Go template snippet showing usage
	Template string `json:"template,omitempty" yaml:"template,omitempty" doc:"Go template snippet showing usage" maxLength:"2048" example:"{{ toHcl .Config }}"`

	// Links contains reference URLs for the example
	Links []string `json:"links,omitempty" yaml:"links,omitempty" doc:"Reference URLs for the example" maxItems:"10"`
}

// ExtFunctionList is a list of ExtFunction entries.
type ExtFunctionList []ExtFunction

// GetName returns the function name, implementing the named interface
// for use with generic filter helpers.
func (f ExtFunction) GetName() string {
	return f.Name
}

// FuncMap merges all individual Func entries into a single template.FuncMap.
// Later entries override earlier ones if they share the same function name.
func (l ExtFunctionList) FuncMap() template.FuncMap {
	merged := make(template.FuncMap, len(l))
	for _, fn := range l {
		for k, v := range fn.Func {
			merged[k] = v
		}
	}
	return merged
}
