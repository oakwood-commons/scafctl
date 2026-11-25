// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/oakwood-commons/scafctl/pkg/action"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// UsageInfo is the user-facing "how do I consume this solution" view. It is a
// projection over a solution's metadata, parameters, and actions, rendered via
// the kvx pipeline (table/json/yaml/tui).
type UsageInfo struct {
	Name        string              `json:"name" yaml:"name" doc:"Solution name" maxLength:"256"`
	DisplayName string              `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"256"`
	Version     string              `json:"version,omitempty" yaml:"version,omitempty" doc:"Solution version" maxLength:"64"`
	Synopsis    string              `json:"synopsis,omitempty" yaml:"synopsis,omitempty" doc:"How to consume this solution" maxLength:"5000"`
	Details     string              `json:"details,omitempty" yaml:"details,omitempty" doc:"Long-form description of how the solution works and when to use it" maxLength:"20000"`
	Source      string              `json:"source,omitempty" yaml:"source,omitempty" doc:"Resolved solution path" maxLength:"1024"`
	Run         string              `json:"run,omitempty" yaml:"run,omitempty" doc:"Base command to run the solution" maxLength:"2048"`
	Tags        []string            `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable tags" maxItems:"100"`
	Links       []LinkInfo          `json:"links,omitempty" yaml:"links,omitempty" doc:"Related documentation and resources" maxItems:"10"`
	Params      []ParamUsage        `json:"parameters,omitempty" yaml:"parameters,omitempty" doc:"User-supplied parameters" maxItems:"100"`
	Actions     []ActionUsage       `json:"actions,omitempty" yaml:"actions,omitempty" doc:"Runnable actions with commands" maxItems:"200"`
	Examples    []spec.UsageExample `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Curated usage examples" maxItems:"50"`
}

// ParamUsage describes a single user-supplied parameter in the usage view.
type ParamUsage struct {
	Name          string `json:"name" yaml:"name" doc:"Parameter name" maxLength:"256"`
	Type          string `json:"type,omitempty" yaml:"type,omitempty" doc:"Parameter type" maxLength:"64"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty" doc:"Parameter description" maxLength:"512"`
	Example       any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value for the parameter"`
	Default       any    `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value when omitted"`
	Required      bool   `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether the parameter must be supplied"`
	AllowedValues []any  `json:"allowedValues,omitempty" yaml:"allowedValues,omitempty" doc:"Discovered allowed values (best-effort)" maxItems:"100"`
}

// ActionUsage describes a runnable action and the command to invoke it.
type ActionUsage struct {
	Name        string `json:"name" yaml:"name" doc:"Action name" maxLength:"150"`
	Alias       string `json:"alias,omitempty" yaml:"alias,omitempty" doc:"Short alias accepted in place of the action name" maxLength:"100" example:"b" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with letter/underscore, followed by alphanumerics, underscores, or hyphens"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"What the action does" maxLength:"512"`
	Default     bool   `json:"default,omitempty" yaml:"default,omitempty" doc:"Runs on a bare invocation (not explicit, not gated by a when clause)"`
	Command     string `json:"command" yaml:"command" doc:"CLI command to run this action" maxLength:"2048"`
}

// ActionListEntry is a single row in 'run action --list' output. Fields
// intentionally omit omitempty so every serialized row carries an identical key
// set for kvx homogeneous-array detection and CEL -w/-e filters.
type ActionListEntry struct {
	Name        string `json:"name" yaml:"name" doc:"Action name" maxLength:"150" example:"build" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with letter/underscore, followed by alphanumerics, underscores, or hyphens"`
	Alias       string `json:"alias" yaml:"alias" doc:"Short alias accepted in place of the action name (empty when unset)" maxLength:"100" example:"b" pattern:"^([a-zA-Z_][a-zA-Z0-9_-]*)?$" patternDescription:"Empty or an identifier starting with a letter/underscore, followed by alphanumerics, underscores, or hyphens"`
	Default     bool   `json:"default" yaml:"default" doc:"Whether the action runs on a bare invocation (not explicit and not gated by when)"`
	Description string `json:"description" yaml:"description" doc:"Human-readable description of what the action does" maxLength:"512" example:"Compile the binary" pattern:"^[\\s\\S]*$" patternDescription:"Free-form action description"`
	Command     string `json:"command" yaml:"command" doc:"CLI command to run this action" maxLength:"2048" example:"scafctl run action build" pattern:"^\\S.*$" patternDescription:"Non-empty CLI invocation command"`
}

// AllActionsExplicit reports whether w declares at least one non-nil regular
// action and every non-nil regular action has Explicit set to true.
func AllActionsExplicit(w *action.Workflow) bool {
	if w == nil || len(w.Actions) == 0 {
		return false
	}
	hasAction := false
	for _, act := range w.Actions {
		if act == nil {
			continue
		}
		hasAction = true
		if !act.Explicit {
			return false
		}
	}
	return hasAction
}

