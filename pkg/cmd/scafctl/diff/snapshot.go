// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"context"
	"fmt"
	"sort"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SnapshotDiffOptions holds options for the diff snapshot command.
type SnapshotDiffOptions struct {
	BeforeFile      string
	AfterFile       string
	IgnoreUnchanged bool
	IgnoreFields    []string

	flags.KvxOutputFlags
}

// CommandDiffSnapshot creates the `diff snapshot` subcommand.
func CommandDiffSnapshot(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &SnapshotDiffOptions{}
	opts.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:          subSnapshot + " [before-snapshot] [after-snapshot]",
		Short:        "Compare two snapshots",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Compare two resolver execution snapshots and show differences.
			
			This is useful for:
			  - Debugging: See what changed between executions
			  - Testing: Validate resolver behavior with golden files
			  - CI/CD: Detect configuration drift
			  - Development: Verify changes don't affect other resolvers
			
			The default output is a human-readable diff report. Use -o json/yaml
			for the full structured result, or -o table/list/tree (and -i) for the
			flattened, filterable change list.
		`),
		Example: heredoc.Docf(`
			# Compare two snapshots
			$ %[1]s diff snapshot before.json after.json
			
			# Show only changed resolvers
			$ %[1]s diff snapshot before.json after.json --ignore-unchanged
			
			# Ignore timing fields
			$ %[1]s diff snapshot before.json after.json --ignore-fields duration,providerCalls
			
			# Output as JSON
			$ %[1]s diff snapshot before.json after.json -o json
			
			# Browse changes interactively
			$ %[1]s diff snapshot before.json after.json -i
			
			# Save diff to a file with shell redirection
			$ %[1]s diff snapshot before.json after.json -o json > diff.json
		`, binaryName),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if writer.FromContext(ctx) == nil {
				ctx = writer.WithWriter(ctx, writer.New(&ioStreams, cliParams))
				cmd.SetContext(ctx)
			}
			opts.BeforeFile = args[0]
			opts.AfterFile = args[1]
			return runSnapshotDiff(ctx, opts, &ioStreams, binaryName)
		},
	}

	cmd.Flags().BoolVar(&opts.IgnoreUnchanged, "ignore-unchanged", false, "Omit unchanged resolvers from output")
	cmd.Flags().StringSliceVar(&opts.IgnoreFields, "ignore-fields", []string{}, "Fields to ignore (e.g., duration,providerCalls)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

func runSnapshotDiff(ctx context.Context, opts *SnapshotDiffOptions, ioStreams *terminal.IOStreams, binaryName string) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	lgr.V(-1).Info("loading before snapshot", "file", opts.BeforeFile)
	before, err := resolver.LoadSnapshot(opts.BeforeFile)
	if err != nil {
		err = fmt.Errorf("failed to load before snapshot: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	lgr.V(-1).Info("loading after snapshot", "file", opts.AfterFile)
	after, err := resolver.LoadSnapshot(opts.AfterFile)
	if err != nil {
		err = fmt.Errorf("failed to load after snapshot: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	diffOpts := &resolver.DiffOptions{
		IgnoreUnchanged: opts.IgnoreUnchanged,
		IgnoreFields:    opts.IgnoreFields,
	}

	lgr.V(-1).Info("computing diff")
	diff := resolver.DiffSnapshotsWithOptions(before, after, diffOpts)

	return writeDiffOutput(ctx, w, ioStreams, &opts.KvxOutputFlags, binaryName+" diff snapshot", diff, reportFromSnapshotDiff(diff))
}

// reportFromSnapshotDiff maps a resolver.SnapshotDiff into the shared diff
// report. Modified resolvers expand to one entry per field change (Group =
// resolver name) so field-level detail survives the flat model.
func reportFromSnapshotDiff(diff *resolver.SnapshotDiff) *diffreport.Report {
	r := diffreport.New("snapshot", snapshotRef(diff.Before), snapshotRef(diff.After))

	names := make([]string, 0, len(diff.Resolvers))
	for name := range diff.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		rd := diff.Resolvers[name]
		switch rd.Type {
		case resolver.DiffTypeAdded:
			r.Add(diffreport.Entry{Path: name, Kind: diffreport.ChangeAdded, After: resolverValue(rd.After)})
		case resolver.DiffTypeRemoved:
			r.Add(diffreport.Entry{Path: name, Kind: diffreport.ChangeRemoved, Before: resolverValue(rd.Before)})
		case resolver.DiffTypeModified:
			if len(rd.Changes) == 0 {
				r.Add(diffreport.Entry{Path: name, Kind: diffreport.ChangeModified})
				continue
			}
			for _, c := range rd.Changes {
				r.Add(diffreport.Entry{Group: name, Path: c.Field, Kind: diffreport.ChangeModified, Before: c.Before, After: c.After})
			}
		case resolver.DiffTypeUnchanged:
			r.Add(diffreport.Entry{Path: name, Kind: diffreport.ChangeUnchanged})
		}
	}
	return r
}

func snapshotRef(m *resolver.SnapshotMetadata) string {
	if m == nil {
		return ""
	}
	if m.Version != "" {
		return m.Solution + "@" + m.Version
	}
	return m.Solution
}

func resolverValue(sr *resolver.SnapshotResolver) any {
	if sr == nil {
		return nil
	}
	return sr.Value
}
