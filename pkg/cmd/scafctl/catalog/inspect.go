// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	catversion "github.com/oakwood-commons/scafctl/pkg/catalog/version"
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
	Reference         string
	Kind              string // Artifact kind (--kind: solution, provider, auth-handler)
	Catalog           string // Remote catalog (URL or config name, --catalog)
	VersionConstraint string // Semver version constraint (--version)
	PreRelease        bool   // Include pre-release versions (--pre-release)
	Referrers         bool   // Show referrers (--referrers)
	ArtifactType      string // Filter referrers by artifact type (--artifact-type)
	Insecure          bool   // Allow HTTP (--insecure)
	CliParams         *settings.Run
	IOStreams         *terminal.IOStreams
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
	Origin      string            `json:"origin,omitempty" yaml:"origin,omitempty"`
	Catalog     string            `json:"catalog" yaml:"catalog"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// CommandInspect creates the inspect command.
func CommandInspect(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &InspectOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}
	options.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:          "inspect <name[@version]>",
		Aliases:      []string{"info", "show"},
		Short:        "Show detailed information about an artifact",
		SilenceUsage: true,
		Long: heredoc.Docf(`
			Show detailed information about a catalog artifact.

			If no version is specified, shows the latest version.

			By default, inspects from the local catalog. Use --catalog to inspect
			from a named remote catalog, or provide a full OCI reference to inspect
			directly from a remote registry.

			Use --referrers to list all artifacts attached to this artifact
			(SBOMs, signatures, provenance, etc.) via the OCI referrers mechanism.

			Examples:
			  # Inspect latest version
			  %[1]s catalog inspect my-solution

			  # Inspect specific version
			  %[1]s catalog inspect my-solution@1.0.0

			  # Inspect a provider plugin in the local catalog
			  %[1]s catalog inspect exec --kind provider

			  # Include pre-release versions when resolving latest or a version range
			  %[1]s catalog inspect exec --kind provider --pre-release

			  # Inspect from a named remote catalog
			  %[1]s catalog inspect my-solution@1.0.0 --catalog my-registry

			  # Inspect via full OCI reference
			  %[1]s catalog inspect ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Output as YAML
			  %[1]s catalog inspect my-solution -o yaml

			  # List referrers (attached artifacts) from a remote catalog
			  %[1]s catalog inspect my-solution@1.0.0 --referrers --catalog ghcr.io/myorg

			  # Filter referrers by type
			  %[1]s catalog inspect my-solution@1.0.0 --referrers --artifact-type application/spdx+json --catalog ghcr.io/myorg
		`, cliParams.BinaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]

			if _, err := parseInspectKind(cmd.Context(), options.Kind); err != nil {
				return err
			}

			// Validate --version constraint vs @version early (before branching)
			if err := validateVersionConstraint(options.Reference, options.VersionConstraint); err != nil {
				w := writer.FromContext(cmd.Context())
				if w != nil {
					w.Errorf("%v", err)
				}
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// --version and --pre-release are not supported with --referrers
			if options.Referrers && options.VersionConstraint != "" {
				w := writer.FromContext(cmd.Context())
				err := fmt.Errorf("--version cannot be used with --referrers")
				if w != nil {
					w.Errorf("%v", err)
				}
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if options.Referrers && options.PreRelease {
				w := writer.FromContext(cmd.Context())
				err := fmt.Errorf("--pre-release cannot be used with --referrers")
				if w != nil {
					w.Errorf("%v", err)
				}
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			if options.Referrers {
				return runInspectReferrers(cmd.Context(), options, kvxOpts)
			}
			return runInspect(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)
	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind (solution, provider, auth-handler)")
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.VersionConstraint, "version", "", "Semver version constraint (e.g., ^1.0.0, ~2.1, >=1.0 <3.0)")
	cmd.Flags().BoolVar(&options.PreRelease, "pre-release", false, "Include pre-release versions (e.g. 1.0.0-beta.1)")
	cmd.Flags().BoolVar(&options.Referrers, "referrers", false, "List artifacts attached via OCI referrers (SBOMs, signatures, etc.)")
	cmd.Flags().StringVar(&options.ArtifactType, "artifact-type", "", "Filter referrers by artifact type (e.g., application/spdx+json)")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	return cmd
}

// allInspectKinds lists the artifact kinds probed in canonical order when
// --kind is omitted.
var allInspectKinds = []catalog.ArtifactKind{
	catalog.ArtifactKindSolution,
	catalog.ArtifactKindProvider,
	catalog.ArtifactKindAuthHandler,
}

func parseInspectKind(ctx context.Context, kindFlag string) (catalog.ArtifactKind, error) {
	if kindFlag == "" {
		return "", nil
	}
	k, ok := catalog.ParseArtifactKind(kindFlag)
	if !ok {
		err := fmt.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", kindFlag)
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return "", exitcode.WithCode(err, exitcode.InvalidInput)
	}
	return k, nil
}

func runInspect(ctx context.Context, opts *InspectOptions, outputOpts *kvx.OutputOptions) error {
	parsedKind, err := parseInspectKind(ctx, opts.Kind)
	if err != nil {
		return err
	}

	if opts.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	// Full OCI reference: inspect directly from a remote registry.
	if looksLikeRemoteReference(opts.Reference) {
		return runInspectRemote(ctx, opts, parsedKind, outputOpts)
	}

	// --catalog flag: inspect from a named remote catalog ("local" routes to local catalog).
	if opts.Catalog != "" && !strings.EqualFold(opts.Catalog, catalog.LocalCatalogName) {
		return runInspectFromCatalog(ctx, opts, parsedKind, outputOpts)
	}

	return runInspectLocal(ctx, opts, parsedKind, outputOpts)
}

func runInspectLocal(ctx context.Context, opts *InspectOptions, parsedKind catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	ref, err := catalog.ParseReference(parsedKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	kinds := allInspectKinds
	if parsedKind != "" {
		kinds = []catalog.ArtifactKind{parsedKind}
	}

	// When --version constraint is set, resolve the best matching version per kind.
	if opts.VersionConstraint != "" {
		var matches []catalog.ArtifactInfo
		var matchedKinds []string
		for _, k := range kinds {
			artifacts, listErr := localCatalog.List(ctx, k, ref.Name)
			if listErr != nil {
				w.Errorf("failed to list versions: %v", listErr)
				return exitcode.WithCode(listErr, exitcode.CatalogError)
			}
			filtered, filterErr := catversion.FilterArtifacts(artifacts, opts.VersionConstraint, opts.PreRelease)
			if filterErr != nil {
				w.Errorf("%v", filterErr)
				return exitcode.WithCode(filterErr, exitcode.InvalidInput)
			}
			if len(filtered) > 0 {
				matches = append(matches, filtered[0])
				matchedKinds = append(matchedKinds, string(k))
			}
		}
		if len(matches) == 0 {
			w.Errorf("no versions of %q match constraint %q", ref.Name, opts.VersionConstraint)
			return exitcode.WithCode(fmt.Errorf("no matching version"), exitcode.FileNotFound)
		}
		if len(matches) > 1 {
			ambErr := fmt.Errorf("artifact %q matches multiple kinds (%s); specify --kind", opts.Reference, strings.Join(matchedKinds, ", "))
			w.Errorf("%v", ambErr)
			return exitcode.WithCode(ambErr, exitcode.InvalidInput)
		}
		w.Verbosef("Version constraint %q resolved to %s", opts.VersionConstraint, matches[0].Reference.Version.Original())
		info, resolveErr := localCatalog.Resolve(ctx, matches[0].Reference)
		if resolveErr != nil {
			if catalog.IsNotFound(resolveErr) {
				w.Errorf("artifact %q not found in catalog", opts.Reference)
				return exitcode.WithCode(resolveErr, exitcode.FileNotFound)
			}
			w.Errorf("failed to resolve artifact: %v", resolveErr)
			return exitcode.WithCode(resolveErr, exitcode.CatalogError)
		}
		return writeInspectDetail(info, outputOpts)
	}

	var matches []catalog.ArtifactInfo
	var matchedKinds []string
	var lastNotFound error
	for _, k := range kinds {
		candidate := ref
		candidate.Kind = k
		info, resolveErr := localCatalog.Resolve(ctx, candidate)
		if resolveErr != nil {
			if catalog.IsNotFound(resolveErr) {
				lastNotFound = resolveErr
				continue
			}
			w.Errorf("failed to resolve artifact: %v", resolveErr)
			return exitcode.WithCode(resolveErr, exitcode.CatalogError)
		}
		matches = append(matches, info)
		matchedKinds = append(matchedKinds, string(k))
	}

	if len(matches) == 0 {
		if lastNotFound == nil {
			lastNotFound = &catalog.ArtifactNotFoundError{Reference: ref, Catalog: catalog.LocalCatalogName}
		}
		w.Errorf("artifact %q not found in catalog", opts.Reference)
		return exitcode.WithCode(lastNotFound, exitcode.FileNotFound)
	}
	if len(matches) > 1 {
		ambErr := fmt.Errorf("artifact %q matches multiple kinds (%s); specify --kind", opts.Reference, strings.Join(matchedKinds, ", "))
		w.Errorf("%v", ambErr)
		return exitcode.WithCode(ambErr, exitcode.InvalidInput)
	}

	return writeInspectDetail(matches[0], outputOpts)
}

// runInspectRemote inspects an artifact from a full OCI reference.
func runInspectRemote(ctx context.Context, opts *InspectOptions, parsedKind catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	if opts.Catalog != "" {
		w.Error("cannot use --catalog with a full remote reference")
		return exitcode.Errorf("conflicting options")
	}

	w.Verbose("Inspecting from full OCI reference")

	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if opts.VersionConstraint != "" && remoteRef.Tag != "" {
		conflictErr := fmt.Errorf("%w in reference %q; use one or the other", ErrVersionConstraintConflict, opts.Reference)
		w.Errorf("%v", conflictErr)
		return exitcode.WithCode(conflictErr, exitcode.InvalidInput)
	}

	if parsedKind != "" && remoteRef.Kind != "" && remoteRef.Kind != parsedKind {
		conflictErr := fmt.Errorf("--kind %q conflicts with artifact kind %q in remote reference %q", parsedKind, remoteRef.Kind, opts.Reference)
		w.Errorf("%v", conflictErr)
		return exitcode.WithCode(conflictErr, exitcode.InvalidInput)
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if parsedKind != "" && ref.Kind == "" {
		ref.Kind = parsedKind
	}

	registry := remoteRef.Registry
	repository := remoteRef.Repository

	verboseRefInfo(w, remoteRef.Name, string(ref.Kind), remoteRef.Tag)

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, "")
	authScope := catalog.AuthScopeForRegistry(ctx, registry)

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
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// When --version constraint is set, resolve the best matching version
	allowKindlessFallback := remoteRef.Kind == "" && parsedKind == ""
	if opts.VersionConstraint != "" {
		resolved, constraintErr := resolveInspectRemoteConstraint(ctx, w, remoteCatalog, registry, opts.Reference, ref, opts.VersionConstraint, opts.PreRelease)
		if constraintErr != nil {
			return constraintErr
		}
		ref = resolved
		allowKindlessFallback = false
	}

	info, err := resolveInspectRemote(
		ctx,
		w,
		remoteCatalog,
		registry,
		fmt.Sprintf("artifact %q not found in remote catalog", opts.Reference),
		opts.Reference,
		ref,
		allowKindlessFallback,
	)
	if err != nil {
		return err
	}

	return writeInspectDetail(info, outputOpts)
}

// runInspectFromCatalog inspects an artifact from a named remote catalog.
func runInspectFromCatalog(ctx context.Context, opts *InspectOptions, parsedKind catalog.ArtifactKind, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	w.Verbosef("Inspecting from catalog %q", opts.Catalog)

	ref, err := catalog.ParseReference(parsedKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	version := ""
	if ref.Version != nil {
		version = ref.Version.String()
	}
	verboseRefInfo(w, ref.Name, string(ref.Kind), version)

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

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, registry, opts.Catalog)

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
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// When --version constraint is set, resolve the best matching version
	if opts.VersionConstraint != "" {
		resolved, constraintErr := resolveInspectRemoteConstraint(ctx, w, remoteCatalog, registry, opts.Reference, ref, opts.VersionConstraint, opts.PreRelease)
		if constraintErr != nil {
			return constraintErr
		}
		ref = resolved
	}

	info, err := resolveInspectRemote(
		ctx,
		w,
		remoteCatalog,
		registry,
		fmt.Sprintf("artifact %q not found in remote catalog %q", opts.Reference, opts.Catalog),
		opts.Reference,
		ref,
		false,
	)
	if err != nil {
		return err
	}

	return writeInspectDetail(info, outputOpts)
}

func resolveInspectRemote(
	ctx context.Context,
	w *writer.Writer,
	remoteCatalog *catalog.RemoteCatalog,
	registry, notFoundMsg, rawRef string,
	ref catalog.Reference,
	allowKindlessFallback bool,
) (catalog.ArtifactInfo, error) {
	if ref.Kind != "" {
		info, err := remoteCatalog.Resolve(ctx, ref)
		if err != nil {
			if catalog.IsNotFound(err) {
				w.Errorf("%s", notFoundMsg)
				return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.FileNotFound)
			}
			w.Errorf("failed to resolve artifact: %v", err)
			hintOnAuthError(ctx, w, registry, err)
			return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.CatalogError)
		}
		return info, nil
	}

	var matches []catalog.ArtifactInfo
	var matchedKinds []string
	var lastNotFound error
	for _, k := range allInspectKinds {
		candidate := ref
		candidate.Kind = k
		info, err := remoteCatalog.Resolve(ctx, candidate)
		if err != nil {
			if catalog.IsNotFound(err) {
				lastNotFound = err
				continue
			}
			w.Errorf("failed to resolve artifact: %v", err)
			hintOnAuthError(ctx, w, registry, err)
			return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.CatalogError)
		}
		matches = append(matches, info)
		matchedKinds = append(matchedKinds, string(k))
	}

	if len(matches) > 1 {
		ambErr := fmt.Errorf("artifact %q matches multiple kinds (%s); specify --kind", rawRef, strings.Join(matchedKinds, ", "))
		w.Errorf("%v", ambErr)
		return catalog.ArtifactInfo{}, exitcode.WithCode(ambErr, exitcode.InvalidInput)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	if allowKindlessFallback {
		info, err := remoteCatalog.Resolve(ctx, ref)
		if err == nil {
			return info, nil
		}
		if !catalog.IsNotFound(err) {
			w.Errorf("failed to resolve artifact: %v", err)
			hintOnAuthError(ctx, w, registry, err)
			return catalog.ArtifactInfo{}, exitcode.WithCode(err, exitcode.CatalogError)
		}
		lastNotFound = err
	}

	if lastNotFound == nil {
		lastNotFound = &catalog.ArtifactNotFoundError{Reference: ref, Catalog: registry}
	}
	w.Errorf("%s", notFoundMsg)
	return catalog.ArtifactInfo{}, exitcode.WithCode(lastNotFound, exitcode.FileNotFound)
}

func resolveInspectRemoteConstraint(
	ctx context.Context,
	w *writer.Writer,
	remoteCatalog *catalog.RemoteCatalog,
	registry, rawRef string,
	ref catalog.Reference,
	constraint string,
	includePreRelease bool,
) (catalog.Reference, error) {
	kinds := allInspectKinds
	if ref.Kind != "" {
		kinds = []catalog.ArtifactKind{ref.Kind}
	}

	var matches []catalog.ArtifactInfo
	var matchedKinds []string
	for _, k := range kinds {
		artifacts, err := remoteCatalog.List(ctx, k, ref.Name)
		if err != nil {
			if catalog.IsNotFound(err) {
				continue
			}
			w.Errorf("failed to list versions: %v", err)
			hintOnAuthError(ctx, w, registry, err)
			return ref, exitcode.WithCode(err, exitcode.CatalogError)
		}

		filtered, filterErr := catversion.FilterArtifacts(artifacts, constraint, includePreRelease)
		if filterErr != nil {
			w.Errorf("%v", filterErr)
			return ref, exitcode.WithCode(filterErr, exitcode.InvalidInput)
		}
		if len(filtered) > 0 {
			matches = append(matches, filtered[0])
			matchedKinds = append(matchedKinds, string(k))
		}
	}

	if len(matches) == 0 {
		noMatchErr := fmt.Errorf("no versions of %q match constraint %q", ref.Name, constraint)
		w.Errorf("%v", noMatchErr)
		return ref, exitcode.WithCode(noMatchErr, exitcode.FileNotFound)
	}
	if len(matches) > 1 {
		ambErr := fmt.Errorf("artifact %q matches multiple kinds (%s); specify --kind", rawRef, strings.Join(matchedKinds, ", "))
		w.Errorf("%v", ambErr)
		return ref, exitcode.WithCode(ambErr, exitcode.InvalidInput)
	}

	w.Verbosef("Version constraint %q resolved to %s", constraint, matches[0].Reference.Version.Original())
	return matches[0].Reference, nil
}

// writeInspectDetail formats and writes artifact inspect output.
func writeInspectDetail(info catalog.ArtifactInfo, outputOpts *kvx.OutputOptions) error {
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
		Origin:      info.Annotations[catalog.AnnotationOrigin],
		Catalog:     info.Catalog,
		Annotations: info.Annotations,
	}

	// Convert struct to map[string]any so CEL expressions can access fields.
	detailMap, err := kvx.StructToMap(detail)
	if err != nil {
		return fmt.Errorf("failed to normalize artifact detail: %w", err)
	}

	return outputOpts.Write(detailMap)
}

// runInspectReferrers lists OCI referrers for a remote artifact.
func runInspectReferrers(ctx context.Context, opts *InspectOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	parsedKind, err := parseInspectKind(ctx, opts.Kind)
	if err != nil {
		return err
	}

	if opts.PreRelease {
		preErr := fmt.Errorf("--pre-release cannot be used with --referrers")
		if w != nil {
			w.Errorf("%v", preErr)
		}
		return exitcode.WithCode(preErr, exitcode.InvalidInput)
	}

	if opts.Catalog == "" {
		w.Error("--catalog is required with --referrers (referrers are a remote registry feature)")
		return exitcode.Errorf("catalog required")
	}
	if strings.EqualFold(opts.Catalog, catalog.LocalCatalogName) {
		localErr := fmt.Errorf("--referrers cannot be used with the local catalog (referrers are a remote registry feature)")
		w.Errorf("%v", localErr)
		return exitcode.WithCode(localErr, exitcode.InvalidInput)
	}

	ref, err := catalog.ParseReference(parsedKind, opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
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

	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	authHandler := resolveAuthHandler(ctx, registry, opts.Catalog)
	authScope := resolveAuthScope(ctx, registry, opts.Catalog)

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
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if ref.Kind == "" || (!ref.HasVersion() && !ref.HasDigest()) {
		info, resolveErr := resolveInspectRemote(
			ctx,
			w,
			remoteCatalog,
			registry,
			fmt.Sprintf("artifact %q not found in remote catalog", opts.Reference),
			opts.Reference,
			ref,
			ref.Kind == "",
		)
		if resolveErr != nil {
			return resolveErr
		}
		ref = info.Reference
	}

	referrers, err := remoteCatalog.Referrers(ctx, ref, opts.ArtifactType)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in remote catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to list referrers: %v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if len(referrers) == 0 {
		w.Infof("No referrers found for %s", opts.Reference)
		return nil
	}

	return outputOpts.Write(referrers)
}
