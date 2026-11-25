// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
)

const (
	// ProviderName is the unique identifier for the solution provider.
	ProviderName = "solution"

	// Version is the semantic version of this provider.
	Version = "1.0.0"

	// defaultMaxDepth is the default maximum nesting depth for recursive composition.
	defaultMaxDepth = 10
)

// Loader is the interface for loading solutions.
// prepare.subSolutionLoader adapts a load.Loader to it.
type Loader interface {
	Get(ctx context.Context, path string) (*solution.Solution, error)
}

// SolutionProvider executes sub-solutions and returns their results.
type SolutionProvider struct {
	loader            Loader
	registry          *provider.Registry
	descriptor        *provider.Descriptor
	officialProviders *official.Registry
	pluginFetcher     *plugin.Fetcher
	pluginConfig      *plugin.ProviderConfig
	clientOpts        []plugin.ClientOption

	// mu protects mutable state from concurrent Configure/Execute/Close calls.
	mu           sync.Mutex
	childClients []*plugin.Client
}

// Option configures a SolutionProvider.
type Option func(*SolutionProvider)

// WithLoader sets the solution loader.
func WithLoader(l Loader) Option {
	return func(p *SolutionProvider) { p.loader = l }
}

// WithRegistry sets the provider registry used for resolver and action execution.
func WithRegistry(r *provider.Registry) Option {
	return func(p *SolutionProvider) { p.registry = r }
}

// WithOfficialProviders sets the official provider registry for child auto-resolution.
func WithOfficialProviders(r *official.Registry) Option {
	return func(p *SolutionProvider) { p.officialProviders = r }
}

// WithPluginFetcher sets the fetcher used to download plugins for child solutions.
func WithPluginFetcher(f *plugin.Fetcher) Option {
	return func(p *SolutionProvider) { p.pluginFetcher = f }
}

// WithPluginConfig sets plugin configuration for child auto-resolution.
func WithPluginConfig(cfg *plugin.ProviderConfig) Option {
	return func(p *SolutionProvider) { p.pluginConfig = cfg }
}

// WithClientOptions sets plugin client options for child auto-resolution.
func WithClientOptions(opts ...plugin.ClientOption) Option {
	return func(p *SolutionProvider) { p.clientOpts = opts }
}

// New creates a new SolutionProvider with the given options.
func New(opts ...Option) *SolutionProvider {
	p := &SolutionProvider{}
	for _, opt := range opts {
		opt(p)
	}
	p.descriptor = buildDescriptor()
	return p
}

// Configure applies functional options to an existing SolutionProvider.
// This is used to inject runtime dependencies (loader, registry, etc.)
// into a provider that was registered in DefaultRegistry without them.
func (p *SolutionProvider) Configure(opts ...Option) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, opt := range opts {
		opt(p)
	}
}

// Close kills all plugin clients started by child auto-resolution.
// Must be called when the solution provider is no longer needed.
func (p *SolutionProvider) Close() {
	p.mu.Lock()
	clients := p.childClients
	p.childClients = nil
	p.mu.Unlock()
	for _, c := range clients {
		c.Kill()
	}
}

