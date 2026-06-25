// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kubeconfig

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
)

// Manager drives the kubeconfig provider from the host side. It mirrors
// pkg/state.Manager but, because it is invoked by a command outside a solution
// DAG, it resolves the provider itself: it first checks the registry and, on a
// miss, fetches and registers the official kubeconfig provider on demand. The
// manager owns any plugin clients it spawns and kills them on Close.
//
// The manager is not safe for concurrent use; a single command invocation drives
// it sequentially and calls Close (typically deferred) when done.
type Manager struct {
	registry   *provider.Registry
	clients    []*plugin.Client // spawned by ensure; killed on Close
	registered []string         // provider names registered by ensure; unregistered on Close
	binaryName string
	resolved   provider.Provider // cached after the first successful ensure
}

// Option configures a Manager.
type Option func(*Manager)

// WithRegistry injects a provider registry. Tests use this to register a mock
// kubeconfig provider so ensure resolves it without fetching. When omitted, the
// manager creates its own registry and fetches the provider on demand.
func WithRegistry(reg *provider.Registry) Option {
	return func(m *Manager) {
		m.registry = reg
	}
}

// NewManager creates a kubeconfig manager. binaryName is baked into the
// kubeconfig exec block (as exec_command) for write operations so embedders get
// correct exec args; an empty binaryName falls back to settings.CliBinaryName.
func NewManager(binaryName string, opts ...Option) *Manager {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	m := &Manager{binaryName: binaryName}
	for _, opt := range opts {
		opt(m)
	}
	if m.registry == nil {
		m.registry = provider.NewRegistry()
	}
	return m
}

// ensure resolves the kubeconfig provider, fetching and registering it on
// demand when absent. All failures are wrapped as ErrProviderUnavailable so the
// Phase 3 command can fall back to a static exec-credential kubeconfig.
func (m *Manager) ensure(ctx context.Context) (provider.Provider, error) {
	if m.resolved != nil {
		return m.resolved, nil
	}

	// Already registered (e.g. a solution run pre-loaded it or a test injected a
	// mock).
	if prov, ok := m.registry.Get(ProviderName); ok {
		if err := assertKubeconfigCapability(prov); err != nil {
			return nil, err
		}
		m.resolved = prov
		return prov, nil
	}

	// Fetch-then-register, modeled on autoResolveProviderByName.
	officialReg := official.RegistryFromContext(ctx)
	if officialReg == nil {
		return nil, fmt.Errorf("%w: official registry not available in context", ErrProviderUnavailable)
	}

	entry, ok := officialReg.Get(ProviderName)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not an official provider", ErrProviderUnavailable, ProviderName)
	}

	fetcher, err := prepare.BuildPluginFetcher(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: build plugin fetcher: %w", ErrProviderUnavailable, err)
	}

	dep := entry.ToPluginDependency()
	results, err := fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch provider %q: %w", ErrProviderUnavailable, ProviderName, err)
	}

	pluginCfg := &plugin.ProviderConfig{
		BinaryName: m.binaryName,
	}
	clientOpts := plugin.AuthClientOptsFromContext(ctx)
	before := nameSet(m.registry.List())
	clients, err := plugin.RegisterFetchedPlugins(ctx, m.registry, results, pluginCfg, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: register provider %q: %w", ErrProviderUnavailable, ProviderName, err)
	}
	m.clients = append(m.clients, clients...)
	for _, name := range m.registry.List() {
		if !before[name] {
			m.registered = append(m.registered, name)
		}
	}

	prov, ok := m.registry.Get(ProviderName)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q not registered after fetch", ErrProviderUnavailable, ProviderName)
	}
	if err := assertKubeconfigCapability(prov); err != nil {
		return nil, err
	}
	m.resolved = prov
	return prov, nil
}

