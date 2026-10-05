// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/git"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ValidPackageOutputTypes lists the machine-readable formats accepted by the
// package/build solution -o/--output flag.
var ValidPackageOutputTypes = []string{"json", "yaml"}

// SolutionOptions holds options for the package solution command.
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
	AllowDirty      bool
	NoGitMetadata   bool
	BaseDir         string
	Bump            string
	Strict          bool
	NoVerify        bool
	Output          string
	ComposedOut     string
	CliParams       *settings.Run
	IOStreams       *terminal.IOStreams
}

// CommandPackageSolution creates the package solution command.
func CommandPackageSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &SolutionOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "solution",
		Aliases:      []string{"sol", "s"},
		Short:        "Package a solution into the local catalog",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Package a solution file into the local catalog.

			The solution is packaged as an OCI artifact with the specified name and version.
			If name is not specified, it is extracted from the solution metadata.
			If version is not specified, it is extracted from the solution metadata;
			a solution without a version is packaged as the next patch of the latest
			locally built version (0.1.0 for the first build).

			The packaging process:
			  1. Runs pre-flight checks (lint and functional tests)
			  2. Composes multi-file solutions (if compose: is set)
			  3. Discovers local file dependencies via static analysis
			  4. Expands bundle.include globs
			  5. Vendors catalog dependencies and pins plugins (unless --no-vendor)
			  6. Creates a bundle tar with all discovered files
			  7. Verifies the bundle is complete (unless --no-verify)
			  8. Stores the solution YAML + bundle as an OCI artifact

			Nothing is stored when verification fails.

			Use --no-bundle to skip bundling entirely (store only the solution YAML).
			Use --dry-run to see what would be bundled without writing anything.

			Examples:
			  # Package solution using version from metadata (auto-discovery)
			  scafctl package solution

			  # Package solution from a specific file
			  scafctl package solution -f ./my-solution.yaml

			  # Package with explicit version (overrides metadata)
			  scafctl package solution -f ./solution.yaml --version 1.0.0

			  # Package with explicit name
			  scafctl package solution -f ./solution.yaml --name my-solution --version 1.0.0

			  # Package with a tag (shorthand for --name and --version)
			  scafctl package solution -f ./solution.yaml -t my-solution@1.0.0

			  # Package with a full remote reference (for push later)
			  scafctl package solution -t ghcr.io/myorg/solutions/my-solution@1.0.0

			  # Overwrite existing version
			  scafctl package solution -f ./solution.yaml --version 1.0.0 --force

			  # Preview what would be bundled
			  scafctl package solution -f ./solution.yaml --dry-run

			  # Package without bundling
			  scafctl package solution -f ./solution.yaml --no-bundle

			  # Emit a machine-readable build report on stdout (progress goes to stderr)
			  scafctl package solution -f ./solution.yaml -o json

			  # Preview the build result as JSON without storing anything
			  scafctl package solution -f ./solution.yaml --dry-run -o json

			  # Write the composed (flattened) solution to a file
			  scafctl package solution -f ./solution.yaml --composed-out composed.yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPackageSolution(cmd.Context(), options)
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
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show what would be bundled without writing anything")
	cmd.Flags().BoolVar(&options.Dedupe, "dedupe", true, "Enable content-addressable deduplication")
	cmd.Flags().StringVar(&options.DedupeThreshold, "dedupe-threshold", "4KB", "Minimum file size for individual layer extraction (smaller files are tarred together)")
	cmd.Flags().BoolVar(&options.NoCache, "no-cache", false, "Skip build cache and force a full rebuild")
	cmd.Flags().BoolVar(&options.SkipLint, "skip-lint", false, "Skip lint pre-flight check")
	cmd.Flags().BoolVar(&options.SkipTests, "skip-tests", false, "Skip functional test pre-flight check")
	cmd.Flags().BoolVar(&options.IgnorePreflight, "ignore-preflight", false, "Run pre-flight checks but proceed even if they fail")
	cmd.Flags().BoolVar(&options.AllowDevVersion, "allow-dev-version", false, "Allow packaging under the development placeholder version (0.0.0-dev)")
	cmd.Flags().BoolVar(&options.AllowDirty, "allow-dirty", false, "Suppress warning when building from a dirty working tree")
	cmd.Flags().BoolVar(&options.NoGitMetadata, "no-git-metadata", false, "Skip git commit and dirty-state annotations on the built artifact")
	cmd.Flags().StringVar(&options.BaseDir, "base-dir", "", "Override base directory for resolving relative paths in the solution (default: solution file's directory)")
	cmd.Flags().StringVar(&options.Bump, "bump", "", "Auto-increment version based on last built version (patch, minor, major)")
	cmd.Flags().BoolVar(&options.Strict, "strict", false, "Fail if the built bundle has completeness warnings")
	cmd.Flags().BoolVar(&options.NoVerify, "no-verify", false, "Skip built-bundle completeness verification")
	cmd.Flags().StringVarP(&options.Output, "output", "o", "", fmt.Sprintf("Emit a machine-readable build report on stdout. One of: (%s)", strings.Join(ValidPackageOutputTypes, ", ")))
	cmd.Flags().StringVar(&options.ComposedOut, "composed-out", "", "Write the composed (flattened) solution document to this path (.json for JSON, otherwise YAML)")

	return cmd
}

