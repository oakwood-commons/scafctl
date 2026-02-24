// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ExplainOptions holds options for the lint explain command.
type ExplainOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
}

// CommandExplainRule creates the 'lint explain' command.
func CommandExplainRule(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ExplainOptions{}

	cCmd := &cobra.Command{
		Use:   "explain <rule-name>",
		Short: "Explain a specific lint rule in detail",
		Long: heredoc.Doc(`
			Show detailed information about a specific lint rule, including
			its severity, category, description, why it matters, how to fix it,
			and examples that would trigger it.

			Examples:
			  # Explain a rule
			  scafctl lint explain missing-description

			  # Explain with JSON output
			  scafctl lint explain unknown-provider-input -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

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

			return opts.Run(ctx, args[0])
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)

	return cCmd
}

// Run executes the lint explain command.
func (o *ExplainOptions) Run(ctx context.Context, ruleName string) error {
	w := writer.MustFromContext(ctx)

	rule, found := GetRule(ruleName)
	if !found {
		err := fmt.Errorf("unknown rule %q; run 'scafctl lint rules' to see available rules", ruleName)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Handle structured output formats
	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags, kvx.WithIOStreams(o.IOStreams))
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(rule)
	}

	// Table output
	w.Infof("Rule: %s\n", rule.Rule)
	w.Plain("")
	w.Plainf("Severity:    %s\n", rule.Severity)
	w.Plainf("Category:    %s\n", rule.Category)
	w.Plainf("Description: %s\n", rule.Description)
	w.Plain("")

	if rule.Why != "" {
		w.Infof("Why:")
		w.Plainf("  %s\n", rule.Why)
		w.Plain("")
	}

	if rule.Fix != "" {
		w.Infof("Fix:")
		w.Plainf("  %s\n", rule.Fix)
		w.Plain("")
	}

	if len(rule.Examples) > 0 {
		w.Infof("Examples that trigger this rule:")
		for _, ex := range rule.Examples {
			w.Plainf("  • %s\n", ex)
		}
	}

	return nil
}
