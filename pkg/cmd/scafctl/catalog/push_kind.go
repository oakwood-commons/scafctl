// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

const pushCatalogFlagUsage = `Destination catalog (registry URL or configured catalog name). If not specified, uses the default catalog from config.`

const pushOriginFlagUsage = `Select the local artifact by origin: "built" or the registry URL it was pulled from (e.g. ghcr.io/org/plugins)`

// pushKind describes one kind-scoped `catalog push` subcommand. Every kind
// shares the same command construction; only the metadata differs.
type pushKind struct {
	Kind    catalog.ArtifactKind
	Use     string
	Aliases []string
	Short   string
}

// pushKinds is the table of kind subcommands registered under `catalog push`.
var pushKinds = func() []pushKind {
	out := make([]pushKind, 0, len(catalogKindSpecs()))
	for _, spec := range catalogKindSpecs() {
		out = append(out, pushKind{
			Kind:    spec.Kind,
			Use:     catalogKindUse(spec.Kind, "push"),
			Aliases: spec.Aliases,
			Short:   catalogKindShort(spec.Kind, "push"),
		})
	}
	return out
}()

// commandPushKind builds the `catalog push <kind>` subcommand for a single kind.
// The destination is --catalog, or the default catalog from config.
func commandPushKind(_ *settings.Run, _ *terminal.IOStreams, pk *pushKind, options *pushKindOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          pk.Use,
		Aliases:      pk.Aliases,
		Short:        pk.Short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			options.Kind = string(pk.Kind)
			return runPushKind(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", pushCatalogFlagUsage)
	if pk.Kind == catalog.ArtifactKindSolution {
		cmd.Flags().BoolVar(&options.NoSBOM, "no-sbom", false, "Disable automatic SBOM generation (solutions generate SBOMs by default)")
	}

	return cmd
}

// addPushKindCommands registers every kind subcommand on the push command.
// The subcommands share the root command's options so the persistent flags
// declared on it (--insecure, --dry-run, --as, --force, --latest, --origin)
// take effect when given after the kind.
func addPushKindCommands(parent *cobra.Command, cliParams *settings.Run, ioStreams *terminal.IOStreams, options *pushKindOptions) {
	for i := range pushKinds {
		parent.AddCommand(commandPushKind(cliParams, ioStreams, &pushKinds[i], options))
	}
}

func runPushKind(ctx context.Context, opts *pushKindOptions) error {
	return runKind(ctx, "push", opts.Reference, opts,
		validatePush,
		func(ctx context.Context, opts *pushKindOptions) (pushReferenceBuild, error) {
			return newPushReferenceBuilder(ctx, opts).
				parseShortReference().
				rejectDigest().
				parseOrigin().
				parseTarget().
				resolveCatalogLocation().
				Build()
		},
		runPushResolvedBuiltReference)
}

// validatePush performs pure option checks for the kind subcommands before any
// parsing into domain types. It returns plain errors; runKind assigns the exit
// code.
func validatePush(opts *pushKindOptions) error {
	if opts.Reference == "" {
		return errors.New("name required: use format 'name[@version]' (e.g., 'my-solution@1.0.0')")
	}

	if looksLikeRemoteReference(opts.Reference) {
		return &catalog.InvalidReferenceError{
			Input:   opts.Reference,
			Message: "full remote references are not allowed with kind subcommands; use 'catalog push <full-remote-reference>' instead",
		}
	}

	kind, ok := catalog.ParseArtifactKind(opts.Kind)
	if !ok {
		return &catalog.InvalidReferenceError{
			Input:   opts.Kind,
			Message: "artifact kind is required to validate --as",
		}
	}
	if err := validateTargetName(catalog.Reference{Kind: kind}, opts.TargetName); err != nil {
		return err
	}

	return nil
}
