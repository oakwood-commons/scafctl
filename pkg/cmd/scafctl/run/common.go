// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"errors"
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
	catversion "github.com/oakwood-commons/scafctl/pkg/catalog/version"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	inputflags "github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
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
func makeRunEFunc(cfg runCommandConfig, cmdName string) func(*cobra.Command, []string) error {
	return func(cCmd *cobra.Command, args []string) error {
		cfg.cliParams.EntryPointSettings.Path = filepath.Join(cfg.path, cmdName)
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

	// Strict disables auto-resolution of official providers. When true,
	// missing official providers produce an error instructing the user to
	// declare them explicitly in bundle.plugins.
	Strict bool

	// NoState disables the entire state lifecycle for the run: state is not
	// loaded before resolvers, immutable values are neither verified nor
	// locked, and no state is saved afterward. Resolvers that read the state
	// provider fall back to their defaults. Intended for CI/offline runs.
	NoState bool

	// OnUnknownResolver controls how -r/--resolver parameters whose key is not
	// consumed by any declared parameter resolver are handled: "error"
	// (default) rejects them, "warn" proceeds with a warning, "ignore" accepts
	// them silently. Empty resolves to the config default, then the built-in
	// strict default. Set via the --on-unknown-resolver flag.
	OnUnknownResolver string

	// nonFatalValidation, when true, makes executeResolvers treat resolver
	// validation-only failures as non-fatal: it returns the populated values
	// and resolver context alongside the error instead of discarding them.
	// Resolve- and transform-phase failures remain fatal. Set only by
	// inspection commands (run resolver, validate resolver), never by run
	// solution / run action which keep validation as a hard gate.
	nonFatalValidation bool

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

// isStructuredOutput reports whether the selected output format is a
// machine-readable format for which failures should still emit a parseable
// document on stdout rather than an empty stdout with a stderr-only error.
//
// The structured failure contract is deliberately scoped to json and yaml only
// (matching the command help text and MCP docs). It intentionally excludes the
// other "structured" kvx formats (csv/toml/mermaid): the solution/action
// envelope serializer only supports json/yaml, and injecting the resolver
// failure keys into csv/toml/mermaid would change those formats' output shape
// in a way the documented contract does not describe.
func (o *sharedResolverOptions) isStructuredOutput() bool {
	format, ok := kvx.ParseOutputFormat(o.Output)
	if !ok {
		return false
	}
	return format == kvx.OutputFormatJSON || format == kvx.OutputFormatYAML
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

	// Determine whether to exclude internal resolvers: exclude in table/interactive
	// (human-facing) output, include in structured output for machine consumption.
	excludeInternal := o.shouldExcludeInternal()

	for name, value := range resolverData {
		if r, ok := sol.Spec.Resolvers[name]; ok {
			if excludeInternal && r.Internal {
				continue
			}
			if shouldRedact && r.Sensitive {
				results[name] = "[REDACTED]"
				continue
			}
		}
		results[name] = value
	}

	return results
}

// shouldExcludeInternal determines whether internal resolvers should be excluded
// based on the output format. Structured formats (json, yaml, csv, toml, mermaid)
// include internal resolvers for machine consumption; table/interactive modes exclude them.
func (o *sharedResolverOptions) shouldExcludeInternal() bool {
	format, _ := kvx.ParseOutputFormat(o.Output)
	return !kvx.IsStructuredFormat(format)
}

// shouldRedactSensitive determines whether sensitive values should be redacted based on
// the output format and --show-sensitive flag. Following the Terraform model:
// - Table/interactive output: redacted (human-facing)
// - Structured output (json, yaml, csv, toml, mermaid, quiet): revealed (machine-facing)
// - --show-sensitive: always reveals regardless of format
func (o *sharedResolverOptions) shouldRedactSensitive() bool {
	if o.ShowSensitive {
		return false
	}

	format, _ := kvx.ParseOutputFormat(o.Output)
	// Structured formats are for machine consumption — don't redact.
	// Quiet is also non-redacting (suppresses output entirely).
	if kvx.IsStructuredFormat(format) || kvx.IsQuietFormat(format) {
		return false
	}
	return true
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

// execResolverConfig holds optional overrides for a single executeResolvers call.
type execResolverConfig struct {
	// seed maps resolver names to results produced in an earlier pass (e.g. the
	// state two-phase pre-load). Seeded resolvers are reused instead of being
	// re-executed, avoiding duplicate side effects from http/exec providers.
	seed map[string]*resolver.ExecutionResult
}

// execResolverOption configures a single executeResolvers invocation.
type execResolverOption func(*execResolverConfig)

// withSeededResults reuses previously computed resolver results in this run
// instead of re-executing those resolvers. A nil or empty map is a no-op.
func withSeededResults(seed map[string]*resolver.ExecutionResult) execResolverOption {
	return func(c *execResolverConfig) { c.seed = seed }
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
	opts ...execResolverOption,
) (map[string]any, *resolver.Context, error) {
	var execCfg execResolverConfig
	for _, opt := range opts {
		opt(&execCfg)
	}

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
	// Non-fatal validation (used by `run resolver`) continues execution and
	// collects all errors like validate-all, but keeps dependents of a
	// validation-only failure running so the data layer stays fully inspectable.
	if o.nonFatalValidation {
		executorOpts = append(executorOpts, resolver.WithNonFatalValidation(true))
	}
	if o.SkipValidation {
		executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
	}
	if o.SkipTransform {
		executorOpts = append(executorOpts, resolver.WithSkipTransform(true))
	}

	// Load mocked resolvers from context or environment (set by test runner).
	mockedResolvers, err := loadMockedResolvers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("loading mocked resolvers: %w", err)
	}
	if len(mockedResolvers) > 0 {
		executorOpts = append(executorOpts, resolver.WithMockedResolvers(mockedResolvers))
	}

	if sol.Spec.HasCalls() {
		executorOpts = append(executorOpts, resolver.WithCalls(sol.Spec.Calls))
	}

	// Seed results from an earlier pass (e.g. the state two-phase pre-load) so
	// those resolvers are reused rather than re-executed.
	if len(execCfg.seed) > 0 {
		executorOpts = append(executorOpts, resolver.WithSeededResults(execCfg.seed))
	}

	executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

	// Apply solution-level CEL cost limit if configured
	ctx = execute.ApplySolutionCELCostLimit(ctx, sol)

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

	// Retrieve the resolver context with results. It is populated even on
	// failure, so fetch it before deciding how to handle the error.
	resolverCtx, ctxOK := resolver.FromContext(resultCtx)
	if !ctxOK {
		if progress != nil {
			progress.Wait()
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolver execution failed: %w", err)
		}
		return nil, nil, fmt.Errorf("failed to retrieve resolver results")
	}

	// Build resolver data map. In non-fatal mode, also include partial values
	// captured by resolvers that failed validation so they remain inspectable.
	for name := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if !ok {
			continue
		}
		if result.Status == resolver.ExecutionStatusSuccess {
			resolverData[name] = result.Value
		} else if o.nonFatalValidation && result.Value != nil {
			resolverData[name] = result.Value
		}
	}

	// Wait for progress bars to complete
	if progress != nil {
		progress.Wait()
	}

	if err != nil {
		// In non-fatal mode (run resolver / validate resolver), return the
		// populated values and context alongside the error for ALL failure
		// kinds so the caller can render the successfully-resolved values plus
		// diagnostics instead of dropping them. The caller classifies the error
		// with execute.IsValidationOnlyFailure to choose the exit code:
		// validation-only failures are a soft gate (exit 0 unless
		// --fail-on-validation), while resolve- and transform-phase failures
		// remain a hard failure (non-zero exit) -- but the partial values stay
		// inspectable in the structured output either way. In fatal mode (run
		// solution / run action), all errors discard partial results.
		if o.nonFatalValidation {
			return resolverData, resolverCtx, fmt.Errorf("resolver execution failed: %w", err)
		}
		return nil, nil, fmt.Errorf("resolver execution failed: %w", err)
	}

	lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	return resolverData, resolverCtx, nil
}

// buildStateTwoPhaseInput constructs the input for state's two-phase pre-load,
// wiring the resolver runner to this option set's execution pipeline. The state
// manager uses it to run only the minimal set of resolvers that a load-time
// state field (state.enabled or a backend input) transitively depends on, then
// returns their results as a seed so the main run does not re-execute them.
func (o *sharedResolverOptions) buildStateTwoPhaseInput(
	sol *solution.Solution,
	params map[string]any,
	reg *provider.Registry,
) state.TwoPhaseInput {
	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}
	return state.TwoPhaseInput{
		Resolvers: sol.Spec.ResolversToSlice(),
		Lookup:    lookup,
		Calls:     sol.Spec.Calls,
		RunResolvers: func(rctx context.Context, subset []*resolver.Resolver) (*resolver.Context, error) {
			_, rc, rerr := o.executeResolvers(rctx, sol, subset, params, reg)
			return rc, rerr
		},
	}
}

