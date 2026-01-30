package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderWrapper wraps a plugin provider to implement the provider.Provider interface
type ProviderWrapper struct {
	client       *Client
	providerName string
	descriptor   *provider.Descriptor
	mu           sync.RWMutex
}

// NewProviderWrapper creates a new provider wrapper for a plugin provider
func NewProviderWrapper(client *Client, providerName string) (*ProviderWrapper, error) {
	// Get the descriptor to validate the provider exists
	desc, err := client.GetProviderDescriptor(context.Background(), providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider descriptor: %w", err)
	}

	return &ProviderWrapper{
		client:       client,
		providerName: providerName,
		descriptor:   desc,
	}, nil
}

// Descriptor returns the provider descriptor
func (w *ProviderWrapper) Descriptor() *provider.Descriptor {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.descriptor
}

// Execute executes the provider
func (w *ProviderWrapper) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", w.descriptor.Name, input)
	}

	lgr.V(1).Info("executing plugin provider", "provider", w.descriptor.Name)

	result, err := w.client.ExecuteProvider(ctx, w.providerName, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", w.descriptor.Name, err)
	}

	lgr.V(1).Info("plugin provider completed", "provider", w.descriptor.Name)
	return result, nil
}

// Client returns the underlying plugin client
func (w *ProviderWrapper) Client() *Client {
	return w.client
}

// RegisterPluginProviders discovers plugins and registers them with the provider registry
func RegisterPluginProviders(registry *provider.Registry, pluginDirs []string) error {
	// Discover plugins
	clients, err := Discover(pluginDirs)
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	// Register providers from each plugin
	for _, client := range clients {
		providers, err := client.GetProviders(context.Background())
		if err != nil {
			client.Kill()
			continue
		}

		for _, providerName := range providers {
			wrapper, err := NewProviderWrapper(client, providerName)
			if err != nil {
				continue
			}

			if err := registry.Register(wrapper); err != nil {
				continue
			}
		}
	}

	return nil
}