// Descriptor returns the provider's metadata and schema.
func (p *SolutionProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// resolveRuntime resolves the provider's runtime dependencies from context
// first (per-execution override), then falls back to struct fields. This
// allows the singleton in DefaultRegistry to remain stateless while callers
// inject deps via context for each execution.
func (p *SolutionProvider) resolveRuntime(ctx context.Context) (loader Loader, reg *provider.Registry, err error) {
	// Context takes priority — enables per-execution override without
	// mutating the shared singleton.
	loader = LoaderFromContext(ctx)
	reg = ProviderRegistryFromContext(ctx)

	// Fall back to struct fields for anything not provided via context.
	if loader == nil || reg == nil {
		p.mu.Lock()
		if loader == nil {
			loader = p.loader
		}
		if reg == nil {
			reg = p.registry
		}
		p.mu.Unlock()
	}

	if loader == nil {
		return nil, nil, fmt.Errorf("%s: no solution loader configured; "+
			"inject a loader via context (WithLoaderCtx) or call Configure(WithLoader(...))", ProviderName)
	}
	if reg == nil {
		return nil, nil, fmt.Errorf("%s: no provider registry configured; "+
			"inject a registry via context (WithProviderRegistry) or call Configure(WithRegistry(...))", ProviderName)
	}

	return loader, reg, nil
}

// resolvePluginRuntime resolves optional plugin-related dependencies from
// context first (per-execution override), then falls back to struct fields.
func (p *SolutionProvider) resolvePluginRuntime(ctx context.Context) (
	officialProviders *official.Registry,
	pluginFetcher *plugin.Fetcher,
	pluginConfig *plugin.ProviderConfig,
	clientOpts []plugin.ClientOption,
) {
	// Context takes priority.
	officialProviders = OfficialProvidersFromContext(ctx)
	pluginFetcher = PluginFetcherFromContext(ctx)
	pluginConfig = PluginConfigFromContext(ctx)
	clientOpts = ClientOptionsFromContext(ctx)

	// Fall back to struct fields for anything not provided via context.
	if officialProviders == nil || pluginFetcher == nil || pluginConfig == nil || len(clientOpts) == 0 {
		p.mu.Lock()
		if officialProviders == nil {
			officialProviders = p.officialProviders
		}
		if pluginFetcher == nil {
			pluginFetcher = p.pluginFetcher
		}
		if pluginConfig == nil {
			pluginConfig = p.pluginConfig
		}
		if len(clientOpts) == 0 {
			clientOpts = p.clientOpts
		}
		p.mu.Unlock()
	}

	return officialProviders, pluginFetcher, pluginConfig, clientOpts
}

// Execute runs the sub-solution and returns its results as a structured envelope.
// The input may be *Input (when called via the action Executor which runs Decode)
// or map[string]any (when called directly by the resolver framework).
func (p *SolutionProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	var in *Input
	switch v := input.(type) {
	case *Input:
		in = v
	case map[string]any:
		decoded, err := decodeInput(v)
		if err != nil {
			return nil, err
		}
		var ok bool
		in, ok = decoded.(*Input)
		if !ok {
			return nil, fmt.Errorf("%s: decodeInput returned unexpected type %T", ProviderName, decoded)
		}
	default:
		return nil, fmt.Errorf("%s: expected *Input or map[string]any, got %T", ProviderName, input)
	}

	// Resolve runtime dependencies (struct fields → context fallback).
	loader, reg, err := p.resolveRuntime(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure a run-scoped redaction tracker is shared between parent and child.
	tracker := redact.FromContext(ctx)
	if tracker == nil {
		tracker = redact.NewTracker()
		ctx = redact.WithTracker(ctx, tracker)
	}

	lgr := logger.FromContext(ctx)

	// Resolve file-based sources against the solution directory before
	// canonicalization so ancestor tracking and progress prefixes match the
	// resolved file path regardless of action working directory.
	source := in.Source
	if isFilePath(source) && !filepath.IsAbs(source) {
		if solDir, ok := provider.SolutionDirectoryFromContext(ctx); ok && solDir != "" {
			source = filepath.Join(solDir, source)
		}
	}

	// Retain raw canonicalName/source for loader and ancestor cycle checks, while
	// redacting display strings exposed to progress observers, logs, warnings, and errors.
	canonicalName := Canonicalize(ctx, source)
	secretSource := provider.SecretPropertiesFromContext(ctx)["source"]
	if secretSource {
		tracker.Track(in.Source)
		tracker.Track(source)
		tracker.Track(canonicalName)
	}
	rawSourceVals := []string{canonicalName, source, in.Source}
	displayName := redactSourceDisplay(ctx, canonicalName)
	displaySource := redactSourceDisplay(ctx, in.Source)
	displayResolvedSource := redactSourceDisplay(ctx, source)

	// Check circular references — always a hard error regardless of propagateErrors.
	ctx, err = PushAncestor(ctx, canonicalName)
	if err != nil {
		return nil, scrubSourceError(err, tracker, secretSource, rawSourceVals...)
	}

	// Check depth limit — always a hard error.
	if err := CheckDepth(ctx, in.maxDepthOrDefault()); err != nil {
		return nil, scrubSourceError(err, tracker, secretSource, rawSourceVals...)
	}

	lgr.V(1).Info("loading sub-solution", "source", displayResolvedSource)

	sol, loadErr := loader.Get(ctx, source)
	if loadErr != nil {
		scrubbedLoadErr := scrubSourceError(loadErr, tracker, secretSource, rawSourceVals...)
		if in.shouldPropagateErrors() {
			return nil, fmt.Errorf("solution %q: failed to load: %w", displayResolvedSource, scrubbedLoadErr)
		}
		return buildLoadFailureOutput(ctx, displayResolvedSource, scrubbedLoadErr), nil
	}
	p.normalizeSolutionNames(sol)

	// Auto-resolve official providers needed by the child solution.
	// Clients are tracked at the struct level and cleaned up via Close()
	// to avoid killing shared plugins while parallel Execute calls are in flight.
	if err := p.autoResolveChildProviders(ctx, sol, reg); err != nil {
		scrubbedPluginErr := scrubSourceError(err, tracker, secretSource, rawSourceVals...)
		if in.shouldPropagateErrors() {
			return nil, fmt.Errorf("solution %q: %w", displayResolvedSource, scrubbedPluginErr)
		}
		return buildLoadFailureOutput(ctx, displayResolvedSource, scrubbedPluginErr), nil
	}

	// Dry-run: validate source (by loading) but don't execute.
	mode, _ := provider.ExecutionModeFromContext(ctx)
	if provider.DryRunFromContext(ctx) {
		isAction := mode == provider.CapabilityAction
		envelope := BuildDryRunEnvelope(isAction)
		return &provider.Output{Data: envelope.ToMap()}, nil
	}

	// Build isolated context for sub-solution execution.
	subCtx := buildIsolatedContext(ctx, displayName, in.Inputs)

	return p.executeSubSolution(subCtx, mode, displayName, displaySource, sol, in, reg)
}

// normalizeSolutionNames populates map-key-derived Name fields on resolvers and
// workflow actions under p.mu so concurrent executions sharing an in-memory
// *solution.Solution never race on initial Name assignment.
func (p *SolutionProvider) normalizeSolutionNames(sol *solution.Solution) {
	if sol == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, r := range sol.Spec.Resolvers {
		if r != nil && r.Name != name {
			r.Name = name
		}
	}
	if sol.Spec.Workflow != nil {
		for name, a := range sol.Spec.Workflow.Actions {
			if a != nil && a.Name != name {
				a.Name = name
			}
		}
		for name, a := range sol.Spec.Workflow.Finally {
			if a != nil && a.Name != name {
				a.Name = name
			}
		}
	}
}

// executeSubSolution runs the child solution through the shared execute.Resolvers
// and (in CapabilityAction mode) execute.Actions pipelines.
func (p *SolutionProvider) executeSubSolution(
	ctx context.Context,
	mode provider.Capability,
	displayName string,
	displaySource string,
	sol *solution.Solution,
	in *Input,
	reg *provider.Registry,
) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)
	tracker := redact.FromContext(ctx)
	isAction := mode == provider.CapabilityAction
	parentCtx := ctx

	// Apply timeout if specified across both resolver and workflow phases.
	if d, ok, err := in.timeoutDuration(); err != nil {
		return nil, err
	} else if ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	executedResolvers := sol.Spec.ResolversToSlice()
	if len(in.Resolvers) > 0 {
		var err error
		executedResolvers, err = filterResolvers(executedResolvers, in.Resolvers)
		if err != nil {
			return nil, err
		}
	}

	resolverData := make(map[string]any)
	var (
		resolverErrors  []ResolverError
		resolverCtx     *resolver.Context
		resolverElapsed time.Duration
	)

	if len(executedResolvers) > 0 {
		lgr.V(1).Info("executing sub-solution resolvers", "count", len(executedResolvers), "selected", len(in.Resolvers))
	}

	resolverCfg := buildChildResolverConfig(ctx, executedResolvers)
	resResult, resErr := execute.Resolvers(ctx, sol, in.Inputs, reg, resolverCfg)
	if resErr != nil {
		if parentCtx.Err() != nil {
			return nil, fmt.Errorf("solution %q: %w", displaySource, parentCtx.Err())
		}

		var execErr *execute.ResolverExecutionError
		if errors.As(resErr, &execErr) && execErr.PartialResult != nil {
			resolverData = execErr.PartialResult.Data
			resolverCtx = execErr.PartialResult.Context
		}

		resolverErrors = append(resolverErrors, ResolverError{
			Resolver: "_executor",
			Message:  tracker.RedactString(resErr.Error()),
		})

		if in.shouldPropagateErrors() {
			if isAction {
				return nil, fmt.Errorf("solution %q: resolver phase failed: %w", displaySource, tracker.RedactError(resErr))
			}
			return nil, fmt.Errorf("solution %q: resolver execution failed: %w", displaySource, tracker.RedactError(resErr))
		}

		if isAction {
			sanitized := execute.SanitizeResolverDataForResponse(sol, resolverCtx, resolverData, tracker)
			envelope := BuildActionEnvelope(sanitized, nil, resolverErrors)
			return &provider.Output{
				Data: envelope.ToMap(),
				Warnings: []string{
					fmt.Sprintf("sub-solution %q resolver phase failed", displaySource),
				},
			}, nil
		}

		envelope := BuildFromEnvelope(resolverData, resolverErrors)
		return &provider.Output{
			Data: envelope.ToMap(),
			Warnings: []string{
				fmt.Sprintf("sub-solution %q had %d resolver error(s)", displaySource, len(resolverErrors)),
			},
		}, nil
	}

	resolverData = resResult.Data
	resolverCtx = resResult.Context
	resolverElapsed = resResult.Elapsed

	if !isAction {
		envelope := BuildFromEnvelope(resolverData, resolverErrors)
		return &provider.Output{Data: envelope.ToMap()}, nil
	}

	// CapabilityAction always returns an action envelope (including success: bool)
	// even when the child solution defines no workflow. Sanitize sensitive and
	// credential-backed child resolvers before exposing them in action results.
	if !sol.Spec.HasWorkflow() {
		sanitized := execute.SanitizeResolverDataForResponse(sol, resolverCtx, resolverData, tracker)
		envelope := BuildActionEnvelope(sanitized, nil, resolverErrors)
		return &provider.Output{Data: envelope.ToMap()}, nil
	}

	lgr.V(1).Info("executing sub-solution workflow")

	actionCfg := buildChildActionConfig(ctx, displayName, resolverCtx, executedResolvers, resolverElapsed)
	actResult, actErr := execute.Actions(ctx, sol, resolverData, reg, actionCfg)

	var workflowResult *WorkflowResult
	if actResult != nil && actResult.Result != nil {
		workflowResult = buildWorkflowResult(actResult.Result)
	} else if actErr != nil {
		var execErr *execute.ActionExecutionError
		if errors.As(actErr, &execErr) && execErr.PartialResult != nil {
			workflowResult = buildWorkflowResult(execErr.PartialResult)
		}
	}

	if actErr != nil {
		if parentCtx.Err() != nil {
			return nil, fmt.Errorf("solution %q: %w", displaySource, parentCtx.Err())
		}
		if in.shouldPropagateErrors() {
			return nil, fmt.Errorf("solution %q: workflow failed: %w", displaySource, tracker.RedactError(actErr))
		}
		if workflowResult == nil || (workflowResult.FinalStatus == string(action.ExecutionFailed) && len(workflowResult.FailedActions) == 0) {
			resolverErrors = append(resolverErrors, ResolverError{
				Resolver: "_workflow",
				Message:  tracker.RedactString(actErr.Error()),
			})
		}
	}

	sanitized := execute.SanitizeResolverDataForResponse(sol, resolverCtx, resolverData, tracker)
	envelope := BuildActionEnvelope(sanitized, workflowResult, resolverErrors)
	output := &provider.Output{Data: envelope.ToMap()}

	if envelope.Status == "failed" {
		output.Warnings = []string{
			fmt.Sprintf("sub-solution %q failed", displaySource),
		}
	}

	return output, nil
}

