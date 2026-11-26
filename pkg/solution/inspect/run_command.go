// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// ExplicitActionInfo describes an opt-in workflow action (explicit: true) and
// the CLI command to execute it selectively.
type ExplicitActionInfo struct {
	Name        string `json:"name" yaml:"name" doc:"Action name" maxLength:"150" example:"deploy" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with letter/underscore, followed by alphanumerics, underscores, or hyphens"`
	Alias       string `json:"alias,omitempty" yaml:"alias,omitempty" doc:"Short alias accepted in place of the action name" maxLength:"100" example:"d" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with letter/underscore, followed by alphanumerics, underscores, or hyphens"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of what the action does" maxLength:"512" example:"Deploy the application" pattern:"^[\\s\\S]*$" patternDescription:"Free-form action description"`
	Command     string `json:"command" yaml:"command" doc:"CLI command to run this opt-in action" maxLength:"2048" example:"scafctl run solution -f ./my-solution.yaml --action deploy" pattern:"^\\S.*$" patternDescription:"Non-empty CLI invocation command"`
}

// RunCommandInfo holds the structured result of analyzing a solution to
// determine how to run it. This is the domain representation used by
// CLI, MCP, and future API consumers.
type RunCommandInfo struct {
	// Command is the full CLI command string with all flags.
	Command string `json:"command" yaml:"command" doc:"Full CLI command to run the solution" maxLength:"2048" example:"scafctl run solution -f ./my-solution.yaml -r name=hello"`

	// Subcommand is the base command (e.g., "scafctl run solution").
	Subcommand string `json:"subcommand" yaml:"subcommand" doc:"Base CLI subcommand" maxLength:"128" example:"scafctl run solution"`

	// BaseCommand is the subcommand plus the resolved solution source
	// (e.g. "scafctl run solution -f ./my-solution.yaml"), without any example
	// parameter flags. Append parameter flags to this to build a runnable command
	// that targets the correct solution rather than relying on cwd auto-discovery.
	BaseCommand string `json:"baseCommand" yaml:"baseCommand" doc:"Subcommand plus the solution source (-f/name), without example params" maxLength:"1152" example:"scafctl run solution -f ./my-solution.yaml"`

	// CatalogCommand is the CLI command to run the solution by catalog name
	// once packaged or pulled into a catalog.
	CatalogCommand string `json:"catalogCommand,omitempty" yaml:"catalogCommand,omitempty" doc:"CLI command to run the solution by catalog name once packaged or pulled" maxLength:"2048" example:"scafctl run solution my-solution -r name=hello" pattern:"^\\S.*$" patternDescription:"Non-empty CLI invocation command"`

	// Explanation describes why this command variant was chosen.
	Explanation string `json:"explanation" yaml:"explanation" doc:"Why this command variant was chosen" maxLength:"512" example:"Solution has a workflow with actions"`

	// Parameters lists the parameter-type resolvers accepted by the solution.
	Parameters []ParamInfo `json:"parameters" yaml:"parameters" doc:"Parameter-type resolvers accepted by the solution" maxItems:"100"`

	// ExplicitActions lists opt-in workflow actions (explicit: true) and their
	// selective CLI commands.
	ExplicitActions []ExplicitActionInfo `json:"explicitActions,omitempty" yaml:"explicitActions,omitempty" doc:"Opt-in workflow actions (explicit: true) with selective --action commands" maxItems:"200"`

	// HasWorkflow indicates whether the solution has a workflow.
	HasWorkflow bool `json:"hasWorkflow" yaml:"hasWorkflow" doc:"Whether the solution has a workflow"`

	// HasResolvers indicates whether the solution has resolvers.
	HasResolvers bool `json:"hasResolvers" yaml:"hasResolvers" doc:"Whether the solution has resolvers"`

	// actionSource holds the pre-formatted source + selector flags for
	// 'run action <name>' commands (e.g. " -f ./sol.yaml" or
	// " -f app --catalog official --version '^1.0.0'").
	actionSource string
}

// RunCommandOption configures source selector flags in BuildRunCommand,
// BuildUsage, and BuildActionList.
type RunCommandOption func(*runCommandConfig)

type runCommandConfig struct {
	catalog    string
	version    string
	preRelease bool
}

// WithCatalog appends '--catalog <name>' to generated commands when the
// solution source is scoped to a specific catalog.
func WithCatalog(name string) RunCommandOption {
	return func(c *runCommandConfig) { c.catalog = name }
}

