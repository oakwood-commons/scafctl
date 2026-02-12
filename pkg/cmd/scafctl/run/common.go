// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/solutionprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// runCommandRunner defines the interface for command options that can run
type runCommandRunner interface {
	Run(ctx context.Context) error
}

// runCommandConfig holds common configuration for building run commands
type runCommandConfig struct {
	cliParams     *settings.Run
	ioStreams     *terminal.IOStreams
	path          string
	runner        runCommandRunner
	getOutputFn   func() string
	setIOStreamFn func(ios *terminal.IOStreams, cli *settings.Run)
}

// makeRunEFunc creates a RunE function for run subcommands
func makeRunEFunc(cfg runCommandConfig, cmdUse string) func(*cobra.Command, []string) error {
	return func(cCmd *cobra.Command, args []string) error {
		cfg.cliParams.EntryPointSettings.Path = filepath.Join(cfg.path, cmdUse)
		ctx := settings.IntoContext(context.Background(), cfg.cliParams)

		lgr := logger.FromContext(cCmd.Context())
		if lgr != nil {
			ctx = logger.WithLogger(ctx, lgr)
		}

		// Transfer config from parent context
		if appCfg := config.FromContext(cCmd.Context()); appCfg != nil {
			ctx = config.WithConfig(ctx, appCfg)
		}

		// Transfer auth registry from parent context
		if authRegistry := auth.RegistryFromContext(cCmd.Context()); authRegistry != nil {
			ctx = auth.WithRegistry(ctx, authRegistry)
		}

		// Get writer from parent context or create new one
		w := writer.FromContext(cCmd.Context())
		if w == nil {
			w = writer.New(cfg.ioStreams, cfg.cliParams)
		}
		ctx = writer.WithWriter(ctx, w)

		cfg.setIOStreamFn(cfg.ioStreams, cfg.cliParams)

		// Only validate that there are no unexpected args if the command doesn't
		// explicitly accept positional arguments (via Args field).
		// Commands with Args: cobra.MaximumNArgs(N) handle arg validation themselves.
		if cCmd.Args == nil {
			if err := output.ValidateCommands(args); err != nil {
				w.Error(err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}

		if currentOutput := cfg.getOutputFn(); currentOutput != "" && currentOutput != "quiet" {
			if err := output.ValidateOutputType(currentOutput, ValidOutputTypes); err != nil {
				w.Error(err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
		}

		return cfg.runner.Run(ctx)
	}
}

// sharedResolverOptions holds the resolver-specific fields shared between
// the run solution and run resolver commands.
type sharedResolverOptions struct {
	IOStreams       *terminal.IOStreams
	CliParams       *settings.Run
	File            string
	ResolverParams  []string
	ResolveAll      bool
	Progress        bool
	ValidateAll     bool
	SkipValidation  bool
	SkipTransform   bool
	ShowMetrics     bool
	WarnValueSize   int64
	MaxValueSize    int64
	ResolverTimeout time.Duration
	PhaseTimeout    time.Duration

	// kvx output integration (shared flags)
	flags.KvxOutputFlags

	// Track which flags were explicitly set by user
	flagsChanged map[string]bool

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry
}

// getEffectiveResolverConfig returns resolver config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *sharedResolverOptions) getEffectiveResolverConfig(ctx context.Context) config.ResolverConfigValues {
	// Start with CLI flag values (which already have settings package defaults)
	result := config.ResolverConfigValues{
		Timeout:        o.ResolverTimeout,
		PhaseTimeout:   o.PhaseTimeout,
		MaxConcurrency: 0, // Not currently a CLI flag, use config if available
		WarnValueSize:  o.WarnValueSize,
		MaxValueSize:   o.MaxValueSize,
		ValidateAll:    o.ValidateAll,
	}

	// If config is available, use its values for non-changed flags
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	// Parse config values
	configValues, err := cfg.Resolver.ToResolverValues()
	if err != nil {
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse resolver config, using CLI defaults", "error", err)
		return result
	}

	// Override with config values for flags that weren't explicitly set.
	// Only apply overrides when flagsChanged is set (i.e., we're in command execution flow).
	// When flagsChanged is nil (e.g., in tests), respect the values set on the options struct.
	if o.flagsChanged != nil {
		if !o.flagsChanged["resolver-timeout"] {
			result.Timeout = configValues.Timeout
		}
		if !o.flagsChanged["phase-timeout"] {
			result.PhaseTimeout = configValues.PhaseTimeout
		}
		if !o.flagsChanged["warn-value-size"] {
			result.WarnValueSize = configValues.WarnValueSize
		}
		if !o.flagsChanged["max-value-size"] {
			result.MaxValueSize = configValues.MaxValueSize
		}
		if !o.flagsChanged["validate-all"] {
			result.ValidateAll = configValues.ValidateAll
		}
	}

	// MaxConcurrency always comes from config (no CLI flag for resolver concurrency)
	result.MaxConcurrency = configValues.MaxConcurrency

	return result
}

// getOrCreateGetter returns the injected getter or creates a default one, caching the result.
func (o *sharedResolverOptions) getOrCreateGetter(ctx context.Context) get.Interface {
	if o.getter != nil {
		return o.getter
	}

	lgr := logger.FromContext(ctx)

	getterOpts := []get.Option{
		get.WithLogger(*lgr),
	}

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		catResolver := catalog.NewSolutionResolver(localCatalog, *lgr)
		getterOpts = append(getterOpts, get.WithCatalogResolver(catResolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	o.getter = get.NewGetter(getterOpts...)
	return o.getter
}

// loadSolutionWithBundle loads a solution and extracts its bundle if present.
// Returns the solution, the path to the extracted bundle directory (empty if no bundle),
// and any error. The caller is responsible for cleaning up the bundle directory.
func (o *sharedResolverOptions) loadSolutionWithBundle(ctx context.Context) (*solution.Solution, string, error) {
	lgr := logger.FromContext(ctx)
	getter := o.getOrCreateGetter(ctx)

	// Handle stdin
	if o.File == "-" {
		data, err := io.ReadAll(o.IOStreams.In)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read from stdin: %w", err)
		}

		var sol solution.Solution
		if err := sol.LoadFromBytes(data); err != nil {
			return nil, "", fmt.Errorf("failed to parse solution from stdin: %w", err)
		}
		return &sol, "", nil
	}

	// Use GetWithBundle for catalog solutions to extract bundle
	sol, bundleData, err := getter.GetWithBundle(ctx, o.File)
	if err != nil {
		return nil, "", err
	}

	// If there's bundle data, extract it to a temp directory
	if len(bundleData) > 0 {
		lgr.V(1).Info("extracting solution bundle", "size", len(bundleData))
		tmpDir, err := os.MkdirTemp("", "scafctl-bundle-*")
		if err != nil {
			return nil, "", fmt.Errorf("failed to create temp directory for bundle: %w", err)
		}

		// Write the solution YAML to the temp dir so relative paths work
		solYAML, err := sol.ToYAML()
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to serialize solution: %w", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), solYAML, 0o600); err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to write solution to temp dir: %w", err)
		}

		// Extract bundle tar
		manifest, err := bundler.ExtractBundleTar(bundleData, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to extract bundle: %w", err)
		}

		lgr.V(1).Info("extracted bundle",
			"files", len(manifest.Files),
			"dir", tmpDir)

		return sol, tmpDir, nil
	}

	return sol, "", nil
}

// getRegistry returns the provider registry (creates default if not injected)
func (o *sharedResolverOptions) getRegistry(ctx context.Context) *provider.Registry {
	if o.registry != nil {
		return o.registry
	}

	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		lgr := logger.Get(0)
		lgr.V(0).Info("warning: failed to register some providers", "error", err)
		return provider.GetGlobalRegistry()
	}

	return reg
}

// exitWithCode prints the error message and returns an ExitError with the appropriate code
func (o *sharedResolverOptions) exitWithCode(ctx context.Context, err error, code int) error {
	if w := writer.FromContext(ctx); w != nil {
		w.Errorf("%v", err)
	} else {
		fmt.Fprintf(o.IOStreams.ErrOut, " ❌ %v\n", err)
	}
	return exitcode.WithCode(err, code)
}

// buildResolverOutputMap builds the output map from resolver data with redaction for sensitive values
func (o *sharedResolverOptions) buildResolverOutputMap(resolverData map[string]any, sol *solution.Solution) map[string]any {
	results := make(map[string]any)

	for name, value := range resolverData {
		if r, ok := sol.Spec.Resolvers[name]; ok && r.Sensitive {
			results[name] = "[REDACTED]"
		} else {
			results[name] = value
		}
	}

	return results
}

// checkValueSizes checks if any values exceed size limits
func (o *sharedResolverOptions) checkValueSizes(results map[string]any, lgr logr.Logger) error {
	for name, value := range results {
		size := calculateValueSize(value)

		if o.MaxValueSize > 0 && size > o.MaxValueSize {
			return fmt.Errorf("resolver %q value exceeds maximum size: %d > %d bytes", name, size, o.MaxValueSize)
		}

		if o.WarnValueSize > 0 && size > o.WarnValueSize {
			lgr.V(0).Info("resolver value exceeds recommended size",
				"resolver", name,
				"size", size,
				"limit", o.WarnValueSize)
		}
	}

	return nil
}

// writeResolverOutput writes the resolver results in the specified format using the shared kvx output handler.
func (o *sharedResolverOptions) writeResolverOutput(ctx context.Context, results map[string]any, appName string) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(appName),
		kvx.WithOutputHelp(appName, []string{
			"Resolver Results Viewer",
			"",
			"Navigate: ↑↓ arrows | Back: ← | Enter: →",
			"Search: / or F3 | Expression: F6",
			"Copy path: F5 | Quit: q or F10",
		}),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(results)
}