// buildChildResolverConfig derives resolver execution settings from context and
// caps timeouts against any remaining context deadline.
func buildChildResolverConfig(ctx context.Context, resolvers []*resolver.Resolver) execute.ResolverExecutionConfig {
	cfg := execute.ResolverExecutionConfigFromContext(ctx)
	cfg.Resolvers = resolvers

	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			if cfg.Timeout == 0 || remaining < cfg.Timeout {
				cfg.Timeout = remaining
			}
			if cfg.PhaseTimeout == 0 || remaining < cfg.PhaseTimeout {
				cfg.PhaseTimeout = remaining
			}
		}
	}
	return cfg
}

// buildChildActionConfig derives action execution settings from application
// config and any enclosing parent action.Executor, wrapping parent progress
// callbacks with the child solution's redacted display name prefix.
func buildChildActionConfig(
	ctx context.Context,
	displayName string,
	resolverCtx *resolver.Context,
	executedResolvers []*resolver.Resolver,
	resolverElapsed time.Duration,
) execute.ActionExecutionConfig {
	cfg := execute.ActionExecutionConfigFromContext(ctx)
	if outDir, hasOutDir := provider.OutputDirectoryFromContext(ctx); hasOutDir {
		cfg.OutputDir = outDir
	}

	if inherited, ok := action.InheritedExecutorConfigFromContext(ctx); ok {
		// Inherit the enclosing executor's effective output directory (including
		// an explicit opt-out to CWD when OutputDirectory is absent on ctx) rather
		// than reapplying the application config default.
		outDir, _ := provider.OutputDirectoryFromContext(ctx)
		cfg.OutputDir = outDir

		if inherited.DefaultTimeout > 0 {
			cfg.DefaultTimeout = inherited.DefaultTimeout
		}
		if inherited.GracePeriod > 0 {
			cfg.GracePeriod = inherited.GracePeriod
		}
		if inherited.MaxConcurrency >= 0 {
			cfg.MaxConcurrency = inherited.MaxConcurrency
		}
		if inherited.Cwd != "" {
			cfg.Cwd = inherited.Cwd
		}
		if inherited.IOStreams != nil {
			cfg.IOStreams = inherited.IOStreams
		}
		if inherited.ProgressCallback != nil {
			cfg.ProgressCallback = action.NewSolutionProgressCallback(
				inherited.ProgressCallback,
				redactSourceDisplay(ctx, displayName),
			)
		}
	}

	if cfg.IOStreams == nil && !provider.IsFallbackIOStreams(ctx) {
		if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil {
			cfg.IOStreams = streams
		}
	}

	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && (cfg.DefaultTimeout == 0 || remaining < cfg.DefaultTimeout) {
			cfg.DefaultTimeout = remaining
		}
	}

	cfg.ResolverContext = resolverCtx
	if resolverCtx != nil {
		cfg.ExecutionData = execute.BuildExecutionData(resolverCtx, executedResolvers, resolverElapsed)
	}

	return cfg
}

