// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// pluginConfig holds the parameters needed to create a plugin client.
type pluginConfig struct {
	handshake    *HandshakeConfigData
	pluginName   string
	grpcPlugin   plugin.Plugin
	cmdFn        func(string) *exec.Cmd // builds the exec.Cmd for the plugin binary
	logger       hclog.Logger           // if nil, a null logger is used
	startTimeout time.Duration          // if >0, bounds plugin startup/handshake
}

// connectPlugin creates a go-plugin client, connects, and dispenses the named plugin.
// It returns the raw dispensed interface and the underlying plugin client.
// The caller is responsible for type-asserting the raw interface and calling
// client.Kill() on failure after this function returns.
func connectPlugin(pluginPath string, cfg pluginConfig) (any, *plugin.Client, error) {
	cmdFn := cfg.cmdFn
	if cmdFn == nil {
		cmdFn = pluginCmd
	}

	//nolint:noctx // Context not available at plugin initialization time
	clientConfig := &plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  cfg.handshake.ProtocolVersion,
			MagicCookieKey:   cfg.handshake.MagicCookieKey,
			MagicCookieValue: cfg.handshake.MagicCookieValue,
		},
		Plugins: map[string]plugin.Plugin{
			cfg.pluginName: cfg.grpcPlugin,
		},
		Cmd:              cmdFn(pluginPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		// Always set an explicit logger so HC_LOG env vars cannot force debug
		// output during normal command invocations. When debug mode is active
		// the caller provides a real logger via pluginConfig.logger.
		Logger: pluginLogger(cfg.logger),
	}
	if cfg.startTimeout > 0 {
		clientConfig.StartTimeout = cfg.startTimeout
	}
	client := plugin.NewClient(clientConfig)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("failed to connect to plugin: %w", err)
	}

	raw, err := rpcClient.Dispense(cfg.pluginName)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("failed to dispense plugin: %w", err)
	}

	return raw, client, nil
}

// pluginLogger returns cfg.logger when non-nil, otherwise a null logger.
// This prevents hclog from auto-detecting HC_LOG or HASHICORP_LOG env vars
// and emitting debug output during normal (non-debug) invocations.
func pluginLogger(l hclog.Logger) hclog.Logger {
	if l != nil {
		return l
	}
	return hclog.NewNullLogger()
}

// safeEnvKeys is the set of environment variables that are safe to pass to
// plugin processes. This prevents leaking sensitive credentials (AWS_*,
// KUBECONFIG, SSH_AUTH_SOCK, etc.) to potentially untrusted plugin binaries.
var safeEnvKeys = map[string]bool{
	"PATH":    true,
	"HOME":    true,
	"TMPDIR":  true,
	"LANG":    true,
	"TZ":      true,
	"USER":    true,
	"LOGNAME": true,

	// Required for go-plugin gRPC communication
	"PLUGIN_MIN_PORT": true,
	"PLUGIN_MAX_PORT": true,
}

// pluginCmd creates an exec.Cmd for a plugin binary with a sanitized
// environment. Only safe environment variables are propagated to prevent
// leaking secrets to external plugin processes.
func pluginCmd(pluginPath string) *exec.Cmd {
	//nolint:gosec // pluginPath is validated via digest verification before reaching here
	cmd := exec.Command(pluginPath) //nolint:noctx // Context not available at plugin initialization time
	return cmd
}

// pluginCmdSanitized creates an exec.Cmd with a minimal sanitized
// environment. Used in API server context to prevent leaking secrets.
func pluginCmdSanitized(pluginPath string) *exec.Cmd {
	//nolint:gosec // pluginPath is validated via digest verification before reaching here
	cmd := exec.Command(pluginPath) //nolint:noctx // Context not available at plugin initialization time
	cmd.Env = safePluginEnv()
	return cmd
}

// safePluginEnv builds a minimal environment from the current process
// environment, keeping only keys in safeEnvKeys.
func safePluginEnv() []string {
	env := make([]string, 0, len(safeEnvKeys))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if safeEnvKeys[key] {
			env = append(env, kv)
		}
	}
	return env
}

