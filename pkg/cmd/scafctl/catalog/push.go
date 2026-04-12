// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/sbom"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

const catalogFlagUsage = `Target catalog (registry URL or configured catalog name). If not specified, uses the default catalog from config.`

// PushOptions holds options for the push command.
type PushOptions struct {
	Reference  string // Artifact reference (name@version)
	Catalog    string // Target catalog (URL or config name, --catalog)
	TargetName string // Optional target name (--as)
	Kind       string // Artifact kind override (--kind)
	Force      bool   // Overwrite existing (--force)
	Insecure   bool   // Allow HTTP (--insecure)
	SBOM       bool   // Auto-generate and attach SBOM (--sbom)
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandPush creates the push command.
func CommandPush(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &PushOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "push <reference>",
		Short: "Push an artifact to a remote registry",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Push a catalog artifact to a remote OCI registry.

			The artifact must exist in the local catalog. Use 'scafctl build'
			to create artifacts from solution files.

			References can be:
			  - Local name:    my-solution@1.0.0  (requires --catalog or default catalog)
			  - Full remote:   ghcr.io/myorg/solutions/my-solution@1.0.0

			If no version is specified, the latest version is pushed.

			When using a local name, the target registry is resolved in this order:
			  1. --catalog flag (URL or configured catalog name)
			  2. Default catalog from config (set via 'scafctl config use-catalog')

			To configure a default catalog:
			  scafctl config add-catalog myregistry --type oci --url ghcr.io/myorg --default

			Examples:
			  # Push using a full remote reference
			  scafctl catalog push ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Push using the configured default catalog
			  scafctl catalog push my-solution@1.0.0

			  # Push to a specific registry URL
			  scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg

			  # Push to a named catalog from config
			  scafctl catalog push my-solution@1.0.0 --catalog myregistry

			  # Push with a different name
			  scafctl catalog push my-solution@1.0.0 --as production-solution

			  # Force overwrite existing
			  scafctl catalog push my-solution@1.0.0 --force
		`), settings.CliBinaryName, cliParams.BinaryName),

		Args:         flags.RequireArg("name@version", cliParams.BinaryName+" catalog push my-solution@1.0.0"),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runPush(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.TargetName, "as", "", "Push with a different artifact name")
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing artifact in remote")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.SBOM, "sbom", false, "Auto-generate and attach an SPDX SBOM after pushing")

	return cmd
}

func runPush(ctx context.Context, opts *PushOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Determine if this is a remote reference (contains /) or a local name
	var ref catalog.Reference
	var registry, repository string

	if looksLikeRemoteReference(opts.Reference) {
		// Remote reference: ghcr.io/myorg/solutions/hello-world@0.1.0
		remoteRef, parseErr := catalog.ParseRemoteReference(opts.Reference)
		if parseErr != nil {
			w.Errorf("invalid reference: %v", parseErr)
			return exitcode.WithCode(parseErr, exitcode.InvalidInput)
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

		// Convert to local reference for catalog lookup
		ref, err = remoteRef.ToReference()
		if err != nil {
			w.Errorf("invalid reference: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		// If kind wasn't in the remote ref path, infer from local catalog
		if ref.Kind == "" {
			version := ""
			if ref.Version != nil {
				version = ref.Version.String()
			}

			ref.Kind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, ref.Name, version)
			if err != nil {
				w.Errorf("failed to infer artifact kind: %v", err)
				w.Infof("Hint: use --kind to specify the artifact kind explicitly")
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}

		// Extract registry/repository from the remote reference
		registry = remoteRef.Registry
		repository = remoteRef.Repository

		// --catalog flag conflicts with a full remote reference
		if opts.Catalog != "" {
			w.Errorf("cannot use --catalog with a full remote reference")
			return exitcode.Errorf("conflicting options")
		}
	} else {
		// Local name: hello-world@0.1.0
		name, version := catalog.ParseNameVersion(opts.Reference)

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
			artifactKind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, name, version)
			if err != nil {
				w.Errorf("failed to infer artifact kind: %v", err)
				w.Infof("Hint: use --kind to specify the artifact kind explicitly")
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}

		// Build reference
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

		// Parse target catalog URL
		registry, repository = catalog.ParseCatalogURL(catalogURL)
		if registry == "" {
			resolveErr = fmt.Errorf("invalid catalog URL: %s", catalogURL)
			w.Errorf("%v", resolveErr)
			return exitcode.WithCode(resolveErr, exitcode.InvalidInput)
		}
	}

	// Resolve to get actual version if not specified
	info, err := localCatalog.Resolve(ctx, ref)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in local catalog", ref.Name)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		err = fmt.Errorf("failed to resolve artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	ref = info.Reference

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

	// Push to remote
	repoPath := remoteCatalog.RepositoryPath(ref)
	w.Infof("Pushing %s@%s to %s...", ref.Name, ref.Version.String(), repoPath)

	result, err := remoteCatalog.CopyFrom(ctx, localCatalog, ref, copyOpts)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("artifact already exists in remote (use --force to overwrite)")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to push artifact: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build display name
	displayName := ref.Name
	if opts.TargetName != "" {
		displayName = opts.TargetName
	}

	w.Successf("Pushed %s@%s (%s)",
		displayName,
		ref.Version.String(),
		format.Bytes(result.Size))

	// Auto-generate and attach SBOM if requested
	if opts.SBOM {
		if err := attachSBOM(ctx, opts, localCatalog, remoteCatalog, ref); err != nil {
			w.Warningf("SBOM attachment failed: %v", err)
			// Non-fatal: the push itself succeeded
		}
	}

	return nil
}

// attachSBOM generates an SPDX SBOM from the local solution content and
// attaches it as a referrer to the pushed remote artifact.
func attachSBOM(ctx context.Context, opts *PushOptions, localCatalog *catalog.LocalCatalog, remoteCatalog *catalog.RemoteCatalog, ref catalog.Reference) error {
	w := writer.FromContext(ctx)

	// Only solutions get SBOMs (providers/auth-handlers are opaque binaries)
	if ref.Kind != catalog.ArtifactKindSolution && ref.Kind != "" {
		w.Infof("SBOM generation skipped: only solution artifacts support SBOM")
		return nil
	}

	// Fetch the solution content from local catalog
	contentData, _, err := localCatalog.Fetch(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to fetch local content for SBOM: %w", err)
	}

	// Parse solution
	var sol solution.Solution
	if err := sol.UnmarshalFromBytes(contentData); err != nil {
		return fmt.Errorf("failed to parse solution for SBOM: %w", err)
	}

	// Generate SBOM
	sbomData, err := sbom.Generate(&sol, sbom.GenerateOptions{
		BinaryName: opts.CliParams.BinaryName,
	})
	if err != nil {
		return fmt.Errorf("failed to generate SBOM: %w", err)
	}

	// Attach to remote
	w.Infof("Attaching SBOM to %s@%s...", ref.Name, ref.Version.String())

	desc, err := remoteCatalog.Attach(ctx, ref, sbom.MediaType, sbomData, map[string]string{
		"org.opencontainers.image.title": fmt.Sprintf("%s-%s.spdx.json", ref.Name, ref.Version.String()),
	})
	if err != nil {
		return fmt.Errorf("failed to attach SBOM: %w", err)
	}

	w.Successf("SBOM attached (%s)", desc.Digest.String())
	return nil
}