// buildLoadFailureOutput constructs a degraded envelope for load/parse failures
// when propagateErrors is false. The envelope reports status "failed" with the
// error detail, allowing the parent to continue execution.
func buildLoadFailureOutput(ctx context.Context, source string, loadErr error) *provider.Output {
	errMsg := redact.FromContext(ctx).RedactString(fmt.Sprintf("%s: %s", source, loadErr.Error()))
	errors := []ResolverError{{
		Resolver: "_loader",
		Message:  errMsg,
	}}

	mode, _ := provider.ExecutionModeFromContext(ctx)
	var envelope *Envelope
	if mode == provider.CapabilityAction {
		envelope = BuildActionEnvelope(nil, nil, errors)
	} else {
		envelope = BuildFromEnvelope(nil, errors)
	}

	return &provider.Output{
		Data: envelope.ToMap(),
		Warnings: []string{
			fmt.Sprintf("sub-solution failed to load: %s", errMsg),
		},
	}
}

// redactSourceDisplay scrubs tracked credentials (and secret-marked source
// inputs) from a raw or canonical source string before exposing it in progress
// prefixes, logger names, warnings, or error messages.
func redactSourceDisplay(ctx context.Context, s string) string {
	if provider.SecretPropertiesFromContext(ctx)["source"] {
		return redact.RedactedPlaceholder
	}
	if t := redact.FromContext(ctx); t != nil {
		return t.RedactString(s)
	}
	return s
}