// WithVersionConstraint appends '--version <constraint>' to generated commands
// when the solution source is scoped by a semver constraint.
func WithVersionConstraint(constraint string) RunCommandOption {
	return func(c *runCommandConfig) { c.version = constraint }
}

// WithPreRelease appends '--pre-release' to generated commands for catalog and
// remote registry sources when pre-release resolution was enabled.
func WithPreRelease(enabled bool) RunCommandOption {
	return func(c *runCommandConfig) { c.preRelease = enabled }
}

// ParamInfo describes a parameter-type resolver that requires a user-provided value.
type ParamInfo struct {
	// Name is the CLI flag name used to supply this parameter (the parameter
	// provider's `inputs.key`, or the first of `inputs.keys`). This is what the
	// user passes via `-r <name>=value`. It may differ from the resolver's name.
	Name string `json:"name" yaml:"name" doc:"Parameter name (CLI flag key)" maxLength:"256" example:"projectName"`

	// ResolverName is the resolver's map-key name in spec.resolvers. CEL
	// when-clauses reference the resolver by this name (e.g. `_.<resolverName>`),
	// which is not necessarily the CLI key.
	ResolverName string `json:"resolverName,omitempty" yaml:"resolverName,omitempty" doc:"Resolver name (as referenced by _.<name> in expressions)" maxLength:"256"`

	Type        string `json:"type,omitempty" yaml:"type,omitempty" doc:"Parameter type" maxLength:"64" example:"string"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Parameter description" maxLength:"512" example:"Name of the project to create"`
	Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value"`

	// Default is the literal default value declared on the parameter provider
	// (its "default" input) or the first reachable unconditional fallback step
	// when that step carries a static literal, if present.
	Default any `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value when the parameter is not supplied"`

	// Required is true when the parameter has no default value and must be
	// supplied by the user.
	Required bool `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether the parameter must be supplied"`
}

