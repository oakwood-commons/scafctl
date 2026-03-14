// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

var (
	// extensionFuncMapFactory is the factory function that provides extension functions.
	// Set via SetExtensionFuncMapFactory during application initialization.
	extensionFuncMapFactory func() template.FuncMap
	extensionFuncMapOnce    sync.Once
	extensionFuncMapMu      sync.RWMutex

	// allowEnvFunctions controls whether sprig's env/expandenv functions are included
	// in the extension func map. Defaults to false (deny) for security.
	// Call SetAllowEnvFunctions after loading config to change this.
	allowEnvFunctionsMu   sync.RWMutex
	allowEnvFunctionsFlag bool // false by default

	// contextFuncBinderFactory is an optional factory that returns context-aware
	// template.FuncMap entries (e.g. a context-bound cel function). Set via
	// SetContextFuncBinderFactory during application initialization. The returned
	// FuncMap is applied to each template clone right before execution so that
	// parent timeouts and cancellation are respected.
	contextFuncBinderFactory func(ctx context.Context) template.FuncMap
	contextFuncBinderMu      sync.RWMutex
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
	Content string `json:"content" yaml:"content" doc:"Template content as a string" maxLength:"1048576" example:"Hello {{ .Name }}"`

	// Name is the reference name/identifier for the template (e.g., file path)
	// Used in logging and error messages
	Name string `json:"name" yaml:"name" doc:"Reference name/identifier for the template" maxLength:"512" example:"greeting.tmpl"`

	// Data is the data source passed to the template during execution
	Data any `json:"data,omitempty" yaml:"data,omitempty" doc:"Data source passed to the template during execution"`

	// LeftDelim sets the left action delimiter (default: "{{")
	LeftDelim string `json:"leftDelim,omitempty" yaml:"leftDelim,omitempty" doc:"Left action delimiter" maxLength:"8" example:"{{"`

	// RightDelim sets the right action delimiter (default: "}}")
	RightDelim string `json:"rightDelim,omitempty" yaml:"rightDelim,omitempty" doc:"Right action delimiter" maxLength:"8" example:"}}"`

	// Replacements is a map of strings to replace before template execution
	// The key is replaced with a UUID placeholder, then restored after execution
	// This helps avoid template parsing errors for content that should be literal
	Replacements []Replacement `json:"replacements,omitempty" yaml:"replacements,omitempty" doc:"String replacements to perform before/after template execution" maxItems:"100"`

	// Funcs is a map of custom template functions to make available
	// These are added to the template's function map
	Funcs template.FuncMap `json:"-" yaml:"-" doc:"Custom template functions to make available"`

	// MissingKey controls the behavior when a map key is missing
	// Default: MissingKeyDefault (prints "<no value>")
	// Options: MissingKeyDefault, MissingKeyZero, MissingKeyError
	MissingKey MissingKeyOption `json:"missingKey,omitempty" yaml:"missingKey,omitempty" doc:"Behavior when a map key is missing" maxLength:"16" example:"default"`

	// DisableBuiltinFuncs disables the built-in template functions
	// By default, basic functions like "html", "js", etc. are available
	DisableBuiltinFuncs bool `json:"disableBuiltinFuncs,omitempty" yaml:"disableBuiltinFuncs,omitempty" doc:"Disables the built-in template functions"`
}

// Replacement defines a string replacement to perform before/after templating
type Replacement struct {
	// Find is the string to search for in the template content
	Find string `json:"find" yaml:"find" doc:"String to search for in the template content" maxLength:"4096" example:"${LITERAL}"`

	// Replace is the temporary replacement value
	// If empty, a UUID will be generated automatically
	Replace string `json:"replace,omitempty" yaml:"replace,omitempty" doc:"Temporary replacement value; if empty, a UUID is generated" maxLength:"4096"`
}

