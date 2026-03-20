// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides business logic for validating solution files.
// This package is the shared domain layer used by CLI, MCP, and future API consumers.
package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/cel-go/cel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/solution/walk"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	yaml "gopkg.in/yaml.v3"
)

// SeverityLevel represents the severity of a lint finding.
type SeverityLevel string

const (
	SeverityError   SeverityLevel = "error"
	SeverityWarning SeverityLevel = "warning"
	SeverityInfo    SeverityLevel = "info"
)

// Finding represents a single lint issue found in the solution.
type Finding struct {
	Severity   SeverityLevel `json:"severity" yaml:"severity" doc:"Issue severity level" maxLength:"16" example:"error"`
	Category   string        `json:"category" yaml:"category" doc:"Issue category" maxLength:"64" example:"validation"`
	Location   string        `json:"location" yaml:"location" doc:"Logical path of the issue" maxLength:"512" example:"spec.resolvers.appName"`
	Message    string        `json:"message" yaml:"message" doc:"Issue description" maxLength:"2048" example:"unknown field"`
	Suggestion string        `json:"suggestion,omitempty" yaml:"suggestion,omitempty" doc:"Suggested fix" maxLength:"2048" example:"Remove the unknown field"`
	RuleName   string        `json:"ruleName" yaml:"ruleName" doc:"Lint rule name" maxLength:"128" example:"unknown-field"`
	Line       int           `json:"line,omitempty" yaml:"line,omitempty" doc:"Source line number" maximum:"1000000" example:"42"`
	Column     int           `json:"column,omitempty" yaml:"column,omitempty" doc:"Source column number" maximum:"10000" example:"5"`
	SourceFile string        `json:"sourceFile,omitempty" yaml:"sourceFile,omitempty" doc:"Source file path" maxLength:"512" example:"solution.yaml"`
}

// Result contains all lint findings for a solution.
type Result struct {
	File       string     `json:"file" yaml:"file" doc:"Solution file path" maxLength:"512" example:"solution.yaml"`
	Findings   []*Finding `json:"findings" yaml:"findings" doc:"Lint findings" maxItems:"1000"`
	ErrorCount int        `json:"errorCount" yaml:"errorCount" doc:"Number of error-level findings" maximum:"1000" example:"2"`
	WarnCount  int        `json:"warnCount" yaml:"warnCount" doc:"Number of warning-level findings" maximum:"1000" example:"3"`
	InfoCount  int        `json:"infoCount" yaml:"infoCount" doc:"Number of info-level findings" maximum:"1000" example:"1"`

	// sourceMap is used internally to enrich findings with source positions.
	sourceMap *sourcepos.SourceMap `json:"-" yaml:"-"`
}

// Solution validates a solution and returns structured lint findings.
// This function is reusable by both CLI and MCP.
func Solution(sol *solution.Solution, filePath string, registry *provider.Registry) *Result {
	result := &Result{
		File:     filePath,
		Findings: make([]*Finding, 0),
	}

	// Capture source map for enriching findings with line/column positions.
	result.sourceMap = sol.SourceMap()

	// Schema validation: validate the raw YAML against the generated JSON Schema.
	// This catches unknown fields, type mismatches, pattern violations, etc.
	lintSchema(filePath, result)

	if !sol.Spec.HasResolvers() && !sol.Spec.HasWorkflow() {
		result.addFinding(SeverityError, "structure", "spec", "Solution has no resolvers or workflow", "", "empty-solution")
		return result
	}

	referencedResolvers := collectReferencedResolvers(sol)

	lintResolvers(sol, result, registry, referencedResolvers)
	lintWorkflow(sol, result, registry)
	lintTests(sol, filePath, result)
	lintProviderInputs(sol, result, registry)

	for _, f := range result.Findings {
		switch f.Severity {
		case SeverityError:
			result.ErrorCount++
		case SeverityWarning:
			result.WarnCount++
		case SeverityInfo:
			result.InfoCount++
		}
	}

	return result
}