// BuildActionList returns the sorted list of regular workflow actions for a
// solution, suitable for 'run action --list' and kvx array rendering. It always
// returns a non-nil slice so structured serialization emits [] instead of null
// when the solution defines no regular actions.
func BuildActionList(ctx context.Context, sol *solution.Solution, path, binaryName string, opts ...RunCommandOption) []ActionListEntry {
	if sol == nil || !sol.Spec.HasWorkflow() || sol.Spec.Workflow == nil || len(sol.Spec.Workflow.Actions) == 0 {
		return []ActionListEntry{}
	}
	usage, err := BuildUsage(ctx, sol, path, binaryName, opts...)
	if err != nil || len(usage.Actions) == 0 {
		return []ActionListEntry{}
	}
	entries := make([]ActionListEntry, 0, len(usage.Actions))
	for _, a := range usage.Actions {
		entries = append(entries, ActionListEntry{
			Name:        a.Name,
			Alias:       a.Alias,
			Default:     a.Default,
			Description: a.Description,
			Command:     a.Command,
		})
	}
	return entries
}

// FormatAvailableActions formats ActionUsage entries into aligned single-line
// rows suitable for terminal stderr output, sanitizing inline text.
func FormatAvailableActions(actions []ActionUsage) []string {
	if len(actions) == 0 {
		return []string{}
	}

	type formattedRow struct {
		label string
		desc  string
		width int
	}

	rows := make([]formattedRow, 0, len(actions))
	maxWidth := 0
	for _, a := range actions {
		label := provdetail.SanitizeInlineText(a.Name)
		if alias := provdetail.SanitizeInlineText(a.Alias); alias != "" {
			label += " (" + alias + ")"
		}
		if a.Default {
			label += " (default)"
		}
		width := utf8.RuneCountInString(label)
		if width > maxWidth {
			maxWidth = width
		}
		rows = append(rows, formattedRow{
			label: label,
			desc:  provdetail.SanitizeInlineText(a.Description),
			width: width,
		})
	}

	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.desc != "" {
			pad := strings.Repeat(" ", maxWidth-r.width)
			lines = append(lines, "  "+r.label+pad+"  "+r.desc)
		} else {
			lines = append(lines, "  "+r.label)
		}
	}
	return lines
}

// BuildUsage assembles the user-facing usage view for a solution. It never fails
// on best-effort discovery (allowed-value inference); it returns an error only
// when the solution cannot be run at all (no resolvers and no workflow).
//
// binaryName is used to generate embedder-safe commands; it falls back to the
// default CLI binary name when empty.
func BuildUsage(ctx context.Context, sol *solution.Solution, path, binaryName string, opts ...RunCommandOption) (*UsageInfo, error) {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	runInfo, err := BuildRunCommand(sol, path, binaryName, opts...)
	if err != nil {
		return nil, fmt.Errorf("building usage view: %w", err)
	}

	runSubcommand := runInfo.Subcommand
	if AllActionsExplicit(sol.Spec.Workflow) {
		runSubcommand = binaryName + " run action <name>"
	}

	info := &UsageInfo{
		Name:        sol.Metadata.Name,
		DisplayName: sol.Metadata.DisplayName,
		Synopsis:    usageSynopsis(sol),
		Source:      sol.Metadata.Source,
		Run:         runSubcommand,
		Tags:        sol.Metadata.Tags,
		Links:       usageLinks(sol.Metadata.Links),
	}
	if sol.Metadata.Version != nil {
		info.Version = sol.Metadata.Version.String()
	}
	if sol.Metadata.Usage != nil {
		info.Details = sol.Metadata.Usage.Details
		info.Examples = sol.Metadata.Usage.Examples
	}

	// Discover allowed parameter values from action when-clauses (best-effort).
	// These are keyed by RESOLVER name (what `_.x` references in a when-clause).
	allowed := discoverAllowedValues(ctx, sol)

	// Map resolver name -> CLI parameter key so allowed values (discovered by
	// resolver name) attach to the right parameter and commands use the right
	// `-r <key>=`.
	resolverToKey := make(map[string]string, len(runInfo.Parameters))
	for _, p := range runInfo.Parameters {
		if p.ResolverName != "" {
			resolverToKey[p.ResolverName] = p.Name
		}
	}

	info.Params = buildParamUsage(runInfo.Parameters, allowed)
	info.Actions = buildActionUsage(ctx, sol, runInfo.BaseCommand, runInfo.actionSource, binaryName, resolverToKey)

	return info, nil
}

// usageSynopsis picks the best available one-line description: an explicit
// metadata.usage.synopsis, else the description, else the display name.
func usageSynopsis(sol *solution.Solution) string {
	if sol.Metadata.Usage != nil && sol.Metadata.Usage.Synopsis != "" {
		return sol.Metadata.Usage.Synopsis
	}
	if sol.Metadata.Description != "" {
		return sol.Metadata.Description
	}
	return sol.Metadata.DisplayName
}