// deferredValidationFailures returns the set of resolver names whose deferred
// (cross-resolver) validation failed, so their immutable values are not locked
// even in collect-errors modes. Returns nil when the deferred validation phase
// did not run or reported no failures.
func deferredValidationFailures(resolverCtx *resolver.Context) map[string]bool {
	if resolverCtx == nil {
		return nil
	}
	summary := resolverCtx.DeferredValidation()
	if summary == nil || len(summary.Results) == 0 {
		return nil
	}
	failed := make(map[string]bool, len(summary.Results))
	for _, rf := range summary.Results {
		failed[rf.ResolverName] = true
	}
	return failed
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
func (o *sharedResolverOptions) prepareSolutionForExecution(ctx context.Context) (*solution.Solution, *provider.Registry, string, func(), func(context.Context) context.Context, error) {
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
	opts = o.appendClientPluginOptions(ctx, opts)

	// Wire auth host deps so that plugin providers can request auth tokens
	// from the host process via gRPC HostService.
	if authOpts := plugin.AuthClientOptsFromContext(ctx); len(authOpts) > 0 {
		opts = append(opts, prepare.WithClientOptions(authOpts...))
	}

	if o.discoveryMode != settings.DiscoveryModeDefault {
		opts = append(opts, prepare.WithDiscoveryMode(o.discoveryMode))
	}

	// Wire plugin auto-fetch so that bundle.plugins declarations trigger
	// automatic download from configured catalogs. Without this, solutions
	// that declare plugins would silently skip plugin loading.
	if fetcher, err := buildPluginFetcher(ctx); err == nil {
		opts = append(opts, prepare.WithPluginFetcher(fetcher))
	}

	// Load lock file for reproducible plugin resolution. The lock file
	// lives alongside the solution file (e.g., bundle.lock.yaml).
	if o.File != "" && o.File != "-" {
		lockPlugins := loadLockPlugins(o.File)
		if len(lockPlugins) > 0 {
			opts = append(opts, prepare.WithLockPlugins(lockPlugins))
		}
	}

	// Pass auth registry so auth handler plugins can be registered
	if authReg := auth.RegistryFromContext(ctx); authReg != nil {
		opts = append(opts, prepare.WithAuthRegistry(authReg))
	}

	// Wire official provider auto-resolution from context.
	if officialReg := official.RegistryFromContext(ctx); officialReg != nil {
		opts = append(opts, prepare.WithOfficialProviders(officialReg))
	}
	if o.Strict {
		opts = append(opts, prepare.WithStrict(true))
	}

	// Resolve binary name once for verbose output and user-facing messages.
	binaryName := settings.CliBinaryName
	if o.CliParams != nil && o.CliParams.BinaryName != "" {
		binaryName = o.CliParams.BinaryName
	}

	// Unified resolution chain for auto-discovery with ambiguity handling.
	// Only applies when -f is not specified and no positional arg was given.
	if o.File == "" {
		getter := get.NewGetterFromContext(ctx)
		if o.discoveryMode != settings.DiscoveryModeDefault {
			getter.SetDiscoveryMode(o.discoveryMode)
		}
		resolvedPath, resolveErr := get.Resolve(ctx, getter, "", "", get.ResolveOptions{
			Risk: get.DiscoveryRiskLow,
		})
		if resolveErr != nil {
			return nil, nil, "", func() {}, nil, resolveErr
		}
		o.File = resolvedPath
	}

	// Emit verbose discovery information before loading
	if w != nil && w.VerboseEnabled() {
		switch o.File {
		case "-":
			w.Verbose("Loading solution from stdin")
		default:
			w.Verbosef("Loading solution from: %s", o.File)
		}
	}

	result, err := prepare.Solution(ctx, o.File, opts...)
	if err != nil {
		return nil, nil, "", func() {}, nil, err
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

	return result.Solution, result.Registry, result.SolutionDir, result.Cleanup, result.ProviderCtx, nil
}

func (o *sharedResolverOptions) appendClientPluginOptions(ctx context.Context, opts []prepare.Option) []prepare.Option {
	if o.CliParams == nil {
		return opts
	}

	opts = append(opts, prepare.WithPluginConfig(&plugin.ProviderConfig{
		Quiet:      o.CliParams.IsQuiet,
		NoColor:    o.CliParams.NoColor,
		BinaryName: o.CliParams.BinaryName,
	}))
	if logger.IsDebugLevel(o.CliParams.MinLogLevel) {
		opts = append(opts, prepare.WithClientOptions(plugin.WithDebugLogging()))
	}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Plugins.GRPCMaxMessageSize > 0 {
		opts = append(opts, prepare.WithClientOptions(plugin.WithGRPCMaxMessageSize(cfg.Plugins.GRPCMaxMessageSize)))
	}

	return opts
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
	cCmd.Flags().BoolVar(&o.Progress, "progress", false, "Show resolver phase progress bars (requires TTY)")
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
	cCmd.Flags().StringVar(&o.BaseDir, "base-dir", "", "Override base directory for resolver path resolution (when unset, paths resolve from the solution file's directory when known, otherwise the current directory; use '.' for CWD)")
	cCmd.Flags().BoolVar(&o.PreRelease, "pre-release", false, "Include pre-release versions when resolving latest from catalog")
	cCmd.Flags().BoolVar(&o.Strict, "strict", false, "Disable auto-resolution of official providers; require explicit bundle.plugins declarations")
	cCmd.Flags().BoolVar(&o.NoState, "no-state", false, "Skip the entire state lifecycle: do not load, verify immutables, or save state (for CI/offline runs)")
	cCmd.Flags().StringVar(&o.OnUnknownResolver, "on-unknown-resolver", string(settings.DefaultUnknownResolverPolicy), "Policy for -r keys not consumed by any declared parameter: error (reject), warn (proceed with warning), or ignore (accept silently)")
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
		Source:      sol.Provenance(),
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

// warnStateSkipped emits a one-line stderr notice when --no-state disables the
// state lifecycle for a solution that actually declares a state block. It is a
// no-op when the solution has no state or when no writer is in context. The
// underlying WarnStderrf respects --quiet.
func warnStateSkipped(ctx context.Context, sol *solution.Solution) {
	if sol == nil || sol.State == nil {
		return
	}
	if w := writer.FromContext(ctx); w != nil {
		w.WarnStderrf("--no-state: skipping state load, immutable checks, and save for solution %q", sol.Metadata.Name)
	}
}

// buildParamFlagHint formats missing param names as -r flags for user hints.
func buildParamFlagHint(params []string) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, fmt.Sprintf("-r %s=<value>", p))
	}
	return strings.Join(parts, " ")
}