func (r *Result) addFinding(severity SeverityLevel, category, location, message, suggestion, rule string) {
	f := &Finding{
		Severity:   severity,
		Category:   category,
		Location:   location,
		Message:    message,
		Suggestion: suggestion,
		RuleName:   rule,
	}

	// Enrich with source position if a source map is available.
	// Lint paths omit the "spec." prefix (e.g. "resolvers.foo"), while the
	// source map records full YAML paths (e.g. "spec.resolvers.foo").
	// Try the raw location first, then the "spec." prefixed variant.
	if r.sourceMap != nil {
		if pos, ok := r.sourceMap.Get(location); ok {
			f.Line = pos.Line
			f.Column = pos.Column
			f.SourceFile = pos.File
		} else if pos, ok := r.sourceMap.Get("spec." + location); ok {
			f.Line = pos.Line
			f.Column = pos.Column
			f.SourceFile = pos.File
		}
	}

	r.Findings = append(r.Findings, f)
}

func lintResolvers(sol *solution.Solution, result *Result, registry *provider.Registry, referencedResolvers map[string]bool) {
	if sol.Spec.Resolvers == nil {
		return
	}

	reservedNames := map[string]bool{
		"__actions": true,
		"__error":   true,
		"__item":    true,
		"__index":   true,
		"_":         true,
	}

	for name, res := range sol.Spec.Resolvers {
		location := fmt.Sprintf("resolvers.%s", name)

		if res == nil {
			result.addFinding(SeverityError, "structure", location,
				fmt.Sprintf("resolver '%s' has a null value — a resolve block is required", name),
				"Define the resolver with at least a resolve block, e.g.:\n  resolve:\n    with:\n      - provider: static\n        inputs:\n          value: \"...\"",
				"null-resolver")
			continue
		}

		if reservedNames[name] {
			result.addFinding(SeverityError, "naming", location,
				fmt.Sprintf("resolver name '%s' is reserved", name),
				"Choose a different name that doesn't conflict with built-in variables",
				"reserved-name")
		}

		if !referencedResolvers[name] {
			result.addFinding(SeverityWarning, "usage", location,
				fmt.Sprintf("resolver '%s' is defined but never referenced", name),
				"Remove unused resolver or reference it in actions/other resolvers",
				"unused-resolver")
		}

		if res.Description == "" {
			result.addFinding(SeverityInfo, "documentation", location,
				"resolver lacks description",
				"Add a description to document the resolver's purpose",
				"missing-description")
		}

		if res.Resolve != nil {
			for i, step := range res.Resolve.With {
				stepLocation := fmt.Sprintf("%s.resolve.with[%d]", location, i)

				if step.Provider != "" {
					if _, found := registry.Get(step.Provider); !found {
						result.addFinding(SeverityError, "provider", stepLocation,
							fmt.Sprintf("provider '%s' not found", step.Provider),
							"Check spelling or register the provider",
							"missing-provider")
					}
				}

				lintNilInputs(step.Inputs, stepLocation, result)
				lintExpressions(step.Inputs, stepLocation, result)
			}
		}

		// Check for empty transform.with / validate.with arrays.
		if res.Transform != nil && len(res.Transform.With) == 0 {
			result.addFinding(SeverityWarning, "structure", location+".transform",
				"transform phase has empty 'with' array; no transformations will be applied",
				"Add transform steps or remove the transform section entirely",
				"empty-transform-with")
		}
		if res.Validate != nil && len(res.Validate.With) == 0 {
			result.addFinding(SeverityWarning, "structure", location+".validate",
				"validate phase has empty 'with' array; no validations will be applied",
				"Add validation rules or remove the validate section entirely",
				"empty-validate-with")
		}

		// Check for nil inputs in transform/validate phases.
		if res.Transform != nil {
			for i, step := range res.Transform.With {
				stepLocation := fmt.Sprintf("%s.transform.with[%d]", location, i)
				lintNilInputs(step.Inputs, stepLocation, result)
			}
		}
		if res.Validate != nil {
			for i, step := range res.Validate.With {
				stepLocation := fmt.Sprintf("%s.validate.with[%d]", location, i)
				lintNilInputs(step.Inputs, stepLocation, result)
			}
		}

		// Check for self-references in transform/validate phases.
		// Using _.resolverName in these phases creates a circular dependency;
		// the correct idiom is __self.
		lintResolverSelfReferences(name, res, location, result)
	}
}

