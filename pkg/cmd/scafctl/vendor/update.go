// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"context"
	"os"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// UpdateOptions holds options for the vendor update command.
type UpdateOptions struct {
	SolutionPath string
	Dependencies []string
	DryRun       bool
	LockOnly     bool
	PreRelease   bool
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams
}

// CommandUpdate creates the vendor update subcommand.
func CommandUpdate(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "update [solution-path]",
		Short:        "Update vendored dependencies",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Re-resolve and update vendored dependencies without a full rebuild.

			Parses the solution YAML and lock file, re-resolves catalog references
			against the current registry state (respecting version constraints),
			fetches updated dependencies, and writes them to the vendor directory.

			Use --dependency to update a specific dependency only.
			Use --dry-run to preview what would be updated.
			Use --lock-only to update the lock file without re-vendoring files.

			Examples:
			  # Update all vendored dependencies
			  scafctl vendor update

			  # Update from a specific solution file
			  scafctl vendor update ./my-solution.yaml

			  # Preview updates without making changes
			  scafctl vendor update --dry-run

			  # Update only a specific dependency
			  scafctl vendor update --dependency deploy-to-k8s@2.0.0

			  # Update lock file only (don't re-vendor files)
			  scafctl vendor update --lock-only
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.SolutionPath = args[0]
			} else {
				opts.SolutionPath = "./solution.yaml"
			}
			return runVendorUpdate(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringSliceVar(&opts.Dependencies, "dependency", nil, "Update only this dependency (repeatable)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be updated without making changes")
	cmd.Flags().BoolVar(&opts.LockOnly, "lock-only", false, "Update lock file without re-vendoring files")
	cmd.Flags().BoolVar(&opts.PreRelease, "pre-release", false, "Include pre-release versions when resolving")

	return cmd
}

func runVendorUpdate(ctx context.Context, opts *UpdateOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Read and parse solution
	content, err := os.ReadFile(opts.SolutionPath)
	if err != nil {
		w.Errorf("failed to read solution file: %v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		w.Errorf("failed to parse solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	absPath, err := filepath.Abs(opts.SolutionPath)
	if err != nil {
		w.Errorf("failed to resolve path: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	bundleRoot := filepath.Dir(absPath)

	// Load existing lock file
	lockPath := filepath.Join(bundleRoot, bundler.DefaultLockFileName)
	existingLock, err := bundler.LoadLockFile(lockPath)
	if err != nil {
		w.Errorf("failed to load lock file: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if existingLock == nil {
		w.Errorf("no lock file found at %s; run 'scafctl build solution' first", lockPath)
		return exitcode.Errorf("no lock file")
	}

	lgr.V(1).Info("loaded lock file", "path", lockPath,
		"deps", len(existingLock.Dependencies),
		"plugins", len(existingLock.Plugins))

	// Create catalog fetcher
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	fetcher := &bundler.LocalCatalogFetcherAdapter{Catalog: localCatalog}

	// Filter dependencies if --dependency was specified
	deps := existingLock.Dependencies
	if len(opts.Dependencies) > 0 {
		deps, err = bundler.FilterDependencies(existingLock.Dependencies, opts.Dependencies)
		if err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Check each dependency for updates
	w.Plainf("")
	if opts.DryRun {
		w.Plainf("Checking vendored dependencies for %s...", opts.SolutionPath)
	} else {
		w.Plainf("Updating vendored dependencies...")
	}
	w.Plainf("")

	entries, err := bundler.CheckForUpdates(ctx, deps, fetcher, *lgr)
	if err != nil {
		w.Errorf("failed to check for updates: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	var updateCount int
	for _, entry := range entries {
		if entry.NeedsUpdate {
			updateCount++
		}
		if entry.LatestDigest == "" && entry.NeedsUpdate {
			w.Plainf("  ✗ %s: failed to resolve", entry.Ref)
		}
	}

	// Print status
	for _, entry := range entries {
		if entry.NeedsUpdate {
			if opts.DryRun {
				w.Plainf("  %s:", entry.Ref)
				w.Plainf("    locked:   %s (%s)", entry.LockedVersion, bundler.TruncateDigest(entry.LockedDigest))
				w.Plainf("    latest:   %s (%s)", entry.LatestVersion, bundler.TruncateDigest(entry.LatestDigest))
				w.Plainf("    action:   would update")
			} else {
				w.Successf("  %s: %s → %s", entry.Ref, bundler.TruncateDigest(entry.LockedDigest), bundler.TruncateDigest(entry.LatestDigest))
			}
		} else if entry.LatestDigest != "" {
			w.Plainf("  • %s: up to date", entry.Ref)
		}
	}

	// Also check plugin dependencies
	for _, msg := range bundler.CheckPluginUpdates(existingLock) {
		w.Plainf("%s", msg)
	}

	w.Plainf("")

	if opts.DryRun {
		if updateCount > 0 {
			w.Plainf("Summary: %d dependency(ies) would be updated", updateCount)
		} else {
			w.Plainf("All dependencies are up to date")
		}
		return nil
	}

	// Apply updates: write vendored files and build new lock
	newLock, err := bundler.ApplyUpdates(entries, existingLock, bundleRoot, opts.LockOnly, *lgr)
	if err != nil {
		w.Errorf("failed to apply updates: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if err := bundler.WriteLockFile(lockPath, newLock); err != nil {
		w.Errorf("failed to update lock file: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Successf("Updated %s", bundler.DefaultLockFileName)

	return nil
}
