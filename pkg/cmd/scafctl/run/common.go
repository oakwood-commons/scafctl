// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	catversion "github.com/oakwood-commons/scafctl/pkg/catalog/version"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/state"
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
		ctx := settings.IntoContext(cCmd.Context(), cfg.cliParams)

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

// ResolverParametersHelp is the help text block for resolver parameter
// passing conventions including positional args, used by run resolver.
const ResolverParametersHelp = `RESOLVER PARAMETERS:
  Parameters can be passed in two equivalent ways:

  1. Positional key=value (recommended):
       key=value         After resolver names or on its own
       key=@-            Read raw stdin as value for key
       key=@file         Read raw file content as value for key
       @file.yaml        Load parameters from a file (parsed as YAML/JSON)
       @-                Read parameters from stdin (parsed as YAML/JSON)

  2. Explicit -r/--resolver flag:
       -r key=value      Repeatable flag
       -r key=val1,val2  Multiple values become an array
       -r key=@-         Read raw stdin as value for key
       -r key=@file      Read raw file content as value for key
       -r @file.yaml     Load parameters from a YAML file
       -r @file.json     Load parameters from a JSON file
       -r @-             Read parameters from stdin (YAML or JSON)

  Both forms can be mixed. When the same key appears multiple
  times, values are merged into an array rather than replaced.

  Note: @- cannot be combined with -f - (both read from stdin).

  Bare words (without '=') are treated as resolver names (or the solution
  reference if -f is not provided — see SOLUTION SOURCE above).
  Words containing '=' or starting with '@' are treated as parameters.`

// ResolverParametersFlagHelp is the flag-only help text block for resolver
// parameter passing, used by run solution (which does not accept positional
// key=value parameters).
const ResolverParametersFlagHelp = `RESOLVER PARAMETERS:
  Parameters are passed using the -r/--resolver flag:
    -r key=value      Repeatable flag
    -r key=val1,val2  Multiple values become an array
    -r key=@-         Read raw stdin as value for key
    -r key=@file      Read raw file content as value for key
    -r @file.yaml     Load parameters from a YAML file
    -r @file.json     Load parameters from a JSON file
    -r @-             Read parameters from stdin (YAML or JSON)

  When the same key appears multiple times, values are merged
  into an array rather than replaced.

  Note: @- cannot be combined with -f - (both read from stdin).`

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
	ShowSensitive   bool
	NoCache         bool
	WarnValueSize   int64
	MaxValueSize    int64
	ResolverTimeout time.Duration
	PhaseTimeout    time.Duration

	// VersionConstraint is a semver version constraint (e.g., ^1.0.0, ~2.1).
	// When set, the best matching version from the catalog is resolved before
	// fetching the solution. Mutually exclusive with @version in the name.
	VersionConstraint string

	// OutputDir is the target directory for action file operations.
	// When set, actions resolve relative paths against this directory instead of CWD.
	// Resolvers are unaffected and always use CWD.
	OutputDir string

	// BaseDir overrides the base directory for resolver path resolution.
	// When set, resolver-phase relative paths resolve from this directory
	// instead of CWD. Use "." to explicitly set CWD.
	BaseDir string

	// PreRelease includes pre-release versions (e.g. 1.0.0-beta.1) when
	// resolving the latest catalog version. By default, pre-release versions
	// are excluded.
	PreRelease bool

	// kvx output integration (shared flags)
	flags.KvxOutputFlags

	// TestName is the desired test name when using -o test output format.
	// When empty, a name is derived from the command and resolver parameters.
	TestName string

	// Track which flags were explicitly set by user
	flagsChanged map[string]bool

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry

	// discoveryMode controls which file names auto-discovery searches for.
	discoveryMode settings.DiscoveryMode
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

// exitWithCode prints the error message and returns an ExitError with the appropriate code
func (o *sharedResolverOptions) exitWithCode(ctx context.Context, err error, code int) error {
	if w := writer.FromContext(ctx); w != nil {
		w.Errorf("%v", err)
	}
	return exitcode.WithCode(err, code)
}