// ExecuteResult contains the result of template execution
type ExecuteResult struct {
	// Output is the rendered template content
	Output string `json:"output" yaml:"output" doc:"Rendered template content" maxLength:"10485760"`

	// TemplateName is the name/identifier of the template
	TemplateName string `json:"templateName" yaml:"templateName" doc:"Name/identifier of the template" maxLength:"512" example:"greeting.tmpl"`

	// ReplacementsMade is the number of replacements that were applied
	ReplacementsMade int `json:"replacementsMade" yaml:"replacementsMade" doc:"Number of replacements that were applied" maximum:"1000" example:"3"`
}

// Service provides template execution capabilities
type Service struct {
	// defaultFuncs are the default custom functions available to all templates
	defaultFuncs template.FuncMap

	// cache is the template compilation cache. If nil, the package-level default is used.
	cache *TemplateCache
}

// SetExtensionFuncMapFactory sets the factory function used to provide extension
// template functions (sprig + custom). This should be called once during
// application initialization to wire in extension functions without creating
// import cycles.
//
// This function is thread-safe and uses sync.Once to ensure it's only set once.
//
// Example (called during application initialization):
//
//	gotmpl.SetExtensionFuncMapFactory(ext.AllFuncMap)
func SetExtensionFuncMapFactory(factory func() template.FuncMap) {
	extensionFuncMapOnce.Do(func() {
		extensionFuncMapMu.Lock()
		defer extensionFuncMapMu.Unlock()
		extensionFuncMapFactory = factory
	})
}

// SetAllowEnvFunctions controls whether the sprig 'env' and 'expandenv' Go template
// functions are available. Call this once after loading application config.
// Defaults to false (deny) — env functions are stripped unless explicitly enabled.
func SetAllowEnvFunctions(allow bool) {
	allowEnvFunctionsMu.Lock()
	allowEnvFunctionsFlag = allow
	allowEnvFunctionsMu.Unlock()
}

// SetContextFuncBinderFactory registers a factory that produces context-aware
// template.FuncMap entries. It is called once per template execution with the
// current context so that functions like `cel` can respect cancellation and
// timeouts. Call this during application initialization to avoid import cycles.
func SetContextFuncBinderFactory(factory func(ctx context.Context) template.FuncMap) {
	contextFuncBinderMu.Lock()
	contextFuncBinderFactory = factory
	contextFuncBinderMu.Unlock()
}

// getContextFuncBinder returns context-bound function overrides, or nil.
func getContextFuncBinder(ctx context.Context) template.FuncMap {
	contextFuncBinderMu.RLock()
	factory := contextFuncBinderFactory
	contextFuncBinderMu.RUnlock()
	if factory == nil {
		return nil
	}
	return factory(ctx)
}

// getExtensionFuncMap returns the extension function map from the factory.
// When allowEnvFunctions is false (the default), the sprig 'env' and 'expandenv'
// functions are removed to prevent templates from exfiltrating process secrets.
func getExtensionFuncMap() template.FuncMap {
	extensionFuncMapMu.RLock()
	var fm template.FuncMap
	if extensionFuncMapFactory != nil {
		fm = extensionFuncMapFactory()
	} else {
		fm = make(template.FuncMap)
	}
	extensionFuncMapMu.RUnlock()

	allowEnvFunctionsMu.RLock()
	allow := allowEnvFunctionsFlag
	allowEnvFunctionsMu.RUnlock()

	if !allow {
		delete(fm, "env")
		delete(fm, "expandenv")
	}
	return fm
}

// NewService creates a new template service with all registered extension
// functions (sprig + custom scafctl extensions) available by default.
// If additionalFuncs is provided, those functions are merged on top of the
// extensions, allowing callers to override any extension function.
//
// Extension functions are provided via SetExtensionFuncMapFactory, which
// should be called during application initialization.
//
// Use NewServiceRaw to create a service without auto-registered extensions.
func NewService(additionalFuncs template.FuncMap) *Service {
	// Start with all extension functions (sprig + custom)
	defaultFuncs := getExtensionFuncMap()

	// Merge caller-provided functions on top (overrides extensions)
	for k, v := range additionalFuncs {
		defaultFuncs[k] = v
	}

	return &Service{
		defaultFuncs: defaultFuncs,
	}
}