// BuildRunCommand analyzes a solution and returns the exact CLI command to run it,
// including any parameter-type resolvers that need values passed via -r flags.
// The binaryName parameter is used in the generated command (e.g., "scafctl" or a
// custom name when scafctl is embedded as a library).
// Returns nil and an error message if the solution has nothing to run.
func BuildRunCommand(sol *solution.Solution, path, binaryName string, opts ...RunCommandOption) (*RunCommandInfo, error) {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	var cfg runCommandConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	hasResolvers := sol.Spec.HasResolvers()
	hasWorkflow := sol.Spec.HasWorkflow()

	var command, explanation string
	switch {
	case hasWorkflow:
		command = binaryName + " run solution"
		explanation = "Solution has a workflow with actions — use 'run solution'"
	case hasResolvers:
		command = binaryName + " run resolver"
		explanation = "Solution has resolvers but no workflow — use 'run resolver'"
	default:
		return nil, fmt.Errorf("solution has neither resolvers nor a workflow")
	}

	// Find parameter-type resolvers
	var parameters []ParamInfo
	if hasResolvers {
		names := make([]string, 0, len(sol.Spec.Resolvers))
		for name := range sol.Spec.Resolvers {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			rslvr := sol.Spec.Resolvers[name]
			if rslvr == nil || rslvr.Resolve == nil {
				continue
			}
			steps := trimLeadingDisabledSteps(rslvr.Resolve.With)
			if len(steps) == 0 {
				continue
			}
			if steps[0].Provider == "parameter" {
				firstStep := steps[0]
				if parameterStepIsAllMode(firstStep) {
					continue
				}
				inputs := firstStep.Inputs
				hasInputDefault := (inputs != nil && inputs["default"] != nil) || parameterStepIsMapMode(firstStep)
				hasFallback := hasGuaranteedFallback(steps)
				def := resolverDefaultValue(steps)
				cliKey := parameterCLIKey(inputs, name)
				parameters = append(parameters, ParamInfo{
					Name:         cliKey,
					ResolverName: name,
					Type:         string(rslvr.Type),
					Description:  rslvr.Description,
					Example:      rslvr.Example,
					Default:      def,
					Required:     !hasInputDefault && !hasFallback,
				})
			}
		}
	}

	// Build the source argument (how to point the command at this solution).
	// It is derived from how the solution was actually loaded, so catalog/URL
	// references become positional args and auto-discovery falls back to the
	// resolved path -- never a bogus "-f ./" or "-f ./my-catalog-ref".
	sourceVal, useFileFlag := resolveSolutionSource(sol, path)
	sourceArg := formatSourceArg(sourceVal, useFileFlag)
	selectorFlags := formatSelectorFlags(sol, sourceVal, useFileFlag, cfg)
	baseCommand := command
	if sourceArg != "" {
		baseCommand = command + " " + sourceArg + selectorFlags
	}
	actionSource := buildActionSourceFlags(sourceVal, selectorFlags)

	var paramFlags strings.Builder
	for _, p := range parameters {
		if !p.Required {
			continue
		}
		exampleVal := "<value>"
		if p.Example != nil {
			exampleVal = fmt.Sprintf("%v", p.Example)
		}
		if hasWorkflow {
			// run solution uses -r flags
			fmt.Fprintf(&paramFlags, " -r %s=%s", p.Name, exampleVal)
		} else {
			// run resolver uses positional key=value
			fmt.Fprintf(&paramFlags, " %s=%s", p.Name, exampleVal)
		}
	}
	requiredFlags := paramFlags.String()
	fullCommand := baseCommand + requiredFlags

	var catalogCommand string
	if hasWorkflow && catalog.IsValidName(sol.Metadata.Name) && !AllActionsExplicit(sol.Spec.Workflow) {
		catalogCommand = command + " " + sol.Metadata.Name + requiredFlags
	}

	var explicitActions []ExplicitActionInfo
	if hasWorkflow && sol.Spec.Workflow != nil && len(sol.Spec.Workflow.Actions) > 0 {
		explicitNames := make([]string, 0)
		for name, act := range sol.Spec.Workflow.Actions {
			if act != nil && act.Explicit {
				explicitNames = append(explicitNames, name)
			}
		}
		sort.Strings(explicitNames)
		if len(explicitNames) > 0 {
			explicitActions = make([]ExplicitActionInfo, 0, len(explicitNames))
			for _, name := range explicitNames {
				act := sol.Spec.Workflow.Actions[name]
				explicitActions = append(explicitActions, ExplicitActionInfo{
					Name:        name,
					Alias:       act.Alias,
					Description: act.Description,
					Command:     baseCommand + " --action " + shellQuote(name) + requiredFlags,
				})
			}
		}
	}

	return &RunCommandInfo{
		Command:         fullCommand,
		Subcommand:      command,
		BaseCommand:     baseCommand,
		CatalogCommand:  catalogCommand,
		Explanation:     explanation,
		Parameters:      parameters,
		ExplicitActions: explicitActions,
		HasWorkflow:     hasWorkflow,
		HasResolvers:    hasResolvers,
		actionSource:    actionSource,
	}, nil
}

func isCatalogOrRemoteSource(sol *solution.Solution, sourceVal string, useFileFlag bool) bool {
	if sourceVal == "" || useFileFlag {
		return false
	}
	if sol != nil {
		resolved := sol.GetPath()
		if strings.HasPrefix(resolved, "catalog:") || strings.HasPrefix(resolved, "remote:") {
			return true
		}
	}
	return strings.HasPrefix(sourceVal, "oci://") || !strings.Contains(sourceVal, "://")
}

func formatSelectorFlags(sol *solution.Solution, sourceVal string, useFileFlag bool, cfg runCommandConfig) string {
	if !isCatalogOrRemoteSource(sol, sourceVal, useFileFlag) {
		return ""
	}
	var b strings.Builder
	if cfg.catalog != "" {
		b.WriteString(" --catalog ")
		b.WriteString(shellQuote(cfg.catalog))
	}
	if cfg.version != "" {
		b.WriteString(" --version ")
		b.WriteString(shellQuote(cfg.version))
	}
	if cfg.preRelease {
		b.WriteString(" --pre-release")
	}
	return b.String()
}

func formatSourceArg(sourceVal string, useFileFlag bool) string {
	if sourceVal == "" {
		return ""
	}
	if useFileFlag {
		return "-f " + shellQuote(sourceVal)
	}
	return shellQuote(sourceVal)
}

func buildActionSourceFlags(sourceVal, selectorFlags string) string {
	if sourceVal == "" {
		return ""
	}
	return " -f " + shellQuote(sourceVal) + selectorFlags
}

