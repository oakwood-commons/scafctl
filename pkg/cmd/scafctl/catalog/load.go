package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// LoadOptions holds options for the load command.
type LoadOptions struct {
	InputPath string
	Force     bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// LoadOutput represents the output of a load operation.
type LoadOutput struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Kind      string `json:"kind" yaml:"kind"`
	Digest    string `json:"digest" yaml:"digest"`
	Size      int64  `json:"size" yaml:"size"`
	CreatedAt string `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
}

// CommandLoad creates the load command.
func CommandLoad(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &LoadOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "load --input <file.tar>",
		Short:        "Import an artifact from a tar archive",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Import a catalog artifact from an OCI Image Layout tar archive.

			The archive should have been created with 'scafctl catalog save'
			or any OCI-compatible tool.

			If the artifact already exists in the catalog, use --force to overwrite.

			Examples:
			  # Import an artifact
			  scafctl catalog load --input my-solution.tar

			  # Overwrite existing artifact
			  scafctl catalog load --input my-solution.tar --force

			  # Output result as JSON
			  scafctl catalog load --input my-solution.tar -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runLoad(cmd.Context(), options, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&options.InputPath, "input", "", "Input file path (required)")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing artifact")
	_ = cmd.MarkFlagRequired("input")

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runLoad(ctx context.Context, opts *LoadOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Load from archive
	result, err := localCatalog.Load(ctx, opts.InputPath, opts.Force)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("artifact already exists (use --force to overwrite)")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to load artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build output
	versionStr := ""
	if result.Reference.Version != nil {
		versionStr = result.Reference.Version.String()
	}

	output := LoadOutput{
		Name:    result.Reference.Name,
		Version: versionStr,
		Kind:    string(result.Reference.Kind),
		Digest:  result.Digest,
		Size:    result.Size,
	}
	if !result.CreatedAt.IsZero() {
		output.CreatedAt = result.CreatedAt.Format("2006-01-02 15:04:05")
	}

	// For table output, print success message first
	if outputOpts.Format == "" || outputOpts.Format == "table" {
		w.Successf("Loaded artifact from %s", opts.InputPath)
		w.Plainln("")
	}

	return outputOpts.Write(output)
}
