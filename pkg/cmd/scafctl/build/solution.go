package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds options for the build solution command.
type SolutionOptions struct {
	File      string
	Name      string
	Version   string
	Force     bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandBuildSolution creates the build solution command.
func CommandBuildSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &SolutionOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "solution [file]",
		Aliases: []string{"sol", "s"},
		Short:   "Build a solution into the local catalog",
		Long: heredoc.Doc(`
			Build a solution file into the local catalog.

			The solution is packaged as an OCI artifact with the specified name and version.
			If name is not specified, it is extracted from the solution metadata.
			If version is not specified, it is extracted from the solution metadata.

			Examples:
			  # Build solution using version from metadata
			  scafctl build solution ./my-solution.yaml

			  # Build with explicit version (overrides metadata)
			  scafctl build solution ./solution.yaml --version 1.0.0

			  # Build with explicit name
			  scafctl build solution ./solution.yaml --name my-solution --version 1.0.0

			  # Overwrite existing version
			  scafctl build solution ./solution.yaml --version 1.0.0 --force
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.File = args[0]
			return runBuildSolution(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVar(&options.Name, "name", "", "Artifact name (default: extracted from solution metadata)")
	cmd.Flags().StringVar(&options.Version, "version", "", "Semantic version (default: extracted from solution metadata)")
	cmd.Flags().BoolVar(&options.Force, "force", false, "Overwrite existing version")

	return cmd
}

func runBuildSolution(ctx context.Context, opts *SolutionOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Read solution file
	content, err := os.ReadFile(opts.File)
	if err != nil {
		w.Errorf("failed to read solution file: %v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Parse solution to extract metadata
	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		w.Errorf("failed to parse solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Determine artifact name (priority: --name flag > metadata.name > filename)
	name := opts.Name
	if name == "" {
		// Try to get name from solution metadata
		if sol.Metadata.Name != "" {
			name = sol.Metadata.Name
		} else {
			// Fall back to filename (e.g., my-solution.yaml -> my-solution)
			base := filepath.Base(opts.File)
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext)
		}
	}

	// Validate name format
	if !catalog.IsValidName(name) {
		w.Errorf("invalid name %q: must be lowercase alphanumeric with hyphens (e.g., 'my-solution')", name)
		return exitcode.Errorf("invalid name")
	}

	// Determine version (priority: --version flag > metadata.version)
	var version *semver.Version
	switch {
	case opts.Version != "":
		// User provided --version flag
		version, err = semver.NewVersion(opts.Version)
		if err != nil {
			w.Errorf("invalid version %q: %v", opts.Version, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		// Warn if overriding metadata version
		if sol.Metadata.Version != nil && !sol.Metadata.Version.Equal(version) {
			w.Warningf("--version %s overrides metadata version %s", version.String(), sol.Metadata.Version.String())
		}
	case sol.Metadata.Version != nil:
		// Use metadata version
		version = sol.Metadata.Version
		lgr.V(1).Info("using version from solution metadata", "version", version.String())
	default:
		// No version available
		w.Error("solution has no version in metadata; use --version to specify one")
		return exitcode.Errorf("no version")
	}

	// Create reference
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: version,
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build annotations
	annotations := catalog.NewAnnotationBuilder().
		Set(catalog.AnnotationSource, opts.File).
		Build()

	// Store the artifact
	info, err := localCatalog.Store(ctx, ref, content, annotations, opts.Force)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("%v\nUse --force to overwrite", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		w.Errorf("failed to store solution: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	lgr.V(1).Info("built solution",
		"name", info.Reference.Name,
		"version", info.Reference.Version.String(),
		"digest", info.Digest)

	w.Successf("Built %s@%s", info.Reference.Name, info.Reference.Version.String())
	w.Infof("  Digest: %s", info.Digest)
	w.Infof("  Catalog: %s", localCatalog.Path())

	return nil
}
