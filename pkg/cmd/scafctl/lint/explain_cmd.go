// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed lint_explain_schema.json
var lintExplainSchemaJSON []byte

// RuleOptions holds options for the 'lint rule' command (and its deprecated
// 'lint explain' alias).
type RuleOptions struct {
	BinaryName     string
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
}

// CommandRule creates the canonical 'lint rule' command.
func CommandRule(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	return newRuleCommand(cliParams, ioStreams, path, "rule <rule-name>", "", false)
}

// CommandExplainRuleDeprecated creates the hidden, deprecated 'lint explain'
// alias. It shares the same RunE/flags as the canonical 'lint rule' command via
// newRuleCommand.
func CommandExplainRuleDeprecated(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	name := cliParams.BinaryName
	if name == "" {
		name = settings.CliBinaryName
	}
	deprecated := fmt.Sprintf(`use "%s lint rule" instead`, name)
	return newRuleCommand(cliParams, ioStreams, path, "explain <rule-name>", deprecated, true)
}

// newRuleCommand is the shared builder for both the canonical 'lint rule'
// command and the deprecated 'lint explain' alias. Both forms wire identical
// Options, RunE, and flags so their behavior is guaranteed to match.
func newRuleCommand(cliParams *settings.Run, ioStreams *terminal.IOStreams, path, use, deprecated string, hidden bool) *cobra.Command {
	opts := &RuleOptions{}

	name := cliParams.BinaryName
	if name == "" {
		name = settings.CliBinaryName
	}

	cCmd := &cobra.Command{
		Use:   use,
		Short: "Explain a specific lint rule in detail",
		Long: fmt.Sprintf(`Show detailed information about a specific lint rule, including
its severity, category, description, why it matters, how to fix it,
and examples that would trigger it.

Examples:
  # Explain a rule
  %[1]s lint rule missing-description

  # Explain with JSON output
  %[1]s lint rule unknown-provider-input -o json`, name),
		Deprecated: deprecated,
		Hidden:     hidden,
		Args:       cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.BinaryName = cliParams.BinaryName
			opts.KvxOutputFlags.AppName = cliParams.BinaryName

			return opts.Run(ctx, args[0])
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)

	return cCmd
}

// explainColumnOrder controls field display order in KVX visual output.
var explainColumnOrder = []string{"rule", "severity", "category", "description", "why", "fix", "examples"}

// Run executes the lint rule command.
func (o *RuleOptions) Run(ctx context.Context, ruleName string) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	rule, found := GetRule(ruleName)
	if !found {
		err := fmt.Errorf("unknown rule %q; run '%s lint rules' to see available rules", ruleName, o.BinaryName)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" lint rule"),
		kvx.WithOutputDisplaySchemaJSON(lintExplainSchemaJSON),
		kvx.WithOutputColumnOrder(explainColumnOrder),
	)

	// For structured formats (JSON/YAML/CSV/TOML), emit the full RuleMeta.
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(rule)
	}

	// For interactive mode, wrap in an array and use the rules list schema
	// so the TUI enters list view and the user can drill into the detail view.
	if o.KvxOutputFlags.Interactive {
		interactiveOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
			kvx.WithIOStreams(o.IOStreams),
			kvx.WithOutputContext(ctx),
			kvx.WithOutputNoColor(o.CliParams.NoColor),
			kvx.WithOutputAppName(o.BinaryName+" lint rule"),
			kvx.WithOutputDisplaySchemaJSON(lintRulesSchemaJSON),
		)
		return interactiveOpts.Write([]any{projectRule(rule)})
	}

	// For visual formats (auto/table/list), project to a map omitting empty fields.
	return outputOpts.Write(projectRule(rule))
}

// projectRule converts a RuleMeta to a map with only non-empty fields for
// visual rendering. Examples are formatted as a numbered list.
func projectRule(rule pkglint.RuleMeta) map[string]any {
	m := map[string]any{
		"rule":        rule.Rule,
		"severity":    rule.Severity,
		"category":    rule.Category,
		"description": rule.Description,
	}
	if rule.Why != "" {
		m["why"] = rule.Why
	}
	if rule.Fix != "" {
		m["fix"] = rule.Fix
	}
	if len(rule.Examples) > 0 {
		m["examples"] = strings.Join(rule.Examples, "\n---\n")
	}
	return m
}