// lintResolverSelfReferences checks whether a resolver's transform or validate
// expressions reference their own name via _.resolverName instead of __self.
func lintResolverSelfReferences(name string, res *resolver.Resolver, location string, result *Result) {
	// Build the pattern to detect: _.resolverName (with optional field access)
	selfPattern := "_." + name

	checkInputs := func(inputs map[string]*spec.ValueRef, stepLoc string) {
		for _, val := range inputs {
			if val == nil {
				continue
			}
			if val.Expr != nil && strings.Contains(string(*val.Expr), selfPattern) {
				result.addFinding(SeverityError, "expression", stepLoc,
					fmt.Sprintf("resolver '%s' references itself via _.%s in an expression; use __self instead", name, name),
					fmt.Sprintf("Replace _.%s with __self in the expression to avoid a circular dependency", name),
					"resolver-self-reference")
			}
			if val.Tmpl != nil && strings.Contains(string(*val.Tmpl), selfPattern) {
				result.addFinding(SeverityError, "expression", stepLoc,
					fmt.Sprintf("resolver '%s' references itself via _.%s in a template; use __self instead", name, name),
					fmt.Sprintf("Replace _.%s with __self in the template to avoid a circular dependency", name),
					"resolver-self-reference")
			}
		}
	}

	if res.Transform != nil {
		for i, step := range res.Transform.With {
			stepLoc := fmt.Sprintf("%s.transform.with[%d]", location, i)
			checkInputs(step.Inputs, stepLoc)
		}
	}

	if res.Validate != nil {
		for i, step := range res.Validate.With {
			stepLoc := fmt.Sprintf("%s.validate.with[%d]", location, i)
			checkInputs(step.Inputs, stepLoc)
			// Also check the message field which can be a ValueRef
			if step.Message != nil {
				if step.Message.Expr != nil && strings.Contains(string(*step.Message.Expr), selfPattern) {
					msgLoc := fmt.Sprintf("%s.validate.with[%d].message", location, i)
					result.addFinding(SeverityError, "expression", msgLoc,
						fmt.Sprintf("resolver '%s' references itself via _.%s in message; use __self instead", name, name),
						fmt.Sprintf("Replace _.%s with __self in the message expression", name),
						"resolver-self-reference")
				}
				if step.Message.Tmpl != nil && strings.Contains(string(*step.Message.Tmpl), selfPattern) {
					msgLoc := fmt.Sprintf("%s.validate.with[%d].message", location, i)
					result.addFinding(SeverityError, "expression", msgLoc,
						fmt.Sprintf("resolver '%s' references itself via _.%s in message template; use __self instead", name, name),
						fmt.Sprintf("Replace _.%s with __self in the message template", name),
						"resolver-self-reference")
				}
			}
		}
	}
}