// WriteKubeconfig merges and writes a kubeconfig exec-credential entry. When
// ExecCommand is empty it defaults to the manager's binary name so embedders get
// correct kubeconfig exec args.
func (m *Manager) WriteKubeconfig(ctx context.Context, in WriteInput) (WriteResult, error) {
	if in.ExecCommand == "" {
		in.ExecCommand = m.binaryName
	}
	result, err := m.execute(ctx, OperationWrite, in.toInputs())
	if err != nil {
		return WriteResult{}, err
	}
	return decodeOutput[WriteResult](result)
}

// RemoveEntry removes a kubeconfig cluster/context/user entry.
func (m *Manager) RemoveEntry(ctx context.Context, in RemoveInput) (RemoveResult, error) {
	result, err := m.execute(ctx, OperationRemove, in.toInputs())
	if err != nil {
		return RemoveResult{}, err
	}
	return decodeOutput[RemoveResult](result)
}

// CurrentServer reads the current server URL from a kubeconfig. It returns an
// empty string when the provider reports the lookup did not succeed (for
// example, no current-context or a missing kubeconfig).
func (m *Manager) CurrentServer(ctx context.Context, in CurrentServerInput) (string, error) {
	result, err := m.execute(ctx, OperationCurrentServer, in.toInputs())
	if err != nil {
		return "", err
	}
	out, err := decodeOutput[currentServerResult](result)
	if err != nil {
		return "", err
	}
	if !out.Success {
		return "", nil
	}
	return out.Server, nil
}

// DetectAuthType probes a server to detect the authentication method.
func (m *Manager) DetectAuthType(ctx context.Context, in DetectInput) (DetectResult, error) {
	result, err := m.execute(ctx, OperationDetectAuthType, in.toInputs())
	if err != nil {
		return DetectResult{}, err
	}
	return decodeOutput[DetectResult](result)
}

// Reachable checks whether an API server is reachable.
func (m *Manager) Reachable(ctx context.Context, in ReachableInput) (ReachableResult, error) {
	result, err := m.execute(ctx, OperationReachable, in.toInputs())
	if err != nil {
		return ReachableResult{}, err
	}
	return decodeOutput[ReachableResult](result)
}

// Whoami runs a SelfSubjectReview to identify the token's subject.
func (m *Manager) Whoami(ctx context.Context, in WhoamiInput) (WhoamiResult, error) {
	result, err := m.execute(ctx, OperationWhoami, in.toInputs())
	if err != nil {
		return WhoamiResult{}, err
	}
	return decodeOutput[WhoamiResult](result)
}

// Close unregisters any providers the manager registered and kills any plugin
// clients it spawned. Unregistering happens first so a caller-owned registry
// (injected via WithRegistry) that outlives the manager is not left holding dead
// wrappers that point at killed clients. It is safe to call multiple times and
// on a manager that never spawned a client.
func (m *Manager) Close() error {
	for _, name := range m.registered {
		m.registry.Unregister(name)
	}
	m.registered = nil
	for _, c := range m.clients {
		if c != nil {
			c.Kill()
		}
	}
	m.clients = nil
	return nil
}

// nameSet builds a lookup set from a slice of provider names.
func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// execute resolves the provider, sets the operation discriminator, and runs the
// provider with the kubeconfig execution mode.
func (m *Manager) execute(ctx context.Context, op string, inputs map[string]any) (*provider.ExecutionResult, error) {
	prov, err := m.ensure(ctx)
	if err != nil {
		return nil, err
	}
	inputs[inputOperation] = op
	execCtx := provider.WithExecutionMode(ctx, provider.CapabilityKubeconfig)
	result, err := provider.Execute(execCtx, prov, inputs)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig: execute %s: %w", op, err)
	}
	return result, nil
}

// assertKubeconfigCapability verifies the resolved provider advertises
// CapabilityKubeconfig.
func assertKubeconfigCapability(prov provider.Provider) error {
	for _, cap := range prov.Descriptor().Capabilities {
		if cap == provider.CapabilityKubeconfig {
			return nil
		}
	}
	return fmt.Errorf("%w: provider %q lacks CapabilityKubeconfig", ErrProviderUnavailable, ProviderName)
}
