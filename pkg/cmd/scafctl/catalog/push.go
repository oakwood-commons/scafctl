// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
		Use:   "push <name[@version]>",
		Short: "Push an artifact to a remote registry",
		Long: heredoc.Doc(`
			Push a catalog artifact to a remote OCI registry.

			The artifact must exist in the local catalog. Use 'scafctl build'
			to create artifacts from solution files.

			If no version is specified, the latest version is pushed.

			The target registry is resolved in this order:
			  1. --catalog flag (URL or configured catalog name)
			  2. Default catalog from config (set via 'scafctl config use-catalog')

			To configure a default catalog:
			  scafctl config add-catalog myregistry --type oci --url ghcr.io/myorg --default

			Examples:
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
		`),

		Args:         cobra.ExactArgs(1),
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

	return cmd
}

func runPush(ctx context.Context, opts *PushOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse reference
	name, version := catalog.ParseNameVersion(opts.Reference)

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		err = fmt.Errorf("failed to open local catalog: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Determine artifact kind - first try --kind flag, then infer from local catalog
	var artifactKind catalog.ArtifactKind
	if opts.Kind != "" {
		kind, ok := catalog.ParseArtifactKind(opts.Kind)
		if !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
		artifactKind = kind
	} else {
		// Infer kind from local catalog by trying each kind
		artifactKind, err = catalog.InferKindFromLocalCatalog(ctx, localCatalog, name, version)
		if err != nil {
			w.Errorf("failed to infer artifact kind: %v", err)
			w.Infof("Hint: use --kind to specify the artifact kind explicitly")
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Build reference
	ref := catalog.Reference{
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

	// Resolve to get actual version if not specified
	info, err := localCatalog.Resolve(ctx, ref)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in local catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		err = fmt.Errorf("failed to resolve artifact: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	ref = info.Reference

	// Resolve catalog URL from flag, config name, or default
	catalogURL, err := catalog.ResolveCatalogURL(ctx, opts.Catalog)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse target catalog URL
	registry, repository := catalog.ParseCatalogURL(catalogURL)
	if registry == "" {
		err = fmt.Errorf("invalid catalog URL: %s", catalogURL)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

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
	w.Infof("Pushing %s@%s to %s...", ref.Name, ref.Version.String(), catalogURL)

	result, err := remoteCatalog.CopyFrom(ctx, localCatalog, ref, copyOpts)
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("artifact already exists in remote (use --force to overwrite)")
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		err = fmt.Errorf("failed to push artifact: %w", err)
		w.Errorf("%v", err)
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

	return nil
}