// handleStateLoadError checks whether a state load error is caused by missing
// CLI parameters and, if so, prints an actionable hint to stderr before
// returning the exit error. This is called from run resolver, run solution,
// and run action when stateMgr.Load() fails.
func (o *sharedResolverOptions) handleStateLoadError(ctx context.Context, loadErr error) error {
	var missingErr *state.MissingParamsError
	if errors.As(loadErr, &missingErr) {
		paramFlags := buildParamFlagHint(missingErr.Missing)
		err := fmt.Errorf("state load: missing required parameters [%s]. Supply with: %s",
			strings.Join(missingErr.Missing, ", "), paramFlags)
		return o.exitWithCode(ctx, err, exitcode.GeneralError)
	}
	return o.exitWithCode(ctx, fmt.Errorf("state load: %w", loadErr), exitcode.GeneralError)
}

// validateResolverParams enforces the effective unknown-resolver policy for the
// supplied -r/--resolver parameters. It centralizes the typo-detection logic
// shared by run resolver, run solution, and run action.
//
// The policy is resolved from --on-unknown-resolver, falling back to the
// resolver config default and then the strict built-in default. An invalid flag
// or config value is reported as an error regardless of whether any parameters
// were supplied.
//
// When the policy permits (warn/ignore), unknown keys do not fail the command:
// warn emits a stderr warning naming the keys, ignore is silent. The strict
// default (error) returns the validation error unchanged.
func (o *sharedResolverOptions) validateResolverParams(ctx context.Context, sol *solution.Solution, params map[string]any) error {
	policy, err := o.effectiveUnknownResolverPolicy(ctx)
	if err != nil {
		return err
	}

	if len(params) == 0 {
		return nil
	}

	resolvers := sol.Spec.ResolversToSlice()
	// Skip typo detection when a resolver reads every supplied parameter
	// (all: true) -- any -r key is valid in that case.
	if resolversAcceptAllParameters(resolvers) {
		return nil
	}

	paramKeys := extractParameterKeys(resolvers)
	if len(paramKeys) == 0 {
		return nil
	}

	verr := inputflags.ValidateInputKeys(params, paramKeys, "solution")
	if verr == nil {
		return nil
	}

	switch policy {
	case settings.UnknownResolverIgnore:
		return nil
	case settings.UnknownResolverWarn:
		if w := writer.FromContext(ctx); w != nil {
			w.WarnStderrf("%v", verr)
		}
		return nil
	case settings.UnknownResolverError:
		return verr
	}
	// Unreachable: effectiveUnknownResolverPolicy only returns known policies.
	return verr
}

