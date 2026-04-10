// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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

// InspectOptions holds options for the inspect command.
type InspectOptions struct {
	Reference    string
	Catalog      string // Remote catalog (URL or config name, --catalog)
	Referrers    bool   // Show referrers (--referrers)
	ArtifactType string // Filter referrers by artifact type (--artifact-type)
	Insecure     bool   // Allow HTTP (--insecure)
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams
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
		Use:          "inspect <name[@version]>",
		Aliases:      []string{"info", "show"},
		Short:        "Show detailed information about an artifact",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Show detailed information about a catalog artifact.

			If no version is specified, shows the latest version.

			Use --referrers to list all artifacts attached to this artifact
			(SBOMs, signatures, provenance, etc.) via the OCI referrers mechanism.

			Examples:
			  # Inspect latest version
			  scafctl catalog inspect my-solution

			  # Inspect specific version
			  scafctl catalog inspect my-solution@1.0.0

			  # Output as YAML
			  scafctl catalog inspect my-solution -o yaml

			  # List referrers (attached artifacts) from a remote catalog
			  scafctl catalog inspect my-solution@1.0.0 --referrers --catalog ghcr.io/myorg

			  # Filter referrers by type
			  scafctl catalog inspect my-solution@1.0.0 --referrers --artifact-type application/spdx+json --catalog ghcr.io/myorg
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			if options.Referrers {
				return runInspectReferrers(cmd.Context(), options, kvxOpts)
			}
			return runInspect(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&options.Referrers, "referrers", false, "List artifacts attached via OCI referrers (SBOMs, signatures, etc.)")
	cmd.Flags().StringVar(&options.ArtifactType, "artifact-type", "", "Filter referrers by artifact type (e.g., application/spdx+json)")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

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

	if opts.Catalog == "" {
		w.Error("--catalog is required with --referrers (referrers are a remote registry feature)")
		return exitcode.Errorf("catalog required")
	}

	ref, err := catalog.ParseReference("", opts.Reference)
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
	authScope := resolveAuthScope(ctx, opts.Catalog)

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