// executeResolvers runs the resolver execution pipeline on the given resolvers.
// Returns the resolver data map (name -> value), the resolver context with full
// execution metadata, and any error.
func (o *sharedResolverOptions) executeResolvers(
	ctx context.Context,
	sol *solution.Solution,
	resolvers []*resolver.Resolver,
	params map[string]any,
	reg *provider.Registry,
) (map[string]any, *resolver.Context, error) {
	lgr := logger.FromContext(ctx)

	resolverData := make(map[string]any)
	if len(resolvers) == 0 {
		lgr.V(0).Info("no resolvers to execute")
		return resolverData, resolver.NewContext(), nil
	}

	resolverAdapter := &registryAdapter{registry: reg}

	// Set up progress reporter if enabled
	var progress *ProgressReporter
	var progressCallback *ProgressCallback
	if o.Progress {
		progress = NewProgressReporter(o.IOStreams.ErrOut, len(resolvers))
		progressCallback = NewProgressCallback(progress)
	}

	// Get effective resolver config (CLI flags override app config)
	resolverCfg := o.getEffectiveResolverConfig(ctx)

	// Create executor with options
	executorOpts := []resolver.ExecutorOption{
		resolver.WithDefaultTimeout(resolverCfg.Timeout),
		resolver.WithPhaseTimeout(resolverCfg.PhaseTimeout),
	}
	if resolverCfg.MaxConcurrency > 0 {
		executorOpts = append(executorOpts, resolver.WithMaxConcurrency(resolverCfg.MaxConcurrency))
	}
	if resolverCfg.WarnValueSize > 0 {
		executorOpts = append(executorOpts, resolver.WithWarnValueSize(resolverCfg.WarnValueSize))
	}
	if resolverCfg.MaxValueSize > 0 {
		executorOpts = append(executorOpts, resolver.WithMaxValueSize(resolverCfg.MaxValueSize))
	}
	if progressCallback != nil {
		executorOpts = append(executorOpts, resolver.WithProgressCallback(progressCallback))
	}
	if resolverCfg.ValidateAll {
		executorOpts = append(executorOpts, resolver.WithValidateAll(true))
	}
	if o.SkipValidation {
		executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
	}
	if o.SkipTransform {
		executorOpts = append(executorOpts, resolver.WithSkipTransform(true))
	}
	executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

	// Execute resolvers
	resultCtx, err := executor.Execute(ctx, resolvers, params)
	if err != nil {
		if progress != nil {
			progress.Wait()
		}
		return nil, nil, fmt.Errorf("resolver execution failed: %w", err)
	}

	// Get resolver context with results
	resolverCtx, ok := resolver.FromContext(resultCtx)
	if !ok {
		if progress != nil {
			progress.Wait()
		}
		return nil, nil, fmt.Errorf("failed to retrieve resolver results")
	}

	// Build resolver data map
	for name := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if ok && result.Status == resolver.ExecutionStatusSuccess {
			resolverData[name] = result.Value
		}
	}

	// Wait for progress bars to complete
	if progress != nil {
		progress.Wait()
	}

	lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	return resolverData, resolverCtx, nil
}