// discoverExecutables scans the given directories for executable files and
// calls newClientFn for each one. Errors from newClientFn are silently skipped
// (non-loadable plugins are ignored).
func discoverExecutables[T any](pluginDirs []string, newClientFn func(path string) (T, error)) ([]T, error) {
	var clients []T
	seen := make(map[string]bool)

	for _, dir := range pluginDirs {
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to stat plugin directory %s: %w", dir, err)
		}
		if !info.IsDir() {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read plugin directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			fi, err := entry.Info()
			if err != nil {
				continue
			}
			// On Windows permission bits are not meaningful; accept any
			// regular file. On Unix require at least one execute bit.
			if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
				continue
			}
			if seen[path] {
				continue
			}
			seen[path] = true

			client, err := newClientFn(path)
			if err != nil {
				continue
			}
			clients = append(clients, client)
		}
	}

	return clients, nil
}

// pluginNameFromPath extracts a short name from a plugin binary path.
func pluginNameFromPath(pluginPath string) string {
	name := filepath.Base(pluginPath)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// Client wraps a plugin client and manages its lifecycle
type Client struct {
	pluginClient *plugin.Client
	plugin       ProviderPlugin
	path         string
	name         string

	// descriptorCache caches GetProviderDescriptor results to avoid redundant
	// RPCs for the same provider within a single client lifecycle.
	mu              sync.RWMutex
	descriptorCache map[string]*provider.Descriptor
}

// ClientOption configures plugin client creation.
type ClientOption func(*clientOptions)

type clientOptions struct {
	hostDeps     *HostServiceDeps
	sanitizeEnv  bool          // strip sensitive env vars from plugin process
	debugLog     bool          // emit plugin startup/lifecycle debug traces
	startTimeout time.Duration // bounds plugin startup/handshake
}

// WithHostDeps provides host-side dependencies (secrets, auth) that are
// exposed to the plugin via the HostService gRPC callback server.
func WithHostDeps(deps *HostServiceDeps) ClientOption {
	return func(o *clientOptions) {
		o.hostDeps = deps
	}
}

// WithSanitizedEnv restricts the environment variables passed to the plugin
// process. Only safe variables (PATH, HOME, TMPDIR, etc.) are inherited.
// Use this in API server contexts to prevent leaking secrets to plugins.
func WithSanitizedEnv() ClientOption {
	return func(o *clientOptions) {
		o.sanitizeEnv = true
	}
}

// WithDebugLogging enables plugin lifecycle debug traces (plugin started,
// RPC address, protocol negotiation) in the hclog output. Pass this option
// when --debug or --log-level debug is active so plugin internals are
// visible. Without it a null logger is used and no plugin noise appears.
func WithDebugLogging() ClientOption {
	return func(o *clientOptions) {
		o.debugLog = true
	}
}

// WithStartTimeout bounds the plugin startup/handshake phase.
// If the plugin binary does not complete the gRPC handshake within
// this duration, the connection attempt fails and the process is killed.
func WithStartTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.startTimeout = d
	}
}

// buildPluginClient is a shared helper that processes options, builds the command
// function, constructs an hclog logger if needed, and returns the result.
func buildPluginClient[T any](
	pluginPath string,
	opts []ClientOption,
	connectFn func(string, pluginConfig) (any, *plugin.Client, error),
	makeCfg func(clientOptions, hclog.Logger, func(string) *exec.Cmd) pluginConfig,
	assertPlugin func(any) (T, error),
) (T, *plugin.Client, error) {
	var zero T
	if connectFn == nil {
		connectFn = connectPlugin
	}

	var o clientOptions
	for _, opt := range opts {
		opt(&o)
	}

	cmdFn := pluginCmd
	if o.sanitizeEnv {
		cmdFn = pluginCmdSanitized
	}

	var hcLogger hclog.Logger
	if o.debugLog {
		hcLogger = hclog.New(&hclog.LoggerOptions{
			Name:  "plugin",
			Level: hclog.Debug,
		})
	}

	cfg := makeCfg(o, hcLogger, cmdFn)
	raw, client, err := connectFn(pluginPath, cfg)
	if err != nil {
		return zero, nil, err
	}

	typed, err := assertPlugin(raw)
	if err != nil {
		client.Kill()
		return zero, nil, err
	}

	return typed, client, nil
}

