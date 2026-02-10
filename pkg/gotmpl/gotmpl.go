// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// DefaultLeftDelim is the default left delimiter for templates
	DefaultLeftDelim = "{{"

	// DefaultRightDelim is the default right delimiter for templates
	DefaultRightDelim = "}}"
)

type GoTemplatingContent string

// MissingKeyOption defines the behavior when a map key is missing during template execution
type MissingKeyOption string

const (
	// MissingKeyDefault continues execution and prints "<no value>" for missing keys
	// This is the default behavior
	MissingKeyDefault MissingKeyOption = "default"

	// MissingKeyZero returns the zero value for the map type's element
	MissingKeyZero MissingKeyOption = "zero"

	// MissingKeyError stops execution immediately with an error
	MissingKeyError MissingKeyOption = "error"
)

// TemplateOptions contains configuration for template execution
type TemplateOptions struct {
	// Content is the template content as a string
	Content string

	// Name is the reference name/identifier for the template (e.g., file path)
	// Used in logging and error messages
	Name string

	// Data is the data source passed to the template during execution
	Data any

	// LeftDelim sets the left action delimiter (default: "{{")
	LeftDelim string

	// RightDelim sets the right action delimiter (default: "}}")
	RightDelim string

	// Replacements is a map of strings to replace before template execution
	// The key is replaced with a UUID placeholder, then restored after execution
	// This helps avoid template parsing errors for content that should be literal
	Replacements []Replacement

	// Funcs is a map of custom template functions to make available
	// These are added to the template's function map
	Funcs template.FuncMap

	// MissingKey controls the behavior when a map key is missing
	// Default: MissingKeyDefault (prints "<no value>")
	// Options: MissingKeyDefault, MissingKeyZero, MissingKeyError
	MissingKey MissingKeyOption

	// DisableBuiltinFuncs disables the built-in template functions
	// By default, basic functions like "html", "js", etc. are available
	DisableBuiltinFuncs bool
}

// Replacement defines a string replacement to perform before/after templating
type Replacement struct {
	// Find is the string to search for in the template content
	Find string

	// Replace is the temporary replacement value
	// If empty, a UUID will be generated automatically
	Replace string
}

// ExecuteResult contains the result of template execution
type ExecuteResult struct {
	// Output is the rendered template content
	Output string

	// TemplateName is the name/identifier of the template
	TemplateName string

	// ReplacementsMade is the number of replacements that were applied
	ReplacementsMade int
}

// Service provides template execution capabilities
type Service struct {
	// defaultFuncs are the default custom functions available to all templates
	defaultFuncs template.FuncMap
}

// NewService creates a new template service with optional default functions
func NewService(defaultFuncs template.FuncMap) *Service {
	if defaultFuncs == nil {
		defaultFuncs = make(template.FuncMap)
	}
	return &Service{
		defaultFuncs: defaultFuncs,
	}
}

// Execute renders a template with the provided options
func (s *Service) Execute(ctx context.Context, opts TemplateOptions) (*ExecuteResult, error) {
	lgr := logger.FromContext(ctx)

	// Validate required fields
	if opts.Content == "" {
		return nil, fmt.Errorf("template content cannot be empty")
	}
	if opts.Name == "" {
		opts.Name = "unnamed-template"
		lgr.V(1).Info("template name not provided, using default", "name", opts.Name)
	}

	lgr.V(1).Info("executing template",
		"name", opts.Name,
		"contentLength", len(opts.Content),
		"hasData", opts.Data != nil,
		"customFuncCount", len(opts.Funcs),
		"replacementCount", len(opts.Replacements))

	// Apply replacements before template parsing
	modifiedContent, replacementMap := s.applyReplacements(ctx, opts.Content, opts.Replacements)

	lgr.V(2).Info("applied replacements",
		"name", opts.Name,
		"replacementsMade", len(replacementMap),
		"originalLength", len(opts.Content),
		"modifiedLength", len(modifiedContent))

	// Create and configure the template
	tmpl, err := s.createTemplate(ctx, opts.Name, modifiedContent, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create template '%s': %w", opts.Name, err)
	}

	// Execute the template
	output, err := s.executeTemplate(ctx, tmpl, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to execute template '%s': %w", opts.Name, err)
	}

	// Restore original strings from replacements
	restoredOutput := s.restoreReplacements(ctx, output, replacementMap)

	lgr.V(1).Info("template execution completed successfully",
		"name", opts.Name,
		"outputLength", len(restoredOutput))

	return &ExecuteResult{
		Output:           restoredOutput,
		TemplateName:     opts.Name,
		ReplacementsMade: len(replacementMap),
	}, nil
}

