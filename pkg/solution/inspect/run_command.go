// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

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

	// Explanation describes why this command variant was chosen.
	Explanation string `json:"explanation" yaml:"explanation" doc:"Why this command variant was chosen" maxLength:"512" example:"Solution has a workflow with actions"`

	// Parameters lists the parameter-type resolvers that need values.
	Parameters []ParamInfo `json:"parameters" yaml:"parameters" doc:"Parameter-type resolvers requiring values" maxItems:"100"`

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
	// (its "default" input), when present.
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
			if rslvr.Resolve == nil || len(rslvr.Resolve.With) == 0 {
				continue
			}
			if rslvr.Resolve.With[0].Provider == "parameter" {
				inputs := rslvr.Resolve.With[0].Inputs
				def := parameterDefault(inputs)
				cliKey := parameterCLIKey(inputs, name)
				parameters = append(parameters, ParamInfo{
					Name:         cliKey,
					ResolverName: name,
					Type:         string(rslvr.Type),
					Description:  rslvr.Description,
					Example:      rslvr.Example,
					Default:      def,
					Required:     def == nil,
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
	fullCommand := baseCommand
	for _, p := range parameters {
		exampleVal := "<value>"
		if p.Example != nil {
			exampleVal = fmt.Sprintf("%v", p.Example)
		}
		if hasWorkflow {
			// run solution uses -r flags
			fullCommand += fmt.Sprintf(" -r %s=%s", p.Name, exampleVal)
		} else {
			// run resolver uses positional key=value
			fullCommand += fmt.Sprintf(" %s=%s", p.Name, exampleVal)
		}
	}

	return &RunCommandInfo{
		Command:      fullCommand,
		Subcommand:   command,
		BaseCommand:  baseCommand,
		Explanation:  explanation,
		Parameters:   parameters,
		HasWorkflow:  hasWorkflow,
		HasResolvers: hasResolvers,
		actionSource: actionSource,
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
	flag := "-f"
	if c, err := source.Classify(sourceVal); err == nil && c.Kind.IsReference() {
		flag = "-s"
	}
	return " " + flag + " " + shellQuote(sourceVal) + selectorFlags
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

// parameterDefault extracts the literal "default" input from a parameter
// provider's inputs, if declared. Returns nil when there is no default or when
// the default is not a literal (e.g. an expression/template resolved at runtime).
func parameterDefault(inputs map[string]*resolver.ValueRef) any {
	if inputs == nil {
		return nil
	}
	ref, ok := inputs["default"]
	if !ok || ref == nil {
		return nil
	}
	return ref.Literal
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