// resolveSolutionSource determines the raw source argument value and whether
// 'run solution'/'run resolver' takes it via '-f' (local files, stdin, and
// auto-discovery) or positionally (catalog names, remote OCI refs, and URLs).
// Whitespace in file paths is preserved verbatim so shellQuote can quote the
// exact filename without altering it.
func resolveSolutionSource(sol *solution.Solution, rawPath string) (string, bool) {
	resolved := ""
	if sol != nil {
		resolved = sol.GetPath()
	}

	// Catalog reference: run solution accepts the bare name positionally.
	if ref, ok := strings.CutPrefix(resolved, "catalog:"); ok {
		// catalogPath may be "catalog:<name[@ver]>" or "catalog:<cat>:<name[@ver]>".
		if c, err := source.Classify(rawPath); err == nil && c.Kind.IsReference() {
			return rawPath, false
		}
		if i := strings.LastIndex(ref, ":"); i >= 0 {
			ref = ref[i+1:]
		}
		if ref == "" {
			return "", false
		}
		return ref, false
	}
	// Remote OCI reference: positional.
	if ref, ok := strings.CutPrefix(resolved, "remote:"); ok {
		if rawPath != "" {
			return rawPath, false
		}
		if ref == "" {
			return "", false
		}
		return ref, false
	}
	// URL: positional (no ./ prefixing).
	if strings.Contains(resolved, "://") {
		return resolved, false
	}
	if strings.Contains(rawPath, "://") {
		return rawPath, false
	}

	// Local file. Prefer the user's own -f path when they gave one (preserves
	// their relative form and any intentional whitespace); otherwise fall back
	// to the resolved discovery path.
	filePath := rawPath
	if filePath == "" {
		filePath = resolved
	}
	if filePath == "" {
		return "", false
	}
	if filePath == "-" {
		return "-", true
	}
	// Ensure relative paths have a "./" prefix so chat UIs do not auto-linkify
	// bare filenames into content-reference URLs.
	if !strings.HasPrefix(filePath, "/") && !strings.HasPrefix(filePath, "./") &&
		!strings.HasPrefix(filePath, "../") && !strings.Contains(filePath, "://") {
		filePath = "./" + filePath
	}
	return filePath, true
}

// isLiteralValueRef reports whether ref holds a plain static literal rather
// than a dynamic expression, template, resolver, or credential reference.
func isLiteralValueRef(ref *resolver.ValueRef) bool {
	return ref != nil &&
		ref.Expr == nil && ref.Tmpl == nil && ref.Resolver == nil &&
		ref.Secret == nil && ref.Token == nil && ref.Literal != nil
}

// stepAllowsGuaranteedFallthrough reports whether a resolve source continues to
// the next fallback source when it is skipped or fails. Statically disabled
// sources (when: false) skip directly to the next source regardless of error
// policy; otherwise errors continue by default unless onError is "fail" or
// continueOnError is set to a non-"true" expression (continueOnError wins when
// both are set).
func stepAllowsGuaranteedFallthrough(step resolver.ProviderSource) bool {
	if isStaticallyDisabledStep(step) {
		return true
	}
	if step.ContinueOnError != nil && step.ContinueOnError.Expr != nil {
		return strings.TrimSpace(string(*step.ContinueOnError.Expr)) == "true"
	}
	return step.OnError != resolver.ErrorBehaviorFail
}

// resolverDefaultValue extracts the literal default value for a parameter
// resolver: stopping at steps[0]'s declared "default" input when present
// (returning nil when dynamic), or stopping at the first reachable
// unconditional fallback step (returning its literal when static/defaulted, or
// nil when dynamic or when an earlier source blocks fallthrough on error).
func resolverDefaultValue(steps []resolver.ProviderSource) any {
	steps = trimLeadingDisabledSteps(steps)
	if len(steps) == 0 {
		return nil
	}
	if steps[0].Inputs != nil {
		if defRef, ok := steps[0].Inputs["default"]; ok && defRef != nil {
			if isLiteralValueRef(defRef) {
				return defRef.Literal
			}
			return nil
		}
	}
	if parameterStepIsMapMode(steps[0]) || !stepAllowsGuaranteedFallthrough(steps[0]) {
		return nil
	}
	for _, step := range steps[1:] {
		if !isUnconditionalStep(step) {
			if !stepAllowsGuaranteedFallthrough(step) {
				return nil
			}
			continue
		}
		switch step.Provider {
		case "static":
			if step.Inputs != nil {
				if valRef, ok := step.Inputs["value"]; ok && isLiteralValueRef(valRef) {
					return valRef.Literal
				}
			}
			return nil
		case "parameter":
			if step.Inputs != nil {
				if defRef, ok := step.Inputs["default"]; ok && defRef != nil {
					if isLiteralValueRef(defRef) {
						return defRef.Literal
					}
					return nil
				}
			}
			if parameterStepIsMapMode(step) || !stepAllowsGuaranteedFallthrough(step) {
				return nil
			}
		default:
			return nil
		}
	}
	return nil
}