type scrubbedSourceError struct {
	msg   string
	cause error
}

func (e *scrubbedSourceError) Error() string        { return e.msg }
func (e *scrubbedSourceError) Is(target error) bool { return errors.Is(e.cause, target) }
func (e *scrubbedSourceError) As(target any) bool   { return errors.As(e.cause, target) }

// scrubSourceError redacts tracked secrets from err and, when secretSource is
// true, also replaces any short (< redact.MinSecretLength) raw source tokens
// while preserving errors.Is and errors.As matching on the underlying cause.
func scrubSourceError(err error, tracker *redact.Tracker, secretSource bool, rawVals ...string) error {
	if err == nil {
		return nil
	}
	err = tracker.RedactError(err)
	if !secretSource {
		return err
	}
	msg := err.Error()
	changed := false
	for _, raw := range rawVals {
		if raw != "" && strings.Contains(msg, raw) {
			msg = strings.ReplaceAll(msg, raw, redact.RedactedPlaceholder)
			changed = true
		}
	}
	if !changed {
		return err
	}
	return &scrubbedSourceError{msg: msg, cause: err}
}

// buildIsolatedContext constructs an isolated context for sub-solution execution.
// Parent CLI parameters, forEach iteration context, persisted state data,
// author-defined template functions, and resolved values are stripped so they
// never leak into the child, while logger, writer, auth, config, dry-run,
// redaction tracker, and ancestor stack are inherited from the parent.
func buildIsolatedContext(ctx context.Context, displayName string, params map[string]any) context.Context {
	safeName := redactSourceDisplay(ctx, displayName)

	// Mask parent-scoped keys before installing child-scoped values.
	ctx = &isolatedContext{Context: ctx}

	// Scoped logger.
	subLogger := logger.FromContext(ctx).WithName("solution:" + safeName)
	ctx = logger.WithLogger(ctx, &subLogger)

	// Fresh resolver context — sub-solution cannot see parent's resolved values.
	ctx = provider.WithResolverContext(ctx, map[string]any{})

	// Inject inputs as parameters for the sub-solution's parameter provider.
	if params != nil {
		ctx = provider.WithParameters(ctx, params)
	}

	return ctx
}

// buildWorkflowResult converts an action.ExecutionResult to a WorkflowResult for the envelope.
func buildWorkflowResult(result *action.ExecutionResult) *WorkflowResult {
	wr := &WorkflowResult{
		FinalStatus:    string(result.FinalStatus),
		FailedActions:  result.FailedActions,
		SkippedActions: result.SkippedActions,
	}

	if wr.FailedActions == nil {
		wr.FailedActions = []string{}
	}
	if wr.SkippedActions == nil {
		wr.SkippedActions = []string{}
	}

	return wr
}