// packageInputs are the flag values parsed and checked before any I/O.
type packageInputs struct {
	identity builder.IdentityOptions
	archive  builder.ArchiveOptions
}

// validate checks every flag that can be checked without I/O and returns the
// parsed values.
func (o *SolutionOptions) validate() (packageInputs, error) {
	if err := output.ValidateOutputType(o.Output, ValidPackageOutputTypes); err != nil {
		return packageInputs{}, err
	}
	identity := builder.IdentityOptions{
		Name:            o.Name,
		Version:         o.Version,
		Tag:             o.Tag,
		Bump:            o.Bump,
		AllowDevVersion: o.AllowDevVersion,
	}
	if err := identity.Validate(); err != nil {
		return packageInputs{}, err
	}
	limits, err := builder.ParseBundleLimits(o.BundleMaxSize, o.DedupeThreshold)
	if err != nil {
		return packageInputs{}, err
	}
	return packageInputs{
		identity: identity,
		archive:  builder.ArchiveOptions{Limits: limits, Dedupe: o.Dedupe},
	}, nil
}

// runPackageSolution validates the flags, loads the solution, resolves and
// stamps its identity, runs the package pipeline, and renders the result.
//
// TODO: with -o json|yaml, only the pipeline/verify path emits a report on
// stdout. Failures from validate, catalog open, loadSource, resolveIdentity,
// stampSource, and the store step return via fail() with nothing on stdout,
// so machine consumers get an empty document. Emit a failed-status report
// envelope (status + diagnostics) for every failure path when -o is set.
func runPackageSolution(ctx context.Context, opts *SolutionOptions) error {
	ctx, w := packageWriter(ctx, opts)

	in, err := opts.validate()
	if err != nil {
		return fail(w, err, exitcode.InvalidInput)
	}

	cat, err := catalog.NewLocalCatalog(*logger.FromContext(ctx))
	if err != nil {
		return fail(w, fmt.Errorf("failed to open catalog: %w", err), exitcode.CatalogError)
	}

	src, err := loadSource(ctx, w, opts)
	if err != nil {
		return err
	}

	id, err := resolveIdentity(ctx, w, opts, cat, src, in.identity)
	if err != nil {
		return err
	}

	if err := stampSource(ctx, w, opts, src, id); err != nil {
		return err
	}

	packager, err := builder.NewPackager(cat)
	if err != nil {
		return fail(w, err, exitcode.CatalogError)
	}
	res, err := packager.
		Solution(src).
		As(id.Ref).
		Preflight(preflightOptions(ctx, opts)).
		Vendor(builder.VendorOptions{Skip: opts.NoVendor}).
		Cache(builder.CacheOptions{Skip: opts.NoCache}).
		Archive(in.archive).
		ComposedOut(opts.ComposedOut).
		DryRun(opts.DryRun).
		Store(opts.Force).
		Verify(builder.VerifyOptions{Skip: opts.NoVerify, Strict: opts.Strict}).
		Do(ctx)

	r := &packageRenderer{ctx: ctx, w: w, opts: opts, catalogPath: cat.Path(), src: src, id: id}
	return r.render(res, err)
}

