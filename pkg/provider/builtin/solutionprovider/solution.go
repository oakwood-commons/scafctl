// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
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
// get.Getter satisfies this via Go structural typing.
type Loader interface {
	Get(ctx context.Context, path string) (*solution.Solution, error)
}

// SolutionProvider executes sub-solutions and returns their results.
type SolutionProvider struct {
	loader     Loader
	registry   *provider.Registry
	descriptor *provider.Descriptor
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

// New creates a new SolutionProvider with the given options.
func New(opts ...Option) *SolutionProvider {
	p := &SolutionProvider{}
	for _, opt := range opts {
		opt(p)
	}
	p.descriptor = buildDescriptor()
	return p
}

// Descriptor returns the provider's metadata and schema.
func (p *SolutionProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
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

	lgr := logger.FromContext(ctx)

	// Resolve canonical name for ancestor tracking.
	canonicalName := Canonicalize(in.Source)

	// Check circular references — always a hard error regardless of propagateErrors.
	ctx, err := PushAncestor(ctx, canonicalName)
	if err != nil {
		return nil, err
	}

	// Check depth limit — always a hard error.
	if err := CheckDepth(ctx, in.maxDepthOrDefault()); err != nil {
		return nil, err
	}

	// Load the sub-solution.
	lgr.V(1).Info("loading sub-solution", "source", in.Source)

	sol, err := p.loader.Get(ctx, in.Source)
	if err != nil {
		return nil, fmt.Errorf("solution %q: failed to load: %w", in.Source, err)
	}

	// Dry-run: validate source (by loading) but don't execute.
	if provider.DryRunFromContext(ctx) {
		mode, _ := provider.ExecutionModeFromContext(ctx)
		isAction := mode == provider.CapabilityAction
		envelope := BuildDryRunEnvelope(isAction)
		return &provider.Output{Data: envelope.ToMap()}, nil
	}

	// Build isolated context for sub-solution execution.
	subCtx := buildIsolatedContext(ctx, canonicalName, in.Inputs)

	// Determine execution mode and run.
	mode, _ := provider.ExecutionModeFromContext(ctx)
	if mode == provider.CapabilityAction && sol.Spec.HasWorkflow() {
		return p.executeWithWorkflow(subCtx, sol, in)
	}

	return p.executeResolversOnly(subCtx, sol, in)
}

// executeResolversOnly executes only the resolver phase (from capability).
func (p *SolutionProvider) executeResolversOnly(ctx context.Context, sol *solution.Solution, in *Input) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// Apply timeout if specified.
	if d, ok, err := in.timeoutDuration(); err != nil {
		return nil, err
	} else if ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	resolverData := make(map[string]any)
	var resolverErrors []ResolverError

	if sol.Spec.HasResolvers() {
		resolvers := sol.Spec.ResolversToSlice()

		// Filter to requested resolvers if specified.
		var err error
		resolvers, err = filterResolvers(resolvers, in.Resolvers)
		if err != nil {
			return nil, err
		}

		lgr.V(1).Info("executing sub-solution resolvers", "count", len(resolvers), "selected", len(in.Resolvers))

		adapter := &resolverRegistryAdapter{registry: p.registry}
		executor := resolver.NewExecutor(adapter)

		resultCtx, err := executor.Execute(ctx, resolvers, in.Inputs)
		if err != nil {
			resolverErrors = append(resolverErrors, ResolverError{
				Resolver: "_executor",
				Message:  err.Error(),
			})

			if in.shouldPropagateErrors() {
				return nil, fmt.Errorf("solution %q: resolver execution failed: %w", in.Source, err)
			}
		}

		// Extract resolver data from result context.
		resolverData = extractResolverData(resultCtx, sol)
	}

	envelope := BuildFromEnvelope(resolverData, resolverErrors)
	output := &provider.Output{Data: envelope.ToMap()}

	if envelope.Status == "failed" {
		output.Warnings = []string{
			fmt.Sprintf("sub-solution %q had %d resolver error(s)", in.Source, len(resolverErrors)),
		}
	}

	return output, nil
}