func lintWorkflow(sol *solution.Solution, result *Result, registry *provider.Registry) {
	if sol.Spec.Workflow == nil {
		return
	}

	workflow := sol.Spec.Workflow

	if len(workflow.Actions) == 0 && len(workflow.Finally) == 0 {
		result.addFinding(SeverityWarning, "structure", "workflow",
			"workflow defined but contains no actions",
			"Add actions or remove the empty workflow section",
			"empty-workflow")
		return
	}

	if len(workflow.Actions) == 0 && len(workflow.Finally) > 0 {
		result.addFinding(SeverityInfo, "structure", "workflow",
			"finally section exists but no regular actions defined",
			"Consider whether finally actions are needed without regular actions",
			"unused-finally")
	}

	actionNames := make(map[string]bool)
	for name := range workflow.Actions {
		actionNames[name] = true
	}
	finallyNames := make(map[string]bool)
	for name := range workflow.Finally {
		finallyNames[name] = true
	}

	for name, act := range workflow.Actions {
		location := fmt.Sprintf("workflow.actions.%s", name)
		lintAction(act, location, actionNames, result, registry)
	}

	for name, act := range workflow.Finally {
		location := fmt.Sprintf("workflow.finally.%s", name)
		lintAction(act, location, finallyNames, result, registry)

		if act.ForEach != nil {
			result.addFinding(SeverityError, "validation", location,
				"forEach not allowed in finally actions",
				"Move the action to workflow.actions or remove forEach",
				"finally-with-foreach")
		}
	}

	// Validate workflow structure (circular deps, etc.)
	adapter := &registryAdapter{registry: registry}
	if err := action.ValidateWorkflow(workflow, adapter); err != nil {
		aggErr := &action.AggregatedValidationError{}
		if errors.As(err, &aggErr) {
			for _, valErr := range aggErr.Errors {
				location := fmt.Sprintf("workflow.%s.%s", valErr.Section, valErr.ActionName)
				result.addFinding(SeverityError, "validation", location, valErr.Message, "", "workflow-validation")
			}
		} else {
			result.addFinding(SeverityError, "validation", "workflow", err.Error(), "", "workflow-validation")
		}
	}
}

// registryAdapter adapts provider.Registry to action.RegistryInterface
type registryAdapter struct {
	registry *provider.Registry
}

func (r *registryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

func (r *registryAdapter) Has(name string) bool {
	return r.registry.Has(name)
}

func lintAction(act *action.Action, location string, validDeps map[string]bool, result *Result, registry *provider.Registry) {
	if act.Description == "" {
		result.addFinding(SeverityInfo, "documentation", location,
			"action lacks description",
			"Add a description to document the action's purpose",
			"missing-description")
	}

	if act.Provider != "" {
		if _, found := registry.Get(act.Provider); !found {
			result.addFinding(SeverityError, "provider", location,
				fmt.Sprintf("provider '%s' not found", act.Provider),
				"Check spelling or register the provider",
				"missing-provider")
		}
	}

	for _, dep := range act.DependsOn {
		if !validDeps[dep] {
			result.addFinding(SeverityError, "dependency", location,
				fmt.Sprintf("depends on non-existent action '%s'", dep),
				"Check the action name or add the missing action",
				"invalid-dependency")
		}
	}

	if act.Timeout != nil {
		timeout := act.Timeout.Duration
		if timeout > 10*time.Minute {
			result.addFinding(SeverityInfo, "performance", location,
				fmt.Sprintf("timeout of %s exceeds recommended 10 minute maximum", act.Timeout.String()),
				"Consider breaking into smaller actions or using async patterns",
				"long-timeout")
		}
	}

	lintExpressions(act.Inputs, location, result)

	if act.When != nil && act.When.Expr != nil {
		if err := validateCELSyntax(string(*act.When.Expr)); err != nil {
			result.addFinding(SeverityError, "expression", location+".when",
				fmt.Sprintf("invalid CEL expression: %v", err),
				"Fix the expression syntax",
				"invalid-expression")
		}
	}

	if act.ResultSchema != nil {
		lintResultSchema(act.ResultSchema, location+".resultSchema", result)
	}
}

func lintResultSchema(schema *jsonschema.Schema, location string, result *Result) {
	// Validate the schema can be resolved (checks for valid $ref, etc.)
	_, err := schema.Resolve(nil)
	if err != nil {
		result.addFinding(SeverityError, "schema", location,
			fmt.Sprintf("invalid result schema: %v", err),
			"Fix the schema definition",
			"invalid-result-schema")
		return
	}

	// Warn if type is not specified (schema is too permissive)
	if schema.Type == "" && len(schema.Types) == 0 {
		result.addFinding(SeverityInfo, "schema", location,
			"result schema has no 'type' specified, which allows any value",
			"Consider adding a 'type' field to constrain the result",
			"permissive-result-schema")
	}

	// Validate required properties exist in properties
	for _, req := range schema.Required {
		if schema.Properties != nil {
			if _, exists := schema.Properties[req]; !exists {
				result.addFinding(SeverityError, "schema", location,
					fmt.Sprintf("required property '%s' not defined in properties", req),
					"Add the property definition or remove from required",
					"undefined-required-property")
			}
		}
	}

	// Lint nested properties
	for name, prop := range schema.Properties {
		lintResultSchema(prop, fmt.Sprintf("%s.properties.%s", location, name), result)
	}

	// Lint array items schema
	if schema.Items != nil {
		lintResultSchema(schema.Items, location+".items", result)
	}
}

// lintNilInputs checks for nil ValueRef entries in provider inputs, which
// typically result from dangling YAML keys with no value.
func lintNilInputs(inputs map[string]*spec.ValueRef, location string, result *Result) {
	for key, val := range inputs {
		if val == nil {
			inputLoc := fmt.Sprintf("%s.inputs.%s", location, key)
			result.addFinding(SeverityError, "provider", inputLoc,
				fmt.Sprintf("input '%s' has no value (dangling YAML key)", key),
				"Provide a value for the input or remove the key entirely",
				"nil-provider-input")
		}
	}
}

func lintExpressions(inputs map[string]*spec.ValueRef, location string, result *Result) {
	for key, val := range inputs {
		if val == nil {
			continue
		}

		inputLoc := fmt.Sprintf("%s.inputs.%s", location, key)

		if val.Expr != nil && string(*val.Expr) != "" {
			if err := validateCELSyntax(string(*val.Expr)); err != nil {
				result.addFinding(SeverityError, "expression", inputLoc,
					fmt.Sprintf("invalid CEL expression: %v", err),
					"Fix the expression syntax",
					"invalid-expression")
			}
		}

		if val.Tmpl != nil && string(*val.Tmpl) != "" {
			if err := validateTemplateSyntax(string(*val.Tmpl)); err != nil {
				result.addFinding(SeverityError, "template", inputLoc,
					fmt.Sprintf("invalid Go template: %v", err),
					"Fix the template syntax",
					"invalid-template")
			}
		}
	}
}

// validateCELSyntax checks if a CEL expression is syntactically valid.
func validateCELSyntax(expr string) error {
	env, err := cel.NewEnv()
	if err != nil {
		return err
	}
	_, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return issues.Err()
	}
	return nil
}