// buildResolverOutputMap builds the output map from resolver data with format-aware redaction for sensitive values.
// Sensitive values are redacted in table/interactive output (human-facing) but revealed in structured
// output formats (json, yaml) since those are typically used for machine consumption.
// Use --show-sensitive to reveal values in all formats.
func (o *sharedResolverOptions) buildResolverOutputMap(resolverData map[string]any, sol *solution.Solution) map[string]any {
	results := make(map[string]any)

	// Determine whether to redact: redact in table/interactive (human-facing) output,
	// reveal in structured output (json/yaml) for machine consumption.
	// --show-sensitive overrides to always reveal.
	shouldRedact := o.shouldRedactSensitive()

	for name, value := range resolverData {
		if shouldRedact {
			if r, ok := sol.Spec.Resolvers[name]; ok && r.Sensitive {
				results[name] = "[REDACTED]"
				continue
			}
		}
		results[name] = value
	}

	return results
}

// shouldRedactSensitive determines whether sensitive values should be redacted based on
// the output format and --show-sensitive flag. Following the Terraform model:
// - Table/interactive output: redacted (human-facing)
// - JSON/YAML output: revealed (machine-facing)
// - --show-sensitive: always reveals regardless of format
func (o *sharedResolverOptions) shouldRedactSensitive() bool {
	if o.ShowSensitive {
		return false
	}

	// Structured formats (json, yaml, quiet) are for machine consumption — don't redact
	format := o.Output
	switch format {
	case "json", "yaml", "quiet":
		return false
	default:
		// Table and interactive modes are human-facing — redact
		return true
	}
}

// checkValueSizes checks if any values exceed size limits
func (o *sharedResolverOptions) checkValueSizes(results map[string]any, lgr logr.Logger) error {
	for name, value := range results {
		size := execute.CalculateValueSize(value)

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

// generateTestOutput generates a functional test definition from the given resolver results
// and writes test YAML to stdout. It is called by subcommands that detect -o test.
//
// command is the subcommand path (e.g. ["run", "resolver"]).
// extraArgs are positional args specific to the subcommand (e.g. resolver names).
// results is the full output map; __execution is excluded from assertion derivation
// but included in the snapshot for normalization purposes.
func (o *sharedResolverOptions) generateTestOutput(ctx context.Context, command, extraArgs []string, results map[string]any) error {
	// For assertion derivation, exclude __execution metadata because it contains
	// volatile timing data that would create brittle assertions.
	assertionData := make(map[string]any, len(results))
	for k, v := range results {
		if k != "__execution" {
			assertionData[k] = v
		}
	}

	// Serialize the full results (including __execution) for the snapshot normalizer.
	rawJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to marshal resolver output for test generation: %w", err), exitcode.GeneralError)
	}

	// Reconstruct the args the generated test should use (-r params + any extra positional args).
	testArgs := make([]string, 0, len(extraArgs)+len(o.ResolverParams)*2)
	testArgs = append(testArgs, extraArgs...)
	for _, param := range o.ResolverParams {
		testArgs = append(testArgs, "-r", param)
	}

	// Determine testdata/ directory relative to the solution file.
	snapshotDir := "testdata"
	if o.File != "" && o.File != "-" {
		snapshotDir = filepath.Join(filepath.Dir(o.File), "testdata")
	}

	result, err := soltesting.Generate(&soltesting.GenerateInput{
		Command:     command,
		Args:        testArgs,
		TestName:    o.TestName,
		SnapshotDir: snapshotDir,
		Data:        assertionData,
		RawJSON:     rawJSON,
	})
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to generate test: %w", err), exitcode.GeneralError)
	}

	yamlData, err := soltesting.GenerateToYAML(result)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to marshal test YAML: %w", err), exitcode.GeneralError)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Plain(string(yamlData))
		if result.SnapshotWritten {
			w.WarnStderrf("Snapshot written: %s", result.SnapshotPath)
		}
	}
	return nil
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

	resolverAdapter := execute.NewResolverRegistryAdapter(reg)

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

	// Attach solution metadata to the context so providers (e.g., metadata) can access it.
	ctx = provider.WithSolutionMetadata(ctx, solutionMetaFromSolution(sol))

	// Inject IOStreams so streaming providers (message, exec, etc.) can write to the terminal
	// during resolver execution. For structured output modes (json/yaml), route provider
	// stdout to stderr to avoid corrupting the data envelope on stdout.
	// For quiet mode, discard all provider output to honour the --quiet contract.
	if o.IOStreams != nil {
		providerOut := o.IOStreams.Out
		providerErr := o.IOStreams.ErrOut
		switch strings.ToLower(o.Output) {
		case "json", "yaml":
			providerOut = o.IOStreams.ErrOut
		case "quiet":
			providerOut = io.Discard
			providerErr = io.Discard
		}
		ctx = provider.WithIOStreams(ctx, &provider.IOStreams{
			Out:    providerOut,
			ErrOut: providerErr,
		})
	}

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

