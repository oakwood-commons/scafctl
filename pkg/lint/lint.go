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
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/cel-go/cel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	resolverRefs "github.com/oakwood-commons/scafctl/pkg/resolver/refs"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/solution/walk"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
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
	if registry == nil {
		registry = provider.NewRegistry()
	}
	// Clone the registry to avoid mutating the shared singleton when
	// marking bundle.plugins and official providers as known.
	registry = registryWithBundlePlugins(registry, sol)

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

	// Scan external template files on disk for resolver references.
	// This prevents false-positive unused-resolver warnings for resolvers
	// consumed only in .tpl files (not referenced in the solution YAML).
	solutionDir := filepath.Dir(filePath)
	templateFileRefs := collectTemplateFileResolverRefs(sol, solutionDir)
	for ref := range templateFileRefs {
		referencedResolvers[ref] = true
	}

	lintResolvers(sol, result, registry, referencedResolvers)
	lintTemplateFileDependencies(sol, solutionDir, result, registry)
	lintResolverCycles(sol, result, registry)
	lintWorkflow(sol, result, registry)
	lintState(sol, result, registry)
	lintImmutableResolvers(sol, result)
	lintTests(sol, filePath, result)
	lintProviderInputs(sol, result, registry)
	lintDeprecatedFields(sol, result)

	// Apply suppression directives parsed from inline YAML comments.
	suppressions := ParseDirectives(sol.RawContent(), filePath).WithSourceMap(result.sourceMap)
	result.Findings = suppressions.Filter(result.Findings)
	result.Findings = append(result.Findings, suppressions.UnusedFindings()...)

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

// parameterProviderName is the name of the built-in parameter provider, whose
// source fails at runtime when the named CLI parameter is missing and no
// 'default' input is declared.
const parameterProviderName = "parameter"

// parameterDefaultInput is the input key the parameter provider treats as a
// fallback value (presence of the key, regardless of value, counts).
const parameterDefaultInput = "default"

// isUnconditionalSource reports whether a resolve source always runs -- it has
// no 'when' condition, or a 'when' that is literally "true".
func isUnconditionalSource(step resolver.ProviderSource) bool {
	return step.When == nil || step.When.Expr == nil || strings.TrimSpace(string(*step.When.Expr)) == "true"
}

// parameterSourceHasDefault reports whether a parameter provider source
// declares a 'default' input. The parameter provider treats the presence of the
// 'default' key (any value) as a fallback.
func parameterSourceHasDefault(step resolver.ProviderSource) bool {
	_, ok := step.Inputs[parameterDefaultInput]
	return ok
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

		if strings.Contains(name, "-") {
			result.addFinding(SeverityWarning, "naming", location,
				fmt.Sprintf("resolver name '%s' contains hyphens — use camelCase for CEL compatibility (e.g., '%s')",
					name, hyphensToCamelCase(name)),
				"Hyphens in resolver names require quoting in CEL expressions: _[\"my-resolver\"] instead of _.myResolver",
				"hyphenated-name")
		}

		// A resolver with a validate block exists for its side effect (aborting
		// execution on validation failure), so it is "used" even if no other
		// resolver or action references it.
		hasValidation := res.Validate != nil && len(res.Validate.With) > 0
		if !referencedResolvers[name] && !hasValidation {
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
					if !registry.Has(step.Provider) {
						result.addFinding(SeverityError, "provider", stepLocation,
							fmt.Sprintf("provider '%s' not found", step.Provider),
							"Check spelling or register the provider",
							"missing-provider")
					}
				}

				lintNilInputs(step.Inputs, stepLocation, result)
				lintExpressions(step.Inputs, stepLocation, result)

				// Warn if forEach is used on a resolve step.
				if step.ForEach != nil {
					result.addFinding(SeverityWarning, "structure", stepLocation,
						"forEach on resolve step is not supported; __self is not available during the resolve phase",
						"Move forEach to a transform step or use a separate resolver to iterate",
						"resolve-foreach")
				}
			}

			// Warn if all sources have 'when' conditions with no unconditional fallback.
			if len(res.Resolve.With) > 0 {
				allConditional := true
				for _, step := range res.Resolve.With {
					if step.When == nil || step.When.Expr == nil || strings.TrimSpace(string(*step.When.Expr)) == "true" {
						allConditional = false
						break
					}
				}
				if allConditional {
					result.addFinding(SeverityWarning, "structure", location+".resolve",
						"all resolve sources have 'when' conditions with no unconditional fallback; resolver will fail if no condition is met",
						"Add a source without a 'when' condition (e.g. a static provider) as a fallback",
						"missing-fallback-source")
				}
			}

			// Warn if the resolver relies on an unconditional 'parameter' source
			// with no 'default' and has no other unconditional source guaranteed
			// to produce a value. Such a resolver passes lint but fails at runtime
			// with "parameter not provided" when the CLI parameter is absent.
			if len(res.Resolve.With) > 0 {
				unconditionalParamWithoutDefault := false
				hasGuaranteedFallback := false
				for _, step := range res.Resolve.With {
					if !isUnconditionalSource(step) {
						continue
					}
					if step.Provider == parameterProviderName {
						if parameterSourceHasDefault(step) {
							hasGuaranteedFallback = true
						} else {
							unconditionalParamWithoutDefault = true
						}
					} else {
						hasGuaranteedFallback = true
					}
				}
				if unconditionalParamWithoutDefault && !hasGuaranteedFallback {
					result.addFinding(SeverityWarning, "structure", location+".resolve",
						"resolver depends on a 'parameter' source with no 'default' and no unconditional fallback; it will fail at runtime if the parameter is not supplied",
						"Add a 'default' to the parameter source, or add an unconditional fallback source (e.g. a static provider)",
						"parameter-missing-default")
				}
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

				// Warn if the provider does not declare validation capability.
				if registry != nil && step.Provider != "" {
					if p, exists := registry.Get(step.Provider); exists {
						desc := p.Descriptor()
						hasValidation := false
						for _, cap := range desc.Capabilities {
							if cap == provider.CapabilityValidation {
								hasValidation = true
								break
							}
						}
						if !hasValidation {
							result.addFinding(SeverityWarning, "provider", stepLocation,
								fmt.Sprintf("provider '%s' does not declare validation capability", step.Provider),
								"Use the 'validation' provider or another provider with validation capability",
								"non-validation-provider")
						}
					}
				}
			}
		}

		// Check for self-references in transform/validate phases.
		// Using _.resolverName in these phases creates a circular dependency;
		// the correct idiom is __self.
		lintResolverSelfReferences(name, res, location, result)

		// Check for redundant dependsOn entries that are already inferred.
		lintRedundantDependsOn(res, location, result, registry)

		// Check for transform steps accessing provider-specific fields when
		// the resolve chain includes a fallback with a different output shape.
		lintTransformShapeMismatch(name, res, location, result, registry)
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

