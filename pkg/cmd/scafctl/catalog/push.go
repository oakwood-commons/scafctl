// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
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

const pushCatalogFlagUsage = `Destination catalog to push to (registry URL or configured catalog name). Required.`

const pushOriginFlagUsage = `Select the local copy by where it came from: "` + catalog.OriginBuilt + `" or the source catalog (e.g. ghcr.io/myorg)`

// PushOptions holds options for the push command.
type PushOptions struct {
	Reference  string // Local source artifact (name[@version|@digest] or registry/repo/<kinds>/name[@version])
	Catalog    string // Destination catalog (URL or configured catalog name); required
	TargetName string // Optional target name (--as)
	Kind       string // Artifact kind (--kind)
	Origin     string // Source-copy selector (--origin)
	Force      bool   // Overwrite existing (--force)
	DryRun     bool   // Show what would be pushed without pushing (--dry-run)
	Insecure   bool   // Allow HTTP (--insecure)
	NoSBOM     bool   // Explicitly disable SBOM generation (--no-sbom)
	Latest     bool   // Tag the pushed version as "latest" (--latest)
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
		Short: "Push a local artifact to a remote registry",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Push an artifact from the local catalog to a remote OCI registry.

			The positional reference always selects the local source artifact;
			--catalog is always the destination and is required (the default
			catalog is never used as a push target).

			Source references:
			  - my-solution                 latest version
			  - my-solution@1.0.0           exact version
			  - my-solution@sha256:...      exact content
			  - ghcr.io/myorg/solutions/my-solution@1.0.0
			                                the local copy pulled from that remote
			                                (shorthand for --origin ghcr.io/myorg)

			The local catalog can hold several copies with the same name and
			version (built locally, pulled from different catalogs). Push never
			picks one silently: when the reference matches more than one, it
			fails and prints a command for each copy. Select one with --origin,
			--kind, or a digest.

			The artifact is published as <catalog>/<kinds>/<name>:<version>
			with its content unchanged. Nothing about where the local copy came
			from is published. A pulled copy cannot be pushed back to the
			catalog it was pulled from.

			Examples:
			  # Push to a registry URL
			  scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg

			  # Push to a named catalog from config
			  scafctl catalog push my-solution@1.0.0 --catalog myregistry

			  # Push the locally built copy when a pulled copy also exists
			  scafctl catalog push my-solution@1.0.0 --origin built --catalog ghcr.io/myorg

			  # Mirror a copy pulled from one registry to another
			  scafctl catalog push ghcr.io/upstream/solutions/my-solution@1.0.0 --catalog ghcr.io/myorg

			  # Publish under a different name
			  scafctl catalog push my-solution@1.0.0 --as production-solution --catalog ghcr.io/myorg

			  # Overwrite an existing version
			  scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg --force
		`), settings.CliBinaryName, cliParams.BinaryName),

		Args:         flags.RequireArg("name@version", cliParams.BinaryName+" catalog push my-solution@1.0.0 --catalog <registry-url|catalog-name>"),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runPush(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", pushCatalogFlagUsage)
	cmd.Flags().StringVar(&options.TargetName, "as", "", "Publish under a different artifact name")
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind (solution, provider, auth-handler); required when the name exists as several kinds")
	cmd.Flags().StringVar(&options.Origin, "origin", "", pushOriginFlagUsage)
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing artifact in remote")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be pushed without actually pushing")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.Flags().BoolVar(&options.NoSBOM, "no-sbom", false, "Disable automatic SBOM generation (solutions generate SBOMs by default)")
	cmd.Flags().BoolVar(&options.Latest, "latest", false, "Tag the pushed version as 'latest' in the remote registry")

	return cmd
}

func runPush(ctx context.Context, opts *PushOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// The destination is always explicit: never fall back to the default catalog.
	if strings.TrimSpace(opts.Catalog) == "" {
		err := catalog.NewPushDestinationRequiredError(ctx)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	sel, err := catalog.ParsePushSelector(opts.Reference, opts.Kind, opts.Origin)
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Open the local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Select the source exactly once; every later step uses info.Digest.
	info, err := localCatalog.ResolveForPush(ctx, sel)
	if err != nil {
		return pushResolveError(w, opts.CliParams.BinaryName, err)
	}
	verboseRefInfo(w, info.Reference.Name, info.Reference.Kind.String(), info.Reference.VersionOrDigest())

	target, err := catalog.PushTarget(info, opts.TargetName)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	registry, repository := catalog.ParseCatalogURL(catalogURL)
	if registry == "" {
		err = fmt.Errorf("invalid catalog URL: %s", catalogURL)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Build a preview remote catalog for destination validation and display.
	// Construction does no network or credential work (auth is wired in
	// below, only once we know we're not in --dry-run), so it's safe to do
	// this before resolving credentials/auth, which can have side effects
	// (e.g. auth.GetHandler may fetch and cache a plugin).
	previewCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       registry,
		Registry:   registry,
		Repository: repository,
		Insecure:   opts.Insecure,
		Logger:     *lgr,
	})
	if err != nil {
		err = fmt.Errorf("failed to create remote catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if err := catalog.ValidatePushDestination(info, previewCatalog.CanonicalID()); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	source := fmt.Sprintf("%s@%s (%s, %s)", info.Reference.Name, info.Reference.VersionOrDigest(),
		pushOriginLabel(info.Canonical), info.Digest)
	dest := previewCatalog.RepositoryPath(target) + ":" + target.RemoteTag()

	// Dry-run mode: show what would be pushed and return, before resolving
	// any credentials/auth.
	if opts.DryRun {
		w.Infof("Would push %s to %s", source, dest)
		if opts.Force {
			w.Infof("  --force: would overwrite if exists")
		}
		if opts.Latest {
			w.Infof("  --latest: would tag as \"latest\"")
		}
		return nil
	}

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Resolve auth provider for automatic token bridging
	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, opts.Catalog)

	verboseRemoteInfo(ctx, w, registry, repository, authHandlerName(authHandler), authScope)

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

	copyOpts := catalog.CopyOptions{
		TargetName: opts.TargetName,
		Force:      opts.Force,
		OnProgress: func(desc ocispec.Descriptor) {
			lgr.V(1).Info("copying blob",
				"digest", desc.Digest.String(),
				"size", desc.Size)
		},
	}

	w.Infof("Pushing %s to %s...", source, dest)

	result, err := remoteCatalog.CopyFrom(ctx, localCatalog, info, copyOpts)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("%s already exists (use --force to overwrite)", dest)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to push artifact: %w", err)
		w.Errorf("%v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Pushed %s (%s)", dest, format.Bytes(result.Size))

	// Auto-generate and attach SBOM for solutions (unless --no-sbom)
	if shouldAttachSBOM(info.Reference.Kind, opts.NoSBOM) {
		if err := attachSBOM(ctx, opts, localCatalog, remoteCatalog, info, result.Reference); err != nil {
			w.Warningf("SBOM attachment failed: %v", err)
			// Non-fatal: the push itself succeeded
		}
	}

	// Tag as "latest" in the remote registry if requested.
	// This intentionally bypasses catalog.ValidateAlias (which rejects "latest")
	// because push --latest is a publish-time concern, not a manual alias operation.
	if opts.Latest {
		display := result.Reference.Name + "@" + result.Reference.VersionOrDigest()
		w.Infof("Tagging %s as \"latest\"...", display)
		if _, tagErr := remoteCatalog.Tag(ctx, result.Reference, "latest"); tagErr != nil {
			w.Warningf("failed to tag as latest: %v", tagErr)
			// Non-fatal: the push itself succeeded
		} else {
			w.Successf("Tagged %s as \"latest\"", display)
		}
	}

	return nil
}

// pushResolveError reports a ResolveForPush failure with its exit code.
// Ambiguity errors are followed by one copy-pasteable command per candidate.
func pushResolveError(w *writer.Writer, binaryName string, err error) error {
	w.Errorf("%v", err)

	var ambArtifact *catalog.AmbiguousArtifactError
	var ambKind *catalog.AmbiguousKindError
	var hints []string
	switch {
	case errors.As(err, &ambArtifact):
		hints = ambArtifact.Hints(binaryName)
	case errors.As(err, &ambKind):
		hints = ambKind.Hints(binaryName)
	}
	if len(hints) > 0 {
		w.Infof("Select one with:")
		for _, h := range hints {
			w.Infof("  %s", h)
		}
	}

	switch {
	case catalog.IsAmbiguous(err), catalog.IsInvalidReference(err):
		return exitcode.WithCode(err, exitcode.InvalidInput)
	case catalog.IsNotFound(err):
		return exitcode.WithCode(err, exitcode.FileNotFound)
	default:
		return exitcode.WithCode(fmt.Errorf("failed to resolve artifact: %w", err), exitcode.CatalogError)
	}
}

// pushOriginLabel renders a source canonical for push output.
func pushOriginLabel(canonical string) string {
	if canonical == "" {
		return "built locally"
	}
	return "from " + canonical
}

// shouldAttachSBOM reports whether an SBOM should be generated and attached
// for the given artifact kind. SBOMs are generated by default for solutions
// (or when kind is unset) and can be opted out with --no-sbom.
func shouldAttachSBOM(kind catalog.ArtifactKind, noSBOM bool) bool {
	if noSBOM {
		return false
	}
	return kind == catalog.ArtifactKindSolution || kind == ""
}

// attachSBOM generates an SPDX SBOM from the selected local solution (by
// digest, so it describes exactly what was pushed) and attaches it as a
// referrer to the pushed remote artifact at target.
func attachSBOM(ctx context.Context, opts *PushOptions, localCatalog *catalog.LocalCatalog, remoteCatalog *catalog.RemoteCatalog, info catalog.ArtifactInfo, target catalog.Reference) error {
	w := writer.FromContext(ctx)

	source := catalog.Reference{Kind: info.Reference.Kind, Name: info.Reference.Name, Digest: info.Digest}
	contentData, _, err := localCatalog.Fetch(ctx, source)
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

	version := target.VersionOrDigest()
	w.Infof("Attaching SBOM to %s@%s...", target.Name, version)

	desc, err := remoteCatalog.Attach(ctx, target, sbom.MediaType, sbomData, map[string]string{
		"org.opencontainers.image.title": fmt.Sprintf("%s-%s.spdx.json", target.Name, version),
	})
	if err != nil {
		return fmt.Errorf("failed to attach SBOM: %w", err)
	}

	w.Successf("SBOM attached (%s)", desc.Digest.String())
	return nil
}