// prepareSolutionForExecution loads a solution, sets up the provider registry,
// and registers the solution provider. It handles bundle extraction, plugin merging,
// and working directory changes. Returns cleanup function that must be deferred.
//
// solutionDir is the directory containing the solution file (empty for stdin/catalog).
// Callers should use it with provider.WithSolutionDirectory when --base-dir is not set.
//
// This method delegates to the standalone prepare.PrepareSolution function,
// passing CLI-specific options (getter, registry, stdin, metrics).
func (o *sharedResolverOptions) prepareSolutionForExecution(ctx context.Context) (*solution.Solution, *provider.Registry, string, func(), error) {
	w := writer.FromContext(ctx)

	var opts []prepare.Option

	if o.getter != nil {
		opts = append(opts, prepare.WithGetter(o.getter))
	}
	if o.NoCache {
		opts = append(opts, prepare.WithNoCache())
	}
	if o.registry != nil {
		opts = append(opts, prepare.WithRegistry(o.registry))
	}
	if o.IOStreams != nil && o.IOStreams.In != nil {
		opts = append(opts, prepare.WithStdin(o.IOStreams.In))
	}
	if o.ShowMetrics && o.IOStreams != nil {
		opts = append(opts, prepare.WithMetrics(o.IOStreams.ErrOut))
	}
	if o.CliParams != nil {
		opts = append(opts, prepare.WithPluginConfig(&plugin.ProviderConfig{
			Quiet:      o.CliParams.IsQuiet,
			NoColor:    o.CliParams.NoColor,
			BinaryName: o.CliParams.BinaryName,
		}))
	}

	if o.discoveryMode != settings.DiscoveryModeDefault {
		opts = append(opts, prepare.WithDiscoveryMode(o.discoveryMode))
	}

	// Resolve binary name once for verbose output and user-facing messages.
	binaryName := settings.CliBinaryName
	if o.CliParams != nil && o.CliParams.BinaryName != "" {
		binaryName = o.CliParams.BinaryName
	}

	// Emit verbose discovery information before loading
	if w != nil && w.VerboseEnabled() {
		switch o.File {
		case "":
			var customActionFiles []string
			if o.CliParams != nil {
				customActionFiles = o.CliParams.ActionDiscoveryFileNames
			}
			folders := settings.SolutionFoldersFor(binaryName)
			fileNames := settings.FileNamesForMode(o.discoveryMode, binaryName, customActionFiles)
			w.Verbosef("Auto-discovering solution (binary=%s, mode=%s)", binaryName, o.discoveryMode)
			w.Verbosef("  Search folders: %v", folders)
			w.Verbosef("  Search filenames: %v", fileNames)
		case "-":
			w.Verbose("Loading solution from stdin")
		default:
			w.Verbosef("Loading solution from: %s", o.File)
		}
	}

	result, err := prepare.Solution(ctx, o.File, opts...)
	if err != nil {
		return nil, nil, "", func() {}, err
	}

	if w != nil {
		sol := result.Solution
		name := sol.Metadata.Name
		var ver string
		if sol.Metadata.Version != nil {
			ver = sol.Metadata.Version.String()
		}
		source := sol.GetPath()

		if w.VerboseEnabled() {
			w.Verbosef("Solution loaded: %s (version=%s, dir=%s)",
				name, ver, result.SolutionDir)
		}

		// Show a concise summary when verbose is enabled.
		switch {
		case name != "" && ver != "" && source != "":
			w.Verbosef("Solution: %s@%s (%s)", name, ver, source)
		case name != "" && ver != "":
			w.Verbosef("Solution: %s@%s", name, ver)
		case name != "" && source != "":
			w.Verbosef("Solution: %s (%s)", name, source)
		case name != "":
			w.Verbosef("Solution: %s", name)
		case source != "":
			w.Verbosef("Solution: %s", source)
		}

		// Emit discovery-specific informational messages.
		disc := result.DiscoveredFrom
		if disc.AlternatePath != "" {
			if disc.IsActionFile {
				w.Verbosef("  (solution.yaml also found at %s)", disc.AlternatePath)
			} else {
				w.Verbosef("  (actions.yaml also found at %s; use '%s run action' to execute actions)", disc.AlternatePath, binaryName)
			}
		}
	}

	return result.Solution, result.Registry, result.SolutionDir, result.Cleanup, nil
}