// newClientWithConnector creates a provider plugin client using the given connector.
// Tests use this to cover success and failure paths without spawning real binaries.
//
//nolint:dupl // ProviderPlugin and AuthHandlerPlugin connectors share structure but serve distinct types
func newClientWithConnector(
	pluginPath string,
	connectFn func(string, pluginConfig) (any, *plugin.Client, error),
	opts ...ClientOption,
) (*Client, error) {
	providerPlugin, client, err := buildPluginClient(
		pluginPath,
		opts,
		connectFn,
		func(o clientOptions, logger hclog.Logger, cmdFn func(string) *exec.Cmd) pluginConfig {
			return pluginConfig{
				handshake:    HandshakeConfig,
				pluginName:   PluginName,
				grpcPlugin:   &GRPCPlugin{HostDeps: o.hostDeps},
				cmdFn:        cmdFn,
				logger:       logger,
				startTimeout: o.startTimeout,
			}
		},
		func(raw any) (ProviderPlugin, error) {
			p, ok := raw.(ProviderPlugin)
			if !ok {
				return nil, fmt.Errorf("plugin does not implement ProviderPlugin interface")
			}
			return p, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		pluginClient: client,
		plugin:       providerPlugin,
		path:         pluginPath,
		name:         pluginNameFromPath(pluginPath),
	}, nil
}

// NewClient creates a new plugin client.
func NewClient(pluginPath string, opts ...ClientOption) (*Client, error) {
	return newClientWithConnector(pluginPath, connectPlugin, opts...)
}

// GetProviders returns all provider names exposed by this plugin
func (c *Client) GetProviders(ctx context.Context) ([]string, error) {
	return c.plugin.GetProviders(ctx)
}

// GetProviderDescriptor returns metadata for a specific provider.
// The result is cached for the lifetime of the client.
//
// Note: concurrent callers for the same uncached provider may both issue an
// RPC (classic check-then-act). This is benign because descriptors are
// immutable — the second writer simply overwrites with an identical value.
func (c *Client) GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error) {
	c.mu.RLock()
	if desc, ok := c.descriptorCache[providerName]; ok {
		c.mu.RUnlock()
		return desc, nil
	}
	c.mu.RUnlock()

	desc, err := c.plugin.GetProviderDescriptor(ctx, providerName)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.descriptorCache == nil {
		c.descriptorCache = make(map[string]*provider.Descriptor)
	}
	c.descriptorCache[providerName] = desc
	c.mu.Unlock()

	return desc, nil
}

// ExecuteProvider executes a provider with the given input
func (c *Client) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
	return c.plugin.ExecuteProvider(ctx, providerName, input)
}

// ConfigureProvider sends host-side configuration to a named provider.
func (c *Client) ConfigureProvider(ctx context.Context, providerName string, cfg ProviderConfig) error {
	return c.plugin.ConfigureProvider(ctx, providerName, cfg)
}

// ExecuteProviderStream executes a provider with streaming output.
func (c *Client) ExecuteProviderStream(ctx context.Context, providerName string, input map[string]any, cb func(StreamChunk)) error {
	return c.plugin.ExecuteProviderStream(ctx, providerName, input, cb)
}

// DescribeWhatIf returns a human-readable description of what the provider would do
func (c *Client) DescribeWhatIf(ctx context.Context, providerName string, input map[string]any) (string, error) {
	return c.plugin.DescribeWhatIf(ctx, providerName, input)
}

// ExtractDependencies returns resolver dependency names from the provider's inputs.
func (c *Client) ExtractDependencies(ctx context.Context, providerName string, inputs map[string]any) ([]string, error) {
	return c.plugin.ExtractDependencies(ctx, providerName, inputs)
}

// Kill terminates the plugin process
func (c *Client) Kill() {
	if c.pluginClient != nil {
		c.pluginClient.Kill()
	}
}

// Name returns the plugin name
func (c *Client) Name() string {
	return c.name
}

// Path returns the plugin path
func (c *Client) Path() string {
	return c.path
}

// Discover discovers plugins from the given directories
func Discover(pluginDirs []string, opts ...ClientOption) ([]*Client, error) {
	return discoverExecutables(pluginDirs, func(path string) (*Client, error) {
		return NewClient(path, opts...)
	})
}

// ---- Auth Handler Client ----

// AuthHandlerClient wraps a plugin client for auth handler plugins.
type AuthHandlerClient struct {
	pluginClient    *plugin.Client
	plugin          AuthHandlerPlugin
	path            string
	name            string
	startupDuration time.Duration
}