// validateTemplateSyntax checks if a Go template is syntactically valid.
func validateTemplateSyntax(tmpl string) error {
	_, err := template.New("lint").Parse(tmpl)
	return err
}

func collectReferencedResolvers(sol *solution.Solution) map[string]bool {
	refs := make(map[string]bool)

	resolverRefPattern := regexp.MustCompile(`_\.([a-zA-Z_][a-zA-Z0-9_]*)|__resolvers\.([a-zA-Z_][a-zA-Z0-9_]*)`)

	_ = walk.Walk(sol, &walk.Visitor{
		ValueRef: func(_ string, vr *spec.ValueRef) error {
			if vr.Resolver != nil {
				refs[*vr.Resolver] = true
			}
			if vr.Expr != nil {
				scanExpressionForResolverRefs(string(*vr.Expr), resolverRefPattern, refs)
			}
			if vr.Tmpl != nil {
				scanExpressionForResolverRefs(string(*vr.Tmpl), resolverRefPattern, refs)
			}
			if vr.Literal != nil {
				scanLiteralForResolverRefs(vr.Literal, resolverRefPattern, refs)
			}
			return nil
		},
		Condition: func(_, _ string, expr *celexp.Expression) error {
			scanExpressionForResolverRefs(string(*expr), resolverRefPattern, refs)
			return nil
		},
	})

	return refs
}

