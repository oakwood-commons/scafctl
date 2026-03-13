// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds options for the build solution command.
type SolutionOptions struct {
	File            string
	Name            string
	Version         string
	Force           bool
	NoBundle        bool
	NoVendor        bool
	NoCache         bool
	BundleMaxSize   string
	DryRun          bool
	Dedupe          bool
	DedupeThreshold string
	CliParams       *settings.Run
	IOStreams       *terminal.IOStreams

	// resolvedPlugins holds plugin lock entries from VendorPlugins,
	// to be merged into the lock file during Step 4.
	resolvedPlugins []bundler.LockPlugin
}

// bundleResult is a type alias for the shared builder.BuildResult.
type bundleResult = builder.BuildResult

// CommandBuildSolution creates the build solution command.
func CommandBuildSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &SolutionOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "solution [file]",
		Aliases:      []string{"sol", "s"},
		Short:        "Build a solution into the local catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Build a solution file into the local catalog.

			The solution is packaged as an OCI artifact with the specified name and version.
			If name is not specified, it is extracted from the solution metadata.
			If version is not specified, it is extracted from the solution metadata.

			The build process:
			  1. Composes multi-file solutions (if compose: is set)
			  2. Discovers local file dependencies via static analysis
			  3. Expands bundle.include globs
			  4. Vendors catalog dependencies (unless --no-vendor)
			  5. Creates a bundle tar with all discovered files
			  6. Stores the solution YAML + bundle as an OCI artifact

			Use --no-bundle to skip bundling entirely (legacy behavior).
			Use --dry-run to see what would be bundled without storing.

			Examples:
			  # Build solution using version from metadata
			  scafctl build solution ./my-solution.yaml

			  # Build with explicit version (overrides metadata)
			  scafctl build solution ./solution.yaml --version 1.0.0

			  # Build with explicit name
			  scafctl build solution ./solution.yaml --name my-solution --version 1.0.0

			  # Overwrite existing version
			  scafctl build solution ./solution.yaml --version 1.0.0 --force

			  # Preview what would be bundled
			  scafctl build solution ./solution.yaml --dry-run

			  # Build without bundling
			  scafctl build solution ./solution.yaml --no-bundle
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.File = args[0]
			return runBuildSolution(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVar(&options.Name, "name", "", "Artifact name (default: extracted from solution metadata)")
	cmd.Flags().StringVar(&options.Version, "version", "", "Semantic version (default: extracted from solution metadata)")
	cmd.Flags().BoolVar(&options.Force, "force", false, "Overwrite existing version")
	cmd.Flags().BoolVar(&options.NoBundle, "no-bundle", false, "Skip bundling entirely (store only the solution YAML)")
	cmd.Flags().BoolVar(&options.NoVendor, "no-vendor", false, "Skip catalog dependency vendoring")
	cmd.Flags().StringVar(&options.BundleMaxSize, "bundle-max-size", "50MB", "Maximum total size for bundled files")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be bundled without storing")
	cmd.Flags().BoolVar(&options.Dedupe, "dedupe", true, "Enable content-addressable deduplication")
	cmd.Flags().StringVar(&options.DedupeThreshold, "dedupe-threshold", "4KB", "Minimum file size for individual layer extraction (smaller files are tarred together)")
	cmd.Flags().BoolVar(&options.NoCache, "no-cache", false, "Skip build cache and force a full rebuild")

	return cmd
}

