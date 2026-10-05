// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

const catalogFlagUsage = `Target catalog (registry URL or configured catalog name). If not specified, uses the default catalog from config.`

// PushOptions holds options for the push command.
type PushOptions struct {
	Reference  string // Artifact reference (full remote reference, or short name for kind subcommands)
	Catalog    string // Target catalog (URL or configured catalog name, kind subcommands only)
	TargetName string // Optional name@version the artifact is published as in the remote (--as)
	Kind       string // Artifact kind (set by the kind subcommands)
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
	options := &pushKindOptions{
		PushOptions: PushOptions{
			CliParams: cliParams,
			IOStreams: ioStreams,
		},
	}

	cmd := &cobra.Command{
		Use:   "push <reference>",
		Short: "Push an artifact to a remote registry",
		Long: heredoc.Docf(`
			Push a catalog artifact from the local catalog to a remote OCI registry.

			The artifact must exist in the local catalog. Use '%[1]s build' to
			create artifacts from solution files.

			The root push command only accepts full remote OCI references. The
			reference names both the artifact (kind, name and version) and the
			destination (registry and repository). Use kind subcommands for short
			references with a --catalog destination:
			  - %[1]s catalog push solution <name[@version]>
			  - %[1]s catalog push provider <name[@version]>
			  - %[1]s catalog push auth-handler <name[@version]>

			If no version is specified, the latest local version is pushed.

			When the same name and version exists in the local catalog under several
			origins (built locally, or pulled from different registries), use --origin
			to choose which copy to push.

			When using --as, the target must be a concrete name@version, which is
			the name the artifact is published under in the remote. Bare names, the
			latest alias, and digest pins are rejected.

			Examples:
			  # Push using a full remote reference
			  %[1]s catalog push ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Push the locally built copy when several origins hold this version
			  %[1]s catalog push ghcr.io/myorg/solutions/my-solution@1.0.0 --origin built

			  # Publish under a different name and version
			  %[1]s catalog push ghcr.io/myorg/solutions/my-solution@1.0.0 --as production-solution@1.0.0

			  # Force overwrite existing
			  %[1]s catalog push ghcr.io/myorg/solutions/my-solution@1.0.0 --force
		`, cliParams.BinaryName),

		Args:         rootArgs("push", cliParams.BinaryName, flags.RequireArg("reference", cliParams.BinaryName+" catalog push ghcr.io/myorg/solutions/my-solution@1.0.0")),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := reportUnknownKind(cmd, "push", cliParams.BinaryName, args); err != nil {
				return err
			}
			options.Reference = args[0]
			return runPush(cmd.Context(), options)
		},
	}

	cmd.PersistentFlags().StringVar(&options.Origin, "origin", "", pushOriginFlagUsage)
	cmd.PersistentFlags().StringVar(&options.TargetName, "as", "", "Publish under a different name and version, as name@version (e.g. my-solution@2.0.0)")
	cmd.PersistentFlags().BoolVarP(&options.Force, "force", "f", false, "Overwrite existing artifact in remote")
	cmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be pushed without actually pushing")
	cmd.PersistentFlags().BoolVar(&options.Insecure, "insecure", false, "Allow insecure HTTP connections")
	cmd.PersistentFlags().BoolVar(&options.Latest, "latest", false, "Tag the pushed version as 'latest' in the remote registry")
	cmd.Flags().BoolVar(&options.NoSBOM, "no-sbom", false, "Disable automatic SBOM generation (solutions generate SBOMs by default)")
	addPushKindCommands(cmd, cliParams, ioStreams, options)

	return cmd
}

// runPush pushes the local artifact named by a full remote reference to the
// registry the reference points at.
func runPush(ctx context.Context, opts *pushKindOptions) error {
	return runKind(ctx, "push", opts.Reference, opts,
		validatePushRoot,
		buildPushRemoteReference,
		runPushResolvedBuiltReference)
}

func buildPushRemoteReference(ctx context.Context, opts *pushKindOptions) (pushReferenceBuild, error) {
	w := writer.FromContext(ctx)
	w.Verbose("Pushing to full OCI reference")

	build, err := newPushReferenceBuilder(ctx, opts).
		parseRemoteReference().
		rejectDigest().
		parseOrigin().
		parseTarget().
		Build()
	if err != nil {
		return pushReferenceBuild{}, err
	}

	verboseRefInfo(w, build.Reference.Name, string(build.Reference.Kind), build.Reference.VersionOrDigest())

	return build, nil
}

// validatePushRoot performs pure option checks for the root push command: the
// reference must be a full remote reference that names its artifact kind. It
// returns plain errors; runKind assigns the exit code.
func validatePushRoot(opts *pushKindOptions) error {
	if !looksLikeRemoteReference(opts.Reference) {
		return fmt.Errorf("%w; use '%s catalog push <kind> <name[@version]> --catalog <catalog>' for short references",
			ErrRemoteReferenceRequired, cliBinaryName(opts.CliParams))
	}

	remoteRef, err := catalog.ParseRemoteReference(opts.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	ref, err := remoteRef.ToReference()
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	if ref.Kind == "" {
		return &catalog.InvalidReferenceError{
			Input:   opts.Reference,
			Message: "reference must include the artifact kind, e.g. ghcr.io/myorg/solutions/my-solution@1.0.0",
		}
	}

	return validateTargetName(ref, opts.TargetName)
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
