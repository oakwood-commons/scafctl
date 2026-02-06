package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DeleteOptions holds options for the delete command.
type DeleteOptions struct {
	Reference string
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandDelete creates the delete command.
func CommandDelete(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &DeleteOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "delete <name@version>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete an artifact from the catalog",
		Long: heredoc.Doc(`
			Delete an artifact from the local catalog.

			You must specify the exact version to delete.

			Examples:
			  # Delete a specific version
			  scafctl catalog delete my-solution@1.0.0
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runDelete(cmd.Context(), options)
		},
	}

	return cmd
}

func runDelete(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse reference - require version
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, opts.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference %q: %w", opts.Reference, err)
	}

	if !ref.HasVersion() {
		return fmt.Errorf("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		return fmt.Errorf("failed to open catalog: %w", err)
	}

	// Delete artifact
	if err := localCatalog.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			return fmt.Errorf("artifact %q not found in catalog", opts.Reference)
		}
		return fmt.Errorf("failed to delete artifact: %w", err)
	}

	w.Successf("Deleted %s", ref.String())

	return nil
}