// resolveVersionConstraintForFile resolves a --version constraint against the
// catalog and updates o.File to include the best matching version. This must be
// called before prepareSolutionForExecution when VersionConstraint is non-empty.
//
// Only applies when File is a bare catalog name (no path separators or file
// extensions). For file paths and OCI references, --version is an error.
func (o *sharedResolverOptions) resolveVersionConstraintForFile(ctx context.Context) error {
	if o.VersionConstraint == "" {
		return nil
	}

	// Check for @version in the name
	if o.File != "" {
		if idx := strings.LastIndex(o.File, "@"); idx > 0 {
			return fmt.Errorf("cannot use --version with an explicit version in reference %q; use one or the other", o.File)
		}
	}

	name := o.File
	if name == "" {
		return fmt.Errorf("--version requires a catalog name (positional argument or -f flag)")
	}

	// Only applicable to bare catalog names
	if !get.IsCatalogReference(name) || strings.Contains(name, "/") {
		return fmt.Errorf("--version can only be used with catalog names, not file paths or OCI references")
	}

	// Validate constraint syntax early to fail fast before catalog I/O
	if err := catversion.ValidateConstraint(o.VersionConstraint); err != nil {
		return err
	}

	lgr := logger.FromContext(ctx)
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		return fmt.Errorf("--version requires catalog access: %w", err)
	}

	remotes := catalog.RemoteCatalogsFromContext(ctx, *lgr)
	catalogs := make([]catalog.Catalog, 0, 1+len(remotes))
	catalogs = append(catalogs, localCatalog)
	catalogs = append(catalogs, remotes...)

	versions, err := catversion.ListCatalogVersions(ctx, catalogs, catalog.ArtifactKindSolution, name)
	if err != nil {
		return err
	}

	bestVersion, err := catversion.BestMatch(versions, o.VersionConstraint)
	if err != nil {
		return err
	}

	if bestVersion == "" {
		return fmt.Errorf("no versions of %q match constraint %q", name, o.VersionConstraint)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Verbosef("Version constraint %q resolved to %s", o.VersionConstraint, bestVersion)
	}

	o.File = name + "@" + bestVersion
	return nil
}