func runBuildSolution(ctx context.Context, opts *SolutionOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	// Read solution file
	content, err := os.ReadFile(opts.File)
	if err != nil {
		w.Errorf("failed to read solution file: %v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Parse solution to extract metadata
	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		w.Errorf("failed to parse solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Determine bundle root (directory containing the solution file)
	absFile, err := provider.AbsFromContext(ctx, opts.File)
	if err != nil {
		w.Errorf("failed to resolve path: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	bundleRoot := filepath.Dir(absFile)

	// Determine artifact name (priority: --name flag > metadata.name > filename)
	name, err := solution.ResolveArtifactName(opts.Name, sol.Metadata.Name, opts.File)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Determine version (priority: --version flag > metadata.version)
	version, overrides, err := solution.ResolveArtifactVersion(opts.Version, sol.Metadata.Version)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if overrides {
		w.Warningf("--version %s overrides metadata version %s", version.String(), sol.Metadata.Version.String())
	} else if opts.Version == "" && sol.Metadata.Version != nil {
		lgr.V(1).Info("using version from solution metadata", "version", version.String())
	}

	// === Bundle pipeline ===
	var br *bundleResult

	if !opts.NoBundle {
		br, err = buildBundle(ctx, &sol, content, bundleRoot, opts, w)
		if err != nil {
			return err
		}
	}

	// Handle build cache hit — artifact already exists in catalog
	if br != nil && br.CacheHit {
		w.Successf("Build cache hit: %s@%s (unchanged)", br.CacheEntry.ArtifactName, br.CacheEntry.ArtifactVersion)
		w.Infof("  Digest: %s", br.CacheEntry.ArtifactDigest)
		w.Infof("  Use --no-cache to force a full rebuild")
		return nil
	}

	// Dry-run: show what would be built but don't store
	if opts.DryRun {
		if br != nil && br.Discovery != nil {
			printDryRunOutput(w, br.Discovery, &sol, opts)
		}
		w.Infof("Dry run: would build %s@%s", name, version.String())
		return nil
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Re-serialize the solution (it may have been modified by compose/vendor)
	if !opts.NoBundle && (len(sol.Compose) > 0 || len(sol.Bundle.Include) > 0) {
		content, err = sol.ToYAML()
		if err != nil {
			w.Errorf("failed to serialize composed solution: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	}

	// Store the artifact (handles dedup vs traditional, plus build cache)
	storeResult, err := builder.StoreSolutionArtifact(ctx, localCatalog, name, version, content, br, builder.StoreOptions{
		Force:  opts.Force,
		Source: opts.File,
	})
	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("%v\nUse --force to overwrite", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		w.Errorf("failed to store solution: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	info := storeResult.Info
	w.Successf("Built %s@%s", info.Reference.Name, info.Reference.Version.String())
	w.Infof("  Digest: %s", info.Digest)
	w.Infof("  Catalog: %s", localCatalog.Path())

	return nil
}

// buildBundle delegates to the shared builder.BuildBundle pipeline and
// displays progress messages via the writer.
func buildBundle(ctx context.Context, sol *solution.Solution, solutionContent []byte, bundleRoot string, opts *SolutionOptions, w *writer.Writer) (*bundleResult, error) {
	lgr := logger.FromContext(ctx)

	br, err := builder.BuildBundle(ctx, sol, solutionContent, bundleRoot, builder.BuildBundleOptions{
		BundleMaxSize:   opts.BundleMaxSize,
		NoVendor:        opts.NoVendor,
		NoCache:         opts.NoCache,
		DryRun:          opts.DryRun,
		Dedupe:          opts.Dedupe,
		DedupeThreshold: opts.DedupeThreshold,
		Logger:          *lgr,
	})
	if err != nil {
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Display progress messages from the pipeline
	if br != nil {
		for _, msg := range br.Messages {
			w.Infof("  %s", msg)
		}
		// Transfer resolved plugins back to CLI options for backward compatibility
		opts.resolvedPlugins = br.ResolvedPlugins
	}

	return br, nil
}

// printDryRunOutput produces a formatted summary of what would be bundled.
func printDryRunOutput(w *writer.Writer, discovery *bundler.DiscoveryResult, sol *solution.Solution, opts *SolutionOptions) {
	w.Plainf("")
	w.Plainf("Bundle analysis for %s:", opts.File)
	w.Plainf("")

	// Composed files
	if len(sol.Compose) > 0 {
		w.Plainf("  Composed files:")
		for _, cf := range sol.Compose {
			w.Plainf("    %s  → merged into solution", cf)
		}
		w.Plainf("")
	}

	// Static analysis discovered files
	var staticFiles, explicitFiles, testFiles []bundler.FileEntry
	for _, f := range discovery.LocalFiles {
		switch f.Source {
		case bundler.StaticAnalysis:
			staticFiles = append(staticFiles, f)
		case bundler.ExplicitInclude:
			explicitFiles = append(explicitFiles, f)
		case bundler.TestInclude:
			testFiles = append(testFiles, f)
		}
	}

	if len(staticFiles) > 0 {
		w.Plainf("  Static analysis discovered:")
		for _, f := range staticFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Explicit includes
	if len(explicitFiles) > 0 {
		w.Plainf("  Explicit includes (bundle.include):")
		for _, f := range explicitFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Test file includes
	if len(testFiles) > 0 {
		w.Plainf("  Test file includes:")
		for _, f := range testFiles {
			w.Plainf("    %s", f.RelPath)
		}
		w.Plainf("")
	}

	// Catalog references
	if len(discovery.CatalogRefs) > 0 {
		if opts.NoVendor {
			w.Plainf("  Catalog references (not vendored):")
		} else {
			w.Plainf("  Vendored catalog dependencies:")
		}
		for _, ref := range discovery.CatalogRefs {
			w.Plainf("    %s  → %s", ref.Ref, ref.VendorPath)
		}
		w.Plainf("")
	}

	// Plugin dependencies
	if len(sol.Bundle.Plugins) > 0 {
		w.Plainf("  Plugin dependencies:")
		for _, p := range sol.Bundle.Plugins {
			defaults := ""
			if len(p.Defaults) > 0 {
				keys := make([]string, 0, len(p.Defaults))
				for k := range p.Defaults {
					keys = append(keys, k)
				}
				defaults = fmt.Sprintf("   defaults: %s", strings.Join(keys, ", "))
			}
			w.Plainf("    %s (%s)        %s%s", p.Name, p.Kind, p.Version, defaults)
		}
		w.Plainf("")
	}

	// Dynamic path warnings
	dynamicWarnings := bundler.DetectDynamicPaths(sol)
	if len(dynamicWarnings) > 0 {
		w.Warningf("Dynamic paths detected (ensure these are covered by bundle.include):")
		for _, dw := range dynamicWarnings {
			w.Plainf("    %s: %s in %s", dw.Location, dw.Kind, dw.Expression)
		}
		w.Plainf("")
	}

	// Summary
	total := len(discovery.LocalFiles)
	vendored := len(discovery.CatalogRefs)
	pluginCount := len(sol.Bundle.Plugins)
	w.Plainf("  Total: %d bundled file(s)", total)
	if vendored > 0 {
		w.Plainf("       + %d vendored dependency(ies)", vendored)
	}
	if pluginCount > 0 {
		w.Plainf("       + %d plugin(s)", pluginCount)
	}
	w.Plainf("")
}