// effectiveUnknownResolverPolicy resolves the unknown-resolver policy honoring
// precedence: the --on-unknown-resolver flag overrides the resolver config
// default, which overrides the strict built-in default. It returns an error if
// the resolved value is not a recognized policy.
func (o *sharedResolverOptions) effectiveUnknownResolverPolicy(ctx context.Context) (settings.UnknownResolverPolicy, error) {
	raw := o.OnUnknownResolver
	// When the flag was not explicitly set, defer to config. flagsChanged is
	// nil outside the command-execution flow (e.g. unit tests), in which case
	// the value on the options struct is authoritative.
	if o.flagsChanged != nil && !o.flagsChanged["on-unknown-resolver"] {
		if cfg := config.FromContext(ctx); cfg != nil && cfg.Resolver.OnUnknownResolver != "" {
			raw = cfg.Resolver.OnUnknownResolver
		}
	}
	return settings.ParseUnknownResolverPolicy(raw)
}

// extractParameterKeys collects the CLI parameter keys accepted by a set of resolvers.
// It scans all resolve-phase provider sources for the "parameter" provider and
// extracts the literal "key" input value plus every entry in the "keys" alias
// list, which together are the names the user may pass via -r key=value.
func extractParameterKeys(resolvers []*resolver.Resolver) []string {
	seen := make(map[string]bool)
	var keys []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		keys = append(keys, name)
	}
	for _, r := range resolvers {
		if r.Resolve == nil {
			continue
		}
		for _, src := range r.Resolve.With {
			if src.Provider != "parameter" {
				continue
			}
			if keyRef, ok := src.Inputs["key"]; ok && keyRef != nil {
				if s, ok := keyRef.Literal.(string); ok {
					add(s)
				}
			}
			if keysRef, ok := src.Inputs["keys"]; ok && keysRef != nil {
				switch list := keysRef.Literal.(type) {
				case []any:
					for _, item := range list {
						if s, ok := item.(string); ok {
							add(s)
						}
					}
				case []string:
					for _, s := range list {
						add(s)
					}
				}
			}
		}
	}
	return keys
}

