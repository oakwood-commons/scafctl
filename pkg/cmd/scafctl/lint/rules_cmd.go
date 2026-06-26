// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed lint_rules_schema.json
var lintRulesSchemaJSON []byte

// RulesOptions holds options for the lint rules command.
type RulesOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	Severity       string
	Category       string
}

// CommandRules creates the 'lint rules' command.
func CommandRules(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &RulesOptions{}

	cCmd := &cobra.Command{
		Use:     "rules",
		Aliases: []string{"r"},
		Short:   "List all available lint rules",
		Long: heredoc.Doc(`
			List all lint rules that scafctl checks for when linting solutions.

			Rules are grouped by severity (error, warning, info) and category.
			Use --severity and --category to filter the list.

			Examples:
			  # List all rules
			  scafctl lint rules

			  # Show only error-level rules
			  scafctl lint rules --severity error

			  # Filter by category
			  scafctl lint rules --category naming

			  # Output as JSON for tooling
			  scafctl lint rules -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
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
			opts.KvxOutputFlags.AppName = cliParams.BinaryName

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	cCmd.Flags().StringVar(&opts.Severity, "severity", "", "Filter by severity: error, warning, info")
	cCmd.Flags().StringVar(&opts.Category, "category", "", "Filter by category")

	return cCmd
}

// rulesColumnHints controls column display widths and priorities for table output.
var rulesColumnHints = map[string]tui.ColumnHint{
	"rule":        {MaxWidth: 25, Priority: 10, DisplayName: "Rule"},
	"severity":    {MaxWidth: 8, Priority: 9, DisplayName: "Severity"},
	"category":    {MaxWidth: 15, Priority: 8, DisplayName: "Category"},
	"description": {DisplayName: "Description", Flex: true},
}

// Run executes the lint rules command.
func (o *RulesOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	rules := ListRules()

	// Filter by severity
	if o.Severity != "" {
		severity := strings.ToLower(o.Severity)
		filtered := make([]RuleMeta, 0, len(rules))
		for _, r := range rules {
			if strings.EqualFold(r.Severity, severity) {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}

	// Filter by category
	if o.Category != "" {
		category := strings.ToLower(o.Category)
		filtered := make([]RuleMeta, 0, len(rules))
		for _, r := range rules {
			if strings.EqualFold(r.Category, category) {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}

	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.CliParams.BinaryName+" lint rules"),
		kvx.WithOutputDisplaySchemaJSON(lintRulesSchemaJSON),
		kvx.WithOutputColumnOrder([]string{"rule", "severity", "category", "description"}),
		kvx.WithOutputColumnHints(rulesColumnHints),
	)

	if len(rules) == 0 {
		if outputOpts.Format == kvx.OutputFormatQuiet {
			return nil
		}
		if kvx.IsStructuredFormat(outputOpts.Format) {
			return outputOpts.Write([]RuleMeta{})
		}
		w.Infof("No rules match the specified filters.")
		return nil
	}

	// For structured formats (json/yaml), emit the full RuleMeta with all
	// fields (why, fix, examples). For interactive mode, project with
	// examples joined as a string for rich detail views. For visual formats
	// (table/list), project to the four visible columns so KVX renders a
	// columnar table instead of falling back to key-value list mode.
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(rules)
	}
	if o.KvxOutputFlags.Interactive {
		return outputOpts.Write(projectRulesInteractive(rules))
	}
	return outputOpts.Write(projectRules(rules))
}

// projectRulesInteractive converts rules to maps with all fields for the
// interactive detail view. Examples are joined into a single string so the
// TUI renders them as formatted text rather than a raw JSON array.
func projectRulesInteractive(rules []RuleMeta) []any {
	rows := make([]any, len(rules))
	for i, r := range rules {
		rows[i] = projectRule(r)
	}
	return rows
}

// projectRules converts rules to maps with only the four table-visible columns.
// This keeps the field count low so KVX renders a columnar table instead of
// falling back to key-value list view.
func projectRules(rules []RuleMeta) []any {
	rows := make([]any, len(rules))
	for i, r := range rules {
		rows[i] = map[string]any{
			"rule":        r.Rule,
			"severity":    r.Severity,
			"category":    r.Category,
			"description": r.Description,
		}
	}
	return rows
}