// filterResolversWithDependencies returns the specified resolvers and all their dependencies.
// When targetNames is empty, all resolvers are returned.
// Uses resolver.ExtractDependencies to detect dependencies from CEL expressions,
// Go templates, explicit rslvr: references, and provider-specific extraction.
func filterResolversWithDependencies(resolvers []*resolver.Resolver, targetNames []string, lookup resolver.DescriptorLookup) []*resolver.Resolver {
	if len(targetNames) == 0 {
		return resolvers
	}

	// Build a map of resolvers by name
	resolverMap := make(map[string]*resolver.Resolver)
	for _, r := range resolvers {
		resolverMap[r.Name] = r
	}

	// Collect all dependencies recursively
	needed := make(map[string]bool)
	var collectDeps func(name string)
	collectDeps = func(name string) {
		if needed[name] {
			return
		}
		needed[name] = true

		r, exists := resolverMap[name]
		if !exists {
			return
		}

		deps := resolver.ExtractDependencies(r, lookup)
		for _, dep := range deps {
			collectDeps(dep)
		}
	}

	for _, name := range targetNames {
		if _, exists := resolverMap[name]; exists {
			collectDeps(name)
		}
	}

	// Filter resolvers to only those needed
	var result []*resolver.Resolver
	for _, r := range resolvers {
		if needed[r.Name] {
			result = append(result, r)
		}
	}

	return result
}

