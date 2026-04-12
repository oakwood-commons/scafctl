// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderWrapper wraps a plugin provider to implement the provider.Provider interface
type ProviderWrapper struct {
	client       *Client
	providerName string
	descriptor   *provider.Descriptor
	configured   bool
	mu           sync.RWMutex
}

// wrapperConfig holds configuration for NewProviderWrapper.
type wrapperConfig struct {
	ctx context.Context
}

// WrapperOption configures NewProviderWrapper.
type WrapperOption func(*wrapperConfig)

// WithContext sets the context used during wrapper initialisation (e.g. for
// the initial GetProviderDescriptor RPC). Defaults to context.Background().
// A nil context is silently replaced with context.Background().
func WithContext(ctx context.Context) WrapperOption {
	return func(c *wrapperConfig) {
		if ctx != nil {
			c.ctx = ctx
		}
	}
}

// NewProviderWrapper creates a new provider wrapper for a plugin provider
func NewProviderWrapper(client *Client, providerName string, opts ...WrapperOption) (*ProviderWrapper, error) {
	wCfg := wrapperConfig{ctx: context.Background()}
	for _, o := range opts {
		o(&wCfg)
	}

	// Get the descriptor to validate the provider exists
	desc, err := client.GetProviderDescriptor(wCfg.ctx, providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider descriptor: %w", err)
	}

	w := &ProviderWrapper{
		client:       client,
		providerName: providerName,
		descriptor:   desc,
	}

	// Wire up WhatIf to call the plugin over gRPC
	name := providerName
	desc.WhatIf = func(ctx context.Context, input any) (string, error) {
		var inputs map[string]any
		if input != nil {
			var ok bool
			inputs, ok = input.(map[string]any)
			if !ok {
				return "", nil
			}
		}
		return client.DescribeWhatIf(ctx, name, inputs)
	}

	// Wire ExtractDependencies when the plugin declares custom extraction.
	// protoToDescriptor sets a placeholder function when the proto
	// has_extract_dependencies flag is true; replace it with a real RPC call.
	// Note: Descriptor.ExtractDependencies does not accept a context, so we use
	// a timeout to prevent blocking indefinitely on an unresponsive plugin.
	if desc.ExtractDependencies != nil {
		desc.ExtractDependencies = func(inputs map[string]any) []string {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			deps, err := client.ExtractDependencies(ctx, name, inputs)
			if err != nil {
				logger.FromContext(ctx).V(1).Info("plugin ExtractDependencies failed, falling back to generic extraction",
					"provider", name, "error", err)
				return nil
			}
			return deps
		}
	}

	return w, nil
}

// Descriptor returns the provider descriptor
func (w *ProviderWrapper) Descriptor() *provider.Descriptor {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.descriptor
}

