package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
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

// CommandPrune creates the prune command.
func CommandPrune(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "prune",
		Aliases: []string{"gc", "clean"},
		Short:   "Remove orphaned blobs from the catalog",
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
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
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
		return fmt.Errorf("failed to open catalog: %w", err)
	}

	// Run prune
	result, err := localCatalog.Prune(ctx)
	if err != nil {
		return fmt.Errorf("failed to prune catalog: %w", err)
	}

	// Format output
	output := PruneOutput{
		RemovedManifests: result.RemovedManifests,
		RemovedBlobs:     result.RemovedBlobs,
		ReclaimedBytes:   result.ReclaimedBytes,
		ReclaimedHuman:   formatBytes(result.ReclaimedBytes),
	}

	// For structured output, use kvx
	if outputOpts.Format == kvx.OutputFormatJSON || outputOpts.Format == kvx.OutputFormatYAML {
		return outputOpts.Write(output)
	}

	// For table/default output, print human-readable message
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

// formatBytes formats bytes as a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