// lintRedundantDependsOn checks if a resolver's explicit dependsOn entries are
// already covered by auto-inferred dependencies from value references.
func lintRedundantDependsOn(res *resolver.Resolver, location string, result *Result, registry *provider.Registry) {
	if len(res.DependsOn) == 0 {
		return
	}

	var lookup resolver.DescriptorLookup
	if registry != nil {
		lookup = registry.DescriptorLookup()
	}

	inferred := resolver.ExtractInferredDependencies(res, lookup)
	inferredSet := make(map[string]bool, len(inferred))
	for _, dep := range inferred {
		inferredSet[dep] = true
	}

	var redundant []string
	for _, dep := range res.DependsOn {
		if inferredSet[dep] {
			redundant = append(redundant, dep)
		}
	}

	if len(redundant) == 0 {
		return
	}

	if len(redundant) == len(res.DependsOn) {
		result.addFinding(SeverityInfo, "dependency", location+".dependsOn",
			"dependsOn is redundant — all listed dependencies are already inferred from value references",
			"Remove the dependsOn field; dependencies are auto-inferred from expr:, rslvr:, and tmpl: references",
			"redundant-depends-on")
	} else {
		result.addFinding(SeverityInfo, "dependency", location+".dependsOn",
			fmt.Sprintf("dependsOn contains redundant entries already inferred from value references: %s", strings.Join(redundant, ", ")),
			"Remove the redundant entries; only keep dependsOn for ordering dependencies not referenced by value",
			"redundant-depends-on")
	}
}

// lintTransformShapeMismatch detects resolvers where the transform phase accesses
// provider-specific fields (like __self.body) but the resolve chain includes a
// fallback with a different output shape (e.g., static returning a scalar/array).
func lintTransformShapeMismatch(_ string, res *resolver.Resolver, location string, result *Result, registry *provider.Registry) {
	if res.Resolve == nil || res.Transform == nil {
		return
	}
	// A non-trivial phase-level when guard already protects every transform step,
	// so the shape access is safe — don't flag.
	if isNonTrivialGuard(res.Transform.When) {
		return
	}
	if len(res.Resolve.With) < 2 || len(res.Transform.With) == 0 {
		return
	}

	// Determine which resolve steps produce structured output (map with known fields)
	// versus scalar/array output.
	type sourceShape struct {
		isStructured bool
		fields       map[string]bool // known output fields (e.g., "body", "statusCode", "headers")
	}

	shapes := make([]sourceShape, len(res.Resolve.With))
	hasStructured := false
	hasNonStructured := false

	for i, step := range res.Resolve.With {
		if step.Provider == "static" || step.Provider == "" {
			// Check if the static value is a map — if so, treat it as structured.
			if valRef, ok := step.Inputs["value"]; ok && valRef != nil {
				if m, isMap := valRef.Literal.(map[string]any); isMap && len(m) > 0 {
					fields := make(map[string]bool, len(m))
					for k := range m {
						fields[k] = true
					}
					shapes[i] = sourceShape{isStructured: true, fields: fields}
					hasStructured = true
					continue
				}
			}
			shapes[i] = sourceShape{isStructured: false}
			hasNonStructured = true
			continue
		}

		// Check the provider's output schema for CapabilityFrom (resolve phase).
		if registry != nil {
			if p, exists := registry.Get(step.Provider); exists {
				desc := p.Descriptor()
				if schema, ok := desc.OutputSchemas[provider.CapabilityFrom]; ok && schema != nil && len(schema.Properties) > 0 {
					fields := make(map[string]bool, len(schema.Properties))
					for fieldName := range schema.Properties {
						fields[fieldName] = true
					}
					shapes[i] = sourceShape{isStructured: true, fields: fields}
					hasStructured = true
					continue
				}
			}
		}

		// If we can't determine the shape, assume it might be structured
		// but don't flag — we only flag when we're confident about a mismatch.
		shapes[i] = sourceShape{isStructured: true}
		hasStructured = true
	}

	// Only proceed if the resolve chain has both structured and non-structured sources.
	if !hasStructured || !hasNonStructured {
		return
	}

	// Collect all known fields from structured providers.
	structuredFields := make(map[string]bool)
	for _, s := range shapes {
		if s.isStructured {
			for f := range s.fields {
				structuredFields[f] = true
			}
		}
	}

	// If we couldn't determine specific fields, fall back to well-known HTTP fields.
	if len(structuredFields) == 0 {
		structuredFields = map[string]bool{
			"body":       true,
			"statusCode": true,
			"headers":    true,
		}
	}

	// Scan transform steps for __self.<field> access without a when guard.
	for i, step := range res.Transform.With {
		stepLocation := fmt.Sprintf("%s.transform.with[%d]", location, i)

		// Skip if the transform step has a when guard.
		if isNonTrivialGuard(step.When) {
			continue
		}

		// Scan all inputs for __self.<field> references.
		for _, val := range step.Inputs {
			if val == nil {
				continue
			}

			var exprText string
			if val.Expr != nil {
				exprText = string(*val.Expr)
			} else if val.Tmpl != nil {
				exprText = string(*val.Tmpl)
			} else if s, ok := val.Literal.(string); ok {
				exprText = s
			}
			if exprText == "" {
				continue
			}

			for field := range structuredFields {
				pattern := "__self." + field
				if strings.Contains(exprText, pattern) {
					result.addFinding(SeverityWarning, "provider", stepLocation,
						fmt.Sprintf("transform accesses '__self.%s' but the resolve chain includes a fallback that produces a different shape — add a 'when' condition to guard the transform", field),
						fmt.Sprintf("Add a when guard, e.g.: when: { expr: \"type(__self) == map_type && has(__self.%s)\" }", field),
						"transform-shape-mismatch")
					return // One finding per resolver is sufficient.
				}
			}
		}
	}
}

