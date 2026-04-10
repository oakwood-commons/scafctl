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

// TagsOptions holds options for the tags command.
type TagsOptions struct {
	Reference string // Remote artifact reference
	Kind      string // Artifact kind override (--kind)
	Insecure  bool   // Allow HTTP (--insecure)
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// TagsListItem represents a tag in tags output.
type TagsListItem struct {
	Tag      string `json:"tag" yaml:"tag"`
	IsSemver bool   `json:"isSemver" yaml:"isSemver"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
}

// CommandTags creates the tags command.
func CommandTags(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &TagsOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "tags <registry/repository[/kind]/name>",
		Short: "List tags for a remote artifact",
		Long: heredoc.Docf(`
			List all tags (versions and aliases) for an artifact in a remote OCI registry.

			The reference should include the full path to the artifact without a version:
			  <registry>/<repository>/<kind>/<name>

			The kind segment may be omitted for Docker-style repositories where the
			artifact lives directly under the repository path. Use --kind to specify
			the artifact kind when it is not part of the path.

			Returns both semver version tags and alias tags (e.g., "stable", "latest").

			Examples:
			  # List tags for a remote solution (kind in path)
			  %[1]s catalog tags ghcr.io/myorg/scafctl/solutions/my-solution

			  # List tags for a Docker-style ref (kind omitted)
			  %[1]s catalog tags ghcr.io/myorg/my-solution

			  # List tags with explicit kind
			  %[1]s catalog tags ghcr.io/myorg/my-solution --kind solution

			  # Output as JSON
			  %[1]s catalog tags ghcr.io/myorg/scafctl/solutions/my-solution -o json
		`, cliParams.BinaryName),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runTags(cmd.Context(), options, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&options.Kind, "kind", "", "Artifact kind override (solution, provider, auth-handler)")
	cmd.Flags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runTags(ctx context.Context, opts *TagsOptions, outputOpts *kvx.OutputOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Parse remote reference
	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		w.Errorf("invalid reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Validate --kind early
	if opts.Kind != "" {
		if _, ok := catalog.ParseArtifactKind(opts.Kind); !ok {
			w.Errorf("invalid kind %q: must be 'solution', 'provider', or 'auth-handler'", opts.Kind)
			return exitcode.Errorf("invalid kind")
		}
	}

	// Apply --kind only when the parsed reference has no kind segment.
	// For Docker-style refs where kind is already in the path, --kind is
	// metadata only and must not inject an extra path segment.
	refKind := remoteRef.Kind
	if opts.Kind != "" && refKind == "" {
		refKind, _ = catalog.ParseArtifactKind(opts.Kind)
	}

	// Convert to reference (ignore version/tag from the input)
	ref := catalog.Reference{
		Kind: refKind,
		Name: remoteRef.Name,
	}

	// Create credential store
	credStore, err := catalog.NewCredentialStore(*lgr)
	if err != nil {
		lgr.V(1).Info("failed to create credential store, using anonymous auth", "error", err.Error())
	}

	// Resolve auth handler
	authHandler := resolveAuthHandler(ctx, remoteRef.Registry, "")
	authScope := resolveAuthScope(ctx, remoteRef.Registry)

	// Create remote catalog
	remoteCatalog, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            remoteRef.Registry,
		Registry:        remoteRef.Registry,
		Repository:      remoteRef.Repository,
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

	// List tags
	repoPath := remoteCatalog.RepositoryPath(ref)
	w.Infof("Listing tags for %s...", repoPath)

	tags, err := remoteCatalog.ListTags(ctx, ref)
	if err != nil {
		w.Errorf("failed to list tags: %v", err)
		hintOnAuthError(ctx, w, remoteRef.Registry, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	if len(tags) == 0 {
		w.Infof("No tags found for %s", repoPath)
		return nil
	}

	// Convert to output format
	items := make([]TagsListItem, len(tags))
	for i, t := range tags {
		items[i] = TagsListItem{
			Tag:      t.Tag,
			IsSemver: t.IsSemver,
			Version:  t.Version,
		}
	}

	return outputOpts.Write(items)
}