// Execute executes the provider. When IOStreams are present in the context,
// it attempts to use streaming execution so that incremental output (e.g. from
// exec-like providers) reaches the terminal in real time. Falls back to the
// unary RPC when the plugin does not support streaming.
//
// Streaming error contract: the GRPCClient.ExecuteProviderStream callback
// receives stdout/stderr chunks as they arrive. Terminal errors are delivered
// in two ways: (1) the callback receives a chunk with Error set, and
// (2) ExecuteProviderStream returns a non-nil error. This wrapper checks
// the return value first — if it signals ErrStreamingNotSupported it falls
// back to unary; for other errors it returns immediately. When the stream
// completes successfully (nil return), any chunk-level error is surfaced.
func (w *ProviderWrapper) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", w.descriptor.Name, input)
	}

	lgr.V(1).Info("executing plugin provider", "provider", w.descriptor.Name)

	// If IOStreams are available, prefer streaming so the user sees output
	// as it arrives. Fall back to unary if the plugin doesn't support it.
	if ioStreams, hasIO := provider.IOStreamsFromContext(ctx); hasIO && ioStreams != nil {
		var lastOutput *provider.Output
		var streamChunkErr string
		streamErr := w.client.ExecuteProviderStream(ctx, w.providerName, inputs, func(chunk StreamChunk) {
			if chunk.Stdout != nil && ioStreams.Out != nil {
				_, _ = ioStreams.Out.Write(chunk.Stdout)
			}
			if chunk.Stderr != nil && ioStreams.ErrOut != nil {
				_, _ = ioStreams.ErrOut.Write(chunk.Stderr)
			}
			if chunk.Error != "" {
				streamChunkErr = chunk.Error
			}
			if chunk.Result != nil {
				lastOutput = chunk.Result
			}
		})
		if streamErr == nil {
			if streamChunkErr != "" {
				return nil, fmt.Errorf("%s: %s", w.descriptor.Name, streamChunkErr)
			}
			lgr.V(1).Info("plugin provider completed (streaming)", "provider", w.descriptor.Name)
			if lastOutput != nil {
				return lastOutput, nil
			}
			return &provider.Output{}, nil
		}
		// Only fall through for streaming-not-supported; other errors are real failures.
		if !errors.Is(streamErr, ErrStreamingNotSupported) {
			return nil, fmt.Errorf("%s: %w", w.descriptor.Name, streamErr)
		}
		lgr.V(2).Info("plugin does not support streaming, falling back to unary", "provider", w.descriptor.Name)
	}

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

// Configure sends host-side configuration to the plugin provider.
// This should be called once after wrapper creation, before any Execute calls.
func (w *ProviderWrapper) Configure(ctx context.Context, cfg ProviderConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.configured {
		return nil
	}

	if err := w.client.ConfigureProvider(ctx, w.providerName, cfg); err != nil {
		return fmt.Errorf("%s: configure: %w", w.descriptor.Name, err)
	}

	w.configured = true
	return nil
}

// ExecuteStream executes the provider with streaming output.
// The callback receives chunks as they arrive from the plugin.
func (w *ProviderWrapper) ExecuteStream(ctx context.Context, input any, cb func(StreamChunk)) error {
	inputs, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected map[string]any, got %T", w.descriptor.Name, input)
	}

	return w.client.ExecuteProviderStream(ctx, w.providerName, inputs, cb)
}

// RegisterPluginProviders discovers plugins and registers them with the provider registry.
// After registration, each wrapper is configured with the provided ProviderConfig.
// If cfg is nil, configuration is skipped (providers will use defaults).
// Returns the created clients; the caller should defer calling KillAll(clients)
// to clean up plugin processes on exit.
func RegisterPluginProviders(ctx context.Context, registry *provider.Registry, pluginDirs []string, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*Client, error) {
	// Discover plugins
	clients, err := Discover(pluginDirs, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover plugins: %w", err)
	}

	// Register providers from each plugin
	for _, client := range clients {
		providers, err := client.GetProviders(ctx)
		if err != nil {
			logger.FromContext(ctx).V(1).Info("failed to get providers from plugin", "plugin", client.Name(), "error", err)
			client.Kill()
			continue
		}

		lgr := logger.FromContext(ctx)
		for _, providerName := range providers {
			wrapper, err := NewProviderWrapper(client, providerName, WithContext(ctx))
			if err != nil {
				lgr.V(1).Info("failed to create provider wrapper", "provider", providerName, "plugin", client.Name(), "error", err)
				continue
			}

			if err := registry.Register(wrapper); err != nil {
				lgr.V(1).Info("failed to register plugin provider", "provider", providerName, "plugin", client.Name(), "error", err)
				continue
			}

			// Send host configuration to the plugin provider
			if cfg != nil {
				if err := wrapper.Configure(ctx, *cfg); err != nil {
					lgr := logger.FromContext(ctx)
					lgr.V(1).Info("failed to configure plugin provider",
						"provider", providerName,
						"error", err)
				}
			}
		}
	}

	return clients, nil
}

// KillAll terminates all plugin processes in the given client list.
// This is safe to call with a nil or empty slice.
func KillAll(clients []*Client) {
	for _, c := range clients {
		if c != nil {
			c.Kill()
		}
	}
}
