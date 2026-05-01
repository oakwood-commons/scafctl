// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds options for the build solution command.
type SolutionOptions struct {
	File            string
	Name            string
	Version         string
	Tag             string
	Force           bool
	NoBundle        bool
	NoVendor        bool
	NoCache         bool
	BundleMaxSize   string
	DryRun          bool
	Dedupe          bool
	DedupeThreshold string
	SkipLint        bool
	SkipTests       bool
	IgnorePreflight bool
	AllowDevVersion bool
	BaseDir         string
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
		Use:          "solution",
		Aliases:      []string{"sol", "s"},
		Short:        "Build a solution into the local catalog",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
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
			  # Build solution using version from metadata (auto-discovery)
			  scafctl build solution

			  # Build solution from a specific file
			  scafctl build solution -f ./my-solution.yaml

			  # Build with explicit version (overrides metadata)
			  scafctl build solution -f ./solution.yaml --version 1.0.0

			  # Build with explicit name
			  scafctl build solution -f ./solution.yaml --name my-solution --version 1.0.0

			  # Build with a tag (shorthand for --name and --version)
			  scafctl build solution -f ./solution.yaml -t my-solution@1.0.0

			  # Build with a full remote reference (for push later)
			  scafctl build solution -t ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Overwrite existing version
			  scafctl build solution -f ./solution.yaml --version 1.0.0 --force

			  # Preview what would be bundled
			  scafctl build solution -f ./solution.yaml --dry-run

			  # Build without bundling
			  scafctl build solution -f ./solution.yaml --no-bundle
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Parse -t/--tag if provided: supports both "name@version" and
			// full remote refs like "registry/repo/solutions/name@version".
			if options.Tag != "" {
				if options.Name != "" || options.Version != "" {
					err := fmt.Errorf("--tag cannot be used together with --name or --version")
					if w := writer.FromContext(cmd.Context()); w != nil {
						w.Errorf("%v", err)
					}
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}

				if catalog.LooksLikeRemoteReference(options.Tag) {
					// Full remote reference
					remoteRef, err := catalog.ParseRemoteReference(options.Tag)
					if err != nil {
						if w := writer.FromContext(cmd.Context()); w != nil {
							w.Errorf("invalid tag %q: %v", options.Tag, err)
						}
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
					if remoteRef.Kind != "" && remoteRef.Kind != catalog.ArtifactKindSolution {
						err := fmt.Errorf("--tag references kind %q but this command builds solutions", remoteRef.Kind)
						if w := writer.FromContext(cmd.Context()); w != nil {
							w.Errorf("%v", err)
						}
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
					options.Name = remoteRef.Name
					if remoteRef.Tag != "" {
						options.Version = remoteRef.Tag
					}
				} else {
					// Local name@version
					ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, options.Tag)
					if err != nil {
						if w := writer.FromContext(cmd.Context()); w != nil {
							w.Errorf("invalid tag %q: %v", options.Tag, err)
						}
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
					// Reject @latest — build requires a concrete version (from
					// metadata or explicit flag). ParseReference normalizes
					// "latest" to nil, which would silently drop the user's intent.
					if ref.Version == nil && strings.Contains(options.Tag, "@") {
						err := fmt.Errorf("--tag %q: 'latest' is not a valid build version; specify a concrete semver", options.Tag)
						if w := writer.FromContext(cmd.Context()); w != nil {
							w.Errorf("%v", err)
						}
						return exitcode.WithCode(err, exitcode.InvalidInput)
					}
					options.Name = ref.Name
					if ref.Version != nil {
						options.Version = ref.Version.String()
					}
				}
			}

			if options.File == "" {
				getter := get.NewGetterFromContext(cmd.Context())
				options.File = getter.FindSolution()
				if options.File == "" {
					err := fmt.Errorf("no -f/--file specified and no solution file found in default locations")
					if w := writer.FromContext(cmd.Context()); w != nil {
						w.Errorf("%v", err)
					}
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
			}

			// Stdin requires --no-bundle: there is no local directory to discover files from.
			if options.File == "-" && !options.NoBundle {
				if w := writer.FromContext(cmd.Context()); w != nil {
					w.Infof("stdin input implies --no-bundle (no local directory for file discovery)")
				}
				options.NoBundle = true
			}

			return runBuildSolution(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVarP(&options.File, "file", "f", "", "Path to the solution file (auto-discovered if not provided, use '-' for stdin)")
	cmd.Flags().StringVarP(&options.Tag, "tag", "t", "", "Artifact reference as name[@version] or full remote ref (version defaults to solution metadata if omitted)")
	cmd.Flags().StringVar(&options.Name, "name", "", "Artifact name (default: extracted from solution metadata)")
	cmd.Flags().StringVar(&options.Version, "version", "", "Semantic version (default: extracted from solution metadata)")
	cmd.Flags().BoolVar(&options.Force, "force", false, "Overwrite existing version in catalog")
	cmd.Flags().BoolVar(&options.NoBundle, "no-bundle", false, "Skip bundling entirely (store only the solution YAML)")
	cmd.Flags().BoolVar(&options.NoVendor, "no-vendor", false, "Skip catalog dependency vendoring")
	cmd.Flags().StringVar(&options.BundleMaxSize, "bundle-max-size", "50MB", "Maximum total size for bundled files")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be bundled without storing")
	cmd.Flags().BoolVar(&options.Dedupe, "dedupe", true, "Enable content-addressable deduplication")
	cmd.Flags().StringVar(&options.DedupeThreshold, "dedupe-threshold", "4KB", "Minimum file size for individual layer extraction (smaller files are tarred together)")
	cmd.Flags().BoolVar(&options.NoCache, "no-cache", false, "Skip build cache and force a full rebuild")
	cmd.Flags().BoolVar(&options.SkipLint, "skip-lint", false, "Skip lint pre-flight check")
	cmd.Flags().BoolVar(&options.SkipTests, "skip-tests", false, "Skip functional test pre-flight check")
	cmd.Flags().BoolVar(&options.IgnorePreflight, "ignore-preflight", false, "Run pre-flight checks but proceed even if they fail")
	cmd.Flags().BoolVar(&options.AllowDevVersion, "allow-dev-version", false, "Allow build without metadata.version set")
	cmd.Flags().StringVar(&options.BaseDir, "base-dir", "", "Override base directory for resolving relative paths in the solution (default: solution file's directory)")

	return cmd
}

func runBuildSolution(ctx context.Context, opts *SolutionOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	w.Verbosef("Reading solution from %s", opts.File)

	// Read solution file (or stdin)
	var content []byte
	var err error
	if opts.File == "-" {
		stdinReader := io.Reader(os.Stdin)
		if opts.IOStreams != nil && opts.IOStreams.In != nil {
			stdinReader = opts.IOStreams.In
		}
		content, err = io.ReadAll(stdinReader)
		if err != nil {
			w.Errorf("failed to read from stdin: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	} else {
		content, err = os.ReadFile(opts.File)
		if err != nil {
			w.Errorf("failed to read solution file: %v", err)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
	}

	// Parse solution to extract metadata
	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		w.Errorf("failed to parse solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	w.Verbosef("Parsed solution: %s (apiVersion: %s)", sol.Metadata.Name, sol.APIVersion)

	// Determine bundle root (--base-dir flag, directory containing the solution file, or cwd for stdin)
	var bundleRoot string
	switch {
	case opts.BaseDir != "":
		bundleRoot, err = filepath.Abs(opts.BaseDir)
		if err != nil {
			w.Errorf("--base-dir: %v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	case opts.File == "-":
		bundleRoot, err = os.Getwd()
		if err != nil {
			w.Errorf("failed to determine working directory: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	default:
		absFile, absErr := provider.AbsFromContext(ctx, opts.File)
		if absErr != nil {
			w.Errorf("failed to resolve path: %v", absErr)
			return exitcode.WithCode(absErr, exitcode.InvalidInput)
		}
		bundleRoot = filepath.Dir(absFile)
	}

	// Determine artifact name (priority: --name flag > metadata.name > filename)
	fileHint := opts.File
	if fileHint == "-" {
		fileHint = "" // stdin has no filename to derive a name from
	}
	name, err := solution.ResolveArtifactName(opts.Name, sol.Metadata.Name, fileHint)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Determine version (priority: --version flag > metadata.version > auto-increment)
	version, versionOverride, err := solution.ResolveArtifactVersion(opts.Version, sol.Metadata.Version)
	if err != nil {
		// No explicit version and no metadata version — try auto-increment
		if opts.Version == "" && sol.Metadata.Version == nil {
			localCat, catErr := catalog.NewLocalCatalog(*lgr)
			if catErr == nil {
				nextVer, nextErr := solution.NextPatchVersion(ctx, localCat, catalog.ArtifactKindSolution, name)
				if nextErr == nil {
					version = nextVer
					w.Infof("Auto-incremented version to %s", version.String())
					err = nil
				}
			}
		}
		if err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}
	if versionOverride {
		w.Warningf("--version %s overrides metadata.version %s", version.String(), sol.Metadata.Version.String())
	}
	w.Verbosef("Resolved artifact: %s@%s", name, version.String())

	// Block dev version unless explicitly allowed
	if version.Equal(solution.DefaultVersion()) && !opts.AllowDevVersion {
		w.Errorf("metadata.version is not set (resolved to %s)", version.String())
		binaryName := settings.BinaryNameFromContext(ctx)
		if opts.CliParams != nil && opts.CliParams.BinaryName != "" {
			binaryName = opts.CliParams.BinaryName
		}
		w.Infof("Set metadata.version, pass --version, or use %s build solution --allow-dev-version to build anyway", binaryName)
		return exitcode.Errorf("dev version not allowed")
	}

	// Stamp resolved name and version into the solution so the stored
	// artifact always carries the authoritative values.
	needsReserialization := false
	if sol.Metadata.Name != name {
		// Name mismatch: error unless --force
		if sol.Metadata.Name != "" && !opts.Force {
			w.Errorf("name mismatch: --name %q does not match metadata.name %q", name, sol.Metadata.Name)
			w.Infof("Use --force to override the metadata name")
			return exitcode.Errorf("name mismatch")
		}
		lgr.V(1).Info("stamping artifact name into solution", "from", sol.Metadata.Name, "to", name)
		sol.Metadata.Name = name
		needsReserialization = true
	}
	if sol.Metadata.Version == nil || !sol.Metadata.Version.Equal(version) {
		lgr.V(1).Info("stamping artifact version into solution", "from", sol.Metadata.Version, "to", version.String())
		sol.Metadata.Version = version
		needsReserialization = true
	}
	if needsReserialization {
		w.Verbose("Stamping resolved name/version into solution YAML")
		content, err = sol.ToYAML()
		if err != nil {
			w.Errorf("failed to serialize stamped solution: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	}

	// === Pre-flight checks ===
	if !opts.DryRun {
		var reg *provider.Registry
		if !opts.SkipLint {
			var regErr error
			reg, regErr = builtin.DefaultRegistry(ctx)
			if regErr != nil {
				reg = provider.GetGlobalRegistry()
			}
		}

		var binaryPath string
		if !opts.SkipTests {
			binaryPath, _ = os.Executable()
		}

		// For stdin builds, pass empty path so preflight skips file-based test
		// discovery (there is no local file to discover tests from).
		preflightPath := opts.File
		if preflightPath == "-" {
			preflightPath = ""
		}

		w.Verbose("Running pre-flight checks")
		pfResult, pfErr := builder.RunPreflight(ctx, &sol, preflightPath, builder.PreflightOptions{
			SkipLint:        opts.SkipLint,
			SkipTests:       opts.SkipTests,
			IgnorePreflight: opts.IgnorePreflight,
			BinaryPath:      binaryPath,
			Registry:        reg,
			Logger:          *lgr,
		})
		if pfErr != nil {
			w.Errorf("pre-flight checks failed: %v", pfErr)
			return exitcode.WithCode(pfErr, exitcode.GeneralError)
		}

		for _, msg := range pfResult.Messages {
			w.Infof("  %s", msg)
		}

		if pfResult.Blocked {
			w.Error("build blocked by pre-flight checks (use --skip-lint/--skip-tests to skip, or --ignore-preflight to override)")
			return exitcode.Errorf("pre-flight checks failed")
		}
	}

	// === Bundle pipeline ===
	var br *bundleResult

	if !opts.NoBundle {
		br, err = buildBundle(ctx, &sol, content, bundleRoot, opts, w)
		if err != nil {
			return err
		}
	}

	// Handle build cache hit — artifact unchanged since last build
	if br != nil && br.CacheHit {
		// Verify the artifact still exists in the local catalog. It may have
		// been deleted (e.g., via `catalog delete`) while the build cache
		// entry remained on disk.
		localCat, catErr := catalog.NewLocalCatalog(*lgr)
		catalogMissing := false
		if catErr == nil {
			ref := catalog.Reference{
				Kind:    catalog.ArtifactKindSolution,
				Name:    br.CacheEntry.ArtifactName,
				Version: semver.MustParse(br.CacheEntry.ArtifactVersion),
			}
			_, existsErr := localCat.Resolve(ctx, ref)
			catalogMissing = existsErr != nil
		}

		if !catalogMissing {
			w.Successf("Build cache hit: %s@%s (unchanged)", br.CacheEntry.ArtifactName, br.CacheEntry.ArtifactVersion)
			w.Infof("  Digest: %s", br.CacheEntry.ArtifactDigest)
			w.Infof("  Use --no-cache to force a full rebuild")
			w.Verbosef("Fingerprint: %s (%d input files)", br.CacheEntry.Fingerprint, br.CacheEntry.InputFiles)
			return nil
		}

		// Artifact missing from catalog — fall through to re-store it.
		w.Verbose("Cache hit but artifact missing from catalog, re-storing")
		lgr.V(1).Info("build cache hit but artifact missing from catalog, re-storing",
			"name", br.CacheEntry.ArtifactName,
			"version", br.CacheEntry.ArtifactVersion)
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
	w.Verbose("Opening local catalog")
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	w.Verbosef("Catalog path: %s", localCatalog.Path())

	// Re-serialize the solution (it may have been modified by compose/vendor)
	if !opts.NoBundle && (len(sol.Compose) > 0 || len(sol.Bundle.Include) > 0) {
		content, err = sol.ToYAML()
		if err != nil {
			w.Errorf("failed to serialize composed solution: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	}

	// Store the artifact (handles dedup vs traditional, plus build cache)
	w.Verbose("Storing artifact in local catalog")
	storeResult, err := builder.StoreSolutionArtifact(ctx, localCatalog, name, version, content, br, builder.StoreOptions{
		Force:            opts.Force,
		Source:           opts.File,
		DisplayName:      sol.Metadata.DisplayName,
		Description:      sol.Metadata.Description,
		Category:         sol.Metadata.Category,
		Tags:             sol.Metadata.Tags,
		ArtifactCacheDir: paths.ArtifactCacheDir(),
		ArtifactCacheTTL: settings.DefaultArtifactCacheTTL,
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
	if storeResult.CacheWritten {
		w.Verbose("Build cache entry written")
	}
	if br != nil && br.BuildFingerprint != "" {
		w.Verbosef("Fingerprint: %s (%d input files)", br.BuildFingerprint, br.InputFileCount)
	}

	return nil
}

// buildBundle delegates to the shared builder.BuildBundle pipeline and
// displays progress messages via the writer.
func buildBundle(ctx context.Context, sol *solution.Solution, solutionContent []byte, bundleRoot string, opts *SolutionOptions, w *writer.Writer) (*bundleResult, error) {
	lgr := logger.FromContext(ctx)

	w.Verbosef("Bundle root: %s", bundleRoot)
	if opts.Dedupe {
		w.Verbosef("Deduplication enabled (threshold: %s)", opts.DedupeThreshold)
	}

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
		if br.Discovery != nil {
			w.Verbosef("Discovered %d local file(s), %d catalog ref(s)", len(br.Discovery.LocalFiles), len(br.Discovery.CatalogRefs))
		}
		if br.CacheHit {
			w.Verbose("Build cache hit — skipping bundle creation")
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