// newAuthHandlerClientWithConnector creates an auth handler plugin client
// using the given connector. Tests use this for deterministic branch coverage.
//
//nolint:dupl // AuthHandlerPlugin and ProviderPlugin connectors share structure but serve distinct types
func newAuthHandlerClientWithConnector(
	pluginPath string,
	connectFn func(string, pluginConfig) (any, *plugin.Client, error),
	opts ...ClientOption,
) (*AuthHandlerClient, error) {
	startupStart := time.Now()
	authPlugin, client, err := buildPluginClient(
		pluginPath,
		opts,
		connectFn,
		func(o clientOptions, logger hclog.Logger, cmdFn func(string) *exec.Cmd) pluginConfig {
			return pluginConfig{
				handshake:    AuthHandlerHandshakeConfig,
				pluginName:   AuthHandlerPluginName,
				grpcPlugin:   &AuthHandlerGRPCPlugin{HostDeps: o.hostDeps},
				cmdFn:        cmdFn,
				logger:       logger,
				startTimeout: o.startTimeout,
			}
		},
		func(raw any) (AuthHandlerPlugin, error) {
			p, ok := raw.(AuthHandlerPlugin)
			if !ok {
				return nil, fmt.Errorf("plugin does not implement AuthHandlerPlugin interface")
			}
			return p, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &AuthHandlerClient{
		pluginClient:    client,
		plugin:          authPlugin,
		path:            pluginPath,
		name:            pluginNameFromPath(pluginPath),
		startupDuration: time.Since(startupStart),
	}, nil
}

// NewAuthHandlerClient creates a new auth handler plugin client.
func NewAuthHandlerClient(pluginPath string, opts ...ClientOption) (*AuthHandlerClient, error) {
	return newAuthHandlerClientWithConnector(pluginPath, connectPlugin, opts...)
}

// GetAuthHandlers returns all auth handler names exposed by this plugin.
func (c *AuthHandlerClient) GetAuthHandlers(ctx context.Context) ([]AuthHandlerInfo, error) {
	return c.plugin.GetAuthHandlers(ctx)
}

// Login delegates to the plugin's Login.
func (c *AuthHandlerClient) Login(ctx context.Context, handlerName string, req LoginRequest, cb func(DeviceCodePrompt)) (*LoginResponse, error) {
	return c.plugin.Login(ctx, handlerName, req, cb)
}

// Logout delegates to the plugin's Logout.
func (c *AuthHandlerClient) Logout(ctx context.Context, handlerName string) error {
	return c.plugin.Logout(ctx, handlerName)
}

// GetStatus delegates to the plugin's GetStatus.
func (c *AuthHandlerClient) GetStatus(ctx context.Context, handlerName string) (*auth.Status, error) {
	return c.plugin.GetStatus(ctx, handlerName)
}

// GetToken delegates to the plugin's GetToken.
func (c *AuthHandlerClient) GetToken(ctx context.Context, handlerName string, req TokenRequest) (*TokenResponse, error) {
	return c.plugin.GetToken(ctx, handlerName, req)
}

// ConfigureAuthHandler delegates to the plugin's ConfigureAuthHandler.
func (c *AuthHandlerClient) ConfigureAuthHandler(ctx context.Context, handlerName string, cfg ProviderConfig) error {
	return c.plugin.ConfigureAuthHandler(ctx, handlerName, cfg)
}

// StopAuthHandler delegates to the plugin's StopAuthHandler.
func (c *AuthHandlerClient) StopAuthHandler(ctx context.Context, handlerName string) error {
	return c.plugin.StopAuthHandler(ctx, handlerName)
}

// HostServiceID returns the broker service ID of the HostService callback server.
// Returns 0 if no HostService was registered.
func (c *AuthHandlerClient) HostServiceID() uint32 {
	if gc, ok := c.plugin.(*AuthHandlerGRPCClient); ok {
		return gc.hostServiceID
	}
	return 0
}

// Kill terminates the plugin process.
func (c *AuthHandlerClient) Kill() {
	if c.pluginClient != nil {
		c.pluginClient.Kill()
	}
}

// Name returns the plugin name.
func (c *AuthHandlerClient) Name() string {
	return c.name
}

// Path returns the plugin path.
func (c *AuthHandlerClient) Path() string {
	return c.path
}

// StartupDuration returns the time taken to start and handshake with the plugin process.
func (c *AuthHandlerClient) StartupDuration() time.Duration {
	return c.startupDuration
}

// DiscoverAuthHandlers discovers auth handler plugins from the given directories.
func DiscoverAuthHandlers(pluginDirs []string, opts ...ClientOption) ([]*AuthHandlerClient, error) {
	return discoverExecutables(pluginDirs, func(path string) (*AuthHandlerClient, error) {
		return NewAuthHandlerClient(path, opts...)
	})
}
