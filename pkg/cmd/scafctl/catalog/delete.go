// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DeleteOptions holds options for the delete command.
type DeleteOptions struct {
	Reference string
	All       bool   // Delete all local artifacts (--all, root command only)
	Force     bool   // Skip the --all confirmation prompt (--force, root command only)
	DryRun    bool   // Show what would be deleted without deleting (--dry-run)
	Catalog   string // Target catalog for remote delete (URL or config name, --catalog, kind subcommands only)
	Kind      string // Artifact kind (set by the kind subcommands)
	Origin    string // Local artifact origin selector: "built" or a registry URL (--origin, kind subcommands only)
	Insecure  bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandDelete creates the delete command.
func CommandDelete(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &DeleteOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "delete <reference>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an artifact from a remote registry, or all local artifacts",
		SilenceUsage: true,
		Long: heredoc.Docf(`
			Delete a catalog artifact from a remote OCI registry.

			The root delete command only accepts full remote OCI references.
			Use kind subcommands to delete from the local catalog or to use a
			short reference with --catalog:
			  - %[1]s catalog delete solution <name@version>
			  - %[1]s catalog delete provider <name@version>
			  - %[1]s catalog delete auth-handler <name@version>

			You must specify the exact version to delete.

			Use --all to delete all artifacts from the local catalog. --all cannot
			be combined with a reference; --force skips its confirmation prompt.

			Examples:
			  # Delete from a remote registry (full reference)
			  %[1]s catalog delete ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0

			  # Delete all local artifacts
			  %[1]s catalog delete --all

			  # Delete all local artifacts (skip confirmation)
			  %[1]s catalog delete --all --force
		`, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.All {
				if len(args) > 0 {
					err := exitcode.WithCode(errors.New("--all cannot be used with positional arguments"), exitcode.InvalidInput)
					reportCmdError(cmd, err)
					return err
				}
				return runDeleteAll(cmd.Context(), options)
			}
			if len(args) != 1 {
				if err := reportUnknownKind(cmd, "delete", cliParams.BinaryName, args); err != nil {
					return err
				}
				err := exitcode.WithCode(
					fmt.Errorf("requires exactly 1 argument: %s catalog delete <full-remote-reference>", cliParams.BinaryName),
					exitcode.InvalidInput,
				)
				reportCmdError(cmd, err)
				return err
			}
			options.Reference = args[0]
			return runDelete(cmd.Context(), options)
		},
	}

	cmd.Flags().BoolVar(&options.All, "all", false, "Delete all artifacts from the local catalog")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Skip the --all confirmation prompt")
	cmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be deleted without actually deleting")
	cmd.PersistentFlags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	addDeleteKindCommands(cmd, cliParams, ioStreams, options)

	return cmd
}

// validateDeleteRoot checks the root delete reference: it must be a full
// remote reference carrying a version or digest. It returns plain errors;
// runKind assigns the exit code.
func validateDeleteRoot(opts *DeleteOptions) error {
	if !looksLikeRemoteReference(opts.Reference) {
		return fmt.Errorf("%w; use '%s catalog delete <kind> <name@version>' for short and local references", ErrRemoteReferenceRequired, cliBinaryName(opts.CliParams))
	}

	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	if remoteRef.Tag == "" {
		return errors.New("version required: use format 'registry/repo/kind/name@version'")
	}

	return nil
}

// buildDeleteRemoteReference derives the delete target from a full remote
// reference. Origin does not apply to remote artifacts, so Source stays unset.
func buildDeleteRemoteReference(ctx context.Context, opts *DeleteOptions) (deleteReferenceBuild, error) {
	// Full remote reference: ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0
	w := writer.FromContext(ctx)
	w.Verbose("Deleting from full OCI reference")

	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		return deleteReferenceBuild{}, fmt.Errorf("invalid remote reference: %w", err)
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		return deleteReferenceBuild{}, fmt.Errorf("invalid reference: %w", err)
	}

	verboseRefInfo(w, remoteRef.Name, string(remoteRef.Kind), remoteRef.Tag)

	return deleteReferenceBuild{
		Reference:  ref,
		Registry:   remoteRef.Registry,
		Repository: remoteRef.Repository,
	}, nil
}

// runDelete deletes the artifact named by a full remote reference. The remote
// catalog is derived from the reference itself, so no --catalog is involved.
func runDelete(ctx context.Context, opts *DeleteOptions) error {
	return runKind(ctx, "delete", opts.Reference, opts,
		validateDeleteRoot,
		buildDeleteRemoteReference,
		deleteFromRemoteCatalog)
}

// runDeleteAll deletes all artifacts from the local catalog.
func runDeleteAll(ctx context.Context, opts *DeleteOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Confirm action (skip in force, dry-run, or quiet mode)
	if !opts.Force && !opts.DryRun && !opts.CliParams.IsQuiet {
		in := input.FromContext(ctx)
		if in == nil {
			return fmt.Errorf("input not initialized in context")
		}
		confirmed, err := in.Confirm(input.NewConfirmOptions().
			WithPrompt("Delete all artifacts from the local catalog?").
			WithDefault(false))
		if err != nil {
			err := fmt.Errorf("failed to read confirmation: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		if !confirmed {
			w.Info("Delete cancelled")
			return nil
		}
	}

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	allKinds := []catalog.ArtifactKind{
		catalog.ArtifactKindSolution,
		catalog.ArtifactKindProvider,
		catalog.ArtifactKindAuthHandler,
	}

	var deleted, failed int
	for _, kind := range allKinds {
		artifacts, listErr := localCatalog.List(ctx, kind, "")
		if listErr != nil {
			lgr.V(1).Info("failed to list artifacts", "kind", kind, "error", listErr)
			failed++
			continue
		}
		for _, info := range artifacts {
			if opts.DryRun {
				w.Infof("Would delete %s", info.Reference.String())
				deleted++
				continue
			}
			if delErr := localCatalog.Delete(ctx, info.Reference); delErr != nil {
				lgr.V(1).Info("failed to delete artifact", "ref", info.Reference.String(), "error", delErr)
				failed++
				continue
			}
			deleted++
		}
	}

	if opts.DryRun {
		if failed > 0 {
			w.Warningf("Failed to list %d artifact kind(s); preview may be incomplete", failed)
		}
		if deleted == 0 && failed == 0 {
			w.Infof("No artifacts in local catalog")
		} else if deleted > 0 {
			w.Infof("Would delete %d artifact(s) from local catalog", deleted)
		}
		return nil
	}

	if failed > 0 {
		w.Warningf("Failed to delete %d artifact(s); check logs for details", failed)
	}

	if deleted == 0 && failed == 0 {
		w.Infof("No artifacts in local catalog")
	} else if deleted > 0 {
		w.Successf("Deleted %d artifact(s) from local catalog", deleted)
	}

	return nil
}

/*
	name ^ version| digest ^ kind ^ origin , we have expanded the identity of an artifact.

*/
