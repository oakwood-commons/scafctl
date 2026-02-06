package catalog

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
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
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if !ref.HasVersion() {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Delete artifact
	if err := localCatalog.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to delete artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Deleted %s", ref.String())

	return nil
}