// isNonTrivialGuard reports whether a when condition is a meaningful guard, i.e.
// it has a non-empty CEL expression that is not the literal "true". Such a guard
// is assumed to protect downstream shape-specific access.
func isNonTrivialGuard(c *resolver.Condition) bool {
	if c == nil || c.Expr == nil {
		return false
	}
	expr := strings.TrimSpace(string(*c.Expr))
	return expr != "" && expr != "true"
}

// lintResolverCycles checks for circular dependencies in the resolver dependency graph.
// When a cycle involves a resolver with a validate block, the suggestion specifically
// recommends extracting the validation into a separate resolver.
func lintResolverCycles(sol *solution.Solution, result *Result, registry *provider.Registry) {
	if sol.Spec.Resolvers == nil {
		return
	}

	var lookup resolver.DescriptorLookup
	if registry != nil {
		lookup = registry.DescriptorLookup()
	}

	// Build the dependency map for all resolvers.
	deps := make(map[string][]string, len(sol.Spec.Resolvers))
	for name, res := range sol.Spec.Resolvers {
		if res == nil {
			continue
		}
		deps[name] = resolver.ExtractDependencies(res, lookup)
	}

	// Detect cycles using DFS-based cycle detection.
	cycles := findResolverCycles(deps)
	for _, cycle := range cycles {
		// Check if any resolver in the cycle has a validate block.
		hasValidate := false
		for _, name := range cycle {
			if res, ok := sol.Spec.Resolvers[name]; ok && res != nil && res.Validate != nil && len(res.Validate.With) > 0 {
				hasValidate = true
				break
			}
		}

		location := fmt.Sprintf("resolvers.%s", cycle[0])
		cycleStr := strings.Join(cycle, " → ")

		suggestion := "Break the cycle by reordering dependencies or removing unnecessary references"
		if hasValidate {
			suggestion = "A validate block is part of this cycle. Extract the validation into a separate resolver that depends on all required values, breaking the cycle"
		}

		result.addFinding(SeverityError, "dependency", location,
			fmt.Sprintf("circular dependency detected: %s", cycleStr),
			suggestion,
			"resolver-cycle")
	}
}

// findResolverCycles detects all unique cycles in a dependency graph using DFS.
// Returns a list of cycles, where each cycle is a list of resolver names
// ending with the name that closes the cycle.
func findResolverCycles(deps map[string][]string) [][]string {
	const (
		white = 0 // not visited
		gray  = 1 // in current path
		black = 2 // fully processed
	)

	color := make(map[string]int, len(deps))
	path := make([]string, 0)
	var cycles [][]string
	reported := make(map[string]bool) // deduplicate cycles by canonical signature

	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		path = append(path, node)

		for _, dep := range deps[node] {
			if color[dep] == gray {
				// Found a cycle: extract it from the path.
				cycleStart := -1
				for i, n := range path {
					if n == dep {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cycle := make([]string, len(path)-cycleStart+1)
					copy(cycle, path[cycleStart:])
					cycle[len(cycle)-1] = dep // close the cycle

					// Deduplicate using a canonical signature derived from the
					// whole cycle path so distinct cycles that share the same
					// smallest node (e.g. a->b->a and a->c->a) are both reported.
					key := canonicalCycleKey(cycle)
					if !reported[key] {
						reported[key] = true
						cycles = append(cycles, cycle)
					}
				}
			} else if color[dep] == white {
				dfs(dep)
			}
		}

		path = path[:len(path)-1]
		color[node] = black
	}

	for node := range deps {
		if color[node] == white {
			dfs(node)
		}
	}

	return cycles
}