// resolversAcceptAllParameters reports whether any resolver reads every
// supplied CLI parameter via the parameter provider's "all: true" map mode.
// When true, callers must not typo-check -r keys against a fixed key list --
// any key is valid.
//
// A non-literal "all" input (set via a resolver reference, CEL expression, or
// Go template) cannot be evaluated statically, so it is treated conservatively
// as "could be true": validation is skipped rather than risk falsely rejecting
// valid -r keys when map mode activates at runtime.
func resolversAcceptAllParameters(resolvers []*resolver.Resolver) bool {
	for _, r := range resolvers {
		if r.Resolve == nil {
			continue
		}
		for _, src := range r.Resolve.With {
			if src.Provider != "parameter" {
				continue
			}
			allRef, ok := src.Inputs["all"]
			if !ok || allRef == nil {
				continue
			}
			if b, ok := allRef.Literal.(bool); ok {
				if b {
					return true
				}
				continue
			}
			// Non-literal "all" (rslvr/expr/tmpl): could resolve to true at
			// runtime, so fail open and skip typo validation.
			if allRef.Resolver != nil || allRef.Expr != nil || allRef.Tmpl != nil {
				return true
			}
		}
	}
	return false
}

// mockedResolversEnvSuffix is the suffix appended to the binary-name-derived
// env var prefix to form the full environment variable name for mocked resolvers.
// The full name is {SAFE_PREFIX}_MOCKED_RESOLVERS_FILE.
const mockedResolversEnvSuffix = "_MOCKED_RESOLVERS_FILE"