// executeWithWorkflow executes resolvers and then the workflow (action capability).
func (p *SolutionProvider) executeWithWorkflow(ctx context.Context, sol *solution.Solution, in *Input) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// Apply timeout if specified.
	if d, ok, err := in.timeoutDuration(); err != nil {
		return nil, err
	} else if ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	resolverData := make(map[string]any)
	var resolverErrors []ResolverError

	// Phase 1: Execute resolvers.
	if sol.Spec.HasResolvers() {
		resolvers := sol.Spec.ResolversToSlice()

		// Filter to requested resolvers if specified.
		var err error
		resolvers, err = filterResolvers(resolvers, in.Resolvers)
		if err != nil {
			return nil, err
		}

		lgr.V(1).Info("executing sub-solution resolvers", "count", len(resolvers), "selected", len(in.Resolvers))

		adapter := &resolverRegistryAdapter{registry: p.registry}
		resolverExec := resolver.NewExecutor(adapter)

		resultCtx, err := resolverExec.Execute(ctx, resolvers, in.Inputs)
		if err != nil {
			resolverErrors = append(resolverErrors, ResolverError{
				Resolver: "_executor",
				Message:  err.Error(),
			})

			if in.shouldPropagateErrors() {
				return nil, fmt.Errorf("solution %q: resolver phase failed: %w", in.Source, err)
			}

			// If resolver failed and not propagating, return envelope without running workflow.
			envelope := BuildActionEnvelope(resolverData, nil, resolverErrors)
			output := &provider.Output{Data: envelope.ToMap()}
			output.Warnings = []string{
				fmt.Sprintf("sub-solution %q resolver phase failed", in.Source),
			}
			return output, nil
		}

		resolverData = extractResolverData(resultCtx, sol)
	}

	// Phase 2: Execute workflow.
	lgr.V(1).Info("executing sub-solution workflow")

	actionAdapter := &actionRegistryAdapter{registry: p.registry}
	actionExec := action.NewExecutor(
		action.WithRegistry(actionAdapter),
		action.WithResolverData(resolverData),
	)

	execResult, err := actionExec.Execute(ctx, sol.Spec.Workflow)

	var workflowResult *WorkflowResult
	if execResult != nil {
		workflowResult = buildWorkflowResult(execResult)
	}

	if err != nil && in.shouldPropagateErrors() {
		return nil, fmt.Errorf("solution %q: workflow failed: %w", in.Source, err)
	}

	envelope := BuildActionEnvelope(resolverData, workflowResult, resolverErrors)
	output := &provider.Output{Data: envelope.ToMap()}

	if envelope.Status == "failed" {
		output.Warnings = []string{
			fmt.Sprintf("sub-solution %q failed", in.Source),
		}
	}

	return output, nil
}

// buildIsolatedContext constructs an isolated context for sub-solution execution.
// The sub-solution gets a fresh resolver context and parameters, but inherits
// logger, writer, auth, config, dry-run, and ancestor stack from the parent.
func buildIsolatedContext(ctx context.Context, canonicalName string, params map[string]any) context.Context {
	// Scoped logger.
	subLogger := logger.FromContext(ctx).WithName("solution:" + canonicalName)
	ctx = logger.WithLogger(ctx, &subLogger)

	// Fresh resolver context — sub-solution cannot see parent's resolved values.
	ctx = provider.WithResolverContext(ctx, map[string]any{})

	// Inject inputs as parameters for the sub-solution's parameter provider.
	if params != nil {
		ctx = provider.WithParameters(ctx, params)
	}

	// Writer, auth, config, dry-run, and ancestor stack are inherited automatically.
	return ctx
}

// extractResolverData builds the resolver data map from execution results.
func extractResolverData(resultCtx context.Context, sol *solution.Solution) map[string]any {
	data := make(map[string]any)

	rctx, ok := resolver.FromContext(resultCtx)
	if !ok {
		return data
	}

	for name := range sol.Spec.Resolvers {
		result, ok := rctx.GetResult(name)
		if ok && result.Status == resolver.ExecutionStatusSuccess {
			data[name] = result.Value
		}
	}

	return data
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
	PropagateErrors *bool          `json:"propagateErrors,omitempty" yaml:"propagateErrors,omitempty" doc:"Whether sub-solution failures cause a Go error (default: true)"`
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
func extractDependencies(inputs map[string]any) []string {
	var deps []string

	if source, ok := inputs["source"]; ok {
		deps = append(deps, extractRefsFromValue(source)...)
	}

	if subInputs, ok := inputs["inputs"].(map[string]any); ok {
		for _, v := range subInputs {
			deps = append(deps, extractRefsFromValue(v)...)
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

// --- Descriptor ---

func buildDescriptor() *provider.Descriptor {
	version, _ := semver.NewVersion(Version)

	return &provider.Descriptor{
		Name:         ProviderName,
		DisplayName:  "Solution Composition",
		APIVersion:   "v1",
		Version:      version,
		Description:  "Executes a sub-solution and returns its results as a structured envelope",
		Category:     "composition",
		MockBehavior: "Returns a mock envelope with empty resolvers and success status without loading or executing the sub-solution",
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

// --- Registry Adapters ---

// resolverRegistryAdapter adapts provider.Registry to resolver.RegistryInterface.
type resolverRegistryAdapter struct {
	registry *provider.Registry
}

func (r *resolverRegistryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *resolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

func (r *resolverRegistryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *resolverRegistryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// actionRegistryAdapter adapts provider.Registry to action.RegistryInterface.
type actionRegistryAdapter struct {
	registry *provider.Registry
}

func (r *actionRegistryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

func (r *actionRegistryAdapter) Has(name string) bool {
	return r.registry.Has(name)
}