// --- Input ---

// Input is the decoded input for the solution provider.
type Input struct {
	Source          string         `json:"source" yaml:"source" doc:"Sub-solution location (file path, catalog reference, or URL)" example:"deploy-to-k8s@2.0.0"`
	Inputs          map[string]any `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Parameters passed to the sub-solution's parameter provider"`
	Resolvers       []string       `json:"resolvers,omitempty" yaml:"resolvers,omitempty" doc:"Resolver names to execute from the child solution; when empty all resolvers run" maxItems:"100"`
	PropagateErrors *bool          `json:"propagateErrors,omitempty" yaml:"propagateErrors,omitempty" doc:"Whether sub-solution failures (including load/parse) cause a Go error (default: true)"`
	MaxDepth        *int           `json:"maxDepth,omitempty" yaml:"maxDepth,omitempty" doc:"Maximum nesting depth for recursive composition (default: 10)" minimum:"1" maximum:"100"`
	Timeout         *string        `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Maximum duration for sub-solution execution (e.g. 30s, 5m)" example:"30s" pattern:"^[0-9]+(ns|us|µs|ms|s|m|h)+$" patternDescription:"Go duration string"`
}

func (i *Input) shouldPropagateErrors() bool {
	if i.PropagateErrors == nil {
		return true
	}
	return *i.PropagateErrors
}

func (i *Input) maxDepthOrDefault() int {
	if i.MaxDepth == nil {
		return defaultMaxDepth
	}
	return *i.MaxDepth
}

func (i *Input) timeoutDuration() (time.Duration, bool, error) {
	if i.Timeout == nil || *i.Timeout == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(*i.Timeout)
	if err != nil {
		return 0, false, fmt.Errorf("%s: invalid timeout %q: %w", ProviderName, *i.Timeout, err)
	}
	if d <= 0 {
		return 0, false, fmt.Errorf("%s: timeout must be positive, got %s", ProviderName, d)
	}
	return d, true, nil
}

// filterResolvers returns only the resolvers whose names appear in the
// allowlist. If the allowlist is empty, all resolvers are returned unchanged.
// Returns an error if any requested name does not exist in the child solution.
func filterResolvers(all []*resolver.Resolver, allowlist []string) ([]*resolver.Resolver, error) {
	if len(allowlist) == 0 {
		return all, nil
	}

	byName := make(map[string]*resolver.Resolver, len(all))
	for _, r := range all {
		byName[r.Name] = r
	}

	filtered := make([]*resolver.Resolver, 0, len(allowlist))
	for _, name := range allowlist {
		r, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%s: requested resolver %q does not exist in child solution", ProviderName, name)
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

// --- Decode ---

// decodeInput converts a map[string]any to *Input.
func decodeInput(m map[string]any) (any, error) {
	in := &Input{}

	source, ok := m["source"].(string)
	if !ok || source == "" {
		return nil, fmt.Errorf("%s: 'source' is required and must be a string", ProviderName)
	}
	in.Source = source

	if inputs, ok := m["inputs"].(map[string]any); ok {
		in.Inputs = inputs
	}

	if v, ok := m["propagateErrors"]; ok {
		switch b := v.(type) {
		case bool:
			in.PropagateErrors = &b
		default:
			return nil, fmt.Errorf("%s: 'propagateErrors' must be a boolean", ProviderName)
		}
	}

	if v, ok := m["maxDepth"]; ok {
		switch n := v.(type) {
		case int:
			in.MaxDepth = &n
		case float64:
			d := int(n)
			in.MaxDepth = &d
		default:
			return nil, fmt.Errorf("%s: 'maxDepth' must be an integer", ProviderName)
		}
	}

	if v, ok := m["resolvers"]; ok {
		switch arr := v.(type) {
		case []any:
			names := make([]string, 0, len(arr))
			for i, item := range arr {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("%s: 'resolvers[%d]' must be a string, got %T", ProviderName, i, item)
				}
				names = append(names, s)
			}
			in.Resolvers = names
		case []string:
			in.Resolvers = arr
		default:
			return nil, fmt.Errorf("%s: 'resolvers' must be an array of strings", ProviderName)
		}
	}

	if v, ok := m["timeout"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s: 'timeout' must be a string (e.g. \"30s\")", ProviderName)
		}
		in.Timeout = &s
	}

	return in, nil
}

// --- Dependency Extraction ---

