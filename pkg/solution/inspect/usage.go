// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// UsageInfo is the user-facing "how do I consume this solution" view. It is a
// projection over a solution's metadata, parameters, and actions, rendered via
// the kvx pipeline (table/json/yaml/tui).
type UsageInfo struct {
	Name     string              `json:"name" yaml:"name" doc:"Solution name" maxLength:"256"`
	Version  string              `json:"version,omitempty" yaml:"version,omitempty" doc:"Solution version" maxLength:"64"`
	Synopsis string              `json:"synopsis,omitempty" yaml:"synopsis,omitempty" doc:"How to consume this solution" maxLength:"5000"`
	Source   string              `json:"source,omitempty" yaml:"source,omitempty" doc:"Resolved solution path" maxLength:"1024"`
	Run      string              `json:"run,omitempty" yaml:"run,omitempty" doc:"Base command to run the solution" maxLength:"2048"`
	Params   []ParamUsage        `json:"parameters,omitempty" yaml:"parameters,omitempty" doc:"User-supplied parameters" maxItems:"100"`
	Actions  []ActionUsage       `json:"actions,omitempty" yaml:"actions,omitempty" doc:"Runnable actions with commands" maxItems:"200"`
	Examples []spec.UsageExample `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Curated usage examples" maxItems:"50"`
}

// ParamUsage describes a single user-supplied parameter in the usage view.
type ParamUsage struct {
	Name          string `json:"name" yaml:"name" doc:"Parameter name" maxLength:"256"`
	Type          string `json:"type,omitempty" yaml:"type,omitempty" doc:"Parameter type" maxLength:"64"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty" doc:"Parameter description" maxLength:"512"`
	Default       any    `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value when omitted"`
	Required      bool   `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether the parameter must be supplied"`
	AllowedValues []any  `json:"allowedValues,omitempty" yaml:"allowedValues,omitempty" doc:"Discovered allowed values (best-effort)" maxItems:"100"`
}

// ActionUsage describes a runnable action and the command to invoke it.
type ActionUsage struct {
	Name        string `json:"name" yaml:"name" doc:"Action name" maxLength:"150"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"What the action does" maxLength:"512"`
	Default     bool   `json:"default,omitempty" yaml:"default,omitempty" doc:"Runs on a bare invocation (not explicit, not gated by a when clause)"`
	Command     string `json:"command" yaml:"command" doc:"CLI command to run this action" maxLength:"2048"`
}

// BuildUsage assembles the user-facing usage view for a solution. It never fails
// on best-effort discovery (allowed-value inference); it returns an error only
// when the solution cannot be run at all (no resolvers and no workflow).
//
// binaryName is used to generate embedder-safe commands; it falls back to the
// default CLI binary name when empty.
func BuildUsage(ctx context.Context, sol *solution.Solution, path, binaryName string) (*UsageInfo, error) {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	runInfo, err := BuildRunCommand(sol, path, binaryName)
	if err != nil {
		return nil, fmt.Errorf("building usage view: %w", err)
	}

	info := &UsageInfo{
		Name:     sol.Metadata.Name,
		Synopsis: usageSynopsis(sol),
		Source:   sol.Metadata.Source,
		Run:      runInfo.Subcommand,
	}
	if sol.Metadata.Version != nil {
		info.Version = sol.Metadata.Version.String()
	}
	if sol.Metadata.Usage != nil {
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
	info.Actions = buildActionUsage(ctx, sol, runInfo.BaseCommand, binaryName, resolverToKey)

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
func buildActionUsage(ctx context.Context, sol *solution.Solution, base, binaryName string, resolverToKey map[string]string) []ActionUsage {
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
			Description: act.Description,
			Default:     isDefault,
			Command:     actionCommand(ctx, binaryName, base, name, act, resolverToKey),
		})
	}
	return out
}

// actionCommand generates the CLI command that triggers a specific action.
//   - default action: the bare run command (with the solution source).
//   - explicit action: 'run action <name>' against the same source.
//   - when-gated action: only when the ENTIRE when-clause reduces to a single
//     `_.param == value` (or membership with one value) do we suggest the
//     enabling `-r <key>=value`; otherwise we fall back to the base command so
//     we never print a command that would not actually satisfy the gate.
func actionCommand(ctx context.Context, binaryName, base, name string, act *action.Action, resolverToKey map[string]string) string {
	if act.Explicit {
		// base is "<bin> run solution -f <src>"; swap the subcommand for
		// "run action <name>" while preserving the source flags.
		if src, ok := sourceArgs(base, binaryName); ok {
			return fmt.Sprintf("%s run action %s%s", binaryName, name, src)
		}
		return fmt.Sprintf("%s run action %s", binaryName, name)
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
					return fmt.Sprintf("%s -r %s=%s", base, key, shellQuote(fmt.Sprintf("%v", vals[0])))
				}
			}
		}
	}
	return base
}

// sourceArgs extracts the source portion (everything after "<bin> run solution")
// from a base command like "<bin> run solution -f ./sol.yaml", so it can be
// reattached to a "run action" variant. Only "-f <path>" sources are reattached
// -- `run action` treats bare positional args as action names, so a positional
// solution ref (catalog/URL) must NOT be appended. Returns ("", false) when
// there is no reattachable -f source.
func sourceArgs(base, binaryName string) (string, bool) {
	prefix := binaryName + " run solution"
	suffix, ok := strings.CutPrefix(base, prefix)
	if !ok {
		return "", false
	}
	// Only "-f <path>" is safe to reattach to `run action`.
	if strings.HasPrefix(strings.TrimSpace(suffix), "-f ") {
		return suffix, true
	}
	return "", false
}

// shellQuote wraps a value in single quotes when it contains characters that
// would break shell word-splitting (spaces, quotes, shell metacharacters), so
// generated commands are copy-paste safe. Simple safe tokens are left as-is.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'`$&|;<>(){}[]*?!#~\\") {
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