// scanLiteralForResolverRefs scans literal values (maps, slices) for nested
// resolver references, expressions, and templates. Nested maps with a single
// "rslvr", "expr", or "tmpl" key are treated as resolver references.
func scanLiteralForResolverRefs(v any, pattern *regexp.Regexp, refs map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		// Check if this map itself is a resolver reference
		if rslvr, ok := val["rslvr"]; ok {
			if name, ok := rslvr.(string); ok {
				refs[name] = true
			}
		}
		if expr, ok := val["expr"]; ok {
			if s, ok := expr.(string); ok {
				scanExpressionForResolverRefs(s, pattern, refs)
			}
		}
		if tmpl, ok := val["tmpl"]; ok {
			if s, ok := tmpl.(string); ok {
				scanExpressionForResolverRefs(s, pattern, refs)
			}
		}
		// Recurse into nested values
		for _, child := range val {
			scanLiteralForResolverRefs(child, pattern, refs)
		}
	case []any:
		for _, item := range val {
			scanLiteralForResolverRefs(item, pattern, refs)
		}
	}
}

func scanExpressionForResolverRefs(expr string, pattern *regexp.Regexp, refs map[string]bool) {
	matches := pattern.FindAllStringSubmatch(expr, -1)
	for _, match := range matches {
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				refs[match[i]] = true
			}
		}
	}
}

func lintTests(sol *solution.Solution, solutionPath string, result *Result) {
	if !sol.Spec.HasTests() {
		return
	}

	solutionDir := filepath.Dir(solutionPath)

	// Test name validation regexes (same as soltesting package).
	testNameRegex := soltesting.TestNamePattern()
	templateNameRegex := soltesting.TemplateNamePattern()

	// Collect all extends references to detect unused templates.
	extendsRefs := make(map[string]bool)
	for _, tc := range sol.Spec.Testing.Cases {
		for _, ext := range tc.Extends {
			extendsRefs[ext] = true
		}
	}

	for name, tc := range sol.Spec.Testing.Cases {
		location := fmt.Sprintf("testing.cases.%s", name)

		// Rule: invalid-test-name — validate naming pattern.
		// Use the map key directly rather than tc.Name, which may not be set yet.
		isTemplate := strings.HasPrefix(name, "_")
		if isTemplate {
			if !templateNameRegex.MatchString(name) {
				result.addFinding(SeverityError, "naming", location,
					fmt.Sprintf("template name %q does not match pattern %s", name, templateNameRegex.String()),
					"Template names must start with _ followed by a letter or digit",
					"invalid-test-name")
			}
		} else {
			if !testNameRegex.MatchString(name) {
				result.addFinding(SeverityError, "naming", location,
					fmt.Sprintf("test name %q does not match pattern %s", name, testNameRegex.String()),
					"Test names must start with a letter or digit and contain only letters, digits, hyphens, and underscores",
					"invalid-test-name")
			}
		}

		// Rule: unbundled-test-file — check files are covered by bundle.include.
		if !sol.Bundle.IsEmpty() && len(tc.Files) > 0 {
			for i, file := range tc.Files {
				fileLoc := fmt.Sprintf("%s.files[%d]", location, i)
				if !isCoveredByBundleInclude(file, sol.Bundle.Include) {
					result.addFinding(SeverityError, "bundling", fileLoc,
						fmt.Sprintf("test file %q is not covered by any bundle.include pattern", file),
						"Add a matching glob pattern to bundle.include so the file is included in the bundle",
						"unbundled-test-file")
				}
			}
		}

		// Rule: unreachable-test-path — warn when a files entry doesn't resolve to anything on disk.
		if solutionDir != "" && len(tc.Files) > 0 {
			for i, file := range tc.Files {
				fileLoc := fmt.Sprintf("%s.files[%d]", location, i)
				if !testFileReachable(solutionDir, file) {
					result.addFinding(SeverityWarning, "testing", fileLoc,
						fmt.Sprintf("test file path %q does not match any existing file or directory", file),
						"Check for typos, verify the file exists, or use a valid glob pattern (e.g., 'templates/**/*.yaml')",
						"unreachable-test-path")
				}
			}
		}

		// Rule: unused-template — templates not referenced by any extends.
		if isTemplate && !extendsRefs[name] {
			result.addFinding(SeverityWarning, "usage", location,
				fmt.Sprintf("test template %q is defined but never referenced by any extends field", name),
				"Remove the unused template or reference it via extends in another test",
				"unused-template")
		}
	}
}

