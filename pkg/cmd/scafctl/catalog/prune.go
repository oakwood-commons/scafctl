// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// PruneOptions holds options for the prune command.
type PruneOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// PruneOutput represents the prune command output.
type PruneOutput struct {
	RemovedManifests int    `json:"removedManifests" yaml:"removedManifests"`
	RemovedBlobs     int    `json:"removedBlobs" yaml:"removedBlobs"`
	ReclaimedBytes   int64  `json:"reclaimedBytes" yaml:"reclaimedBytes"`
	ReclaimedHuman   string `json:"reclaimedHuman" yaml:"reclaimedHuman"`
}

// pruneColumnHints controls column display for prune output.
var pruneColumnHints = map[string]tui.ColumnHint{
	"removedManifests": {DisplayName: "removed manifests"},
	"removedBlobs":     {DisplayName: "removed blobs"},
	"reclaimedHuman":   {DisplayName: "reclaimed"},
	"reclaimedBytes":   {DisplayName: "reclaimed bytes"},
}

// pruneColumnOrder defines the display order for prune output fields.
var pruneColumnOrder = []string{"removedManifests", "removedBlobs", "reclaimedHuman", "reclaimedBytes"}

// CommandPrune creates the prune command.
func CommandPrune(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "prune",
		Aliases:      []string{"gc", "clean"},
		Short:        "Remove orphaned blobs from the catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Remove orphaned blobs and manifests from the local catalog.

			When artifacts are deleted, the underlying blobs remain in the
			catalog storage. This command removes any blobs that are no longer
			referenced by a tagged artifact, reclaiming disk space.

			Examples:
			  # Prune orphaned content
			  scafctl catalog prune

			  # Show what would be removed (JSON output)
			  scafctl catalog prune -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			options.AppName = cliParams.BinaryName
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputContext(cmd.Context()),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputColumnHints(pruneColumnHints),
				kvx.WithOutputColumnOrder(pruneColumnOrder),
			)
			return runPrune(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runPrune(ctx context.Context, _ *PruneOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Run prune
	result, err := localCatalog.Prune(ctx)
	if err != nil {
		w.Errorf("failed to prune catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Format output
	output := PruneOutput{
		RemovedManifests: result.RemovedManifests,
		RemovedBlobs:     result.RemovedBlobs,
		ReclaimedBytes:   result.ReclaimedBytes,
		ReclaimedHuman:   format.Bytes(result.ReclaimedBytes),
	}

	// For the default (no explicit -o, -i, -e, or -w flags), show styled human-readable message.
	// Any explicit flag routes through kvx so filtering and format selection work.
	if !outputOpts.FormatExplicit && !outputOpts.Interactive && outputOpts.Expression == "" && outputOpts.Where == "" {
		if result.RemovedManifests == 0 && result.RemovedBlobs == 0 {
			w.Infof("No orphaned content found")
		} else {
			w.Successf("Pruned catalog")
			if result.RemovedManifests > 0 {
				w.Infof("  Removed manifests: %d", result.RemovedManifests)
			}
			if result.RemovedBlobs > 0 {
				w.Infof("  Removed blobs: %d", result.RemovedBlobs)
			}
			w.Infof("  Reclaimed: %s", output.ReclaimedHuman)
		}
		return nil
	}

	return outputOpts.Write(output)
}
