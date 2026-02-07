package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SaveOptions holds options for the save command.
type SaveOptions struct {
	Reference  string
	OutputPath string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandSave creates the save command.
func CommandSave(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &SaveOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "save <name[@version]> -o <file.tar>",
		Short:        "Export an artifact to a tar archive",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Export a catalog artifact to an OCI Image Layout tar archive.

			The archive can be transferred to another machine and imported
			using 'scafctl catalog load'. This is useful for air-gapped
			environments or sharing solutions without a registry.

			If no version is specified, the latest version is exported.

			The archive format is compatible with OCI tools like 'oras'.

			Examples:
			  # Export latest version
			  scafctl catalog save my-solution -o my-solution.tar

			  # Export specific version
			  scafctl catalog save my-solution@1.0.0 -o my-solution-v1.0.0.tar
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runSave(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.OutputPath, "output", "o", "", "Output file path (required)")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

func runSave(ctx context.Context, opts *SaveOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse reference
	name, version := parseNameVersion(opts.Reference)

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Save to archive
	result, err := localCatalog.Save(ctx, name, version, opts.OutputPath)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		err = fmt.Errorf("failed to save artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Output success message
	versionStr := ""
	if result.Reference.Version != nil {
		versionStr = "@" + result.Reference.Version.String()
	}

	w.Successf("Saved %s%s to %s (%s)",
		result.Reference.Name,
		versionStr,
		opts.OutputPath,
		formatBytes(result.Size))

	return nil
}

// parseNameVersion splits "name@version" into name and version parts.
func parseNameVersion(ref string) (name, version string) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '@' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}