func isUnconditionalStep(step resolver.ProviderSource) bool {
	return step.When == nil || step.When.Expr == nil || strings.TrimSpace(string(*step.When.Expr)) == "true"
}

func isStaticallyDisabledStep(step resolver.ProviderSource) bool {
	return step.When != nil && step.When.Expr != nil && strings.TrimSpace(string(*step.When.Expr)) == "false"
}

func trimLeadingDisabledSteps(steps []resolver.ProviderSource) []resolver.ProviderSource {
	for len(steps) > 0 && isStaticallyDisabledStep(steps[0]) {
		steps = steps[1:]
	}
	return steps
}

// parameterStepIsAllMode reports whether a parameter step sets "all: true",
// which captures every CLI parameter as a map rather than reading a named key.
func parameterStepIsAllMode(step resolver.ProviderSource) bool {
	if step.Inputs == nil {
		return false
	}
	if allRef, ok := step.Inputs["all"]; ok && allRef != nil {
		if b, isBool := allRef.Literal.(bool); isBool && b {
			return true
		}
	}
	return false
}

// parameterStepIsMapMode reports whether a parameter step reads a map instead
// of a required scalar ("all: true" or "keys" + "as: map"), which never fails
// when keys are omitted on the CLI.
func parameterStepIsMapMode(step resolver.ProviderSource) bool {
	if parameterStepIsAllMode(step) {
		return true
	}
	if step.Inputs == nil {
		return false
	}
	if asRef, ok := step.Inputs["as"]; ok && asRef != nil {
		if s, isStr := asRef.Literal.(string); isStr && s == "map" {
			if _, hasKeys := step.Inputs["keys"]; hasKeys {
				return true
			}
		}
	}
	return false
}

// hasGuaranteedFallback reports whether steps[1:] contain a reachable
// unconditional fallback step (respecting continueOnError / onError policies on
// steps[0] and every preceding step) whose provider is either non-parameter or
// a parameter step with a default/map-mode configuration.
func hasGuaranteedFallback(steps []resolver.ProviderSource) bool {
	if len(steps) < 2 || !stepAllowsGuaranteedFallthrough(steps[0]) {
		return false
	}
	for _, step := range steps[1:] {
		if isUnconditionalStep(step) {
			if step.Provider != "parameter" {
				return true
			}
			if (step.Inputs != nil && step.Inputs["default"] != nil) || parameterStepIsMapMode(step) {
				return true
			}
		}
		if !stepAllowsGuaranteedFallthrough(step) {
			return false
		}
	}
	return false
}

// parameterCLIKey returns the CLI flag name a user passes to supply this
// parameter (`-r <key>=value`). The parameter provider looks up values by
// `inputs.key` (preferred) or the first of `inputs.keys`; only that key -- not
// the resolver's map-key name -- matches a CLI-provided parameter. Falls back to
// the resolver name when neither is a usable string literal.
func parameterCLIKey(inputs map[string]*resolver.ValueRef, resolverName string) string {
	if inputs != nil {
		if ref, ok := inputs["key"]; ok && ref != nil {
			if s, isStr := ref.Literal.(string); isStr && s != "" {
				return s
			}
		}
		if ref, ok := inputs["keys"]; ok && ref != nil {
			if first, isStr := firstStringInLiteral(ref.Literal); isStr {
				return first
			}
		}
	}
	return resolverName
}

// firstStringInLiteral returns the first string element of a literal list value
// (e.g. an `inputs.keys` array). Handles []any and []string shapes.
func firstStringInLiteral(lit any) (string, bool) {
	switch v := lit.(type) {
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				return s, true
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				return s, true
			}
		}
	}
	return "", false
}
