// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/kvx/pkg/tui"
	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

//go:embed action_list_schema.json
var actionListSchemaJSON []byte

// runList loads the solution metadata/spec directly without preparing plugins,
// running resolvers, or creating output directories, and renders the workflow's
// regular actions via kvx.
func (o *ActionOptions) runList(ctx context.Context) error {
	if o.DryRun {
		return o.exitWithCode(ctx, fmt.Errorf("--list cannot be combined with --dry-run"), exitcode.InvalidInput)
	}
	if len(o.ResolverParams) > 0 || len(o.DynamicArgs) > 0 {
		return o.exitWithCode(ctx, fmt.Errorf("--list does not accept resolver parameters"), exitcode.InvalidInput)
	}
	if err := cmdflags.ValidateKvxOutputFormat(o.Output); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}
	if o.Output == string(kvx.OutputFormatTest) {
		return o.exitWithCode(ctx,
			fmt.Errorf("output format %q is not supported with --list", kvx.OutputFormatTest),
			exitcode.InvalidInput)
	}

	loader := o.newSolutionLoader(ctx)
	sel, err := loader.Select(ctx, o.File, o.Names, load.ArgsAreNames, o.sourceOptions()...)
	if err != nil {
		// Under ArgsAreNames, unambiguous source refs in args[0] (URL, remote ref,
		// versioned name) are selected before discovery. If discovery failed with
		// ErrNoSolutionFound while positional args were supplied, those args are
		// bare action names, which --list rejects regardless of CWD contents.
		if errors.Is(err, load.ErrNoSolutionFound) && len(o.Names) > 0 {
			return o.exitWithCode(ctx,
				fmt.Errorf("--list does not accept action names (got: %s)", strings.Join(o.Names, ", ")),
				exitcode.InvalidInput)
		}
		return o.exitWithCode(ctx, err, prepare.ErrorExitCode(err))
	}
	if len(sel.Args) > 0 {
		return o.exitWithCode(ctx,
			fmt.Errorf("--list does not accept action names (got: %s)", strings.Join(sel.Args, ", ")),
			exitcode.InvalidInput)
	}

	load.ReportSource(ctx, sel.Source)
	ms, err := loader.SelectionLoader(sel).Load(ctx, sel.Source)
	if err != nil {
		return o.exitWithCode(ctx, err, prepare.ErrorExitCode(err))
	}
	load.ReportDiscovery(ctx, ms)

	sol := ms.Solution
	entries := inspect.BuildActionList(ctx, sol, sel.Source.Ref, o.BinaryName, o.usageCommandOptions(sel.Source)...)
	outputOpts := o.actionListOutputOptions(ctx)

	if len(entries) == 0 && !o.Interactive && o.Expression == "" && o.Where == "" &&
		!kvx.IsStructuredFormat(outputOpts.Format) && !kvx.IsQuietFormat(outputOpts.Format) {
		if w := writer.FromContext(ctx); w != nil {
			if sol != nil && sol.Metadata.Name != "" {
				w.WarnStderrf("No actions defined in solution %q.", sol.Metadata.Name)
			} else {
				w.WarnStderr("No actions defined in solution.")
			}
		}
		return nil
	}

	if err := outputOpts.Write(entries); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}
	return nil
}

// usageCommandOptions translates the selected source's catalog, version, and
// pre-release flags into RunCommandOptions so generated commands target the
// exact same solution selector.
func (o *ActionOptions) usageCommandOptions(src source.Classification) []inspect.RunCommandOption {
	var opts []inspect.RunCommandOption
	if src.Catalog != "" {
		opts = append(opts, inspect.WithCatalog(src.Catalog))
	}
	if src.Version != "" {
		opts = append(opts, inspect.WithVersionConstraint(src.Version))
	}
	if o.PreRelease {
		opts = append(opts, inspect.WithPreRelease(true))
	}
	return opts
}

// actionListOutputOptions builds the kvx output configuration for 'run action --list'.
func (o *ActionOptions) actionListOutputOptions(ctx context.Context) *kvx.OutputOptions {
	noColor := o.CliParams != nil && o.CliParams.NoColor
	return cmdflags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithIOStreams(o.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(noColor),
		kvx.WithOutputAppName(o.BinaryName+" run action"),
		kvx.WithOutputDisplaySchemaJSON(actionListSchemaJSON),
		kvx.WithOutputColumnOrder([]string{"name", "alias", "default", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":        {MaxWidth: 24, Priority: 10},
			"alias":       {MaxWidth: 12, Priority: 8},
			"default":     {MaxWidth: 7, Priority: 7},
			"description": {MaxWidth: 36, Priority: 6, Flex: true},
			"command":     {Hidden: true},
		}),
	)
}