// packageWriter returns the writer for progress output, creating one from
// the command's streams when the context has none. With -o, human progress
// goes to stderr so stdout carries only the report. An existing writer is
// cloned so root-level options (e.g. RootOptions.ExitFunc) are preserved.
func packageWriter(ctx context.Context, opts *SolutionOptions) (context.Context, *writer.Writer) {
	w := writer.FromContext(ctx)
	if w == nil {
		w = writer.New(opts.IOStreams, opts.CliParams)
	}
	if opts.Output != "" {
		w = w.Clone(writer.WithHumanToStderr())
	}
	return writer.WithWriter(ctx, w), w
}

// fail prints err and returns it with the given exit code.
func fail(w *writer.Writer, err error, code int) error {
	w.Errorf("%v", err)
	return exitcode.WithCode(err, code)
}

// loadSource loads the solution from -f, stdin, or auto-discovery.
func loadSource(ctx context.Context, w *writer.Writer, opts *SolutionOptions) (*builder.SolutionSource, error) {
	w.Verbosef("Reading solution from %s", displayPath(opts.File))
	src, err := builder.LoadSolutionSource(ctx, opts.File, opts.BaseDir, stdinReader(opts), builder.WithNoBundle(opts.NoBundle))
	if err != nil {
		return nil, fail(w, err, loadErrorCode(err))
	}
	if _, stdinForced := src.ShouldBundle(); stdinForced {
		w.Infof("stdin input implies --no-bundle (no local directory for file discovery)")
	}
	w.Verbosef("Parsed solution: %s (apiVersion: %s)", src.Solution.Metadata.Name, src.Solution.APIVersion)
	return src, nil
}

// displayPath names the -f value for progress output.
func displayPath(file string) string {
	if file == "" {
		return "auto-discovered solution file"
	}
	return file
}

// stdinReader returns the reader a "-f -" solution is read from.
func stdinReader(opts *SolutionOptions) io.Reader {
	if opts.IOStreams != nil && opts.IOStreams.In != nil {
		return opts.IOStreams.In
	}
	return os.Stdin
}

// loadErrorCode maps a load failure to an exit code.
func loadErrorCode(err error) int {
	if errors.Is(err, fs.ErrNotExist) {
		return exitcode.FileNotFound
	}
	return exitcode.InvalidInput
}

// resolveIdentity picks the artifact name and version and explains where the
// version came from.
func resolveIdentity(ctx context.Context, w *writer.Writer, opts *SolutionOptions, cat *catalog.LocalCatalog, src *builder.SolutionSource, idOpts builder.IdentityOptions) (*builder.Identity, error) {
	id, err := builder.ResolveIdentity(ctx, cat, src, idOpts)
	if err != nil {
		return nil, identityError(ctx, w, opts, err)
	}
	reportVersion(w, opts, id)
	w.Verbosef("Resolved artifact: %s@%s", id.Ref.Name, id.Ref.Version)
	return id, nil
}