// loadMockedResolvers reads the mocked resolvers JSON file. It first checks
// the context (set by the in-process test runner), then falls back to the
// environment variable (set by the subprocess test runner).
// Returns nil if neither source is set.
func loadMockedResolvers(ctx context.Context) (map[string]any, error) {
	// Prefer context-based path (race-free for in-process execution).
	path, ok := settings.MockedResolversFileFromContext(ctx)
	if !ok {
		// Fall back to env var for subprocess execution.
		envVar := settings.SafeEnvPrefix(settings.BinaryNameFromContext(ctx)) + mockedResolversEnvSuffix
		path = os.Getenv(envVar)
	}
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: path comes from test runner context or env var, not user input
	if err != nil {
		return nil, fmt.Errorf("reading mocked resolvers file %q: %w", path, err)
	}

	var mocks map[string]any
	if err := json.Unmarshal(data, &mocks); err != nil {
		return nil, fmt.Errorf("parsing mocked resolvers file: %w", err)
	}

	return mocks, nil
}

// buildPluginFetcher creates a plugin.Fetcher from the context's config and
// auth registry. Delegates to prepare.BuildPluginFetcher.
func buildPluginFetcher(ctx context.Context) (*plugin.Fetcher, error) {
	return prepare.BuildPluginFetcher(ctx)
}

// loadLockPlugins loads plugin entries from a lock file adjacent to the
// solution file. Returns nil if no lock file exists or it cannot be parsed.
func loadLockPlugins(solutionPath string) []bundler.LockPlugin {
	lockPath := filepath.Join(filepath.Dir(solutionPath), bundler.DefaultLockFileName)
	lockFile, err := bundler.LoadLockFile(lockPath)
	if err != nil || lockFile == nil {
		return nil
	}
	return lockFile.Plugins
}

// autoResolveProviderByName checks the official provider registry for a
// provider name and, if found, auto-fetches it via the plugin fetcher.
// This enables `run provider <name>` to work for extracted official providers
// without requiring --plugin-dir or bundle.plugins.
func autoResolveProviderByName(ctx context.Context, name string, reg *provider.Registry) ([]*plugin.Client, error) {
	officialReg := official.RegistryFromContext(ctx)
	if officialReg == nil {
		return nil, fmt.Errorf("official registry not available")
	}

	p, ok := officialReg.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %q is not an official provider", name)
	}

	lgr := logger.FromContext(ctx)
	if lgr != nil {
		lgr.V(0).Info("auto-resolving official provider", "provider", name)
	}

	fetcher, err := buildPluginFetcher(ctx)
	if err != nil {
		return nil, fmt.Errorf("building plugin fetcher: %w", err)
	}

	dep := p.ToPluginDependency()
	results, err := fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching provider %q: %w", name, err)
	}

	pluginCfg := &plugin.ProviderConfig{
		BinaryName: settings.BinaryNameFromContext(ctx),
	}
	clientOpts := plugin.AuthClientOptsFromContext(ctx)
	clients, err := plugin.RegisterFetchedPlugins(ctx, reg, results, pluginCfg, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering provider %q: %w", name, err)
	}

	return clients, nil
}
