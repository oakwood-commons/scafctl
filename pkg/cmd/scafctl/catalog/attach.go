// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"os"
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

// AttachOptions holds options for the attach command.
type AttachOptions struct {
	Reference    string // Subject artifact reference (name@version)
	File         string // Path to the file to attach (--file)
	ArtifactType string // Media type of the attachment (--type)
	Catalog      string // Target catalog (URL or config name, --catalog)
	Insecure     bool   // Allow HTTP (--insecure)
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams
}

// CommandAttach creates the attach command.
func CommandAttach(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &AttachOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "attach <name@version>",
		Short: "Attach an artifact (e.g., SBOM) to a remote catalog entry",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Attach a file to an existing artifact in a remote OCI registry using
			the OCI referrers mechanism.

			The attached file becomes a separate OCI artifact whose manifest
			references the subject artifact via the Subject field. Registries
			that support the OCI Referrers API automatically index these
			relationships.

			Common attachment types:
			  - SBOM:       application/spdx+json
			  - Signature:  application/vnd.cncf.notary.signature
			  - Provenance: application/vnd.in-toto+json

			Examples:
			  # Attach an SPDX SBOM
			  scafctl catalog attach my-solution@1.0.0 --file sbom.spdx.json --type application/spdx+json

			  # Attach to a specific catalog
			  scafctl catalog attach my-solution@1.0.0 --file sbom.spdx.json --type application/spdx+json --catalog ghcr.io/myorg
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			return runAttach(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.File, "file", "f", "", "Path to the file to attach (required)")
	cmd.Flags().StringVarP(&options.ArtifactType, "type", "t", "", "Media type of the attachment (required)")
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

func runAttach(ctx context.Context, opts *AttachOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse reference
	name, version := catalog.ParseNameVersion(opts.Reference)
	if version == "" {
		w.Error("version required: use format 'name@version' (e.g., 'my-solution@1.0.0')")
		return exitcode.Errorf("version required")
	}

	ref, err := catalog.ParseReference("", opts.Reference)
	if err != nil {
		w.Errorf("invalid reference %q: %v", opts.Reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Read attachment file
	data, err := os.ReadFile(opts.File)
	if err != nil {
		w.Errorf("failed to read file %q: %v", opts.File, err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Resolve catalog URL
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
		w.Errorf("failed to create remote catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Infof("Attaching %s to %s@%s...", opts.ArtifactType, name, version)

	desc, err := remoteCatalog.Attach(ctx, ref, opts.ArtifactType, data, nil)
	if err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in remote catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to attach artifact: %v", err)
		hintOnAuthError(ctx, w, registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Attached %s (%s)", opts.ArtifactType, desc.Digest.String())

	return nil
}