// identityError prints an identity failure and maps it to an exit code.
func identityError(ctx context.Context, w *writer.Writer, opts *SolutionOptions, err error) error {
	switch {
	case errors.Is(err, builder.ErrDevVersion):
		w.Errorf("%v", err)
		w.Infof("Pass a release version, or use %s package solution --allow-dev-version to package anyway", binaryName(ctx, opts))
		return exitcode.WithCode(err, exitcode.InvalidInput)
	case errors.Is(err, builder.ErrInvalidIdentity):
		return fail(w, err, exitcode.InvalidInput)
	default:
		return fail(w, fmt.Errorf("resolving version from the local catalog: %w", err), exitcode.CatalogError)
	}
}

// reportVersion tells the user how a version they did not set was chosen,
// and warns when a flag replaced metadata.version.
func reportVersion(w *writer.Writer, opts *SolutionOptions, id *builder.Identity) {
	switch id.VersionSource {
	case builder.VersionFromBump:
		w.Infof("Bumped version to %s (%s)", id.Ref.Version, opts.Bump)
	case builder.VersionFromAutoIncrement:
		w.Infof("Auto-incremented version to %s", id.Ref.Version)
	case builder.VersionFromFlag, builder.VersionFromTag, builder.VersionFromMetadata:
		// The user chose the version; nothing to explain.
	}
	if id.OverriddenVersion != nil {
		flag := "--version"
		if id.VersionSource == builder.VersionFromTag {
			flag = "--tag"
		}
		w.Warningf("%s %s overrides metadata.version %s", flag, id.Ref.Version, id.OverriddenVersion)
	}
}

// binaryName is the CLI name used in hints.
func binaryName(ctx context.Context, opts *SolutionOptions) string {
	if opts.CliParams != nil && opts.CliParams.BinaryName != "" {
		return opts.CliParams.BinaryName
	}
	return settings.BinaryNameFromContext(ctx)
}

// stampSource writes the identity and git metadata into the solution.
func stampSource(ctx context.Context, w *writer.Writer, opts *SolutionOptions, src *builder.SolutionSource, id *builder.Identity) error {
	_, err := src.Stamp(id, resolveGit(ctx, w, opts, src), builder.StampOptions{AllowRename: opts.Force})
	if errors.Is(err, builder.ErrNameMismatch) {
		w.Errorf("%v", err)
		w.Infof("Use --force to override the metadata name")
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if err != nil {
		return fail(w, err, exitcode.GeneralError)
	}
	return nil
}

// resolveGit reads the commit and dirty state recorded on the artifact.
// Git is best effort: a failure is logged and nothing is recorded.
func resolveGit(ctx context.Context, w *writer.Writer, opts *SolutionOptions, src *builder.SolutionSource) builder.GitMetadata {
	meta, err := builder.ResolveGitMetadata(ctx, git.GetRepositoryStatus, src, !opts.NoGitMetadata)
	if err != nil {
		logger.FromContext(ctx).V(1).Info("git metadata unavailable", "error", err)
		return builder.GitMetadata{}
	}
	if meta.Dirty && !opts.AllowDirty {
		w.Warningf("Building from a dirty working tree (uncommitted changes detected)")
	}
	if meta.Recorded() {
		w.Verbosef("Git metadata: commit=%s dirty=%v", meta.Commit, meta.Dirty)
	}
	return meta
}

// preflightOptions maps the pre-flight flags. Lint validates against the
// built-in providers; tests run this executable.
func preflightOptions(ctx context.Context, opts *SolutionOptions) builder.PreflightOptions {
	pf := builder.PreflightOptions{
		SkipLint:        opts.SkipLint,
		SkipTests:       opts.SkipTests,
		IgnorePreflight: opts.IgnorePreflight,
		Logger:          *logger.FromContext(ctx),
	}
	if !opts.SkipLint {
		pf.Registry = lintRegistry(ctx)
	}
	if !opts.SkipTests {
		pf.BinaryPath, _ = os.Executable()
	}
	return pf
}

// lintRegistry returns the provider registry lint validates against.
func lintRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return provider.GetGlobalRegistry()
	}
	return reg
}