// canonicalCycleKey builds a rotation-invariant signature for a cycle so that
// the same directed cycle discovered from different start nodes deduplicates,
// while genuinely distinct cycles (even those sharing their smallest node)
// produce different keys. The input cycle is expected to end with the node that
// closes it (i.e. cycle[len-1] == cycle[0]); that closing duplicate is dropped
// before rotating the remaining nodes to start at the lexicographically
// smallest member.
func canonicalCycleKey(cycle []string) string {
	nodes := cycle
	if len(nodes) > 1 && nodes[0] == nodes[len(nodes)-1] {
		nodes = nodes[:len(nodes)-1]
	}
	if len(nodes) == 0 {
		return ""
	}

	start := 0
	for i := 1; i < len(nodes); i++ {
		if nodes[i] < nodes[start] {
			start = i
		}
	}

	rotated := make([]string, 0, len(nodes))
	for i := 0; i < len(nodes); i++ {
		rotated = append(rotated, nodes[(start+i)%len(nodes)])
	}
	return strings.Join(rotated, "\x00")
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

		if act.Explicit {
			result.addFinding(SeverityWarning, "workflow", location,
				"explicit: true has no effect on finally actions",
				"Remove explicit: true or move the action to workflow.actions",
				"explicit-on-finally")
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

// registryWithBundlePlugins creates a shallow clone of the registry and marks
// provider names declared in sol.Bundle.Plugins as known so the
// missing-provider lint rule does not fire for plugin-managed providers.
// The clone ensures the shared singleton registry is not mutated.
func registryWithBundlePlugins(registry *provider.Registry, sol *solution.Solution) *provider.Registry {
	clone := registry.ShallowClone()
	for _, p := range sol.Bundle.Plugins {
		if p.Kind == solution.PluginKindProvider && p.Name != "" {
			clone.MarkKnown(p.Name)
		}
	}
	// Also mark all official providers as known - auto-resolution fetches
	// them at runtime without requiring explicit bundle.plugins declarations.
	for _, entry := range official.DefaultProviders() {
		clone.MarkKnown(entry.Name)
	}
	return clone
}

// registryAdapter adapts provider.Registry to action.RegistryInterface
type registryAdapter struct {
	registry *provider.Registry
}

func (r *registryAdapter) Get(name string) (provider.Provider, bool) {
	p, ok := r.registry.Get(name)
	if ok {
		return p, true
	}
	// For providers known only by name (bundle.plugins), report as found
	// so workflow validation doesn't emit a duplicate missing-provider error.
	if r.registry.Has(name) {
		return nil, true
	}
	return nil, false
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
		if !registry.Has(act.Provider) {
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

	if act.Fingerprint != nil {
		if len(act.Sources) == 0 {
			result.addFinding(SeverityWarning, "workflow", location,
				"fingerprint block has no effect without sources",
				"Add sources patterns or remove the fingerprint block",
				"fingerprint-without-sources")
		}
		if !act.Fingerprint.Scope.IsValid() {
			result.addFinding(SeverityError, "workflow", location+".fingerprint.scope",
				fmt.Sprintf("unknown fingerprint scope %q", act.Fingerprint.Scope),
				"Use 'all' (check files and inputs) or 'files' (check files only)",
				"invalid-fingerprint-scope")
		}
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
			ast, err := parseCELForLint(string(*val.Expr))
			if err != nil {
				result.addFinding(SeverityError, "expression", inputLoc,
					fmt.Sprintf("invalid CEL expression: %v", err),
					"Fix the expression syntax",
					"invalid-expression")
			} else {
				lintOrValueOnConcrete(ast, inputLoc, result)
			}
		}

		if val.Tmpl != nil && string(*val.Tmpl) != "" {
			tmplStr := string(*val.Tmpl)
			if err := validateTemplateSyntax(tmplStr); err != nil {
				result.addFinding(SeverityError, "template", inputLoc,
					fmt.Sprintf("invalid Go template: %v", err),
					"Fix the template syntax",
					"invalid-template")
			}

			// Check for _.resolverName pattern in Go templates — now valid at
			// runtime, but worth an informational note since direct access is
			// shorter and the alias only exists for CEL/template parity.
			lintTemplateUnderscorePrefix(tmplStr, inputLoc, result)
		}
	}
}

// lintTemplateUnderscorePrefix emits an informational finding when a Go
// template uses {{ ._.resolverName }}. The underscore alias is supported at
// runtime, but direct access ({{ .resolverName }}) is shorter and preferred.
func lintTemplateUnderscorePrefix(tmpl, location string, result *Result) {
	refs, err := gotmpl.GetGoTemplateReferences(tmpl, "", "")
	if err != nil {
		return // Template parse errors are caught by validateTemplateSyntax
	}
	seen := make(map[string]bool)
	for _, ref := range refs {
		// Check for paths starting with "._.something" — the underscore-root pattern.
		// Skip paths starting with ".__" (e.g., .__self, .__actions) which are legitimate.
		if !strings.HasPrefix(ref.Path, "._") || strings.HasPrefix(ref.Path, ".__") {
			continue
		}
		// Extract the resolver name: "._.config.host" → "config"
		parts := strings.SplitN(strings.TrimPrefix(ref.Path, "._"), ".", 3)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		name := parts[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		result.addFinding(SeverityInfo, "template", location,
			fmt.Sprintf("Go template uses '{{ ._.%s }}' — consider using '{{ .%s }}' for brevity (both work; the '._' alias exists for CEL/template parity)", name, name),
			fmt.Sprintf("Replace '._.%s' with '.%s' in the template for shorter syntax", name, name),
			"tmpl-underscore-prefix")
	}
}

// celLintEnv holds the shared CEL environment used for all lint-time parsing.
// It enables cel.OptionalTypes() so optional access syntax (_.?name, _[?"name"])
// parses successfully, matching the env used by the reference collector and
// runtime evaluation in pkg/celexp. The env is immutable and safe for
// concurrent use, so it is built once and reused across every expression
// instead of being rebuilt per call.
var (
	celLintEnv     *cel.Env
	celLintEnvErr  error
	celLintEnvOnce sync.Once
)

// lintCELEnv returns the shared lint CEL environment, building it on first use.
func lintCELEnv() (*cel.Env, error) {
	celLintEnvOnce.Do(func() {
		celLintEnv, celLintEnvErr = cel.NewEnv(cel.OptionalTypes())
	})
	return celLintEnv, celLintEnvErr
}

// parseCELForLint parses expr with the shared lint env and returns the parsed
// AST. This is the single parse used for both syntax validation and the
// orValue-on-concrete check, so callers must not re-parse the same expression.
func parseCELForLint(expr string) (*cel.Ast, error) {
	env, err := lintCELEnv()
	if err != nil {
		return nil, err
	}
	ast, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return ast, nil
}

// validateCELSyntax checks if a CEL expression is syntactically valid.
// It delegates to parseCELForLint and discards the AST, for callers that only
// need a syntax check (e.g. when conditions) without the orValue analysis.
func validateCELSyntax(expr string) error {
	_, err := parseCELForLint(expr)
	return err
}

// lintOrValueOnConcrete flags CEL expressions that call .orValue(...) on a
// provably-concrete (non-optional) receiver. In CEL, orValue() only has an
// overload on optional types, so calling it on a concrete value (e.g.
// __self.orValue([]) or _.field.orValue("")) errors at runtime. The check is
// deliberately conservative: it only flags receivers it can prove are concrete
// (plain identifiers, plain field-access chains, and literals). Anything that
// could produce an optional -- optional access (_.?name, _[?"name"]),
// optional.of/none/ofNonZeroValue, or any function-call result -- is left
// alone, so every finding is a guaranteed runtime failure with no false
// positives.
//
// It accepts the already-parsed AST (produced by parseCELForLint) so the
// expression is parsed exactly once per lint run rather than re-parsed here.
func lintOrValueOnConcrete(ast *cel.Ast, location string, result *Result) {
	parsedExpr, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return
	}

	seen := make(map[string]struct{})
	for _, receiver := range findConcreteOrValueReceivers(parsedExpr.GetExpr()) {
		if _, dup := seen[receiver]; dup {
			continue
		}
		seen[receiver] = struct{}{}
		result.addFinding(SeverityError, "expression", location,
			fmt.Sprintf("'.orValue(...)' is called on non-optional value '%s'; orValue() only has an overload on optional types and will error at runtime", receiver),
			orValueSuggestion(receiver),
			"orvalue-on-non-optional")
	}
}

// orValueSuggestion builds remediation guidance for an orValue()-on-concrete
// finding. When the receiver is a plain "_."-rooted field access, it suggests
// the equivalent optional-access form derived from that field. For any other
// receiver (e.g. "__self", "__parent", or a literal) the "_.?" form cannot be
// mechanically derived, so it falls back to generic guidance instead of
// emitting an invalid example like "_.?__self".
func orValueSuggestion(receiver string) string {
	if field := strings.TrimPrefix(receiver, "_."); field != receiver && field != "" {
		return fmt.Sprintf("Use optional access so the receiver is optional (e.g. '_.?%s'), or drop '.orValue(...)' and provide a fallback another way", field)
	}
	return "Use optional access so the receiver is optional (e.g. '_.?name' or '_[?\"name\"]'), or drop '.orValue(...)' and provide a fallback another way"
}

// findConcreteOrValueReceivers walks a parsed CEL expression and returns a
// rendered description of every receiver on which orValue() is called when that
// receiver is provably concrete.
func findConcreteOrValueReceivers(expr *exprpb.Expr) []string {
	if expr == nil {
		return nil
	}

	var offenders []string
	switch expr.GetExprKind().(type) {
	case *exprpb.Expr_CallExpr:
		call := expr.GetCallExpr()
		if call.GetFunction() == "orValue" && call.GetTarget() != nil && isProvablyConcrete(call.GetTarget()) {
			offenders = append(offenders, renderConcrete(call.GetTarget()))
		}
		// Recurse into target and args to catch nested expressions.
		if call.GetTarget() != nil {
			offenders = append(offenders, findConcreteOrValueReceivers(call.GetTarget())...)
		}
		for _, arg := range call.GetArgs() {
			offenders = append(offenders, findConcreteOrValueReceivers(arg)...)
		}
	case *exprpb.Expr_SelectExpr:
		offenders = append(offenders, findConcreteOrValueReceivers(expr.GetSelectExpr().GetOperand())...)
	case *exprpb.Expr_ListExpr:
		for _, el := range expr.GetListExpr().GetElements() {
			offenders = append(offenders, findConcreteOrValueReceivers(el)...)
		}
	case *exprpb.Expr_StructExpr:
		for _, entry := range expr.GetStructExpr().GetEntries() {
			offenders = append(offenders, findConcreteOrValueReceivers(entry.GetValue())...)
		}
	case *exprpb.Expr_ComprehensionExpr:
		comp := expr.GetComprehensionExpr()
		offenders = append(offenders, findConcreteOrValueReceivers(comp.GetIterRange())...)
		offenders = append(offenders, findConcreteOrValueReceivers(comp.GetAccuInit())...)
		offenders = append(offenders, findConcreteOrValueReceivers(comp.GetLoopCondition())...)
		offenders = append(offenders, findConcreteOrValueReceivers(comp.GetLoopStep())...)
		offenders = append(offenders, findConcreteOrValueReceivers(comp.GetResult())...)
	}
	return offenders
}

// isProvablyConcrete reports whether an expression is guaranteed to be a
// non-optional (concrete) value. Only literals, plain identifiers, and plain
// field-access chains rooted at an identifier or literal qualify. Any call
// (including optional access operators like _?._ and _[?_], optional.of, and a
// prior orValue) is treated as not provably concrete so the check never flags
// a value that might legitimately be optional.
func isProvablyConcrete(expr *exprpb.Expr) bool {
	switch expr.GetExprKind().(type) {
	case *exprpb.Expr_ConstExpr:
		return true
	case *exprpb.Expr_IdentExpr:
		return true
	case *exprpb.Expr_SelectExpr:
		// A regular field access is concrete only when its operand is concrete.
		// (Optional select a.?b is parsed as a CallExpr, not a SelectExpr.)
		return isProvablyConcrete(expr.GetSelectExpr().GetOperand())
	default:
		return false
	}
}

// renderConcrete renders a provably-concrete expression back to a readable
// source-like string for diagnostics (e.g. "_.config.host", "__self").
func renderConcrete(expr *exprpb.Expr) string {
	switch expr.GetExprKind().(type) {
	case *exprpb.Expr_IdentExpr:
		return expr.GetIdentExpr().GetName()
	case *exprpb.Expr_SelectExpr:
		sel := expr.GetSelectExpr()
		return renderConcrete(sel.GetOperand()) + "." + sel.GetField()
	case *exprpb.Expr_ConstExpr:
		return "<literal>"
	default:
		return "<expr>"
	}
}

// hyphensToCamelCase converts a hyphenated name to camelCase.
// e.g., "my-resolver-name" -> "myResolverName"
func hyphensToCamelCase(name string) string {
	parts := strings.Split(name, "-")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// validateTemplateSyntax checks if a Go template is syntactically valid.
// Delegates to gotmpl.ValidateSyntax so that sprig and custom extension
// functions are registered during parsing, matching runtime behavior.
func validateTemplateSyntax(tmpl string) error {
	return gotmpl.ValidateSyntax(tmpl, "", "")
}

func collectReferencedResolvers(sol *solution.Solution) map[string]bool {
	refs := make(map[string]bool)

	// Walk every ValueRef and condition in the solution, extracting resolver
	// references via the same AST-based logic used to build the dependency graph.
	// This recognizes plain (_.name), bracket (_["name"]), and optional
	// (_.?name, _[?"name"]) CEL access, Go-template references, and nested
	// literals, and applies go-template data-scope exclusions -- keeping the
	// unused-resolver rule consistent with actual dependency resolution.
	_ = walk.Walk(sol, &walk.Visitor{
		ValueRef: func(_ string, vr *spec.ValueRef) error {
			resolver.ExtractRefsFromValueRef(vr, refs)
			return nil
		},
		Condition: func(_, _ string, expr *celexp.Expression) error {
			resolver.ExtractRefsFromValueRef(&spec.ValueRef{Expr: expr}, refs)
			return nil
		},
	})

	// A resolver named in another resolver's dependsOn array is used, even when
	// it is not referenced by any expression or template. The walker does not
	// visit dependsOn, so collect those references explicitly.
	if sol.Spec.Resolvers != nil {
		for _, r := range sol.Spec.Resolvers {
			if r == nil {
				continue
			}
			for _, dep := range r.DependsOn {
				if dep != "" {
					refs[dep] = true
				}
			}
		}
	}

	// State configuration (enabled, backend inputs, saveOverrides) can reference
	// resolvers. The walker does not traverse state, so scan it explicitly.
	if sol.State != nil {
		resolver.ExtractRefsFromValueRef(sol.State.Enabled, refs)
		for _, vr := range sol.State.Backend.Inputs {
			resolver.ExtractRefsFromValueRef(vr, refs)
		}
		for _, vr := range sol.State.Backend.SaveOverrides {
			resolver.ExtractRefsFromValueRef(vr, refs)
		}
	}

	return refs
}

// collectTemplateFileResolverRefs scans external template files on disk
// for resolver references. It discovers template files via:
//  1. bundle.include glob patterns
//  2. directory provider 'path' inputs from resolvers
//
// Files are treated as go-template sources by role -- a directory provider
// renders every file it reads and bundle.include packages files for rendering --
// so discovery does not filter by extension. Non-template files are skipped
// naturally when ref extraction fails to parse them.
//
// Returns a set of resolver names referenced in external template files.
func collectTemplateFileResolverRefs(sol *solution.Solution, solutionDir string) map[string]bool {
	refs := make(map[string]bool)
	if solutionDir == "" {
		return refs
	}

	// Collect template file paths from bundle.include globs and directory provider inputs.
	templateFiles := discoverTemplateFiles(sol, solutionDir)

	for _, absPath := range templateFiles {
		fileRefs, err := resolverRefs.ExtractFromTemplateFile(absPath, "", "")
		if err != nil {
			continue // Skip files that can't be parsed (non-template files, syntax errors)
		}
		for _, ref := range fileRefs {
			refs[ref] = true
		}
	}

	return refs
}

// discoverTemplateFiles finds template files on disk from bundle.include patterns
// and directory provider path inputs. Returns absolute paths.
//
// Discovery is role-based: files reached via bundle.include or a directory
// provider are go-template sources regardless of extension (terraform .tf,
// Kubernetes .yaml, etc. are routinely rendered as go-templates), so no
// extension filter is applied. Files that are not valid templates are skipped
// later when ref extraction fails to parse them.
func discoverTemplateFiles(sol *solution.Solution, solutionDir string) []string {
	seen := make(map[string]bool)
	var files []string

	addFile := func(absPath string) {
		if seen[absPath] {
			return
		}
		seen[absPath] = true
		files = append(files, absPath)
	}

	// 1. Scan bundle.include patterns. Every matched file is a candidate
	// go-template source; the include globs themselves scope what is considered.
	for _, pattern := range sol.Bundle.Include {
		absPattern := filepath.Join(solutionDir, pattern)
		matches, err := doublestar.FilepathGlob(absPattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if info, statErr := os.Stat(match); statErr == nil && !info.IsDir() {
				addFile(match)
			}
		}
	}

	// 2. Scan directory provider path inputs for template directories.
	if sol.Spec.Resolvers != nil {
		for _, res := range sol.Spec.Resolvers {
			if res == nil || res.Resolve == nil {
				continue
			}
			for _, step := range res.Resolve.With {
				if step.Provider != "directory" {
					continue
				}
				pathVal, ok := step.Inputs["path"]
				if !ok || pathVal == nil {
					continue
				}
				// Only handle literal string paths (not expressions/templates).
				pathStr, ok := pathVal.Literal.(string)
				if !ok || pathStr == "" {
					continue
				}

				absPath := pathStr
				if !filepath.IsAbs(absPath) {
					absPath = filepath.Join(solutionDir, absPath)
				}

				// Walk the directory for template files, honoring the directory
				// provider's literal recursive/filterGlob inputs.
				walkDirectoryProviderTemplates(step, absPath, addFile)
			}
		}
	}

	return files
}

// directoryWalkOptions holds the subset of directory-provider inputs that the
// lint heuristics can honor statically so discovery better matches the files
// the provider would actually return at runtime. Non-literal (expr/tmpl) inputs
// cannot be evaluated statically and fall back to permissive defaults.
type directoryWalkOptions struct {
	recursive  bool
	filterGlob string
}

// readDirectoryWalkOptions extracts literal recursive and filterGlob inputs from
// a directory-provider step. recursive defaults to false to match the directory
// provider's runtime default (it does not descend into subdirectories unless
// recursive: true is set); filterGlob defaults to empty (no additional
// filtering).
func readDirectoryWalkOptions(step resolver.ProviderSource) directoryWalkOptions {
	opts := directoryWalkOptions{recursive: false}
	if v, ok := step.Inputs["recursive"]; ok && v != nil {
		if b, ok := v.Literal.(bool); ok {
			opts.recursive = b
		}
	}
	if v, ok := step.Inputs["filterGlob"]; ok && v != nil {
		if s, ok := v.Literal.(string); ok && s != "" {
			opts.filterGlob = s
		}
	}
	return opts
}

// walkDirectoryProviderTemplates walks absPath collecting the files the
// directory provider would render, honoring its literal recursive and
// filterGlob inputs. When recursive is false, nested directories are skipped.
// When filterGlob is set, files whose entry name (basename) does not match the
// glob are skipped -- matching the directory provider, which filters on entry
// names rather than the full relative path; an invalid glob is ignored so
// discovery stays best-effort. No extension filter is applied: every rendered
// file is a go-template source by role.
func walkDirectoryProviderTemplates(step resolver.ProviderSource, absPath string, addFile func(string)) {
	opts := readDirectoryWalkOptions(step)
	_ = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error { //nolint:errcheck // best-effort scan
		if err != nil {
			return err
		}
		if info.IsDir() {
			// When not recursive, process the root directory but do not descend.
			if !opts.recursive && path != absPath {
				return filepath.SkipDir
			}
			return nil
		}
		if opts.filterGlob != "" {
			if matched, matchErr := doublestar.Match(opts.filterGlob, info.Name()); matchErr == nil && !matched {
				return nil
			}
		}
		addFile(path)
		return nil
	})
}

// lintTemplateFileDependencies checks that resolvers using the go-template
// provider with render-tree operation have all resolver names referenced in
// their source template files listed in dependsOn (or reachable via the DAG).
func lintTemplateFileDependencies(sol *solution.Solution, solutionDir string, result *Result, registry *provider.Registry) {
	if sol.Spec.Resolvers == nil || solutionDir == "" {
		return
	}

	var lookup resolver.DescriptorLookup
	if registry != nil {
		lookup = registry.DescriptorLookup()
	}

	for name, res := range sol.Spec.Resolvers {
		if res == nil || res.Resolve == nil {
			continue
		}

		for _, step := range res.Resolve.With {
			if step.Provider != "go-template" {
				continue
			}

			// Check if this is a render-tree operation.
			opVal, ok := step.Inputs["operation"]
			if !ok || opVal == nil {
				continue
			}
			opStr, ok := opVal.Literal.(string)
			if !ok || opStr != "render-tree" {
				continue
			}

			// Find the source resolver for entries (usually a directory provider).
			sourceResolverName := findEntriesSourceResolver(step, sol)
			if sourceResolverName == "" {
				continue
			}

			// Discover template files from the source resolver's directory provider.
			templateFiles := discoverTemplateFilesFromResolver(sol, sourceResolverName, solutionDir)
			if len(templateFiles) == 0 {
				continue
			}

			// Honor any custom delimiters configured on the render-tree step so
			// template parsing matches go-template runtime behavior.
			leftDelim := stringInput(step, "leftDelim")
			rightDelim := stringInput(step, "rightDelim")

			// Top-level keys provided by a literal "data" map are local template
			// context, not resolver references, and must not require dependsOn.
			dataKeys := literalDataKeys(step)

			// Collect all resolver names referenced in template files.
			templateRefs := make(map[string]bool)
			for _, f := range templateFiles {
				fileRefs, err := resolverRefs.ExtractFromTemplateFile(f, leftDelim, rightDelim)
				if err != nil {
					continue
				}
				for _, ref := range fileRefs {
					templateRefs[ref] = true
				}
			}

			// Build the set of resolvers reachable from this resolver's dependency graph.
			reachable := collectReachableDependencies(name, sol, lookup)

			// Check for references not covered by dependsOn or inferred deps.
			location := fmt.Sprintf("resolvers.%s", name)
			for ref := range templateRefs {
				// Skip self-reference and reserved names.
				if ref == name || ref == "_" || strings.HasPrefix(ref, "__") {
					continue
				}
				// Skip refs satisfied by a literal data map key, not a resolver.
				if dataKeys[ref] {
					continue
				}
				// Skip if the referenced resolver doesn't exist (could be a template variable).
				if _, exists := sol.Spec.Resolvers[ref]; !exists {
					continue
				}
				if !reachable[ref] {
					result.addFinding(SeverityWarning, "dependency", location,
						fmt.Sprintf("render-tree resolver '%s' uses template files that reference resolver '%s', but '%s' is not in its dependency graph",
							name, ref, ref),
						fmt.Sprintf("Add '%s' to the dependsOn list of resolver '%s'", ref, name),
						"missing-template-dependency")
				}
			}
		}
	}
}

// stringInput returns the literal string value of the named step input, or an
// empty string if the input is absent or not a string literal.
func stringInput(step resolver.ProviderSource, key string) string {
	val, ok := step.Inputs[key]
	if !ok || val == nil {
		return ""
	}
	s, ok := val.Literal.(string)
	if !ok {
		return ""
	}
	return s
}

// literalDataKeys returns the set of top-level keys from a render-tree step's
// literal "data" map. These keys are local template context and must not be
// treated as resolver references when checking template dependencies.
func literalDataKeys(step resolver.ProviderSource) map[string]bool {
	val, ok := step.Inputs["data"]
	if !ok || val == nil || val.Literal == nil {
		return nil
	}
	dataMap, ok := val.Literal.(map[string]any)
	if !ok {
		return nil
	}
	keys := make(map[string]bool, len(dataMap))
	for k := range dataMap {
		keys[k] = true
	}
	return keys
}

// findEntriesSourceResolver extracts the resolver name from a render-tree step's
// entries input. Returns the resolver name if it's a direct rslvr: reference,
// or tries to extract it from an expr: reference.
func findEntriesSourceResolver(step resolver.ProviderSource, _ *solution.Solution) string {
	entriesVal, ok := step.Inputs["entries"]
	if !ok || entriesVal == nil {
		return ""
	}
	if entriesVal.Resolver != nil {
		return *entriesVal.Resolver
	}
	// Check expr for _.resolverName.entries or _["resolver-name"].entries pattern.
	if entriesVal.Expr != nil {
		expr := strings.TrimSpace(string(*entriesVal.Expr))
		// Bracket notation: _["resolver-name"].entries or _['resolver-name'].entries
		if strings.HasPrefix(expr, "_[") {
			rest := strings.TrimPrefix(expr, "_[")
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				quote := rest[0]
				rest = rest[1:]
				if idx := strings.IndexByte(rest, quote); idx > 0 {
					return rest[:idx]
				}
			}
			return ""
		}
		// Dot notation: _.resolverName.entries
		if strings.HasPrefix(expr, "_.") {
			parts := strings.SplitN(strings.TrimPrefix(expr, "_."), ".", 2)
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}
	return ""
}

// discoverTemplateFilesFromResolver finds template files by looking at the
// directory provider configuration of the given source resolver.
func discoverTemplateFilesFromResolver(sol *solution.Solution, resolverName, solutionDir string) []string {
	res, ok := sol.Spec.Resolvers[resolverName]
	if !ok || res == nil || res.Resolve == nil {
		return nil
	}

	for _, step := range res.Resolve.With {
		if step.Provider != "directory" {
			continue
		}
		pathVal, ok := step.Inputs["path"]
		if !ok || pathVal == nil {
			continue
		}
		pathStr, ok := pathVal.Literal.(string)
		if !ok || pathStr == "" {
			continue
		}

		absPath := pathStr
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(solutionDir, absPath)
		}

		var files []string
		walkDirectoryProviderTemplates(step, absPath, func(path string) {
			files = append(files, path)
		})
		return files
	}

	return nil
}

// collectReachableDependencies returns all resolver names reachable from the
// given resolver's dependency graph (direct + transitive). It uses
// resolver.ExtractDependencies so that explicit dependsOn entries and
// dependencies inferred from expr:, rslvr:, and tmpl: references are all
// considered, avoiding false-positive missing-template-dependency warnings.
func collectReachableDependencies(name string, sol *solution.Solution, lookup resolver.DescriptorLookup) map[string]bool {
	reachable := make(map[string]bool)
	var visit func(string)
	visit = func(n string) {
		res, ok := sol.Spec.Resolvers[n]
		if !ok || res == nil {
			return
		}
		for _, dep := range resolver.ExtractDependencies(res, lookup) {
			if dep == "" || dep == n {
				continue
			}
			if !reachable[dep] {
				reachable[dep] = true
				visit(dep)
			}
		}
	}
	visit(name)
	return reachable
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
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "\\") {
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
					fmt.Sprintf("Check the provider's accepted inputs. Run: %s explain provider %s", paths.AppName(), providerName),
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

// lintState validates the solution's state configuration.
func lintState(sol *solution.Solution, result *Result, registry *provider.Registry) {
	if sol.State == nil {
		return
	}

	if !lintStateBackend(sol, result, registry) {
		return
	}
	lintStateResolverRefs(sol, result)
	lintStateSaveOverrides(sol, result)
	lintStateGitHubNoSaveBranch(sol, result)
}

// lintStateBackend validates the backend provider configuration.
// Returns false if further state linting should be skipped (e.g., backend is missing).
func lintStateBackend(sol *solution.Solution, result *Result, registry *provider.Registry) bool {
	location := "state"

	backendName := sol.State.Backend.Provider
	if backendName == "" {
		result.addFinding(SeverityError, "state", location+".backend.provider",
			"state backend provider is not specified",
			"Set backend.provider to a registered provider with CapabilityState (e.g., 'file')",
			"missing-state-backend")
		return false
	}

	prov, found := registry.Get(backendName)
	if !found {
		// If the provider is declared in bundle.plugins (or is an official
		// provider), it will be resolved at runtime. We cannot verify
		// capabilities at lint time, so skip the finding.
		if !registry.Has(backendName) {
			result.addFinding(SeverityError, "state", location+".backend.provider",
				fmt.Sprintf("state backend provider '%s' not found in registry", backendName),
				"Use a registered provider with CapabilityState such as 'file' or 'http'. External providers like 'github' require an installed plugin",
				"invalid-state-backend")
		}
	} else {
		desc := prov.Descriptor()
		hasState := false
		for _, cap := range desc.Capabilities {
			if cap == provider.CapabilityState {
				hasState = true
				break
			}
		}
		if !hasState {
			result.addFinding(SeverityError, "state", location+".backend.provider",
				fmt.Sprintf("provider '%s' does not have CapabilityState", backendName),
				"Use a provider that implements CapabilityState",
				"invalid-state-backend")
		}
	}

	lintNilInputs(sol.State.Backend.Inputs, location+".backend", result)
	return true
}

// lintStateResolverRefs checks for direct rslvr: references in state config.
// These won't work because state loads before resolvers run.
func lintStateResolverRefs(sol *solution.Solution, result *Result) {
	location := "state"

	if sol.State.Enabled != nil && sol.State.Enabled.Resolver != nil {
		result.addFinding(SeverityError, "state", location+".enabled",
			fmt.Sprintf("state.enabled uses rslvr: %q — resolver results are not available at state load time", *sol.State.Enabled.Resolver),
			"Use a literal value or CEL expression referencing CLI params instead (e.g. expr: \"__params.enable_state == true\")",
			"state-resolver-ref")
	}
	for inputKey, input := range sol.State.Backend.Inputs {
		if input != nil && input.Resolver != nil {
			result.addFinding(SeverityError, "state", fmt.Sprintf("%s.backend.inputs.%s", location, inputKey),
				fmt.Sprintf("state backend input %q uses rslvr: %q — resolver results are not available at state load time", inputKey, *input.Resolver),
				"Use a CEL expression referencing a CLI parameter instead (e.g. expr: \"__params.appName + '-state.json'\")",
				"state-resolver-ref")
		}
	}
}

// lintStateSaveOverrides validates saveOverrides fields.
// saveOverrides MAY contain rslvr: references (unlike inputs), but must NOT
// reference the state provider (circular dependency).
func lintStateSaveOverrides(sol *solution.Solution, result *Result) {
	for key, vr := range sol.State.Backend.SaveOverrides {
		location := fmt.Sprintf("state.backend.saveOverrides.%s", key)
		if vr == nil {
			result.addFinding(SeverityError, "provider", location,
				fmt.Sprintf("input '%s' has no value (dangling YAML key)", key),
				"Provide a value for the input or remove the key entirely",
				"nil-provider-input")
			continue
		}
		// Check for state provider references using ReferencedVariables
		if vr.ReferencesVariable("__state") {
			result.addFinding(SeverityError, "state", location,
				fmt.Sprintf("saveOverrides input %q references the state provider, creating a circular dependency", key),
				"Use a resolver reference (rslvr:) or CEL expression that does not depend on the state provider",
				"state-save-override-state-ref")
		}
	}
}

// lintStateGitHubNoSaveBranch fires an info hint when the state backend is
// github and neither inputs.branch nor saveOverrides.branch is configured.
func lintStateGitHubNoSaveBranch(sol *solution.Solution, result *Result) {
	if sol.State.Backend.Provider != "github" {
		return
	}

	hasBranch := false

	// Check inputs for branch
	if _, ok := sol.State.Backend.Inputs["branch"]; ok {
		hasBranch = true
	}

	// Check saveOverrides for branch
	if _, ok := sol.State.Backend.SaveOverrides["branch"]; ok {
		hasBranch = true
	}

	if !hasBranch {
		result.addFinding(SeverityInfo, "state", "state.backend",
			"GitHub state backend has no save branch configured",
			"For PR workflows, create a resolver for the branch name and reference it:\n  saveOverrides:\n    branch: { rslvr: <your-branch-resolver> }\nThis ensures state is saved to the same branch as your scaffolded files",
			"state-github-no-save-branch")
	}
}

// lintImmutableResolvers checks that resolvers with immutable: true have a
// state block configured on the solution. Without state, immutable values
// cannot be persisted or verified across runs.
func lintImmutableResolvers(sol *solution.Solution, result *Result) {
	for name, res := range sol.Spec.Resolvers {
		if res == nil || !res.Immutable {
			continue
		}
		if sol.State == nil {
			location := fmt.Sprintf("resolvers.%s", name)
			result.addFinding(SeverityError, "state", location,
				fmt.Sprintf("resolver %q has immutable: true but no state block is configured on the solution", name),
				"Add a state block with a backend provider to the solution so that the resolver value can be persisted.",
				"immutable-requires-state")
		}
	}
}
