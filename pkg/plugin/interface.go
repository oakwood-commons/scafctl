// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderConfig holds host-side configuration sent to a provider once
// after plugin load via the ConfigureProvider RPC.
type ProviderConfig struct {
	// Quiet suppresses non-essential output.
	Quiet bool `json:"quiet" yaml:"quiet" doc:"Whether non-essential output is suppressed."`
	// NoColor disables colored output.
	NoColor bool `json:"noColor" yaml:"noColor" doc:"Whether colored output is disabled."`
	// BinaryName is the CLI binary name (e.g. "scafctl" or an embedder name).
	BinaryName string `json:"binaryName" yaml:"binaryName" doc:"The CLI binary name." maxLength:"128" example:"scafctl"`
	// HostServiceID is the GRPCBroker service ID for HostService callbacks.
	// Plugins use this to dial back into the host for secrets, auth, etc.
	HostServiceID uint32 `json:"hostServiceId,omitempty" yaml:"hostServiceId,omitempty" doc:"GRPCBroker service ID for HostService callbacks."`
	// Settings holds extensible key-value configuration. Values are JSON-encoded.
	Settings map[string]json.RawMessage `json:"settings,omitempty" yaml:"settings,omitempty" doc:"Extensible JSON-encoded settings."`
}

// StreamChunk represents one chunk from a streaming provider execution.
// Exactly one field is non-nil per chunk.
type StreamChunk struct {
	// Stdout holds raw stdout bytes (nil when this is not a stdout chunk).
	Stdout []byte `json:"stdout,omitempty" yaml:"stdout,omitempty" doc:"Raw stdout bytes."`
	// Stderr holds raw stderr bytes (nil when this is not a stderr chunk).
	Stderr []byte `json:"stderr,omitempty" yaml:"stderr,omitempty" doc:"Raw stderr bytes."`
	// Result holds the final execution result (non-nil only for the last chunk).
	Result *provider.Output `json:"result,omitempty" yaml:"result,omitempty" doc:"Final execution result."`
	// Error holds a terminal error string (non-empty only for the last chunk on failure).
	Error string `json:"error,omitempty" yaml:"error,omitempty" doc:"Terminal error message." maxLength:"4096"`
}

// ProviderPlugin is the interface that plugins must implement
// This wraps the provider.Provider interface for plugin communication
type ProviderPlugin interface {
	// GetProviders returns all provider names exposed by this plugin
	GetProviders(ctx context.Context) ([]string, error)

	// GetProviderDescriptor returns metadata for a specific provider
	GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error)

	// ConfigureProvider sends host-side configuration to a named provider once
	// after plugin load. Implementations store the config internally for
	// subsequent Execute calls. Plugins that do not need configuration may
	// return nil.
	ConfigureProvider(ctx context.Context, providerName string, cfg ProviderConfig) error

	// ExecuteProvider executes a provider with the given input
	ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error)

	// ExecuteProviderStream executes a provider that produces incremental
	// output. The callback is invoked for each chunk; the final chunk carries
	// the Result (or Error). Plugins that do not support streaming may return
	// ErrStreamingNotSupported.
	ExecuteProviderStream(ctx context.Context, providerName string, input map[string]any, cb func(StreamChunk)) error

	// DescribeWhatIf returns a human-readable description of what the provider
	// would do with the given inputs, without executing. Returns an empty string
	// if the plugin does not implement WhatIf for this provider.
	DescribeWhatIf(ctx context.Context, providerName string, input map[string]any) (string, error)

	// ExtractDependencies returns resolver dependency names from the given
	// inputs. Plugins that do not implement custom extraction should return
	// nil. The host falls back to generic extraction when the result is nil.
	ExtractDependencies(ctx context.Context, providerName string, inputs map[string]any) ([]string, error)
}

// ErrStreamingNotSupported is returned by ExecuteProviderStream when the plugin
// does not support streaming execution.
var ErrStreamingNotSupported = errors.New("streaming execution not supported")

// AuthHandlerInfo holds static metadata about an auth handler exposed by a plugin.
type AuthHandlerInfo struct {
	Name         string
	DisplayName  string
	Flows        []auth.Flow
	Capabilities []auth.Capability
}

// LoginRequest contains parameters for a plugin Login call.
type LoginRequest struct {
	TenantID string
	Scopes   []string
	Flow     auth.Flow
	Timeout  time.Duration
}

// LoginResponse contains the result of a plugin Login call.
type LoginResponse struct {
	Claims    *auth.Claims
	ExpiresAt time.Time
}

// DeviceCodePrompt is sent over streaming Login to relay device-code info to the host.
type DeviceCodePrompt struct {
	UserCode        string
	VerificationURI string
	Message         string
}

// LoginStreamMessage represents a message in the Login server-stream.
// Exactly one field is non-nil.
type LoginStreamMessage struct {
	DeviceCodePrompt *DeviceCodePrompt
	Result           *LoginResponse
	Error            string
}

// TokenRequest contains parameters for a plugin GetToken call.
type TokenRequest struct {
	Scope        string
	MinValidFor  time.Duration
	ForceRefresh bool
}

// TokenResponse contains the result of a plugin GetToken call.
type TokenResponse struct {
	AccessToken string //nolint:gosec // G117: not a hardcoded credential, stores runtime token data
	TokenType   string
	ExpiresAt   time.Time
	Scope       string
	CachedAt    time.Time
	Flow        auth.Flow
	SessionID   string
}

// AuthHandlerPlugin is the interface that auth handler plugins must implement.
// This wraps the auth.Handler interface for plugin communication over gRPC.
type AuthHandlerPlugin interface {
	// GetAuthHandlers returns metadata for all auth handlers exposed by this plugin.
	GetAuthHandlers(ctx context.Context) ([]AuthHandlerInfo, error)

	// Login initiates authentication for the named handler.
	// The callback, if non-nil, is invoked when the plugin sends a device-code prompt.
	Login(ctx context.Context, handlerName string, req LoginRequest, deviceCodeCb func(DeviceCodePrompt)) (*LoginResponse, error)

	// Logout clears stored credentials for the named handler.
	Logout(ctx context.Context, handlerName string) error

	// GetStatus returns the current authentication status for the named handler.
	GetStatus(ctx context.Context, handlerName string) (*auth.Status, error)

	// GetToken returns a valid access token for the named handler.
	GetToken(ctx context.Context, handlerName string, req TokenRequest) (*TokenResponse, error)

	// ListCachedTokens returns all cached tokens for the named handler.
	// Returns an empty slice if the handler does not support token listing.
	ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error)

	// PurgeExpiredTokens removes expired tokens for the named handler.
	// Returns the number of tokens removed and an error if the handler
	// does not support token purging.
	PurgeExpiredTokens(ctx context.Context, handlerName string) (int, error)
}

// HandshakeConfig is used to verify provider plugin compatibility.
var HandshakeConfig = &HandshakeConfigData{
	ProtocolVersion:  1,
	MagicCookieKey:   "SCAFCTL_PLUGIN",
	MagicCookieValue: "scafctl_provider_plugin",
}

// AuthHandlerHandshakeConfig is used to verify auth handler plugin compatibility.
var AuthHandlerHandshakeConfig = &HandshakeConfigData{
	ProtocolVersion:  1,
	MagicCookieKey:   "SCAFCTL_AUTH_PLUGIN",
	MagicCookieValue: "scafctl_auth_handler_plugin",
}

// HandshakeConfigData contains the handshake configuration
type HandshakeConfigData struct {
	ProtocolVersion  uint
	MagicCookieKey   string
	MagicCookieValue string
}
