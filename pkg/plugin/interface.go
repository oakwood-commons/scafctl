// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"

// --- SDK type aliases ---

// ProviderConfig holds host-side configuration sent to a provider once
// after plugin load via the ConfigureProvider RPC.
type ProviderConfig = sdkplugin.ProviderConfig

// StreamChunk represents one chunk from a streaming provider execution.
type StreamChunk = sdkplugin.StreamChunk

// ProviderPlugin is the interface that plugins must implement.
type ProviderPlugin = sdkplugin.ProviderPlugin

// AuthHandlerInfo holds static metadata about an auth handler exposed by a plugin.
type AuthHandlerInfo = sdkplugin.AuthHandlerInfo

// LoginRequest contains parameters for a plugin Login call.
type LoginRequest = sdkplugin.LoginRequest

// LoginResponse contains the result of a plugin Login call.
type LoginResponse = sdkplugin.LoginResponse

// DeviceCodePrompt is sent over streaming Login to relay device-code info to the host.
type DeviceCodePrompt = sdkplugin.DeviceCodePrompt

// LoginStreamMessage represents a message in the Login server-stream.
type LoginStreamMessage = sdkplugin.LoginStreamMessage

// TokenRequest contains parameters for a plugin GetToken call.
type TokenRequest = sdkplugin.TokenRequest

// TokenResponse contains the result of a plugin GetToken call.
type TokenResponse = sdkplugin.TokenResponse

// AuthHandlerPlugin is the interface that auth handler plugins must implement.
type AuthHandlerPlugin = sdkplugin.AuthHandlerPlugin

// HandshakeConfigData contains the handshake configuration.
type HandshakeConfigData = sdkplugin.HandshakeConfigData

// ErrStreamingNotSupported is returned by ExecuteProviderStream when the plugin
// does not support streaming execution.
var ErrStreamingNotSupported = sdkplugin.ErrStreamingNotSupported

// PluginProtocolVersion is the current plugin protocol version.
const PluginProtocolVersion = sdkplugin.PluginProtocolVersion

// handshakeConfigVal and authHandlerHandshakeConfigVal cache the SDK handshake
// configs at init time. In the SDK these are function-based to prevent mutation;
// here we store them as pointer variables for backward compatibility with existing
// scafctl code that reads HandshakeConfig.ProtocolVersion etc.
// Do not mutate; read-only for backward compat.
//
//nolint:gochecknoglobals // backward compat with existing callers
var (
	HandshakeConfig            = ptrTo(sdkplugin.HandshakeConfig())
	AuthHandlerHandshakeConfig = ptrTo(sdkplugin.AuthHandlerHandshakeConfig())
)

func ptrTo[T any](v T) *T { return &v }