// extractDependencies scans the raw input map for resolver references.
// The returned slice is sorted for deterministic output.
func extractDependencies(inputs map[string]any) []string {
	var deps []string

	if source, ok := inputs["source"]; ok {
		deps = append(deps, extractRefsFromValue(source)...)
	}

	if subInputs, ok := inputs["inputs"].(map[string]any); ok {
		keys := make([]string, 0, len(subInputs))
		for k := range subInputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			deps = append(deps, extractRefsFromValue(subInputs[k])...)
		}
	}

	return deps
}

// extractRefsFromValue extracts resolver references from a value.
// Handles string CEL expressions (_.resolverName), maps with rslvr/expr/tmpl keys, etc.
func extractRefsFromValue(v any) []string {
	switch val := v.(type) {
	case string:
		return extractRefsFromString(val)
	case map[string]any:
		return extractRefsFromMap(val)
	default:
		return nil
	}
}

// extractRefsFromString extracts resolver references from a string value.
// Scans for patterns like "_.resolverName".
func extractRefsFromString(s string) []string {
	// Simple pattern: look for _.word patterns in CEL-like expressions.
	var refs []string
	for i := 0; i < len(s)-2; i++ {
		if s[i] == '_' && s[i+1] == '.' {
			// Extract the identifier after _.
			j := i + 2
			for j < len(s) && isIdentChar(s[j]) {
				j++
			}
			if j > i+2 {
				refs = append(refs, s[i+2:j])
			}
		}
	}
	return refs
}

// extractRefsFromMap extracts resolver references from a map value.
// Handles rslvr, expr, and tmpl keys.
func extractRefsFromMap(m map[string]any) []string {
	var refs []string

	// Direct resolver reference: {"rslvr": "resolverName"}
	if rslvr, ok := m["rslvr"].(string); ok {
		refs = append(refs, rslvr)
	}

	// CEL expression: {"expr": "_.resolverName + ..."}
	if expr, ok := m["expr"].(string); ok {
		refs = append(refs, extractRefsFromString(expr)...)
	}

	// Go template: {"tmpl": "{{.resolverName}}"}
	if tmpl, ok := m["tmpl"].(string); ok {
		refs = append(refs, extractRefsFromTemplate(tmpl)...)
	}

	return refs
}

// extractRefsFromTemplate extracts resolver references from a Go template string.
// Scans for patterns like {{.resolverName}}.
func extractRefsFromTemplate(tmpl string) []string {
	var refs []string
	for i := 0; i < len(tmpl)-3; i++ {
		if tmpl[i] == '{' && tmpl[i+1] == '{' {
			// Find the closing }}
			end := -1
			for j := i + 2; j < len(tmpl)-1; j++ {
				if tmpl[j] == '}' && tmpl[j+1] == '}' {
					end = j
					break
				}
			}
			if end == -1 {
				continue
			}

			// Extract content between {{ and }}
			content := tmpl[i+2 : end]
			// Look for .identifier patterns (Go template fields)
			for k := 0; k < len(content)-1; k++ {
				if content[k] == '.' && k < len(content)-1 && isIdentStartChar(content[k+1]) {
					j := k + 1
					for j < len(content) && isIdentChar(content[j]) {
						j++
					}
					refs = append(refs, content[k+1:j])
				}
			}
		}
	}
	return refs
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func isIdentStartChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// isFilePath reports whether source looks like a file path rather than a
// catalog reference (e.g. "deploy-to-k8s@2.0.0") or URL. It returns true
// when the source is an absolute path, starts with "./" or "../", or ends
// with a YAML extension.
func isFilePath(source string) bool {
	if filepath.IsAbs(source) {
		return true
	}
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(source))
	return ext == ".yaml" || ext == ".yml"
}

// --- Descriptor ---

