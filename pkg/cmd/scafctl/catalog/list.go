package catalog

import (
	"context"
	"sort"

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

// ListOptions holds options for the list command.
type ListOptions struct {
	Kind      string
	Name      string
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// ArtifactListItem represents an artifact in list output.
type ArtifactListItem struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Kind      string `json:"kind" yaml:"kind"`
	Digest    string `json:"digest" yaml:"digest"`
	CreatedAt string `json:"createdAt" yaml:"createdAt"`
	Catalog   string `json:"catalog" yaml:"catalog"`
}

// CommandList creates the list command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &ListOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List artifacts in the catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			List all artifacts stored in the local catalog.

			Filter by kind (solution, plugin) or by name to narrow results.

			Examples:
			  # List all artifacts
			  scafctl catalog list

			  # List only solutions
			  scafctl catalog list --kind solution

			  # List all versions of a specific solution
			  scafctl catalog list --name my-solution

			  # Output as JSON
			  scafctl catalog list -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runList(cmd.Context(), options, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&options.Kind, "kind", "", "Filter by artifact kind (solution, provider, auth-handler)")
	cmd.Flags().StringVar(&options.Name, "name", "", "Filter by artifact name")

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runList(ctx context.Context, opts *ListOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse kind filter
	var kind catalog.ArtifactKind
	if opts.Kind != "" {
		kind = catalog.ArtifactKind(opts.Kind)
		if !kind.IsValid() {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// List artifacts
	artifacts, err := localCatalog.List(ctx, kind, opts.Name)
	if err != nil {
		w.Errorf("failed to list artifacts: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Sort by name, then version descending
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Reference.Name != artifacts[j].Reference.Name {
			return artifacts[i].Reference.Name < artifacts[j].Reference.Name
		}
		vi := artifacts[i].Reference.Version
		vj := artifacts[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vi.GreaterThan(vj)
	})

	// Convert to output format
	items := make([]ArtifactListItem, len(artifacts))
	for i, a := range artifacts {
		version := ""
		if a.Reference.Version != nil {
			version = a.Reference.Version.String()
		}
		items[i] = ArtifactListItem{
			Name:      a.Reference.Name,
			Version:   version,
			Kind:      string(a.Reference.Kind),
			Digest:    a.Digest,
			CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05"),
			Catalog:   a.Catalog,
		}
	}

	return outputOpts.Write(items)
}
