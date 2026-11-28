// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// UpdateOptions holds options for the update command.
type UpdateOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	CacheDir  string
	Target    string
	Platform  string
	NoCache   bool
	DryRun    bool
	All       bool
	flags.KvxOutputFlags
}

// updateResultItem is the display-friendly representation of an update entry.
type updateResultItem struct {
	Name       string `json:"name" yaml:"name"`
	OldVersion string `json:"oldVersion" yaml:"oldVersion"`
	NewVersion string `json:"newVersion" yaml:"newVersion"`
	Status     string `json:"status" yaml:"status"`
}

// updateColumnHints controls table column display for update results.
var updateColumnHints = map[string]tui.ColumnHint{
	"name":       {MaxWidth: 25, Priority: 10},
	"oldVersion": {MaxWidth: 15, Priority: 9, DisplayName: "FROM"},
	"newVersion": {MaxWidth: 15, Priority: 8, DisplayName: "TO"},
	"status":     {MaxWidth: 12, Priority: 7},
}

// CommandUpdate creates the update subcommand.
func CommandUpdate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	binaryName := settings.CliBinaryName
	if cliParams != nil && cliParams.BinaryName != "" {
		binaryName = cliParams.BinaryName
	}

	opts := &UpdateOptions{
		CliParams:      cliParams,
		IOStreams:      ioStreams,
		KvxOutputFlags: flags.KvxOutputFlags{AppName: binaryName},
	}

	cmd := &cobra.Command{
		Use:          "update [plugin-name ...]",
		Short:        "Update cached plugins to newer versions",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Check for and install newer versions of cached plugins.

			Plugin names can include an exact version (name@version) to pin
			to a specific release. Use --target to constrain how far updates
			are allowed to move (latest, minor, patch).

			Examples:
			  # Update all cached plugins to latest
			  scafctl plugins update --all

			  # Preview updates without downloading
			  scafctl plugins update --all --dry-run

			  # Update specific plugins
			  scafctl plugins update github exec

			  # Update to exact version
			  scafctl plugins update exec@0.6.0

			  # Constrain to same major version
			  scafctl plugins update --all --target minor

			  # Constrain to same minor version
			  scafctl plugins update github --target patch
		`), settings.CliBinaryName, binaryName),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)

			lgr := logger.FromContext(cmd.Context())
			if lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnHints(updateColumnHints),
				kvx.WithOutputColumnOrder([]string{"name", "oldVersion", "newVersion", "status"}),
			)

			return runUpdate(ctx, opts, args, kvxOpts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Update all cached plugins (required when no positional args)")
	cmd.Flags().StringVar(&opts.Target, "target", "latest", "Version boundary: latest, minor (same major/^), patch (same minor/~)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview what would change without downloading")
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "Target platform override")
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Force fresh fetch from catalog")
	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", fmt.Sprintf("Plugin cache directory (default: $XDG_CACHE_HOME/%s/plugins/)", path))
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

func runUpdate(ctx context.Context, opts *UpdateOptions, args []string, kvxOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Parse name@version from args.
	var names []string
	var pinned map[string]string // name -> exact version
	for _, arg := range args {
		name, ver := parseNameVersion(arg)
		names = append(names, name)
		if ver != "" {
			if pinned == nil {
				pinned = make(map[string]string)
			}
			pinned[name] = ver
		}
	}

	if len(names) == 0 && !opts.All {
		err := fmt.Errorf("no plugin names specified; use positional args or --all")
		w.Errorf("%s", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Validate target.
	target := plugin.UpdateTarget(opts.Target)
	switch target {
	case plugin.UpdateTargetLatest, plugin.UpdateTargetMinor, plugin.UpdateTargetPatch:
		// valid
	default:
		err := fmt.Errorf("invalid --target %q (valid: latest, minor, patch)", opts.Target)
		w.Errorf("%s", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		var binaryName string
		if opts.CliParams != nil {
			binaryName = opts.CliParams.BinaryName
		}
		cacheDir = settings.PluginCacheDirFor(binaryName)
	}

	cache := plugin.NewCache(cacheDir)
	quietOutput := kvxOpts != nil && kvx.IsQuietFormat(kvxOpts.Format)

	// Build catalog chain.
	appCfg := config.FromContext(ctx)
	lgr := logger.FromContext(ctx)
	var chainLogger logr.Logger
	if lgr != nil {
		chainLogger = *lgr
	} else {
		chainLogger = logr.Discard()
	}

	remoteFetcher, err := catalog.BuildRemotePluginFetcher(
		appCfg,
		auth.RegistryFromContext(ctx),
		chainLogger,
		catalog.WithPluginWarningHandler(func(msg string) {
			if !quietOutput {
				w.WarnStderrf("%s", msg)
			}
		}),
	)
	if err != nil {
		// Remote catalogs may not be configured (e.g. offline, no config).
		// Proceed without a catalog — pinned versions and empty caches still
		// work. PlanUpdates will fail for non-pinned plugins that need
		// catalog resolution, which is the correct behavior.
		chainLogger.V(1).Info("remote catalogs not available, proceeding without catalog", "error", err)
	}

	var catalogFetcher plugin.CatalogFetcher
	if remoteFetcher != nil {
		catalogFetcher = remoteFetcher
	}

	// Plan updates.
	plan, err := plugin.PlanUpdates(ctx, cache, catalogFetcher, plugin.UpdateOptions{
		Names:    names,
		All:      opts.All,
		Target:   target,
		Platform: opts.Platform,
	})
	if err != nil {
		w.Errorf("failed to plan updates: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Resolve effective platform for pinned version lookups.
	pinnedPlatform := opts.Platform
	if pinnedPlatform == "" {
		pinnedPlatform = plugin.CurrentPlatform()
	}

	// Apply pinned versions — override plan entries.
	for name, ver := range pinned {
		// Pinned versions bypass catalog lookup; drop any catalog-resolution
		// failure recorded for this plugin before evaluating the pin against cache.
		plan.Failed = removeFailedByName(plan.Failed, name)

		found := false
		for i := range plan.Updates {
			if plan.Updates[i].Name == name {
				plan.Updates[i].NewVersion = ver
				found = true
				break
			}
		}
		if !found {
			// Add pinned version even if plan didn't detect it as an update.
			_, currentVer, ok := cache.GetLatestCached(name, pinnedPlatform)
			if !ok {
				plan.Failed = append(plan.Failed, plugin.UpdateError{
					Name:  name,
					Error: "not found in cache; use 'plugins install' to add it",
				})
				continue
			}
			_, inferredKind := plugin.PluginKindFromCacheKey(name)
			if ver != currentVer {
				plan.Updates = append(plan.Updates, plugin.UpdateEntry{
					Name:       name,
					OldVersion: currentVer,
					NewVersion: ver,
					Kind:       string(inferredKind),
				})
			}
		}
	}

	// Report failures.
	if !quietOutput {
		for _, f := range plan.Failed {
			w.WarnStderrf("%s: %s", f.Name, f.Error)
		}
	}

	if len(plan.Updates) == 0 {
		if err := writeEmptyUpdateResults(kvxOpts); err != nil {
			return err
		}
		if len(plan.Failed) > 0 {
			return failedPlanError(plan.Failed)
		}
		if !quietOutput {
			w.PlainStderrf("All plugins are up to date.")
		}
		return nil
	}

	// Build display items.
	items := make([]updateResultItem, 0, len(plan.Updates))
	for _, u := range plan.Updates {
		status := "pending"
		items = append(items, updateResultItem{
			Name:       u.Name,
			OldVersion: u.OldVersion,
			NewVersion: u.NewVersion,
			Status:     status,
		})
	}

	if opts.DryRun {
		if !quietOutput {
			w.PlainStderrf("Dry run: %d plugin(s) would be updated:", len(plan.Updates))
		}
		if err := kvxOpts.Write(items); err != nil {
			return err
		}
		return failedPlanError(plan.Failed)
	}

	// Guard: downloading requires a remote catalog chain.
	if catalogFetcher == nil {
		err := fmt.Errorf("cannot download updates: no remote catalogs available")
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	sigPolicy, sigErr := prepare.ResolveSignaturePolicy(ctx, appCfg, nil, chainLogger)
	if sigErr != nil {
		w.Errorf("%v", sigErr)
		return exitcode.WithCode(sigErr, exitcode.ConfigError)
	}

	// Perform the updates by fetching new versions.
	if !quietOutput {
		w.PlainStderrf("Updating %d plugin(s)...", len(plan.Updates))
	}

	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		CatalogFetcher:  catalogFetcher,
		Cache:           cache,
		Platform:        opts.Platform,
		NoCache:         opts.NoCache,
		BinaryName:      settings.BinaryNameFromContext(ctx),
		Logger:          chainLogger,
		SignaturePolicy: sigPolicy,
	})

	var deps []solution.PluginDependency
	for _, u := range plan.Updates {
		bareName, kind := plugin.PluginKindFromCacheKey(u.Name)
		deps = append(deps, solution.PluginDependency{
			Name:    bareName,
			Kind:    kind,
			Version: u.NewVersion,
		})
	}

	results, err := fetcher.FetchPlugins(ctx, deps, nil)
	if err != nil {
		w.Errorf("failed to fetch updates: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Build result display.
	resultItems := make([]updateResultItem, 0, len(results))
	for i, r := range results {
		status := "updated"
		if r.FromCache {
			status = "cached"
		}
		resultItems = append(resultItems, updateResultItem{
			Name:       r.Name,
			OldVersion: plan.Updates[i].OldVersion,
			NewVersion: r.Version,
			Status:     status,
		})
	}

	if err := kvxOpts.Write(resultItems); err != nil {
		return fmt.Errorf("rendering output: %w", err)
	}

	// Invalidate descriptor cache.
	descCache := plugin.NewDescriptorCache("", 0)
	descCache.InvalidateAll()

	if !quietOutput {
		w.PlainStderrf("Updated %d plugin(s).", len(results))
	}
	return failedPlanError(plan.Failed)
}

func removeFailedByName(failed []plugin.UpdateError, name string) []plugin.UpdateError {
	if len(failed) == 0 {
		return failed
	}
	out := failed[:0]
	for _, f := range failed {
		if f.Name != name {
			out = append(out, f)
		}
	}
	return out
}

func failedPlanError(failed []plugin.UpdateError) error {
	if len(failed) == 0 {
		return nil
	}
	if len(failed) == 1 {
		return exitcode.WithCode(fmt.Errorf("%s: %s", failed[0].Name, failed[0].Error), exitcode.CatalogError)
	}
	return exitcode.WithCode(
		fmt.Errorf("failed to resolve %d plugins (first: %s: %s)", len(failed), failed[0].Name, failed[0].Error),
		exitcode.CatalogError,
	)
}

// writeEmptyUpdateResults writes [] to real stdout for structured, interactive,
// and expression modes, and routes human/quiet modes through an io.Discard stream
// so unsupported formats (-o test) and malformed --where filters still fail validation
// without rendering an empty kvx box on stdout.
func writeEmptyUpdateResults(kvxOpts *kvx.OutputOptions) error {
	if kvxOpts == nil {
		return nil
	}
	if kvx.IsStructuredFormat(kvxOpts.Format) || kvxOpts.Interactive || kvxOpts.Expression != "" {
		return kvxOpts.Write([]updateResultItem{})
	}
	discardOpts := *kvxOpts
	discardStreams := &terminal.IOStreams{Out: io.Discard, ErrOut: io.Discard}
	if kvxOpts.IOStreams != nil {
		cp := *kvxOpts.IOStreams
		cp.Out = io.Discard
		discardStreams = &cp
	}
	discardOpts.IOStreams = discardStreams
	return discardOpts.Write([]updateResultItem{})
}