func buildDescriptor() *provider.Descriptor {
	version, _ := semver.NewVersion(Version)

	return &provider.Descriptor{
		Name:        ProviderName,
		DisplayName: "Solution Composition",
		APIVersion:  "v1",
		Version:     version,
		Description: "Executes a sub-solution and returns its results as a structured envelope",
		Category:    "composition",
		WhatIf: func(ctx context.Context, input any) (string, error) {
			var source string
			switch v := input.(type) {
			case *Input:
				source = v.Source
			case map[string]any:
				source, _ = v["source"].(string)
			}
			if source != "" {
				return fmt.Sprintf("Would execute solution %q", redactSourceDisplay(ctx, source)), nil
			}
			return "Would execute sub-solution", nil
		},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityAction,
		},
		Schema: schemahelper.ObjectSchema([]string{"source"}, map[string]*jsonschema.Schema{
			"source": schemahelper.StringProp("Sub-solution location (file path, catalog reference, or URL)",
				schemahelper.WithExample("deploy-to-k8s@2.0.0"),
			),
			"inputs": {
				Type:                 "object",
				Description:          "Parameters passed to the sub-solution's parameter provider",
				AdditionalProperties: &jsonschema.Schema{},
			},
			"resolvers": schemahelper.ArrayProp("Resolver names to execute from the child solution; when empty all resolvers run",
				schemahelper.WithItems(schemahelper.StringProp("Resolver name")),
			),
			"propagateErrors": schemahelper.BoolProp("Whether sub-solution failures cause a Go error (default: true)"),
			"maxDepth": schemahelper.IntProp("Maximum nesting depth for recursive composition",
				schemahelper.WithMinimum(1),
				schemahelper.WithMaximum(100),
			),
			"timeout": schemahelper.StringProp("Maximum duration for sub-solution execution (e.g. 30s, 5m)",
				schemahelper.WithExample("30s"),
			),
		}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"resolvers": schemahelper.AnyProp("Resolver values from the sub-solution"),
				"status":    schemahelper.StringProp("Overall status: success or failed"),
				"errors":    schemahelper.ArrayProp("Resolver errors encountered during execution"),
			}),
			provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"resolvers": schemahelper.AnyProp("Resolver values from the sub-solution"),
				"workflow":  schemahelper.AnyProp("Aggregate workflow status"),
				"status":    schemahelper.StringProp("Overall status: success or failed"),
				"errors":    schemahelper.ArrayProp("Resolver errors encountered during execution"),
				"success":   schemahelper.BoolProp("Whether the solution succeeded"),
			}),
		},
		Decode:              decodeInput,
		ExtractDependencies: extractDependencies,
		Tags:                []string{"composition", "orchestration", "solution"},
		Examples: []provider.Example{
			{
				Name:        "Load child solution (from capability)",
				Description: "Execute a child solution and consume its resolver values",
				YAML: `name: child-data
provider: solution
inputs:
  source: "./child-solution.yaml"
  inputs:
    environment: "production"`,
			},
			{
				Name:        "Compose with catalog reference (action capability)",
				Description: "Execute a cataloged solution as an action step",
				YAML: `name: deploy-infra
provider: solution
inputs:
  source: "deploy-to-k8s@2.0.0"
  inputs:
    cluster: "prod-west"
  propagateErrors: false`,
			},
			{
				Name:        "Selective resolver execution",
				Description: "Run only specific resolvers from a child solution",
				YAML: `name: db-config
provider: solution
inputs:
  source: "infra-config@1.0.0"
  resolvers:
    - database-url
    - cache-ttl
  timeout: "30s"`,
			},
		},
	}
}

// autoResolveChildProviders fetches official providers needed by a child
// solution that are not yet in the parent registry.
// Clients are appended to p.childClients for deferred cleanup via Close().
// Returns an error if fetching or registering plugins fails.
func (p *SolutionProvider) autoResolveChildProviders(ctx context.Context, sol *solution.Solution, reg *provider.Registry) error {
	officialProviders, pluginFetcher, pluginCfg, clientOpts := p.resolvePluginRuntime(ctx)

	if officialProviders == nil || officialProviders.Len() == 0 || pluginFetcher == nil {
		return nil
	}

	// Use Solution.ReferencedProviderNames() to collect all provider references
	// (including the state backend provider), then filter to those that are
	// missing and official.
	var missing []official.Provider
	for _, name := range sol.ReferencedProviderNames() {
		if reg.Has(name) {
			continue
		}
		if op, ok := officialProviders.Get(name); ok {
			missing = append(missing, op)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	lgr := logger.FromContext(ctx)
	if lgr != nil {
		names := make([]string, len(missing))
		for i, m := range missing {
			names[i] = m.Name
		}
		lgr.V(1).Info("auto-resolving child solution providers", "providers", names)
	}

	deps := make([]solution.PluginDependency, len(missing))
	for i, m := range missing {
		deps[i] = m.ToPluginDependency()
	}

	results, err := pluginFetcher.FetchPlugins(ctx, deps, nil)
	if err != nil {
		return fmt.Errorf("auto-fetching child solution providers: %w", err)
	}

	cfg := pluginCfg
	if cfg == nil {
		cfg = &plugin.ProviderConfig{}
	}

	pClients, err := plugin.RegisterFetchedPlugins(ctx, reg, results, cfg, clientOpts...)
	if err != nil {
		return fmt.Errorf("registering child solution providers: %w", err)
	}

	if len(pClients) > 0 {
		// Prefer per-execution tracker from context so concurrent runs
		// sharing the singleton don't interfere with each other's clients.
		if tracker := ChildClientTrackerFromContext(ctx); tracker != nil {
			tracker.Add(pClients...)
		} else {
			p.mu.Lock()
			p.childClients = append(p.childClients, pClients...)
			p.mu.Unlock()
		}
	}
	return nil
}
