// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

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

	// Handle structured output formats
	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags, kvx.WithIOStreams(o.IOStreams))
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(rules)
	}

	// Table output
	if len(rules) == 0 {
		w.Infof("No rules match the specified filters.")
		return nil
	}

	// Find max lengths for alignment
	maxRule := 0
	maxSev := 0
	maxCat := 0
	for _, r := range rules {
		if len(r.Rule) > maxRule {
			maxRule = len(r.Rule)
		}
		if len(r.Severity) > maxSev {
			maxSev = len(r.Severity)
		}
		if len(r.Category) > maxCat {
			maxCat = len(r.Category)
		}
	}

	w.Infof("Lint Rules (%d)\n", len(rules))
	w.Plain("")
	w.Plainf("%-*s  %-*s  %-*s  %s\n", maxRule, "RULE", maxSev, "SEVERITY", maxCat, "CATEGORY", "DESCRIPTION")
	w.Plainf("%-*s  %-*s  %-*s  %s\n", maxRule, strings.Repeat("─", maxRule), maxSev, strings.Repeat("─", maxSev), maxCat, strings.Repeat("─", maxCat), strings.Repeat("─", 40))

	for _, r := range rules {
		w.Plainf("%-*s  %-*s  %-*s  %s\n", maxRule, r.Rule, maxSev, r.Severity, maxCat, r.Category, r.Description)
	}

	return nil
}
