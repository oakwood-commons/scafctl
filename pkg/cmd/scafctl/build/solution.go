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
	"github.com/Masterminds/semver/v3"
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

// SolutionOptions holds options for the build solution command.
type SolutionOptions struct {
	File            string
	Name            string
	Version         string
	Force           bool
	NoBundle        bool
	NoVendor        bool
	BundleMaxSize   string
	DryRun          bool
	Dedupe          bool
	DedupeThreshold string
	CliParams       *settings.Run
	IOStreams       *terminal.IOStreams
}

// bundleResult holds the output of the bundle pipeline.
// Either tarData is set (v1) or dedup is set (v2).
type bundleResult struct {
	// tarData is the traditional single tar archive (v1).
	tarData []byte
	// dedup is the content-addressable dedup result (v2).
	dedup *bundler.DedupeResult
}

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
	absFile, err := filepath.Abs(opts.File)
	if err != nil {
		w.Errorf("failed to resolve path: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	bundleRoot := filepath.Dir(absFile)

	// Determine artifact name (priority: --name flag > metadata.name > filename)
	name := opts.Name
	if name == "" {
		// Try to get name from solution metadata
		if sol.Metadata.Name != "" {
			name = sol.Metadata.Name
		} else {
			// Fall back to filename (e.g., my-solution.yaml -> my-solution)
			base := filepath.Base(opts.File)
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext)
		}
	}

	// Validate name format
	if !catalog.IsValidName(name) {
		w.Errorf("invalid name %q: must be lowercase alphanumeric with hyphens (e.g., 'my-solution')", name)
		return exitcode.Errorf("invalid name")
	}

	// Determine version (priority: --version flag > metadata.version)
	var version *semver.Version
	switch {
	case opts.Version != "":
		// User provided --version flag
		version, err = semver.NewVersion(opts.Version)
		if err != nil {
			w.Errorf("invalid version %q: %v", opts.Version, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		// Warn if overriding metadata version
		if sol.Metadata.Version != nil && !sol.Metadata.Version.Equal(version) {
			w.Warningf("--version %s overrides metadata version %s", version.String(), sol.Metadata.Version.String())
		}
	case sol.Metadata.Version != nil:
		// Use metadata version
		version = sol.Metadata.Version
		lgr.V(1).Info("using version from solution metadata", "version", version.String())
	default:
		// No version available
		w.Error("solution has no version in metadata; use --version to specify one")
		return exitcode.Errorf("no version")
	}

	// === Bundle pipeline ===
	var br *bundleResult

	if !opts.NoBundle {
		br, err = buildBundle(ctx, &sol, bundleRoot, opts, w)
		if err != nil {
			return err
		}
	}

	// Dry-run: show what would be built but don't store
	if opts.DryRun {
		w.Infof("Dry run: would build %s@%s", name, version.String())
		return nil
	}

	// Create reference
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: version,
	}

	// Create local catalog
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build annotations
	annotations := catalog.NewAnnotationBuilder().
		Set(catalog.AnnotationSource, opts.File).
		Build()

	// Re-serialize the solution (it may have been modified by compose/vendor)
	if !opts.NoBundle && (len(sol.Compose) > 0 || len(sol.Bundle.Include) > 0) {
		content, err = sol.ToYAML()
		if err != nil {
			w.Errorf("failed to serialize composed solution: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	}

	// Store the artifact
	var info catalog.ArtifactInfo

	if br != nil && br.dedup != nil {
		// Content-addressable dedup storage (v2)
		var blobLayers [][]byte
		for _, blob := range br.dedup.LargeBlobs {
			blobLayers = append(blobLayers, blob.Content)
		}
		info, err = localCatalog.StoreDedup(ctx, ref, content, br.dedup.ManifestJSON, br.dedup.SmallBlobsTar, blobLayers, annotations, opts.Force)
	} else {
		var bundleData []byte
		if br != nil {
			bundleData = br.tarData
		}
		info, err = localCatalog.Store(ctx, ref, content, bundleData, annotations, opts.Force)
	}

	if err != nil {
		if catalog.IsExists(err) {
			w.Errorf("%v\nUse --force to overwrite", err)
			return exitcode.WithCode(err, exitcode.CatalogError)
		}
		w.Errorf("failed to store solution: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	lgr.V(1).Info("built solution",
		"name", info.Reference.Name,
		"version", info.Reference.Version.String(),
		"digest", info.Digest)

	w.Successf("Built %s@%s", info.Reference.Name, info.Reference.Version.String())
	w.Infof("  Digest: %s", info.Digest)
	w.Infof("  Catalog: %s", localCatalog.Path())

	return nil
}

// buildBundle runs the compose → discover → vendor → tar/dedup pipeline.
func buildBundle(ctx context.Context, sol *solution.Solution, bundleRoot string, opts *SolutionOptions, w *writer.Writer) (*bundleResult, error) {
	lgr := logger.FromContext(ctx)

	// Parse max bundle size
	maxSize, err := parseByteSize(opts.BundleMaxSize)
	if err != nil {
		w.Errorf("invalid --bundle-max-size: %v", err)
		return nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Step 1: Compose multi-file solutions
	if len(sol.Compose) > 0 {
		lgr.V(1).Info("composing solution", "files", sol.Compose)
		composed, err := bundler.Compose(sol, bundleRoot)
		if err != nil {
			w.Errorf("failed to compose solution: %v", err)
			return nil, exitcode.WithCode(err, exitcode.InvalidInput)
		}
		*sol = *composed
		w.Infof("  Composed %d file(s) into solution", len(sol.Compose)+1)
	}

	// Step 2: Load .scafctlignore
	ignoreChecker, err := bundler.LoadScafctlIgnore(bundleRoot)
	if err != nil {
		w.Errorf("failed to load .scafctlignore: %v", err)
		return nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Step 3: Discover files via static analysis + glob expansion
	discovery, err := bundler.DiscoverFiles(sol, bundleRoot, bundler.WithIgnoreChecker(ignoreChecker))
	if err != nil {
		w.Errorf("failed to discover files: %v", err)
		return nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	lgr.V(1).Info("discovered files",
		"localFiles", len(discovery.LocalFiles),
		"catalogRefs", len(discovery.CatalogRefs))

	// Step 3.5: Validate plugin dependencies
	if len(sol.Bundle.Plugins) > 0 {
		if err := bundler.ValidatePlugins(sol); err != nil {
			w.Errorf("plugin validation failed: %v", err)
			return nil, exitcode.WithCode(err, exitcode.InvalidInput)
		}
		lgr.V(1).Info("validated plugin dependencies", "count", len(sol.Bundle.Plugins))

		// Merge plugin defaults into provider inputs (before DAG construction)
		bundler.MergePluginDefaults(sol)
	}

	// Step 4: Vendor catalog dependencies
	if !opts.NoVendor && len(discovery.CatalogRefs) > 0 {
		lgr.V(1).Info("vendoring catalog dependencies", "count", len(discovery.CatalogRefs))

		lockPath := filepath.Join(bundleRoot, "solution.lock")
		vendorDir := filepath.Join(bundleRoot, ".scafctl", "vendor")

		vendorResult, err := bundler.VendorDependencies(ctx, sol, discovery.CatalogRefs, bundler.VendorOptions{
			BundleRoot: bundleRoot,
			VendorDir:  vendorDir,
			LockPath:   lockPath,
		})
		if err != nil {
			w.Errorf("failed to vendor catalog dependencies: %v", err)
			return nil, exitcode.WithCode(err, exitcode.CatalogError)
		}

		// Add vendored files to the discovery result
		for _, vf := range vendorResult.VendoredFiles {
			discovery.LocalFiles = append(discovery.LocalFiles, bundler.FileEntry{
				RelPath: vf,
				Source:  bundler.ExplicitInclude,
			})
		}

		w.Infof("  Vendored %d catalog dependency(ies)", len(vendorResult.VendoredFiles))
	}

	// Step 5: Dry-run output
	if opts.DryRun {
		printDryRunOutput(w, discovery, sol, opts)
		return nil, nil
	}

	// Step 6: No files to bundle — skip tar creation
	if len(discovery.LocalFiles) == 0 {
		lgr.V(1).Info("no files to bundle")
		return nil, nil
	}

	// Step 7: Collect plugin entries from bundle.plugins
	var plugins []bundler.BundlePluginEntry
	for _, p := range sol.Bundle.Plugins {
		plugins = append(plugins, bundler.BundlePluginEntry{
			Name:    p.Name,
			Kind:    string(p.Kind),
			Version: p.Version,
		})
	}

	// Step 8: Create bundle (dedup v2 or tar v1)
	if opts.Dedupe {
		dedupeThreshold, err := parseByteSize(opts.DedupeThreshold)
		if err != nil {
			w.Errorf("invalid --dedupe-threshold: %v", err)
			return nil, exitcode.WithCode(err, exitcode.InvalidInput)
		}

		dedupeResult, err := bundler.CreateDeduplicatedBundle(bundleRoot, discovery.LocalFiles, plugins,
			bundler.WithDedupeThreshold(dedupeThreshold),
			bundler.WithDedupeMaxSize(maxSize))
		if err != nil {
			w.Errorf("failed to create deduplicated bundle: %v", err)
			return nil, exitcode.WithCode(err, exitcode.GeneralError)
		}

		w.Infof("  Bundled %d file(s) (%s, deduplicated: %d layer(s))",
			len(dedupeResult.Manifest.Files),
			formatByteSize(dedupeResult.TotalSize),
			len(dedupeResult.LargeBlobs)+1) // +1 for small files tar if present

		return &bundleResult{dedup: dedupeResult}, nil
	}

	// Non-dedup path: create v1 tar
	tarData, manifest, err := bundler.CreateBundleTar(bundleRoot, discovery.LocalFiles, plugins,
		bundler.WithMaxBundleSize(maxSize))
	if err != nil {
		w.Errorf("failed to create bundle: %v", err)
		return nil, exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Infof("  Bundled %d file(s) (%s)", len(manifest.Files), formatByteSize(int64(len(tarData))))

	return &bundleResult{tarData: tarData}, nil
}

// parseByteSize parses a human-readable byte size string (e.g., "50MB", "100KB").
func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	// Check longer suffixes first to avoid "KB" matching "B"
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			numStr := strings.TrimSuffix(s, entry.suffix)
			numStr = strings.TrimSpace(numStr)
			if numStr == "" {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			var n int64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * entry.mult, nil
		}
	}

	// Plain number — treat as bytes
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}

// formatByteSize formats bytes as a human-readable string.
func formatByteSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
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
	var staticFiles, explicitFiles []bundler.FileEntry
	for _, f := range discovery.LocalFiles {
		switch f.Source {
		case bundler.StaticAnalysis:
			staticFiles = append(staticFiles, f)
		case bundler.ExplicitInclude:
			explicitFiles = append(explicitFiles, f)
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