// discoverAllowedValues scans every action's when-clause for `_.param == value`
// / `_.param in [...]` comparisons and aggregates the discovered literals per
// parameter. It is strictly best-effort: expressions it cannot statically reduce
// contribute nothing.
func discoverAllowedValues(ctx context.Context, sol *solution.Solution) map[string][]any {
	if !sol.Spec.HasWorkflow() || sol.Spec.Workflow == nil {
		return nil
	}
	agg := make(map[string][]any)
	collect := func(actions map[string]*action.Action) {
		for _, act := range actions {
			if act == nil || act.When == nil || act.When.Expr == nil {
				continue
			}
			eqs, ok := act.When.Expr.ParamEqualities(ctx)
			if !ok {
				continue
			}
			for name, vals := range eqs {
				agg[name] = append(agg[name], vals...)
			}
		}
	}
	collect(sol.Spec.Workflow.Actions)
	collect(sol.Spec.Workflow.Finally)

	// Deduplicate/sort each list for stable output.
	for name, vals := range agg {
		agg[name] = dedupeSortAny(vals)
	}
	return agg
}

// usageLinks converts solution metadata links into the inspect view's link shape.
func usageLinks(links []solution.Link) []LinkInfo {
	if len(links) == 0 {
		return nil
	}
	out := make([]LinkInfo, 0, len(links))
	for _, l := range links {
		out = append(out, LinkInfo{Name: l.Name, URL: l.URL})
	}
	return out
}

// buildParamUsage merges run-command parameter info with discovered allowed
// values. Allowed values are discovered keyed by RESOLVER name (what a
// when-clause references), so they are looked up by ParamInfo.ResolverName; the
// displayed parameter name is the CLI key (ParamInfo.Name).
func buildParamUsage(params []ParamInfo, allowedByResolver map[string][]any) []ParamUsage {
	if len(params) == 0 {
		return nil
	}
	out := make([]ParamUsage, 0, len(params))
	for _, p := range params {
		out = append(out, ParamUsage{
			Name:          p.Name,
			Type:          p.Type,
			Description:   p.Description,
			Example:       p.Example,
			Default:       p.Default,
			Required:      p.Required,
			AllowedValues: allowedByResolver[p.ResolverName],
		})
	}
	return out
}

// buildActionUsage produces the per-action command list. An action is "default"
// when it runs on a bare invocation: not explicit, and not gated by a when
// clause. Explicit actions and when-gated actions get an explanatory command
// that names the action or the parameter that enables it. base is the run
// command with the solution source already included (e.g. "... -f ./sol.yaml").
func buildActionUsage(ctx context.Context, sol *solution.Solution, base, actionSource, binaryName string, resolverToKey map[string]string) []ActionUsage {
	if !sol.Spec.HasWorkflow() || sol.Spec.Workflow == nil {
		return nil
	}
	names := make([]string, 0, len(sol.Spec.Workflow.Actions))
	for name := range sol.Spec.Workflow.Actions {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ActionUsage, 0, len(names))
	for _, name := range names {
		act := sol.Spec.Workflow.Actions[name]
		if act == nil {
			continue
		}
		isDefault := !act.Explicit && act.When == nil
		out = append(out, ActionUsage{
			Name:        name,
			Alias:       act.Alias,
			Description: act.Description,
			Default:     isDefault,
			Command:     actionCommand(ctx, binaryName, base, actionSource, name, act, resolverToKey),
		})
	}
	return out
}

// actionCommand generates the CLI command that triggers a specific action.
//   - default action: the bare run command (with the solution source).
//   - explicit action: 'run action <name>' against the same source (preserving
//     -f, catalog, version, and pre-release selectors).
//   - when-gated action: only when the ENTIRE when-clause reduces to a single
//     `_.param == value` (or membership with one value) do we suggest the
//     enabling `-r <key>=value`; otherwise we fall back to the base command so
//     we never print a command that would not actually satisfy the gate.
func actionCommand(ctx context.Context, binaryName, base, actionSource, name string, act *action.Action, resolverToKey map[string]string) string {
	if act.Explicit {
		return fmt.Sprintf("%s run action %s%s", binaryName, shellQuote(name), actionSource)
	}
	if act.When != nil && act.When.Expr != nil {
		eqs, reducible, ok := act.When.Expr.ParamEqualitiesFullyReducible(ctx)
		// Only emit a runnable -r command when the whole clause is satisfied by
		// setting a single parameter to a single value.
		if ok && reducible && len(eqs) == 1 {
			for resolverName, vals := range eqs {
				if len(vals) == 1 {
					key := resolverName
					if k, mapped := resolverToKey[resolverName]; mapped {
						key = k
					}
					return fmt.Sprintf("%s -r %s=%s", base, shellQuote(key), shellQuote(fmt.Sprintf("%v", vals[0])))
				}
			}
		}
	}
	return base
}

// shellQuote wraps a value in single quotes when it contains characters that
// would break shell word-splitting (spaces, quotes, shell metacharacters), so
// generated commands are copy-paste safe. Simple safe tokens are left as-is.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'`$&|;<>(){}[]*?!#~\\^") {
		return s
	}
	// Single-quote and escape embedded single quotes as '\''.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// dedupeSortAny removes duplicate literal values and sorts them for stable output.
func dedupeSortAny(vals []any) []any {
	seen := make(map[string]struct{}, len(vals))
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		key := fmt.Sprintf("%T:%v", v, v)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%v", out[i]) < fmt.Sprintf("%v", out[j])
	})
	return out
}