// prepareSolutionForExecution loads a solution, sets up the provider registry,
// and registers the solution provider. It handles bundle extraction, plugin merging,
// and working directory changes. Returns cleanup function that must be deferred.
func (o *sharedResolverOptions) prepareSolutionForExecution(ctx context.Context) (*solution.Solution, *provider.Registry, func(), error) {
	lgr := logger.FromContext(ctx)

	// Enable metrics collection if requested
	if o.ShowMetrics {
		provider.GlobalMetrics.Enable()
	}

	// Load the solution (with bundle if available)
	sol, bundleDir, err := o.loadSolutionWithBundle(ctx)
	if err != nil {
		return nil, nil, func() {}, err
	}

	// Build cleanup function
	cleanup := func() {
		if o.ShowMetrics {
			writeMetrics(o.IOStreams.ErrOut)
		}
		if bundleDir != "" {
			os.RemoveAll(bundleDir)
		}
	}

	// Change to bundle directory if needed
	if bundleDir != "" {
		originalDir, wdErr := os.Getwd()
		if wdErr != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("failed to get working directory: %w", wdErr)
		}
		if chErr := os.Chdir(bundleDir); chErr != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("failed to change to bundle directory: %w", chErr)
		}
		origCleanup := cleanup
		cleanup = func() {
			_ = os.Chdir(originalDir)
			origCleanup()
		}
		lgr.V(1).Info("using bundle extraction directory as working directory", "dir", bundleDir)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"hasResolvers", sol.Spec.HasResolvers(),
		"hasWorkflow", sol.Spec.HasWorkflow())

	// Merge plugin defaults into provider inputs before DAG construction
	if len(sol.Bundle.Plugins) > 0 {
		bundler.MergePluginDefaults(sol)
		lgr.V(1).Info("merged plugin defaults", "pluginCount", len(sol.Bundle.Plugins))
	}

	// Set up provider registry
	reg := o.getRegistry(ctx)

	// Register the solution provider
	if !reg.Has(solutionprovider.ProviderName) {
		solGetter := o.getOrCreateGetter(ctx)
		solProvider := solutionprovider.New(
			solutionprovider.WithLoader(solGetter),
			solutionprovider.WithRegistry(reg),
		)
		if err := reg.Register(solProvider); err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("registering solution provider: %w", err)
		}
	}

	return sol, reg, cleanup, nil
}

