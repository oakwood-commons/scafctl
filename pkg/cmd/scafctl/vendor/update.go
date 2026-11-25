// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

const defaultSolutionPath = "./solution.yaml"

// ValidVendorUpdateOutputTypes lists the machine-readable output formats accepted by -o/--output.
var ValidVendorUpdateOutputTypes = []string{"json", "yaml"}

// UpdateOptions holds options for the vendor update command.
type UpdateOptions struct {
	BinaryName   string
	SolutionPath string
	Dependencies []string
	DryRun       bool
	LockOnly     bool
	PreRelease   bool
	Output       string
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams
}

type vendorUpdateDeps struct {
	solutionFetcher bundler.CatalogFetcher
	buildPluginOpts func(ctx context.Context, w *writer.Writer) (bundler.VendorAllPluginsOptions, error)
}

func resolveBinaryName(ctx context.Context, cliParams *settings.Run, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if cliParams != nil && cliParams.BinaryName != "" {
		return cliParams.BinaryName
	}
	if fromCtx := settings.BinaryNameFromContext(ctx); fromCtx != "" {
		return fromCtx
	}
	return settings.CliBinaryName
}

// CommandUpdate creates the vendor update subcommand.
func CommandUpdate(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	helpBinName := resolveBinaryName(context.Background(), cliParams, "")
	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update vendored dependencies and locked plugins",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Re-resolve and update vendored dependencies and locked plugins without a full rebuild.

			Parses the solution YAML (composing partial files when compose is configured)
			and solution.lock, re-resolves sub-solution catalog references and bundle.plugins
			entries against the live catalog chain (respecting version constraints),
			fetches updated sub-solution files into .scafctl/vendor/, and updates solution.lock.

			Use --dependency to update a specific sub-solution or plugin only.
			Use --dry-run to preview what would be updated without writing files.
			Use --lock-only to update solution.lock without writing vendored files.
			Use -o json or -o yaml to emit a machine-readable report on stdout.

			Examples:
			  # Update all vendored dependencies and plugins (auto-discover solution)
			  scafctl vendor update

			  # Update from a specific solution file
			  scafctl vendor update -f ./my-solution.yaml

			  # Preview updates without making changes
			  scafctl vendor update --dry-run

			  # Update only a specific sub-solution or plugin
			  scafctl vendor update --dependency deploy-to-k8s
			  scafctl vendor update --dependency provider/exec

			  # Update lock file only (don't re-vendor files)
			  scafctl vendor update --lock-only

			  # Emit a machine-readable update report on stdout
			  scafctl vendor update --dry-run -o json
		`), settings.CliBinaryName, helpBinName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.SolutionPath == "" {
				path, err := load.DiscoverPath(cmd.Context(), load.DiscoveryRiskLow)
				if err != nil {
					path = defaultSolutionPath
				}
				opts.SolutionPath = path
			}
			return runVendorUpdate(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.SolutionPath, "file", "f", "", "Path to the solution file (auto-discovered if not provided)")
	cmd.Flags().StringSliceVar(&opts.Dependencies, "dependency", nil, "Update only this dependency or plugin (repeatable)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be updated without making changes")
	cmd.Flags().BoolVar(&opts.LockOnly, "lock-only", false, "Update lock file without re-vendoring files")
	cmd.Flags().BoolVar(&opts.PreRelease, "pre-release", false, "Include pre-release versions when resolving")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "", fmt.Sprintf("Emit a machine-readable update report on stdout. One of: (%s)", strings.Join(ValidVendorUpdateOutputTypes, ", ")))

	return cmd
}

func vendorWriter(ctx context.Context, opts *UpdateOptions) (context.Context, *writer.Writer) {
	w := writer.FromContext(ctx)
	if w == nil {
		ioStreams := opts.IOStreams
		if ioStreams == nil {
			ioStreams = terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, false)
		}
		cliParams := opts.CliParams
		if cliParams == nil {
			cliParams = settings.NewCliParams()
		}
		w = writer.New(ioStreams, cliParams)
	}
	if opts.Output != "" {
		w = w.Clone(writer.WithHumanToStderr())
	}
	return writer.WithWriter(ctx, w), w
}

func runVendorUpdate(ctx context.Context, opts *UpdateOptions) error {
	ctx, w := vendorWriter(ctx, opts)
	if err := output.ValidateOutputType(opts.Output, ValidVendorUpdateOutputTypes); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if opts.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	lgr := logger.FromContext(ctx)
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	appCfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)

	solChain, err := catalog.BuildCatalogChain(
		appCfg,
		authReg,
		*lgr,
		catalog.WithLocalCatalog(localCatalog),
		catalog.WithoutRemoteCache(),
	)
	if err != nil {
		w.Errorf("failed to build catalog chain: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	var cacheCat *catalog.LocalCatalog
	if !opts.DryRun {
		cacheCat = localCatalog
	}
	deps := vendorUpdateDeps{
		solutionFetcher: &bundler.CatalogChainAdapter{
			Catalog:      solChain,
			LocalCatalog: cacheCat,
		},
		buildPluginOpts: func(pctx context.Context, pw *writer.Writer) (bundler.VendorAllPluginsOptions, error) {
			chainOpts := []catalog.ChainCatalogOption{
				catalog.WithLocalCatalog(localCatalog),
				catalog.WithoutRemoteCache(),
			}
			if pw != nil {
				chainOpts = append(chainOpts, catalog.WithPluginWarningHandler(func(msg string) {
					pw.WarnStderrf("%s", msg)
				}))
			}
			pluginFetcher, pfErr := catalog.BuildPluginFetcher(appCfg, authReg, *lgr, chainOpts...)
			if pfErr != nil {
				return bundler.VendorAllPluginsOptions{}, fmt.Errorf("building plugin catalog chain: %w", pfErr)
			}
			verifySig, sigErr := builder.BuildLockSignatureVerifier(pctx, *lgr)
			if sigErr != nil {
				return bundler.VendorAllPluginsOptions{}, sigErr
			}
			return bundler.VendorAllPluginsOptions{
				CatalogResolver: catalogindex.FromCatalogs(pluginFetcher.Catalogs()),
				PluginFetcher:   pluginFetcher,
				Platform:        runtime.GOOS + "/" + runtime.GOARCH,
				VerifySignature: verifySig,
			}, nil
		},
	}

	return runVendorUpdateWithDeps(ctx, opts, deps)
}

func runVendorUpdateWithFetcher(ctx context.Context, opts *UpdateOptions, fetcher bundler.CatalogFetcher) error {
	return runVendorUpdateWithDeps(ctx, opts, vendorUpdateDeps{solutionFetcher: fetcher})
}

func runVendorUpdateWithDeps(ctx context.Context, opts *UpdateOptions, deps vendorUpdateDeps) error {
	binName := resolveBinaryName(ctx, opts.CliParams, opts.BinaryName)
	ctx, w := vendorWriter(ctx, opts)

	if err := output.ValidateOutputType(opts.Output, ValidVendorUpdateOutputTypes); err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if opts.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	lgr := logger.FromContext(ctx)
	absPath, err := provider.AbsFromContext(ctx, opts.SolutionPath)
	if err != nil {
		w.Errorf("failed to resolve path: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	composedSol, bundleRoot, err := bundler.LoadComposedSolution(absPath)
	if err != nil {
		if errors.Is(err, bundler.ErrSolutionRead) {
			w.Errorf("failed to read solution file %s: %v", opts.SolutionPath, err)
			return exitcode.WithCode(err, exitcode.FileNotFound)
		}
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	lockPath := filepath.Join(bundleRoot, bundler.DefaultLockFileName)
	existingLock, err := bundler.LoadLockFile(lockPath)
	if err != nil {
		w.Errorf("failed to load lock file: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if existingLock == nil {
		w.Errorf("no lock file found at %s; run '%s package solution' first", lockPath, binName)
		return exitcode.Errorf("no lock file")
	}
	lgr.V(1).Info("loaded lock file", "path", lockPath, "deps", len(existingLock.Dependencies), "plugins", len(existingLock.Plugins))

	var pluginOpts *bundler.VendorAllPluginsOptions
	if deps.buildPluginOpts != nil && (len(composedSol.Bundle.Plugins) > 0 || len(existingLock.Plugins) > 0) {
		builtOpts, pErr := deps.buildPluginOpts(ctx, w)
		if pErr != nil {
			w.Errorf("failed to check plugin updates: %v", pErr)
			return exitcode.WithCode(pErr, exitcode.CatalogError)
		}
		pluginOpts = &builtOpts
	}

	result, err := bundler.UpdateVendoredDependencies(ctx, composedSol, existingLock, bundler.VendorUpdateRunOptions{
		BundleRoot:      bundleRoot,
		LockPath:        lockPath,
		Dependencies:    opts.Dependencies,
		DryRun:          opts.DryRun,
		LockOnly:        opts.LockOnly,
		SolutionFetcher: deps.solutionFetcher,
		PluginOptions:   pluginOpts,
	})
	if err != nil {
		if errors.Is(err, bundler.ErrInvalidUpdateTarget) {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		w.Errorf("failed to update dependencies: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	renderVendorUpdateProgress(opts, w, result)
	return emitVendorUpdateReport(opts, w, *result)
}

func renderVendorUpdateProgress(opts *UpdateOptions, w *writer.Writer, result *bundler.VendorUpdateResult) {
	w.Plainln("")
	if opts.DryRun {
		w.Plainlnf("Checking vendored dependencies for %s...", opts.SolutionPath)
	} else {
		w.Plainln("Updating vendored dependencies...")
	}
	w.Plainln("")

	for _, entry := range result.Entries {
		switch entry.Status {
		case bundler.UpdateStatusPruned:
			if opts.DryRun {
				w.Plainlnf("  %s: locked %s (would prune)", entry.Ref, displayLockedVersion(entry.LockedVersion))
			} else {
				w.Successf("  %s: pruned (%s)", entry.Ref, displayLockedVersion(entry.LockedVersion))
			}
		case bundler.UpdateStatusAdded, bundler.UpdateStatusUpdated:
			if opts.DryRun {
				w.Plainlnf("  %s:", entry.Ref)
				w.Plainlnf("    locked:   %s (%s)", displayLockedVersion(entry.LockedVersion), displayLockedDigest(entry.LockedDigest))
				w.Plainlnf("    latest:   %s (%s)", entry.LatestVersion, bundler.TruncateDigest(entry.LatestDigest))
				w.Plainln("    action:   would update")
			} else {
				w.Successf("  %s: %s -> %s (%s -> %s)", entry.Ref, displayLockedVersion(entry.LockedVersion), entry.LatestVersion, displayLockedDigest(entry.LockedDigest), bundler.TruncateDigest(entry.LatestDigest))
			}
		default:
			w.Plainlnf("  - %s: up to date (%s)", entry.Ref, entry.LatestVersion)
		}
	}

	for _, pe := range result.PluginEntries {
		switch pe.Status {
		case bundler.UpdateStatusPruned:
			if opts.DryRun {
				w.Plainlnf("  %s (%s): locked %s (would prune)", pe.DisplayName(), pe.Kind, displayLockedVersion(pe.LockedVersion))
			} else {
				w.Successf("  %s (%s): pruned (%s)", pe.DisplayName(), pe.Kind, displayLockedVersion(pe.LockedVersion))
			}
		case bundler.UpdateStatusAdded, bundler.UpdateStatusUpdated:
			if opts.DryRun {
				w.Plainlnf("  %s (%s):", pe.DisplayName(), pe.Kind)
				w.Plainlnf("    locked:   %s (%s)", displayLockedVersion(pe.LockedVersion), displayLockedDigest(pe.LockedDigest))
				w.Plainlnf("    latest:   %s (%s)", pe.LatestVersion, bundler.TruncateDigest(pe.LatestDigest))
				w.Plainln("    action:   would update")
			} else {
				w.Successf("  %s (%s): %s -> %s (%s -> %s)", pe.DisplayName(), pe.Kind, displayLockedVersion(pe.LockedVersion), pe.LatestVersion, displayLockedDigest(pe.LockedDigest), bundler.TruncateDigest(pe.LatestDigest))
			}
		default:
			w.Plainlnf("  - %s (%s): %s up to date (%s)", pe.DisplayName(), pe.Kind, pe.LatestVersion, bundler.TruncateDigest(pe.LatestDigest))
		}
	}

	if len(result.PluginEntries) == 0 {
		for _, msg := range result.PluginMessages {
			w.Plainln(msg)
		}
	}
	w.Plainln("")

	switch {
	case opts.DryRun && result.UpdateCount > 0:
		w.Plainlnf("Summary: %d dependency(ies) would be updated", result.UpdateCount)
	case opts.DryRun:
		w.Plainln("All dependencies are up to date")
	case result.UpdateCount == 0:
		w.Plainln("All dependencies are up to date")
	default:
		w.Successf("Updated %s", bundler.DefaultLockFileName)
	}
}

func emitVendorUpdateReport(opts *UpdateOptions, w *writer.Writer, result bundler.VendorUpdateResult) error {
	if opts.Output == "" {
		return nil
	}
	var ioStreams *terminal.IOStreams
	if w != nil {
		ioStreams = w.IOStreams()
	}
	if ioStreams == nil {
		ioStreams = opts.IOStreams
	}
	if ioStreams == nil {
		ioStreams = terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, false)
	}
	if err := output.WriteOutput(ioStreams, opts.Output, result, nil); err != nil {
		if w != nil {
			w.Errorf("failed to write output: %v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	return nil
}

func displayLockedVersion(v string) string {
	if v == "" {
		return "(unlocked)"
	}
	return v
}

func displayLockedDigest(d string) string {
	if d == "" {
		return "none"
	}
	return bundler.TruncateDigest(d)
}
