// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// pullKind describes one kind-scoped `catalog pull` subcommand. Every kind
// shares the same command construction; only the metadata differs.
type pullKind struct {
	Kind    catalog.ArtifactKind
	Use     string
	Aliases []string
	Short   string
}

// pullKinds is the table of kind subcommands registered under `catalog pull`.
var pullKinds = func() []pullKind {
	out := make([]pullKind, 0, len(catalogKindSpecs()))
	for _, spec := range catalogKindSpecs() {
		out = append(out, pullKind{
			Kind:    spec.Kind,
			Use:     catalogKindUse(spec.Kind, "pull"),
			Aliases: spec.Aliases,
			Short:   catalogKindShort(spec.Kind, "pull"),
		})
	}
	return out
}()

// commandPullKind builds the `catalog pull <kind>` subcommand for a single kind.
func commandPullKind(_ *settings.Run, _ *terminal.IOStreams, pk *pullKind, options *PullOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          pk.Use,
		Aliases:      pk.Aliases,
		Short:        pk.Short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			options.Kind = string(pk.Kind)
			return runPullKind(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	if pk.Kind == catalog.ArtifactKindSolution {
		cmd.Flags().BoolVar(&options.Strict, "strict", false, "Fail if the pulled bundle is incomplete")
		cmd.Flags().BoolVar(&options.NoVerify, "no-verify", false, "Skip pulled-bundle completeness verification")
	}

	return cmd
}

// addPullKindCommands registers every kind subcommand on the pull command.
func addPullKindCommands(parent *cobra.Command, cliParams *settings.Run, ioStreams *terminal.IOStreams, options *PullOptions) {
	for _, pk := range pullKinds {
		parent.AddCommand(commandPullKind(cliParams, ioStreams, &pk, options))
	}
}

func runPullKind(ctx context.Context, opts *PullOptions) error {
	return runKind(ctx, "pull", opts.Reference, opts,
		Validate,
		func(ctx context.Context, opts *PullOptions) (pullReferenceBuild, error) {
			return newPullReferenceBuilder(ctx, opts).
				parseShortReference().
				resolveCatalogLocation().
				Build()
		},
		runPullResolvedBuiltReference)
}

func Validate(opts *PullOptions) error {
	if opts.Reference == "" {
		return &catalog.InvalidReferenceError{
			Input:   opts.Reference,
			Message: "reference cannot be empty",
		}
	}

	if looksLikeRemoteReference(opts.Reference) {
		return &catalog.InvalidReferenceError{
			Input:   opts.Reference,
			Message: "full remote references are not allowed with kind subcommands; use 'catalog pull <full-remote-reference>' instead",
		}
	}

	kind, ok := catalog.ParseArtifactKind(opts.Kind)
	if !ok {
		return &catalog.InvalidReferenceError{
			Input:   opts.Kind,
			Message: "artifact kind is required to validate --as",
		}
	}

	if err := validateVersionConstraint(opts.Reference, opts.VersionConstraint); err != nil {
		return err
	}

	if opts.TargetName != "" {
		if err := validateTargetName(catalog.Reference{Kind: kind}, opts.TargetName); err != nil {
			return err
		}
	}

	return nil
}
