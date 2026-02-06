package catalog

import (
	"context"

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

// InspectOptions holds options for the inspect command.
type InspectOptions struct {
	Reference string
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// ArtifactDetail represents detailed artifact information.
type ArtifactDetail struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Kind        string            `json:"kind" yaml:"kind"`
	Digest      string            `json:"digest" yaml:"digest"`
	Size        int64             `json:"size" yaml:"size"`
	CreatedAt   string            `json:"createdAt" yaml:"createdAt"`
	Catalog     string            `json:"catalog" yaml:"catalog"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// CommandInspect creates the inspect command.
func CommandInspect(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &InspectOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "inspect <name[@version]>",
		Aliases: []string{"info", "show"},
		Short:   "Show detailed information about an artifact",
		Long: heredoc.Doc(`
			Show detailed information about a catalog artifact.

			If no version is specified, shows the latest version.

			Examples:
			  # Inspect latest version
			  scafctl catalog inspect my-solution

			  # Inspect specific version
			  scafctl catalog inspect my-solution@1.0.0

			  # Output as YAML
			  scafctl catalog inspect my-solution -o yaml
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runInspect(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runInspect(ctx context.Context, opts *InspectOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse reference - try as solution first
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Resolve to find artifact
	info, err := localCatalog.Resolve(ctx, ref)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to resolve artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build detail output
	version := ""
	if info.Reference.Version != nil {
		version = info.Reference.Version.String()
	}

	detail := ArtifactDetail{
		Name:        info.Reference.Name,
		Version:     version,
		Kind:        string(info.Reference.Kind),
		Digest:      info.Digest,
		Size:        info.Size,
		CreatedAt:   info.CreatedAt.Format("2006-01-02 15:04:05"),
		Catalog:     info.Catalog,
		Annotations: info.Annotations,
	}

	return outputOpts.Write(detail)
}