// addSharedResolverFlags adds common resolver flags to a cobra command.
func addSharedResolverFlags(cCmd *cobra.Command, o *sharedResolverOptions) {
	cCmd.Flags().StringVarP(&o.File, "file", "f", "", "Solution file path or catalog name (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&o.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value, key=@- for raw stdin, @file.yaml, or @- for stdin). Available as __params in state backend expressions")
	flags.AddKvxOutputFlagsToStruct(cCmd, &o.KvxOutputFlags)

	cCmd.Flags().BoolVar(&o.ResolveAll, "resolve-all", false, "Execute all resolvers regardless of action requirements")
	cCmd.Flags().BoolVar(&o.Progress, "progress", false, "Show execution progress (output to stderr)")
	cCmd.Flags().BoolVar(&o.ValidateAll, "validate-all", false, "Continue execution and show all validation/resolver errors")
	cCmd.Flags().BoolVar(&o.SkipValidation, "skip-validation", false, "Skip the validation phase of all resolvers")
	cCmd.Flags().BoolVar(&o.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion (output to stderr)")
	cCmd.Flags().BoolVar(&o.ShowSensitive, "show-sensitive", false, "Reveal sensitive values in all output formats (by default, sensitive values are redacted in table output but shown in json/yaml)")
	cCmd.Flags().BoolVar(&o.NoCache, "no-cache", false, "Bypass the artifact cache and fetch directly from the catalog")
	cCmd.Flags().Int64Var(&o.WarnValueSize, "warn-value-size", settings.DefaultWarnValueSize, "Warn when value exceeds this size in bytes (default: 1MB)")
	cCmd.Flags().Int64Var(&o.MaxValueSize, "max-value-size", settings.DefaultMaxValueSize, "Fail when value exceeds this size in bytes (default: 10MB)")
	cCmd.Flags().DurationVar(&o.ResolverTimeout, "resolver-timeout", settings.DefaultResolverTimeout, "Timeout per resolver")
	cCmd.Flags().DurationVar(&o.PhaseTimeout, "phase-timeout", settings.DefaultPhaseTimeout, "Timeout per resolver phase")
	cCmd.Flags().StringVar(&o.VersionConstraint, "version", "", "Semver version constraint for catalog resolution (e.g., ^1.0.0, ~2.1, >=1.0 <3.0)")
	cCmd.Flags().StringVar(&o.TestName, "test-name", "", "Test name for -o test output (derived from command and args when not set)")
	cCmd.Flags().StringVar(&o.OutputDir, "output-dir", "", "Target directory for action file operations (actions resolve relative paths here instead of CWD)")
	cCmd.Flags().StringVar(&o.BaseDir, "base-dir", "", "Override base directory for resolver path resolution (when unset, paths resolve from CWD)")
	cCmd.Flags().BoolVar(&o.PreRelease, "pre-release", false, "Include pre-release versions when resolving latest from catalog")
}

// writeMetrics outputs provider execution metrics to stderr
func writeMetrics(ctx context.Context) {
	w := writer.FromContext(ctx)
	if w == nil {
		return
	}
	allMetrics := provider.GlobalMetrics.GetAllMetrics()
	if len(allMetrics) == 0 {
		return
	}

	w.WarnStderrf("")
	w.WarnStderrf("Provider Execution Metrics:")
	w.WarnStderrf("%s", strings.Repeat("-", 80))
	w.WarnStderrf("%-25s %8s %8s %8s %12s %12s",
		"Provider", "Total", "Success", "Failure", "Avg Duration", "Success %")
	w.WarnStderrf("%s", strings.Repeat("-", 80))

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
		w.WarnStderrf("%-25s %8d %8d %8d %12s %11.1f%%",
			name,
			m.ExecutionCount,
			m.SuccessCount,
			m.FailureCount,
			avgDuration.Round(time.Millisecond),
			successRate)
	}
	w.WarnStderrf("%s", strings.Repeat("-", 80))
}

// solutionMetaFromSolution converts a solution's metadata to a provider.SolutionMeta.
func solutionMetaFromSolution(sol *solution.Solution) *provider.SolutionMeta {
	meta := &provider.SolutionMeta{
		Name:        sol.Metadata.Name,
		DisplayName: sol.Metadata.DisplayName,
		Description: sol.Metadata.Description,
		Category:    sol.Metadata.Category,
		Tags:        sol.Metadata.Tags,
	}
	if sol.Metadata.Version != nil {
		meta.Version = sol.Metadata.Version.String()
	}
	return meta
}

// buildCommandInfo creates a state.CommandInfo from parsed parameters and subcommand.
func buildCommandInfo(subcommand string, params map[string]any) state.CommandInfo {
	paramStrs := make(map[string]string, len(params))
	for k, v := range params {
		paramStrs[k] = fmt.Sprintf("%v", v)
	}
	return state.CommandInfo{
		Subcommand: subcommand,
		Parameters: paramStrs,
	}
}

// buildStateSolutionMeta creates a state.SolutionMeta from a solution.
func buildStateSolutionMeta(sol *solution.Solution) state.SolutionMeta {
	meta := state.SolutionMeta{
		Name: sol.Metadata.Name,
	}
	if sol.Metadata.Version != nil {
		meta.Version = sol.Metadata.Version.String()
	}
	return meta
}

// extractParameterKeys collects the CLI parameter keys accepted by a set of resolvers.
// It scans all resolve-phase provider sources for the "parameter" provider and
// extracts the literal "key" input value, which is the actual name the user
// must pass via -r key=value.
func extractParameterKeys(resolvers []*resolver.Resolver) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, r := range resolvers {
		if r.Resolve == nil {
			continue
		}
		for _, src := range r.Resolve.With {
			if src.Provider != "parameter" {
				continue
			}
			keyRef, ok := src.Inputs["key"]
			if !ok || keyRef == nil || keyRef.Literal == nil {
				continue
			}
			if s, ok := keyRef.Literal.(string); ok && s != "" && !seen[s] {
				seen[s] = true
				keys = append(keys, s)
			}
		}
	}
	return keys
}
