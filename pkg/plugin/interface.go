package plugin

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderPlugin is the interface that plugins must implement
// This wraps the provider.Provider interface for plugin communication
type ProviderPlugin interface {
	// GetProviders returns all provider names exposed by this plugin
	GetProviders(ctx context.Context) ([]string, error)

	// GetProviderDescriptor returns metadata for a specific provider
	GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error)

	// ExecuteProvider executes a provider with the given input
	ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error)
}

// HandshakeConfig is used to verify plugin compatibility
var HandshakeConfig = &HandshakeConfigData{
	ProtocolVersion:  1,
	MagicCookieKey:   "SCAFCTL_PLUGIN",
	MagicCookieValue: "scafctl_provider_plugin",
}

// HandshakeConfigData contains the handshake configuration
type HandshakeConfigData struct {
	ProtocolVersion  uint
	MagicCookieKey   string
	MagicCookieValue string
}
