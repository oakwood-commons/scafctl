// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// deleteVersionRequiredMsg is reported when a delete reference omits its version.
const deleteVersionRequiredMsg = "version required: use format 'name@version' (e.g., 'my-solution@1.0.0')"

// deleteKind describes one kind-scoped `catalog delete` subcommand. Every kind
// shares the same command construction; only the metadata differs.
type deleteKind struct {
	Kind    catalog.ArtifactKind
	Use     string
	Aliases []string
	Short   string
}

// deleteKinds is the table of kind subcommands registered under `catalog delete`.
var deleteKinds = func() []deleteKind {
	out := make([]deleteKind, 0, len(catalogKindSpecs()))
	for _, spec := range catalogKindSpecs() {
		out = append(out, deleteKind{
			Kind:    spec.Kind,
			Use:     catalogKindUse(spec.Kind, "delete"),
			Aliases: spec.Aliases,
			Short:   catalogKindShort(spec.Kind, "delete"),
		})
	}
	return out
}()

// commandDeleteKind builds the `catalog delete <kind>` subcommand for a single kind.
func commandDeleteKind(_ *settings.Run, _ *terminal.IOStreams, dk *deleteKind, options *DeleteOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          dk.Use,
		Aliases:      dk.Aliases,
		Short:        dk.Short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Reference = args[0]
			options.Kind = string(dk.Kind)
			return runDeleteKind(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVarP(&options.Catalog, "catalog", "c", "", catalogFlagUsage)
	cmd.Flags().StringVar(&options.Origin, "origin", "", `Select the local artifact by origin: "built" or the registry URL it was pulled from (e.g. ghcr.io/org/plugins)`)

	return cmd
}

// addDeleteKindCommands registers every kind subcommand on the delete command.
func addDeleteKindCommands(parent *cobra.Command, cliParams *settings.Run, ioStreams *terminal.IOStreams, options *DeleteOptions) {
	for i := range deleteKinds {
		parent.AddCommand(commandDeleteKind(cliParams, ioStreams, &deleteKinds[i], options))
	}
}

func runDeleteKind(ctx context.Context, opts *DeleteOptions) error {
	return runKind(ctx, "delete", opts.Reference, opts,
		ValidateDelete,
		func(ctx context.Context, opts *DeleteOptions) (deleteReferenceBuild, error) {
			return newDeleteReferenceBuilder(ctx, opts).
				parseReference().
				parseOrigin().
				resolveCatalogLocation().
				Build()
		},
		runDeleteResolvedBuiltReference)
}

// ValidateDelete performs pure option checks before any parsing into domain
// types. It returns plain errors; runKind assigns the exit code.
func ValidateDelete(opts *DeleteOptions) error {
	if opts.Reference == "" {
		return errors.New(deleteVersionRequiredMsg)
	}

	if looksLikeRemoteReference(opts.Reference) {
		return &catalog.InvalidReferenceError{
			Input:   opts.Reference,
			Message: "full remote references are not allowed with kind subcommands; use 'catalog delete <full-remote-reference>' instead",
		}
	}

	if _, version := catalog.ParseNameVersion(opts.Reference); version == "" {
		return errors.New(deleteVersionRequiredMsg)
	}

	if opts.Origin != "" && opts.Catalog != "" {
		return ErrOriginWithCatalog
	}

	return nil
}

// runDeleteResolvedBuiltReference deletes the built target. An empty --catalog
// targets the local catalog; otherwise the resolved remote catalog is used.
func runDeleteResolvedBuiltReference(ctx context.Context, opts *DeleteOptions, build deleteReferenceBuild) error {
	if opts.Catalog != "" {
		return deleteFromRemoteCatalog(ctx, opts, build)
	}

	return deleteFromLocalCatalog(ctx, opts, build)
}

func deleteFromLocalCatalog(ctx context.Context, opts *DeleteOptions, build deleteReferenceBuild) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	return deleteFromLocal(ctx, opts, localCatalog, build)
}

// deleteFromLocal deletes the target from the given local catalog.
//
// With no origin selected (SourceAny) the artifact is resolved first: a version
// held by several origins yields an ambiguity error instead of guessing, and a
// single match supplies the exact reference (including its origin) to delete.
// With an explicit origin the reference is fully specified, so it is deleted
// directly.
func deleteFromLocal(ctx context.Context, opts *DeleteOptions, localCatalog *catalog.LocalCatalog, build deleteReferenceBuild) error {
	w := writer.FromContext(ctx)
	ref := build.Reference

	if build.Source.Kind == catalog.SourceAny {
		info, err := localCatalog.FindStrict(ctx, catalog.ArtifactQuery{
			Name:    ref.Name,
			Kind:    ref.Kind,
			Version: ref.Version.String(),
			Source:  build.Source,
		})
		if err != nil {
			var ambiguous *catalog.AmbiguousArtifactError
			if errors.As(err, &ambiguous) {
				w.Errorf("%v\n%s", err, originHint(ambiguous))
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if catalog.IsNotFound(err) {
				w.Errorf("artifact %q not found in catalog", opts.Reference)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}
			w.Errorf("failed to resolve artifact: %v", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		ref = info.Reference
	}

	if opts.DryRun {
		exists, err := localCatalog.Exists(ctx, ref)
		if err != nil {
			w.Errorf("failed to check artifact: %v", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		if !exists {
			err := fmt.Errorf("artifact %q not found in catalog", opts.Reference)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Infof("Would delete %s from local catalog", ref.String())
		return nil
	}

	if err := localCatalog.Delete(ctx, ref); err != nil {
		if catalog.IsNotFound(err) {
			w.Errorf("artifact %q not found in catalog", opts.Reference)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("failed to delete artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	w.Successf("Deleted %s", ref.String())
	return nil
}
