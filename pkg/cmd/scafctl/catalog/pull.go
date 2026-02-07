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
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

// PullOptions holds options for the pull command.
type PullOptions struct {
	Reference  string // Remote artifact reference
	TargetName string // Optional local name (--as)
	Kind       string // Artifact kind override (--kind)
	Force      bool   // Overwrite existing (--force)
	Insecure   bool   // Allow HTTP (--insecure)
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandPull creates the pull command.
func CommandPull(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PullOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "pull <registry/repository/kind/name[@version]>",
		Short: "Pull an artifact from a remote registry",
		Long: heredoc.Doc(`
			Pull a catalog artifact from a remote OCI registry to the local catalog.

			The reference should include the full path to the artifact:
			  <registry>/<repository>/<kind>/<name>[@version]

			Where:
			  - registry: The OCI registry (e.g., ghcr.io)
			  - repository: The repository path (e.g., myorg/scafctl)
			  - kind: The artifact kind (solutions or plugins)
			  - name: The artifact name
			  - version: Optional version (defaults to latest)

			Examples:
			  # Pull a solution
			  scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0

			  # Pull the latest version
			  scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution

			  # Pull with a different local name
			  scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0 --as local-solution

			  # Force overwrite existing
			  scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0 --force
		`),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runPull(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVar(&options.TargetName, "as", "", "Store with a different local name")
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing local artifact")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

func runPull(ctx context.Context, opts *PullOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse remote reference
	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Override kind if specified
	if opts.Kind != "" {
		kind, ok := catalog.ParseArtifactKind(opts.Kind)
		if !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
		remoteRef.Kind = kind
	}

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Create remote catalog
	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            remoteRef.Registry,
		Registry:        remoteRef.Registry,
		Repository:      remoteRef.Repository,
		CredentialStore: credStore,
		Insecure:        opts.Insecure,
		Logger:          *lgr,
	})
	if err != nil {
		err = fmt.Errorf("failed to create remote catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Convert to local reference
	ref, err := remoteRef.ToReference()
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Resolve to get actual version if not specified
	info, err := remoteCatalog.Resolve(ctx, ref)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in remote registry", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		err = fmt.Errorf("failed to resolve artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	ref = info.Reference

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Prepare copy options
	copyOpts := catalog.CopyOptions{
		TargetName: opts.TargetName,
		Force:      opts.Force,
		OnProgress: func(desc ocispec.Descriptor) {
			lgr.V(1).Info("copying blob",
				"digest", desc.Digest.String(),
				"size", desc.Size)
		},
	}

	// Pull from remote
	displayRef := formatRemoteRef(*remoteRef)
	w.Infof("Pulling %s...", displayRef)

	result, err := remoteCatalog.CopyTo(ctx, ref, localCatalog, copyOpts)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("artifact already exists locally (use --force to overwrite)")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to pull artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build display name
	displayName := ref.Name
	if opts.TargetName != "" {
		displayName = opts.TargetName
	}

	w.Successf("Pulled %s@%s (%s)",
		displayName,
		ref.Version.String(),
		formatBytes(result.Size))

	return nil
}

// formatRemoteRef formats a remote reference for display.
func formatRemoteRef(ref catalog.RemoteReference) string {
	var sb strings.Builder
	sb.WriteString(ref.Registry)
	if ref.Repository != "" {
		sb.WriteString("/")
		sb.WriteString(ref.Repository)
	}
	sb.WriteString("/")
	sb.WriteString(string(ref.Kind))
	sb.WriteString("/")
	sb.WriteString(ref.Name)
	if ref.Tag != "" {
		sb.WriteString("@")
		sb.WriteString(ref.Tag)
	}
	return sb.String()
}