// NewServiceWithCache creates a new template service with a custom cache.
// This is useful for testing or when you want isolated cache instances.
func NewServiceWithCache(additionalFuncs template.FuncMap, cache *TemplateCache) *Service {
	svc := NewService(additionalFuncs)
	svc.cache = cache
	return svc
}

// NewServiceRaw creates a new template service without any auto-registered
// extension functions. Only the explicitly provided functions will be available.
// Use this when you want a bare template service without sprig or custom extensions.
func NewServiceRaw(defaultFuncs template.FuncMap) *Service {
	if defaultFuncs == nil {
		defaultFuncs = make(template.FuncMap)
	}
	return &Service{
		defaultFuncs: defaultFuncs,
	}
}

// getCache returns the cache to use for this service.
// If a custom cache was provided, it is used; otherwise the package-level default is used.
func (s *Service) getCache() *TemplateCache {
	if s.cache != nil {
		return s.cache
	}
	return GetDefaultCache()
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

// createTemplate creates and configures a new template, using the cache when possible.
func (s *Service) createTemplate(ctx context.Context, name, content string, opts TemplateOptions) (*template.Template, error) {
	lgr := logger.FromContext(ctx)

	// Resolve effective options
	leftDelim := opts.LeftDelim
	rightDelim := opts.RightDelim
	if leftDelim == "" {
		leftDelim = DefaultLeftDelim
	}
	if rightDelim == "" {
		rightDelim = DefaultRightDelim
	}

	missingKey := opts.MissingKey
	if missingKey == "" {
		missingKey = MissingKeyDefault
	}

	// Build the effective function map keys for cache key generation
	funcMapKeys := s.collectFuncMapKeys(opts)

	// Check the cache
	cache := s.getCache()
	cacheKey := generateTemplateCacheKey(content, leftDelim, rightDelim, missingKey, funcMapKeys)

	if cached, ok := cache.Get(cacheKey); ok {
		lgr.V(2).Info("template cache hit", "name", name, "key", cacheKey[:12])
		return cached, nil
	}

	lgr.V(2).Info("template cache miss, parsing", "name", name, "key", cacheKey[:12])

	// Create base template
	tmpl := template.New(name)

	// Only log if non-default delimiters are used
	if leftDelim != DefaultLeftDelim || rightDelim != DefaultRightDelim {
		lgr.V(2).Info("setting custom delimiters",
			"name", name,
			"leftDelim", leftDelim,
			"rightDelim", rightDelim)
	}

	tmpl = tmpl.Delims(leftDelim, rightDelim)

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

	// Store in cache
	cache.Put(cacheKey, parsedTmpl, name)
	lgr.V(2).Info("template parsed and cached", "name", name, "key", cacheKey[:12])

	return parsedTmpl, nil
}

// collectFuncMapKeys returns the sorted list of function names that will be in
// the effective FuncMap for a given set of template options.
func (s *Service) collectFuncMapKeys(opts TemplateOptions) []string {
	seen := make(map[string]struct{})

	if !opts.DisableBuiltinFuncs {
		for k := range s.defaultFuncs {
			seen[k] = struct{}{}
		}
	}
	for k := range opts.Funcs {
		seen[k] = struct{}{}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

// executeTemplate executes a parsed template with the provided data
func (s *Service) executeTemplate(ctx context.Context, tmpl *template.Template, opts TemplateOptions) (string, error) {
	lgr := logger.FromContext(ctx)

	lgr.V(2).Info("executing template with data",
		"name", opts.Name,
		"dataProvided", opts.Data != nil)

	// Rebind context-aware functions (e.g. cel) on a clone of the cached
	// template so that the current request's timeouts and cancellation are
	// respected without mutating the shared cached instance (which would
	// introduce a data race under concurrent execution).
	if ctxFuncs := getContextFuncBinder(ctx); len(ctxFuncs) > 0 {
		var cloneErr error
		tmpl, cloneErr = tmpl.Clone()
		if cloneErr != nil {
			return "", fmt.Errorf("cloning template for context-aware funcs: %w", cloneErr)
		}
		tmpl = tmpl.Funcs(ctxFuncs)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts.Data); err != nil {
		lgr.Error(err, "template execution failed",
			"name", opts.Name,
			"dataType", fmt.Sprintf("%T", opts.Data))
		return "", fmt.Errorf("execution error: %w\n%s", err, diagnoseTemplateError(err, opts))
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

// diagnoseTemplateError inspects a template execution error and returns
// actionable guidance for common failure modes.
func diagnoseTemplateError(err error, opts TemplateOptions) string {
	msg := err.Error()
	var hints []string

	// Nil pointer / interface conversion (usually accessing a field on nil data).
	if strings.Contains(msg, "nil pointer") || strings.Contains(msg, "interface conversion") {
		hints = append(hints, "A value used in the template is nil. Check that all referenced resolver fields have been resolved before this template runs.")
	}

	// Wrong type for range (e.g., ranging over a string).
	if strings.Contains(msg, "range can't iterate over") {
		hints = append(hints, "The 'range' action received a non-iterable value. Ensure the variable is a list/slice or map, not a string or number.")
	}

	// Method/field not found.
	if strings.Contains(msg, "can't evaluate field") || strings.Contains(msg, "is not a field of struct") {
		hints = append(hints, "A referenced field does not exist on the data object. Double-check field names and casing. Resolver data keys are case-sensitive.")
	}

	// Function not defined.
	if strings.Contains(msg, "function") && strings.Contains(msg, "not defined") {
		hints = append(hints, "A template function is not registered. Available custom functions: slugify, toDnsString, where, selectField, cel, toHcl, toYaml, fromYaml. Sprig functions are also available.")
	}

	// Wrong number of args.
	if strings.Contains(msg, "wrong number of args") {
		hints = append(hints, "A function or method was called with the wrong number of arguments. Check the function signature.")
	}

	// Map has no entry.
	if strings.Contains(msg, "map has no entry for key") {
		hints = append(hints, "A map key was not found. Use 'index' to safely access map keys, or set missingKey to 'zero' or 'default'.")
	}

	// Describe the data type passed.
	if opts.Data != nil {
		hints = append(hints, fmt.Sprintf("Template data type: %T", opts.Data))
		if m, ok := opts.Data.(map[string]any); ok && len(m) > 0 {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) > 20 {
				keys = append(keys[:20], "...")
			}
			hints = append(hints, fmt.Sprintf("Available top-level keys: %s", strings.Join(keys, ", ")))
		}
	} else {
		hints = append(hints, "Template data is nil — no resolver data was passed to this template.")
	}

	if len(hints) == 0 {
		return ""
	}
	return "Hints: " + strings.Join(hints, " | ")
}

// Execute is a convenience function that creates a service and executes a template
// For one-off template execution without custom default functions
func Execute(ctx context.Context, opts TemplateOptions) (*ExecuteResult, error) {
	svc := NewService(nil)
	return svc.Execute(ctx, opts)
}

// ValidateSyntax checks a Go template for parse errors without executing it.
// It returns nil if the template is syntactically valid.
// Use leftDelim/rightDelim to override the default "{{" / "}}" delimiters
// (pass empty strings for defaults).
func ValidateSyntax(content, leftDelim, rightDelim string) error {
	tmpl := template.New("validate")

	switch {
	case leftDelim != "" && rightDelim != "":
		tmpl = tmpl.Delims(leftDelim, rightDelim)
	case leftDelim != "":
		tmpl = tmpl.Delims(leftDelim, DefaultRightDelim)
	case rightDelim != "":
		tmpl = tmpl.Delims(DefaultLeftDelim, rightDelim)
	}

	_, err := tmpl.Parse(content)
	return err
}