// applyReplacements replaces specified strings with UUID placeholders
// Returns the modified content and a map to reverse the replacements
func (s *Service) applyReplacements(ctx context.Context, content string, replacements []Replacement) (string, map[string]string) {
	lgr := logger.FromContext(ctx)

	if len(replacements) == 0 {
		lgr.V(2).Info("no replacements to apply")
		return content, nil
	}

	reversalMap := make(map[string]string)
	modified := content

	for i, r := range replacements {
		if r.Find == "" {
			lgr.V(1).Info("skipping empty replacement", "index", i)
			continue
		}

		// Generate UUID placeholder if replacement not specified
		placeholder := r.Replace
		if placeholder == "" {
			placeholder = fmt.Sprintf("UUID_%s", strings.ReplaceAll(uuid.New().String(), "-", "_"))
		}

		// Check if the string to find actually exists
		if !strings.Contains(modified, r.Find) {
			lgr.V(2).Info("replacement string not found in content",
				"index", i,
				"find", r.Find)
			continue
		}

		// Apply replacement
		count := strings.Count(modified, r.Find)
		modified = strings.ReplaceAll(modified, r.Find, placeholder)
		reversalMap[placeholder] = r.Find

		lgr.V(2).Info("applied replacement",
			"index", i,
			"find", truncateString(r.Find, 50),
			"placeholder", placeholder,
			"occurrences", count)
	}

	return modified, reversalMap
}

// restoreReplacements reverses the UUID placeholder replacements
func (s *Service) restoreReplacements(ctx context.Context, content string, reversalMap map[string]string) string {
	lgr := logger.FromContext(ctx)

	if len(reversalMap) == 0 {
		lgr.V(2).Info("no replacements to restore")
		return content
	}

	restored := content
	restoredCount := 0

	for placeholder, original := range reversalMap {
		if strings.Contains(restored, placeholder) {
			count := strings.Count(restored, placeholder)
			restored = strings.ReplaceAll(restored, placeholder, original)
			restoredCount++

			lgr.V(2).Info("restored replacement",
				"placeholder", placeholder,
				"original", truncateString(original, 50),
				"occurrences", count)
		} else {
			lgr.V(2).Info("placeholder not found in output (may have been removed by template logic)",
				"placeholder", placeholder)
		}
	}

	lgr.V(2).Info("completed replacement restoration",
		"totalReplacements", len(reversalMap),
		"restored", restoredCount)

	return restored
}

// createTemplate creates and configures a new template
func (s *Service) createTemplate(ctx context.Context, name, content string, opts TemplateOptions) (*template.Template, error) {
	lgr := logger.FromContext(ctx)

	// Create base template
	tmpl := template.New(name)

	// Set delimiters (use defaults if not specified)
	leftDelim := opts.LeftDelim
	rightDelim := opts.RightDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	// Only log if non-default delimiters are used
	if leftDelim != DefaultLeftDelim || rightDelim != DefaultRightDelim {
		lgr.V(2).Info("setting custom delimiters",
			"name", name,
			"leftDelim", leftDelim,
			"rightDelim", rightDelim)
	}

	tmpl = tmpl.Delims(leftDelim, rightDelim)

	// Set missing key behavior (default to "default" if not specified)
	missingKey := opts.MissingKey
	if missingKey == "" {
		missingKey = MissingKeyDefault
	}

	optionStr := fmt.Sprintf("missingkey=%s", missingKey)
	lgr.V(2).Info("setting missing key option",
		"name", name,
		"option", optionStr)

	tmpl = tmpl.Option(optionStr)

	// Merge function maps (default funcs + custom funcs)
	funcMap := make(template.FuncMap)

	// Add default service functions first
	if !opts.DisableBuiltinFuncs {
		for k, v := range s.defaultFuncs {
			funcMap[k] = v
		}
	} else {
		lgr.V(2).Info("built-in functions disabled", "name", name)
	}

	// Add custom functions (override defaults if same name)
	for k, v := range opts.Funcs {
		if _, exists := funcMap[k]; exists {
			lgr.V(2).Info("overriding default function with custom function",
				"name", name,
				"function", k)
		}
		funcMap[k] = v
	}

	if len(funcMap) > 0 {
		lgr.V(2).Info("adding template functions",
			"name", name,
			"count", len(funcMap))
		tmpl = tmpl.Funcs(funcMap)
	}

	// Parse the template
	lgr.V(2).Info("parsing template", "name", name)
	parsedTmpl, err := tmpl.Parse(content)
	if err != nil {
		lgr.Error(err, "failed to parse template",
			"name", name,
			"contentLength", len(content))
		return nil, fmt.Errorf("parse error: %w", err)
	}

	lgr.V(2).Info("template parsed successfully", "name", name)
	return parsedTmpl, nil
}

// executeTemplate executes a parsed template with the provided data
func (s *Service) executeTemplate(ctx context.Context, tmpl *template.Template, opts TemplateOptions) (string, error) {
	lgr := logger.FromContext(ctx)

	lgr.V(2).Info("executing template with data",
		"name", opts.Name,
		"dataProvided", opts.Data != nil)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts.Data); err != nil {
		lgr.Error(err, "template execution failed",
			"name", opts.Name,
			"dataType", fmt.Sprintf("%T", opts.Data))
		return "", fmt.Errorf("execution error: %w", err)
	}

	output := buf.String()
	lgr.V(2).Info("template executed successfully",
		"name", opts.Name,
		"outputLength", len(output))

	return output, nil
}

// truncateString truncates a string to maxLen characters for logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Execute is a convenience function that creates a service and executes a template
// For one-off template execution without custom default functions
func Execute(ctx context.Context, opts TemplateOptions) (*ExecuteResult, error) {
	svc := NewService(nil)
	return svc.Execute(ctx, opts)
}
