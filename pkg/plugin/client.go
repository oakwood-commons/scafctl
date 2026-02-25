// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// pluginConfig holds the parameters needed to create a plugin client.
type pluginConfig struct {
	handshake  *HandshakeConfigData
	pluginName string
	grpcPlugin plugin.Plugin
}

// connectPlugin creates a go-plugin client, connects, and dispenses the named plugin.
// It returns the raw dispensed interface and the underlying plugin client.
// The caller is responsible for type-asserting the raw interface and calling
// client.Kill() on failure after this function returns.
func connectPlugin(pluginPath string, cfg pluginConfig) (any, *plugin.Client, error) {
	//nolint:noctx // Context not available at plugin initialization time
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  cfg.handshake.ProtocolVersion,
			MagicCookieKey:   cfg.handshake.MagicCookieKey,
			MagicCookieValue: cfg.handshake.MagicCookieValue,
		},
		Plugins: map[string]plugin.Plugin{
			cfg.pluginName: cfg.grpcPlugin,
		},
		Cmd:              exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})

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
			if fi.Mode()&0o111 == 0 {
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
}

// NewClient creates a new plugin client
func NewClient(pluginPath string) (*Client, error) {
	raw, client, err := connectPlugin(pluginPath, pluginConfig{
		handshake:  HandshakeConfig,
		pluginName: PluginName,
		grpcPlugin: &GRPCPlugin{},
	})
	if err != nil {
		return nil, err
	}

	providerPlugin, ok := raw.(ProviderPlugin)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("plugin does not implement ProviderPlugin interface")
	}

	return &Client{
		pluginClient: client,
		plugin:       providerPlugin,
		path:         pluginPath,
		name:         pluginNameFromPath(pluginPath),
	}, nil
}

// GetProviders returns all provider names exposed by this plugin
func (c *Client) GetProviders(ctx context.Context) ([]string, error) {
	return c.plugin.GetProviders(ctx)
}

// GetProviderDescriptor returns metadata for a specific provider
func (c *Client) GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error) {
	return c.plugin.GetProviderDescriptor(ctx, providerName)
}

// ExecuteProvider executes a provider with the given input
func (c *Client) ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error) {
	return c.plugin.ExecuteProvider(ctx, providerName, input)
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
func Discover(pluginDirs []string) ([]*Client, error) {
	return discoverExecutables(pluginDirs, NewClient)
}

// ---- Auth Handler Client ----

// AuthHandlerClient wraps a plugin client for auth handler plugins.
type AuthHandlerClient struct {
	pluginClient *plugin.Client
	plugin       AuthHandlerPlugin
	path         string
	name         string
}

// NewAuthHandlerClient creates a new auth handler plugin client.
func NewAuthHandlerClient(pluginPath string) (*AuthHandlerClient, error) {
	raw, client, err := connectPlugin(pluginPath, pluginConfig{
		handshake:  AuthHandlerHandshakeConfig,
		pluginName: AuthHandlerPluginName,
		grpcPlugin: &AuthHandlerGRPCPlugin{},
	})
	if err != nil {
		return nil, err
	}

	authPlugin, ok := raw.(AuthHandlerPlugin)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("plugin does not implement AuthHandlerPlugin interface")
	}

	return &AuthHandlerClient{
		pluginClient: client,
		plugin:       authPlugin,
		path:         pluginPath,
		name:         pluginNameFromPath(pluginPath),
	}, nil
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

// DiscoverAuthHandlers discovers auth handler plugins from the given directories.
func DiscoverAuthHandlers(pluginDirs []string) ([]*AuthHandlerClient, error) {
	return discoverExecutables(pluginDirs, NewAuthHandlerClient)
}
