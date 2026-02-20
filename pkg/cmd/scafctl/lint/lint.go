// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides the lint command for validating solutions.
package lint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/cel-go/cel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/solutionprovider"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
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
	Severity   SeverityLevel `json:"severity" yaml:"severity"`
	Category   string        `json:"category" yaml:"category"`
	Location   string        `json:"location" yaml:"location"`
	Message    string        `json:"message" yaml:"message"`
	Suggestion string        `json:"suggestion,omitempty" yaml:"suggestion,omitempty"`
	RuleName   string        `json:"ruleName" yaml:"ruleName"`
}

// Result contains all lint findings for a solution.
type Result struct {
	File       string     `json:"file" yaml:"file"`
	Findings   []*Finding `json:"findings" yaml:"findings"`
	ErrorCount int        `json:"errorCount" yaml:"errorCount"`
	WarnCount  int        `json:"warnCount" yaml:"warnCount"`
	InfoCount  int        `json:"infoCount" yaml:"infoCount"`
}

// Options holds command flags and settings.
type Options struct {
	File      string
	Output    string
	Severity  string
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandLint creates the lint command.
func CommandLint(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "lint",
		Aliases: []string{"l", "check"},
		Short:   "Lint a solution file for issues and best practices",
		Long: heredoc.Doc(`
			Analyze a solution file for potential issues, anti-patterns, and best practices.

			LINT RULES:
			  Errors (will cause execution failures):
			    - unused-resolver          Resolver defined but never referenced
			    - invalid-dependency       Action depends on non-existent action
			    - missing-provider         Referenced provider not registered
			    - invalid-expression       Invalid CEL expression syntax
			    - invalid-template         Invalid Go template syntax
			    - unbundled-test-file      Test file not covered by bundle.include
			    - invalid-test-name        Test name does not match naming pattern
			    - schema-violation         Solution YAML violates the JSON Schema
			    - unknown-provider-input   Input key not declared in provider schema
			    - invalid-provider-input-type  Literal input value violates provider schema type

			  Warnings (may cause problems):
			    - empty-workflow       Workflow defined but no actions
			    - finally-with-foreach forEach not allowed in finally actions
			    - unused-template      Test template not referenced by any extends

			  Info (suggestions):
			    - missing-description  Action/resolver lacks description
			    - long-timeout        Timeout exceeds recommended maximum
			    - unused-finally      Finally actions with no regular actions

			OUTPUT FORMATS:
			  table   Human-readable table (default)
			  json    JSON output for tooling integration
			  yaml    YAML output
			  quiet   Exit code only (0=clean, 1=issues found)
		`),
		Example: heredoc.Doc(`
			# Lint a solution file
			scafctl lint -f ./solution.yaml

			# Show only errors (skip warnings and info)
			scafctl lint -f ./solution.yaml --severity error

			# Output as JSON for CI integration
			scafctl lint -f ./solution.yaml -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)
			lgr := logger.FromContext(cmd.Context())
			ctx = logger.WithLogger(ctx, lgr)

			return runLint(ctx, options)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (required)")
	cmd.Flags().StringVarP(&options.Output, "output", "o", "table", "Output format: table, json, yaml, quiet")
	cmd.Flags().StringVar(&options.Severity, "severity", "info", "Minimum severity to report: error, warning, info")

	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func runLint(ctx context.Context, opts *Options) error {
	lgr := logger.FromContext(ctx)

	// Set up getter with catalog resolver for bare name resolution
	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetter(getterOpts...)
	sol, err := getter.Get(ctx, opts.File)
	if err != nil {
		writeError(opts, fmt.Sprintf("failed to load solution: %v", err))
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	lgr.V(1).Info("linting solution", "file", opts.File, "name", sol.Metadata.Name)

	registry := getRegistry(ctx)
	result := Solution(sol, opts.File, registry)
	result = FilterBySeverity(result, opts.Severity)

	if opts.Output == "quiet" {
		if result.ErrorCount > 0 {
			return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
		}
		return nil
	}

	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		opts.Output,
		false,
		"",
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl lint"),
	)
	kvxOpts.IOStreams = opts.IOStreams

	if err := kvxOpts.Write(result); err != nil {
		writeError(opts, fmt.Sprintf("failed to write output: %v", err))
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if result.ErrorCount > 0 {
		return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
	}

	return nil
}

func getRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		reg = provider.GetGlobalRegistry()
	}
	// The solution provider is not part of DefaultRegistry because it has a
	// circular dependency on the registry itself and requires a loader. For
	// lint purposes we only need the provider to be *registered* (so the
	// missing-provider rule doesn't false-positive); it will never be executed.
	if !reg.Has(solutionprovider.ProviderName) {
		solProvider := solutionprovider.New(
			solutionprovider.WithRegistry(reg),
		)
		_ = reg.Register(solProvider)
	}
	return reg
}

func writeError(opts *Options, msg string) {
	output.NewWriteMessageOptions(
		opts.IOStreams,
		output.MessageTypeError,
		opts.CliParams.NoColor,
		opts.CliParams.ExitOnError,
	).WriteMessage(msg)
}

// Solution validates a solution and returns structured lint findings.
// This function is reusable by both CLI and MCP.
func Solution(sol *solution.Solution, filePath string, registry *provider.Registry) *Result {
	result := &Result{
		File:     filePath,
		Findings: make([]*Finding, 0),
	}

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
	lintTests(sol, result)
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
	r.Findings = append(r.Findings, &Finding{
		Severity:   severity,
		Category:   category,
		Location:   location,
		Message:    message,
		Suggestion: suggestion,
		RuleName:   rule,
	})
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

				lintExpressions(step.Inputs, stepLocation, result)
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

	for _, res := range sol.Spec.Resolvers {
		if res.Resolve == nil {
			continue
		}
		for _, step := range res.Resolve.With {
			scanInputsForResolverRefs(step.Inputs, resolverRefPattern, refs)
		}
	}

	if sol.Spec.Workflow != nil {
		for _, act := range sol.Spec.Workflow.Actions {
			scanInputsForResolverRefs(act.Inputs, resolverRefPattern, refs)
			if act.When != nil && act.When.Expr != nil {
				scanExpressionForResolverRefs(string(*act.When.Expr), resolverRefPattern, refs)
			}
		}
		for _, act := range sol.Spec.Workflow.Finally {
			scanInputsForResolverRefs(act.Inputs, resolverRefPattern, refs)
			if act.When != nil && act.When.Expr != nil {
				scanExpressionForResolverRefs(string(*act.When.Expr), resolverRefPattern, refs)
			}
		}
	}

	return refs
}

func scanInputsForResolverRefs(inputs map[string]*spec.ValueRef, pattern *regexp.Regexp, refs map[string]bool) {
	for _, val := range inputs {
		if val == nil {
			continue
		}
		// Direct resolver reference: rslvr: resolverName
		if val.Resolver != nil {
			refs[*val.Resolver] = true
		}
		if val.Expr != nil {
			scanExpressionForResolverRefs(string(*val.Expr), pattern, refs)
		}
		if val.Tmpl != nil {
			scanExpressionForResolverRefs(string(*val.Tmpl), pattern, refs)
		}
		// Recurse into literal map values to find nested rslvr/expr/tmpl references
		if val.Literal != nil {
			scanLiteralForResolverRefs(val.Literal, pattern, refs)
		}
	}
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

func lintTests(sol *solution.Solution, result *Result) {
	if !sol.Spec.HasTests() {
		return
	}

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
	// Lint resolver inputs
	for name, res := range sol.Spec.Resolvers {
		if res.Resolve != nil {
			for i, step := range res.Resolve.With {
				location := fmt.Sprintf("resolvers.%s.resolve.with[%d]", name, i)
				lintProviderInputsForStep(step.Provider, step.Inputs, location, result, registry)
			}
		}
		if res.Transform != nil {
			for i, step := range res.Transform.With {
				location := fmt.Sprintf("resolvers.%s.transform.with[%d]", name, i)
				lintProviderInputsForStep(step.Provider, step.Inputs, location, result, registry)
			}
		}
		if res.Validate != nil {
			for i, step := range res.Validate.With {
				location := fmt.Sprintf("resolvers.%s.validate.with[%d]", name, i)
				lintProviderInputsForStep(step.Provider, step.Inputs, location, result, registry)
			}
		}
	}

	// Lint workflow action inputs
	if sol.Spec.Workflow != nil {
		for name, act := range sol.Spec.Workflow.Actions {
			location := fmt.Sprintf("workflow.actions.%s", name)
			lintProviderInputsForStep(act.Provider, act.Inputs, location, result, registry)
		}
		for name, act := range sol.Spec.Workflow.Finally {
			location := fmt.Sprintf("workflow.finally.%s", name)
			lintProviderInputsForStep(act.Provider, act.Inputs, location, result, registry)
		}
	}
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
