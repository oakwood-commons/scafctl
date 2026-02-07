package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// TagOptions holds options for the tag command.
type TagOptions struct {
	Reference string // Source artifact reference (name@version)
	Alias     string // Alias tag to create (e.g., "stable", "latest")
	Catalog   string // Target catalog for remote tagging (URL or config name, --catalog)
	Kind      string // Artifact kind override (--kind)
	Insecure  bool   // Allow HTTP (--insecure)
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandTag creates the tag command.
func CommandTag(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &TagOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "tag <name@version> <alias>",
		Short: "Create an alias tag for an artifact",
		Long: heredoc.Doc(`
			Create an alias tag for an existing catalog artifact.

			Tags are freeform aliases that point to a specific version of an artifact.
			Common uses include marking releases as "stable", "latest", or "production".

			The source artifact must exist and have a version specified.
			The alias must not be a valid semver version (use 'scafctl build' for that).

			By default, tags the artifact in the local catalog. Use --catalog to
			tag an artifact in a remote registry.

			Examples:
			  # Tag a solution as stable
			  scafctl catalog tag my-solution@1.0.0 stable

			  # Tag as latest
			  scafctl catalog tag my-solution@2.0.0 latest

			  # Tag in a remote registry
			  scafctl catalog tag my-solution@1.0.0 production --catalog ghcr.io/myorg

			  # Tag with explicit kind
			  scafctl catalog tag echo@1.0.0 stable --kind provider
		`),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			options.Alias = args[1]
			return runTag(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

func runTag(ctx context.Context, opts *TagOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Validate alias - must not be a valid semver version
	if err := validateAlias(opts.Alias); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse reference - require version
	name, version := parseNameVersion(opts.Reference)
	if version == "" {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Determine artifact kind
	var artifactKind catalog.ArtifactKind
	if opts.Kind != "" {
		kind, ok := catalog.ParseArtifactKind(opts.Kind)
		if !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
		artifactKind = kind
	} else {
		artifactKind, err = inferKindFromLocalCatalog(ctx, localCatalog, name, version)
		if err != nil {
			w.Errorf("failed to infer artifact kind: %v", err)
			w.Infof("Hint: use --kind to specify the artifact kind explicitly")
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Build reference
	ref, err := catalog.ParseReference(artifactKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Check if this is a remote tag operation
	if opts.Catalog != "" {
		return runTagRemote(ctx, opts, ref)
	}

	// Tag locally
	if err := localCatalog.Tag(ctx, ref, opts.Alias); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in local catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to tag artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Tagged %s@%s as %q", ref.Name, ref.Version.String(), opts.Alias)

	return nil
}

// runTagRemote tags an artifact in a remote registry.
func runTagRemote(ctx context.Context, opts *TagOptions, ref catalog.Reference) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Resolve catalog URL
	catalogURL, err := resolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	registry, repository := parseCatalogURL(catalogURL)

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Create remote catalog
	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            registry,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		Insecure:        opts.Insecure,
		Logger:          *lgr,
	})
	if err != nil {
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Tag in remote
	w.Infof("Tagging %s@%s as %q in %s...", ref.Name, ref.Version.String(), opts.Alias, catalogURL)

	if err := remoteCatalog.Tag(ctx, ref, opts.Alias); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact not found in remote registry")
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to tag artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Tagged %s@%s as %q in %s", ref.Name, ref.Version.String(), opts.Alias, catalogURL)

	return nil
}

// validateAlias checks that an alias tag is valid.
// It must not be empty, must not be a valid semver version, and must not contain
// characters that are invalid in OCI tags.
func validateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias tag cannot be empty")
	}

	// Must not be a valid semver version (those should be created via build)
	if _, err := catalog.ParseReference(catalog.ArtifactKindSolution, "x@"+alias); err == nil {
		return fmt.Errorf("alias %q looks like a semver version; use 'scafctl build' to create versioned artifacts", alias)
	}

	// OCI tag constraints: must match [a-zA-Z0-9_.-]+
	for _, ch := range alias {
		if !isValidTagChar(ch) {
			return fmt.Errorf("alias %q contains invalid character %q; valid characters: letters, digits, '.', '-', '_'", alias, string(ch))
		}
	}

	// Must not start with a dot or hyphen
	if strings.HasPrefix(alias, ".") || strings.HasPrefix(alias, "-") {
		return fmt.Errorf("alias %q must start with a letter, digit, or underscore", alias)
	}

	return nil
}

// isValidTagChar returns true if the rune is valid for an OCI tag.
func isValidTagChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' || ch == '.' || ch == '-'
}