// isCoveredByBundleInclude checks if a file path matches any bundle.include glob pattern.
func isCoveredByBundleInclude(file string, includes []string) bool {
	for _, pattern := range includes {
		// Use doublestar for ** glob support.
		matched, err := doublestar.Match(pattern, file)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// testFileReachable returns true if a test file entry resolves to at least one
// existing file or directory on disk. Supports plain paths, directories, and glob patterns.
func testFileReachable(solutionDir, entry string) bool {
	cleaned := filepath.Clean(entry)

	// Reject path traversal and absolute paths — let other rules handle those.
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return true // don't double-flag, other rules will catch this
	}

	// Glob patterns: check if they expand to at least one match.
	if strings.ContainsAny(entry, "*?[{") {
		absPattern := filepath.Join(solutionDir, entry)
		matches, err := doublestar.FilepathGlob(absPattern)
		return err == nil && len(matches) > 0
	}

	// Plain path or directory: check if it exists on disk.
	absPath := filepath.Join(solutionDir, cleaned)
	// Verify the resolved path is within solutionDir (defence-in-depth on top
	// of the ".." and IsAbs guards above that already exclude traversal).
	// Use filepath.Rel so that the check is correct when solutionDir is the
	// filesystem root ("/") where cleanedBase+Sep would be "//" — a prefix
	// that no normal path starts with, causing false negatives.
	cleanedBase := filepath.Clean(solutionDir)
	rel, relErr := filepath.Rel(cleanedBase, absPath)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	_, err := os.Stat(absPath) //nolint:gosec // path validated to be within solutionDir
	return err == nil
}

// lintSchema reads the solution file from disk, unmarshals it into a generic
// map (preserving unknown fields), and validates it against the generated
// JSON Schema. Any violations are added to the result as schema-violation findings.
func lintSchema(filePath string, result *Result) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// If we can't read the file, skip schema validation silently.
		// The caller already loaded the solution successfully, so this is
		// likely a non-file source (URL, catalog, etc.).
		return
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// If raw YAML parsing fails, skip — the typed unmarshal already succeeded.
		return
	}

	violations, err := schema.ValidateSolutionAgainstSchema(raw)
	if err != nil {
		// Schema compilation error — report as a single finding so users know.
		result.addFinding(SeverityWarning, "schema", "", fmt.Sprintf("schema validation unavailable: %v", err), "", "schema-error")
		return
	}

	for _, v := range violations {
		location := v.Path
		if location == "" {
			location = "(root)"
		}
		result.addFinding(SeverityError, "schema", location, v.Message,
			"Check field name spelling and value types against the solution schema",
			"schema-violation")
	}
}

// lintProviderInputs validates that resolver and action inputs match the
// provider's declared JSON Schema. It checks:
//   - Unknown input keys (keys not in the provider schema's properties)
//   - Literal values that violate the provider schema's type constraints
//
// Expression, template, and resolver-reference values are silently skipped
// because they can only be validated at runtime.
func lintProviderInputs(sol *solution.Solution, result *Result, registry *provider.Registry) {
	_ = walk.Walk(sol, &walk.Visitor{
		ProviderSource: func(path string, ps *resolver.ProviderSource) error {
			lintProviderInputsForStep(ps.Provider, ps.Inputs, strings.TrimPrefix(path, "spec."), result, registry)
			return nil
		},
		ProviderTransform: func(path string, pt *resolver.ProviderTransform) error {
			lintProviderInputsForStep(pt.Provider, pt.Inputs, strings.TrimPrefix(path, "spec."), result, registry)
			return nil
		},
		ProviderValidation: func(path string, pv *resolver.ProviderValidation) error {
			lintProviderInputsForStep(pv.Provider, pv.Inputs, strings.TrimPrefix(path, "spec."), result, registry)
			return nil
		},
		Action: func(path, _, _ string, act *action.Action) error {
			lintProviderInputsForStep(act.Provider, act.Inputs, strings.TrimPrefix(path, "spec."), result, registry)
			return nil
		},
	})
}

