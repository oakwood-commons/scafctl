// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"context"
	"crypto/sha256"
	"fmt"
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

// updateEntry tracks the state of a single dependency update.
type updateEntry struct {
	ref           string
	lockedVersion string
	lockedDigest  string
	latestVersion string
	latestDigest  string
	content       []byte
	info          catalog.ArtifactInfo
	needsUpdate   bool
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

	fetcher := &catalogFetcherAdapter{catalog: localCatalog}

	// Filter dependencies if --dependency was specified
	deps := existingLock.Dependencies
	if len(opts.Dependencies) > 0 {
		deps, err = filterDependencies(existingLock.Dependencies, opts.Dependencies)
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

	var entries []updateEntry
	var updateCount int

	for _, dep := range deps {
		entry := updateEntry{
			ref:           dep.Ref,
			lockedVersion: extractVersionFromRef(dep.Ref),
			lockedDigest:  dep.Digest,
		}

		// Re-resolve from catalog
		latestContent, latestInfo, fetchErr := fetcher.FetchSolution(ctx, dep.Ref)
		if fetchErr != nil {
			lgr.V(1).Info("failed to re-resolve dependency", "ref", dep.Ref, "error", fetchErr)
			w.Plainf("  ✗ %s: failed to resolve (%v)", dep.Ref, fetchErr)
			entries = append(entries, entry)
			continue
		}

		latestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(latestContent))
		entry.latestVersion = extractVersionFromRef(dep.Ref)
		if latestInfo.Reference.Version != nil {
			entry.latestVersion = latestInfo.Reference.Version.String()
		}
		entry.latestDigest = latestDigest
		entry.content = latestContent
		entry.info = latestInfo

		if latestDigest != dep.Digest {
			entry.needsUpdate = true
			updateCount++
		}

		entries = append(entries, entry)
	}

	// Print status
	for _, entry := range entries {
		if entry.needsUpdate {
			if opts.DryRun {
				w.Plainf("  %s:", entry.ref)
				w.Plainf("    locked:   %s (%s)", entry.lockedVersion, truncateDigest(entry.lockedDigest))
				w.Plainf("    latest:   %s (%s)", entry.latestVersion, truncateDigest(entry.latestDigest))
				w.Plainf("    action:   would update")
			} else {
				// Write vendored file
				if !opts.LockOnly {
					vendorDir := filepath.Join(bundleRoot, bundler.VendorDirName)
					vendoredName := bundler.VendorFileNameFromRef(entry.ref, entry.info)
					vendorPath := filepath.Join(vendorDir, vendoredName)

					if err := os.MkdirAll(filepath.Dir(vendorPath), 0o755); err != nil {
						w.Errorf("failed to create vendor directory: %v", err)
						return exitcode.WithCode(err, exitcode.GeneralError)
					}
					if err := os.WriteFile(vendorPath, entry.content, 0o600); err != nil {
						w.Errorf("failed to write vendored file: %v", err)
						return exitcode.WithCode(err, exitcode.GeneralError)
					}
				}
				w.Successf("  %s: %s → %s", entry.ref, truncateDigest(entry.lockedDigest), truncateDigest(entry.latestDigest))
			}
		} else if entry.latestDigest != "" {
			w.Plainf("  • %s: up to date", entry.ref)
		}
	}

	// Also check plugin dependencies
	checkPluginUpdates(ctx, existingLock, w)

	w.Plainf("")

	if opts.DryRun {
		if updateCount > 0 {
			w.Plainf("Summary: %d dependency(ies) would be updated", updateCount)
		} else {
			w.Plainf("All dependencies are up to date")
		}
		return nil
	}

	// Update lock file
	newLock := &bundler.LockFile{
		Version:      bundler.LockFileVersion,
		Dependencies: make([]bundler.LockDependency, 0, len(existingLock.Dependencies)),
		Plugins:      existingLock.Plugins, // preserve existing plugin entries
	}

	// Rebuild dependency list with updates
	for _, dep := range existingLock.Dependencies {
		updated := false
		for _, entry := range entries {
			if entry.ref == dep.Ref && entry.needsUpdate {
				newLock.Dependencies = append(newLock.Dependencies, bundler.LockDependency{
					Ref:          entry.ref,
					Digest:       entry.latestDigest,
					ResolvedFrom: entry.info.Catalog,
					VendoredAt:   dep.VendoredAt,
				})
				updated = true
				break
			}
		}
		if !updated {
			newLock.Dependencies = append(newLock.Dependencies, dep)
		}
	}

	if err := bundler.WriteLockFile(lockPath, newLock); err != nil {
		w.Errorf("failed to update lock file: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Successf("Updated %s", bundler.DefaultLockFileName)

	return nil
}

// filterDependencies returns only the dependencies whose refs match the filter list.
func filterDependencies(deps []bundler.LockDependency, filter []string) ([]bundler.LockDependency, error) {
	filterSet := make(map[string]bool)
	for _, f := range filter {
		filterSet[f] = true
	}

	var result []bundler.LockDependency
	for _, dep := range deps {
		if filterSet[dep.Ref] {
			result = append(result, dep)
			delete(filterSet, dep.Ref)
		}
	}

	for f := range filterSet {
		return nil, fmt.Errorf("dependency %q not found in lock file", f)
	}

	return result, nil
}

// extractVersionFromRef extracts the version part from a catalog reference.
func extractVersionFromRef(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '@' {
			return ref[i+1:]
		}
	}
	return "latest"
}

// truncateDigest truncates a digest for display.
func truncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}

// catalogFetcherAdapter adapts LocalCatalog to the CatalogFetcher interface.
type catalogFetcherAdapter struct {
	catalog *catalog.LocalCatalog
}

func (a *catalogFetcherAdapter) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, nameWithVersion)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("invalid reference %q: %w", nameWithVersion, err)
	}

	content, info, err := a.catalog.Fetch(ctx, ref)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}

	return content, info, nil
}

// ListSolutions returns all available versions of a named solution artifact.
func (a *catalogFetcherAdapter) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	return a.catalog.List(ctx, catalog.ArtifactKindSolution, name)
}

// checkPluginUpdates reports the locked state of plugin dependencies.
// Plugin version resolution requires a plugin registry; for now we just
// report the locked state since plugins are binary artifacts.
func checkPluginUpdates(_ context.Context, lock *bundler.LockFile, w *writer.Writer) {
	for _, p := range lock.Plugins {
		w.Plainf("  • %s (%s): %s (locked at %s)", p.Name, p.Kind, p.Version, truncateDigest(p.Digest))
	}
}
