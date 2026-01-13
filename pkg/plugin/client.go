package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// Client wraps a plugin client and manages its lifecycle
type Client struct {
	pluginClient *plugin.Client
	plugin       ProviderPlugin
	path         string
	name         string
}

// NewClient creates a new plugin client
func NewClient(pluginPath string) (*Client, error) {
	// Get plugin name from path
	name := filepath.Base(pluginPath)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	// Create the plugin client
	//nolint:noctx // Context not available at plugin initialization time
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  HandshakeConfig.ProtocolVersion,
			MagicCookieKey:   HandshakeConfig.MagicCookieKey,
			MagicCookieValue: HandshakeConfig.MagicCookieValue,
		},
		Plugins: map[string]plugin.Plugin{
			PluginName: &GRPCPlugin{},
		},
		Cmd:              exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})

	// Connect to the plugin
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to connect to plugin: %w", err)
	}

	// Get the plugin interface
	raw, err := rpcClient.Dispense(PluginName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense plugin: %w", err)
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
		name:         name,
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
	var clients []*Client
	seen := make(map[string]bool)

	for _, dir := range pluginDirs {
		// Check if directory exists
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

		// Read directory entries
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read plugin directory %s: %w", dir, err)
		}

		// Find executable files
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// Check if file is executable
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			// Skip non-executable files
			if info.Mode()&0o111 == 0 {
				continue
			}

			// Skip if already seen
			if seen[path] {
				continue
			}
			seen[path] = true

			// Try to create a client
			client, err := NewClient(path)
			if err != nil {
				// Skip plugins that fail to load
				continue
			}

			clients = append(clients, client)
		}
	}

	return clients, nil
}
