// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

// PullOptions holds options for the pull command.
type PullOptions struct {
	Reference  string // Artifact reference (short name or full remote)
	Catalog    string // Source catalog (URL or config name, --catalog)
	TargetName string // Optional local name (--as)
	Kind       string // Artifact kind override (--kind)
	Force      bool   // Overwrite existing (--force)
	Insecure   bool   // Allow HTTP (--insecure)
	NoCache    bool   // Invalidate artifact cache after pull (--no-cache)
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
		Use:   "pull <reference>",
		Short: "Pull an artifact from a remote registry",
		Long: heredoc.Docf(`
			Pull a catalog artifact from a remote OCI registry to the local catalog.

			References can be:
			  - Short name:    my-solution@1.0.0  (requires --catalog or default catalog)
			  - Full remote:   ghcr.io/myorg/solutions/my-solution@1.0.0

			If no version is specified, the latest version is pulled.

			When using a short name, the source registry is resolved in this order:
			  1. --catalog flag (URL or configured catalog name)
			  2. Default catalog from config

			Examples:
			  # Pull using the configured default catalog
			  %[1]s catalog pull my-solution

			  # Pull a specific version from the default catalog
			  %[1]s catalog pull my-solution@1.0.0

			  # Pull from a named catalog
			  %[1]s catalog pull my-solution --catalog myregistry

			  # Pull using a full remote reference
			  %[1]s catalog pull ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Pull with a different local name
			  %[1]s catalog pull my-solution@1.0.0 --as local-solution

			  # Force overwrite existing
			  %[1]s catalog pull my-solution@1.0.0 --force
		`, cliParams.BinaryName),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runPull(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.TargetName, "as", "", "Store with a different local name")
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing local artifact")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.NoCache, "no-cache", false, "Invalidate the artifact cache for this artifact after pulling")

	return cmd
}

func runPull(ctx context.Context, opts *PullOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	var ref catalog.Reference
	var registry, repository string
	var err error

	// Validate --kind early (used for local tagging after pull)
	if opts.Kind != "" {
		if _, ok := catalog.ParseArtifactKind(opts.Kind); !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	}

	if looksLikeRemoteReference(opts.Reference) {
		// Full remote reference: ghcr.io/myorg/solutions/my-solution@1.0.0
		remoteRef, parseErr := catalog.ParseRemoteReference(opts.Reference)
		if parseErr != nil {
			w.Errorf("invalid reference: %v", parseErr)
			return exitcode.WithCode(parseErr, exitcode.InvalidInput)
		}

		ref, err = remoteRef.ToReference()
		if err != nil {
			w.Errorf("invalid reference: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		registry = remoteRef.Registry
		repository = remoteRef.Repository

		if opts.Catalog != "" {
			w.Errorf("cannot use --catalog with a full remote reference")
			return exitcode.Errorf("conflicting options")
		}
	} else {
		// Short name: my-solution or my-solution@1.0.0
		name, version := catalog.ParseNameVersion(opts.Reference)

		var artifactKind catalog.ArtifactKind
		if opts.Kind != "" {
			artifactKind, _ = catalog.ParseArtifactKind(opts.Kind) // already validated above
		}

		ref = catalog.Reference{
			Kind: artifactKind,
			Name: name,
		}

		if version != "" {
			ref, err = catalog.ParseReference(ref.Kind, opts.Reference)
			if err != nil {
				w.Errorf("invalid reference: %v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}

		// Resolve catalog URL from flag, config name, or default
		catalogURL, resolveErr := catalog.ResolveCatalogURL(ctx, opts.Catalog)
		if resolveErr != nil {
			w.Errorf("%v", resolveErr)
			return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
		}

		registry, repository = catalog.ParseCatalogURL(catalogURL)
		if registry == "" {
			resolveErr = fmt.Errorf("invalid catalog URL: %s", catalogURL)
			w.Errorf("%v", resolveErr)
			return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
		}
	}

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Resolve auth handler for automatic token bridging
	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)

	// Create remote catalog
	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            registry,
		Registry:        registry,
		Repository:      repository,
		CredentialStore: credStore,
		AuthHandler:     authHandler,
		AuthScope:       authScope,
		Insecure:        opts.Insecure,
		Logger:          *lgr,
	})
	if err != nil {
		err = fmt.Errorf("failed to create remote catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
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
	repoPath := remoteCatalog.RepositoryPath(ref)
	w.Infof("Pulling %s@%s from %s...", ref.Name, ref.VersionOrDigest(), repoPath)

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

	// Apply kind for local tagging after the remote fetch is complete.
	// --kind overrides, otherwise default kindless refs to "solution" so
	// local tags are well-formed (e.g., "solution/name:1.0.0" not "/name:1.0.0").
	if opts.Kind != "" && ref.Kind == "" {
		ref.Kind, _ = catalog.ParseArtifactKind(opts.Kind) // already validated above
	}
	if ref.Kind == "" {
		ref.Kind = catalog.ArtifactKindSolution
	}

	// Build display name
	displayName := ref.Name
	if opts.TargetName != "" {
		displayName = opts.TargetName
	}

	w.Successf("Pulled %s@%s (%s)",
		displayName,
		ref.VersionOrDigest(),
		format.Bytes(result.Size))

	// When --no-cache is set, invalidate any stale artifact cache entry so that
	// subsequent run/render/get commands fetch the freshly pulled artifact from
	// the local catalog rather than a cached copy.
	if opts.NoCache {
		targetName := ref.Name
		if opts.TargetName != "" {
			targetName = opts.TargetName
		}
		version := ""
		if ref.Version != nil {
			version = ref.Version.String()
		}
		if err := cache.InvalidateArtifact(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL, string(ref.Kind), targetName, version); err != nil {
			lgr.V(1).Info("failed to invalidate artifact cache (ignoring)", "error", err)
		} else {
			lgr.V(1).Info("artifact cache invalidated", "kind", ref.Kind, "name", targetName, "version", version)
		}
	}

	return nil
}
