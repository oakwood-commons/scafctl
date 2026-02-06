// Package lint provides the lint command for validating solutions.
package lint

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/google/cel-go/cel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
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
			    - unused-resolver      Resolver defined but never referenced
			    - invalid-dependency   Action depends on non-existent action
			    - missing-provider     Referenced provider not registered
			    - invalid-expression   Invalid CEL expression syntax
			    - invalid-template     Invalid Go template syntax

			  Warnings (may cause problems):
			    - empty-workflow       Workflow defined but no actions
			    - finally-with-foreach forEach not allowed in finally actions

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
		return err
	}

	lgr.V(1).Info("linting solution", "file", opts.File, "name", sol.Metadata.Name)

	registry := getRegistry()
	result := lintSolution(sol, opts.File, registry)
	result = filterBySeverity(result, opts.Severity)

	if opts.Output == "quiet" {
		if result.ErrorCount > 0 {
			return fmt.Errorf("found %d errors", result.ErrorCount)
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
		return err
	}

	if result.ErrorCount > 0 {
		return fmt.Errorf("found %d errors", result.ErrorCount)
	}

	return nil
}

func getRegistry() *provider.Registry {
	reg, err := builtin.DefaultRegistry()
	if err != nil {
		return provider.GetGlobalRegistry()
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

func lintSolution(sol *solution.Solution, filePath string, registry *provider.Registry) *Result {
	result := &Result{
		File:     filePath,
		Findings: make([]*Finding, 0),
	}

	if !sol.Spec.HasResolvers() && !sol.Spec.HasWorkflow() {
		result.addFinding(SeverityError, "structure", "spec", "Solution has no resolvers or workflow", "", "empty-solution")
		return result
	}

	referencedResolvers := collectReferencedResolvers(sol)

	lintResolvers(sol, result, registry, referencedResolvers)
	lintWorkflow(sol, result, registry)

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
		timeout := act.Timeout.AsDuration()
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
		if val.Expr != nil {
			scanExpressionForResolverRefs(string(*val.Expr), pattern, refs)
		}
		if val.Tmpl != nil {
			scanExpressionForResolverRefs(string(*val.Tmpl), pattern, refs)
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

func filterBySeverity(result *Result, minSeverity string) *Result {
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