// lintProviderInputsForStep validates inputs for a single provider step.
func lintProviderInputsForStep(providerName string, inputs map[string]*spec.ValueRef, location string, result *Result, registry *provider.Registry) {
	if providerName == "" || inputs == nil {
		return
	}

	p, found := registry.Get(providerName)
	if !found {
		// missing-provider is already reported by lintResolvers/lintAction.
		return
	}

	desc := p.Descriptor()
	if desc.Schema == nil {
		return
	}

	// Get the allowed property names from the provider schema.
	allowedProps := desc.Schema.Properties

	for key, val := range inputs {
		inputLoc := fmt.Sprintf("%s.inputs.%s", location, key)

		// Check for unknown input keys.
		if allowedProps != nil {
			if _, exists := allowedProps[key]; !exists {
				// If the schema allows additional properties, skip the unknown-input check.
				if desc.Schema.AdditionalProperties != nil {
					continue
				}
				result.addFinding(SeverityError, "provider", inputLoc,
					fmt.Sprintf("unknown input %q for provider %q", key, providerName),
					fmt.Sprintf("Check the provider's accepted inputs. Run: scafctl explain provider %s", providerName),
					"unknown-provider-input")
				continue
			}
		}

		// Validate literal values against the property schema type.
		if val != nil && val.Literal != nil && allowedProps != nil {
			propSchema, exists := allowedProps[key]
			if !exists || propSchema == nil {
				continue
			}

			resolved, err := propSchema.Resolve(nil)
			if err != nil {
				continue
			}

			if err := resolved.Validate(val.Literal); err != nil {
				result.addFinding(SeverityError, "provider", inputLoc,
					fmt.Sprintf("invalid value for input %q of provider %q: %v", key, providerName, err),
					"Check the expected type and constraints for this input",
					"invalid-provider-input-type")
			}
		}
	}

	// Security rule: warn when exec provider's 'command' input uses a dynamic expression or
	// template. Shell metacharacters in resolved values can cause command injection.
	// Pass dynamic data via 'args' instead — args are shell-quoted before being appended
	// to the command, which reduces injection risk compared to embedding dynamic values
	// directly in the command string.
	if providerName == "exec" {
		if cmdVal, ok := inputs["command"]; ok && cmdVal != nil {
			if cmdVal.Expr != nil || cmdVal.Tmpl != nil {
				result.addFinding(SeverityWarning, "security", location+".inputs.command",
					"exec provider 'command' uses a dynamic expression or template; shell metacharacters in resolved values may cause command injection",
					"Pass dynamic values via the 'args' input instead — args are shell-quoted before being appended to the command, reducing injection risk",
					"exec-command-injection")
			}
		}
	}
}

// FilterBySeverity filters lint findings to only include those at or above
// the specified minimum severity level.
func FilterBySeverity(result *Result, minSeverity string) *Result {
	severityOrder := map[SeverityLevel]int{
		SeverityError:   3,
		SeverityWarning: 2,
		SeverityInfo:    1,
	}

	minLevel := severityOrder[SeverityLevel(strings.ToLower(minSeverity))]
	if minLevel == 0 {
		minLevel = 1
	}

	filtered := &Result{
		File:     result.File,
		Findings: make([]*Finding, 0),
	}

	for _, f := range result.Findings {
		if severityOrder[f.Severity] >= minLevel {
			filtered.Findings = append(filtered.Findings, f)
			switch f.Severity {
			case SeverityError:
				filtered.ErrorCount++
			case SeverityWarning:
				filtered.WarnCount++
			case SeverityInfo:
				filtered.InfoCount++
			}
		}
	}

	return filtered
}