// addSharedResolverFlags adds common resolver flags to a cobra command.
func addSharedResolverFlags(cCmd *cobra.Command, o *sharedResolverOptions) {
	cCmd.Flags().StringVarP(&o.File, "file", "f", "", "Solution file path or catalog name (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&o.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	flags.AddKvxOutputFlagsToStruct(cCmd, &o.KvxOutputFlags)

	cCmd.Flags().BoolVar(&o.ResolveAll, "resolve-all", false, "Execute all resolvers regardless of action requirements")
	cCmd.Flags().BoolVar(&o.Progress, "progress", false, "Show execution progress (output to stderr)")
	cCmd.Flags().BoolVar(&o.ValidateAll, "validate-all", false, "Continue execution and show all validation/resolver errors")
	cCmd.Flags().BoolVar(&o.SkipValidation, "skip-validation", false, "Skip the validation phase of all resolvers")
	cCmd.Flags().BoolVar(&o.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion (output to stderr)")
	cCmd.Flags().Int64Var(&o.WarnValueSize, "warn-value-size", settings.DefaultWarnValueSize, "Warn when value exceeds this size in bytes (default: 1MB)")
	cCmd.Flags().Int64Var(&o.MaxValueSize, "max-value-size", settings.DefaultMaxValueSize, "Fail when value exceeds this size in bytes (default: 10MB)")
	cCmd.Flags().DurationVar(&o.ResolverTimeout, "resolver-timeout", settings.DefaultResolverTimeout, "Timeout per resolver")
	cCmd.Flags().DurationVar(&o.PhaseTimeout, "phase-timeout", settings.DefaultPhaseTimeout, "Timeout per resolver phase")
}

// writeMetrics outputs provider execution metrics to stderr
func writeMetrics(errOut io.Writer) {
	allMetrics := provider.GlobalMetrics.GetAllMetrics()
	if len(allMetrics) == 0 {
		return
	}

	fmt.Fprintln(errOut, "")
	fmt.Fprintln(errOut, "Provider Execution Metrics:")
	fmt.Fprintln(errOut, strings.Repeat("-", 80))
	fmt.Fprintf(errOut, "%-25s %8s %8s %8s %12s %12s\n",
		"Provider", "Total", "Success", "Failure", "Avg Duration", "Success %")
	fmt.Fprintln(errOut, strings.Repeat("-", 80))

	// Sort provider names for consistent output
	names := make([]string, 0, len(allMetrics))
	for name := range allMetrics {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		m := allMetrics[name]
		avgDuration := m.AverageDuration()
		successRate := m.SuccessRate()
		fmt.Fprintf(errOut, "%-25s %8d %8d %8d %12s %11.1f%%\n",
			name,
			m.ExecutionCount,
			m.SuccessCount,
			m.FailureCount,
			avgDuration.Round(time.Millisecond),
			successRate)
	}
	fmt.Fprintln(errOut, strings.Repeat("-", 80))
}

// calculateValueSize estimates the size of a value in bytes
func calculateValueSize(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return int64(len(data))
}

// registryAdapter adapts provider.Registry to resolver.RegistryInterface
type registryAdapter struct {
	registry *provider.Registry
}

func (r *registryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *registryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

func (r *registryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

// buildExecutionData constructs structured execution metadata from resolver results.
// The returned map is extensible — new top-level sections can be added without
// breaking consumers (e.g. "diagrams", "timeline", "warnings").
func buildExecutionData(
	resolverCtx *resolver.Context,
	resolvers []*resolver.Resolver,
	totalElapsed time.Duration,
) map[string]any {
	allResults := resolverCtx.GetAllResults()

	// Build per-resolver execution metadata
	resolverMeta := make(map[string]any, len(resolvers))
	for _, r := range resolvers {
		deps := resolver.ExtractDependencies(r, nil)
		entry := map[string]any{
			"provider":     resolverProviderName(r),
			"dependencies": deps,
		}

		if result, ok := allResults[r.Name]; ok {
			entry["phase"] = result.Phase
			entry["duration"] = result.TotalDuration.Round(time.Millisecond).String()
			entry["status"] = string(result.Status)
			entry["providerCallCount"] = result.ProviderCallCount
			entry["valueSizeBytes"] = result.ValueSizeBytes
			entry["dependencyCount"] = result.DependencyCount

			// Per-phase breakdown (resolve, transform, validate)
			if len(result.PhaseMetrics) > 0 {
				metrics := make([]map[string]any, 0, len(result.PhaseMetrics))
				for _, pm := range result.PhaseMetrics {
					metrics = append(metrics, map[string]any{
						"phase":    pm.Phase,
						"duration": pm.Duration.Round(time.Millisecond).String(),
					})
				}
				entry["phaseMetrics"] = metrics
			}

			// Include failed attempts for debugging
			if len(result.FailedAttempts) > 0 {
				attempts := make([]map[string]any, 0, len(result.FailedAttempts))
				for _, fa := range result.FailedAttempts {
					attempt := map[string]any{
						"provider":   fa.Provider,
						"phase":      fa.Phase,
						"duration":   fa.Duration.Round(time.Millisecond).String(),
						"sourceStep": fa.SourceStep,
					}
					if fa.Error != "" {
						attempt["error"] = fa.Error
					}
					if fa.OnError != "" {
						attempt["onError"] = fa.OnError
					}
					attempts = append(attempts, attempt)
				}
				entry["failedAttempts"] = attempts
			}
		} else {
			entry["phase"] = 0
			entry["duration"] = "0s"
			entry["status"] = "unknown"
		}

		resolverMeta[r.Name] = entry
	}

	// Build summary
	phaseCount := 0
	for _, result := range allResults {
		if result.Phase > phaseCount {
			phaseCount = result.Phase
		}
	}

	summary := map[string]any{
		"totalDuration": totalElapsed.Round(time.Millisecond).String(),
		"resolverCount": len(resolvers),
		"phaseCount":    phaseCount,
	}

	return map[string]any{
		"resolvers": resolverMeta,
		"summary":   summary,
	}
}

// buildProviderSummary aggregates per-provider usage statistics from resolver execution.
func buildProviderSummary(
	resolverCtx *resolver.Context,
	resolvers []*resolver.Resolver,
) map[string]any {
	allResults := resolverCtx.GetAllResults()

	type providerStats struct {
		count         int
		totalDuration time.Duration
		callCount     int
		successCount  int
		failedCount   int
	}

	stats := make(map[string]*providerStats)

	for _, r := range resolvers {
		provName := resolverProviderName(r)
		ps, ok := stats[provName]
		if !ok {
			ps = &providerStats{}
			stats[provName] = ps
		}
		ps.count++

		if result, ok := allResults[r.Name]; ok {
			ps.totalDuration += result.TotalDuration
			ps.callCount += result.ProviderCallCount
			if result.Status == resolver.ExecutionStatusSuccess {
				ps.successCount++
			} else {
				ps.failedCount++
			}
		}
	}

	summary := make(map[string]any, len(stats))
	for name, ps := range stats {
		entry := map[string]any{
			"resolverCount": ps.count,
			"totalDuration": ps.totalDuration.Round(time.Millisecond).String(),
			"callCount":     ps.callCount,
			"successCount":  ps.successCount,
			"failedCount":   ps.failedCount,
		}
		if ps.count > 0 {
			entry["avgDuration"] = (ps.totalDuration / time.Duration(ps.count)).Round(time.Millisecond).String()
		}
		summary[name] = entry
	}

	return summary
}

// renderGraph renders a graph in the specified format using the graphRenderer interface.
func renderGraph(w io.Writer, graph graphRenderer, data any, format string) error {
	switch format {
	case "ascii":
		return graph.RenderASCII(w)
	case "dot":
		return graph.RenderDOT(w)
	case "mermaid":
		return graph.RenderMermaid(w)
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	default:
		return fmt.Errorf("unsupported graph format: %s (supported: ascii, dot, mermaid, json)", format)
	}
}

// graphRenderer defines the interface for types that can render as ASCII, DOT, and Mermaid.
type graphRenderer interface {
	RenderASCII(w io.Writer) error
	RenderDOT(w io.Writer) error
	RenderMermaid(w io.Writer) error
}

// resolverProviderName extracts the primary provider name from a resolver
func resolverProviderName(r *resolver.Resolver) string {
	if r.Resolve != nil && len(r.Resolve.With) > 0 {
		return r.Resolve.With[0].Provider
	}
	return "unknown"
}

func (r *registryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}
