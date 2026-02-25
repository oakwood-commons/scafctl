// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"github.com/hashicorp/go-plugin"
)

// Serve is a helper function for plugin implementers to serve their provider plugins.
// This should be called from the plugin's main() function.
func Serve(impl ProviderPlugin) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  HandshakeConfig.ProtocolVersion,
			MagicCookieKey:   HandshakeConfig.MagicCookieKey,
			MagicCookieValue: HandshakeConfig.MagicCookieValue,
		},
		Plugins: map[string]plugin.Plugin{
			PluginName: &GRPCPlugin{Impl: impl},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

// ServeAuthHandler is a helper function for plugin implementers to serve their
// auth handler plugins. This should be called from the plugin's main() function.
func ServeAuthHandler(impl AuthHandlerPlugin) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  AuthHandlerHandshakeConfig.ProtocolVersion,
			MagicCookieKey:   AuthHandlerHandshakeConfig.MagicCookieKey,
			MagicCookieValue: AuthHandlerHandshakeConfig.MagicCookieValue,
		},
		Plugins: map[string]plugin.Plugin{
			AuthHandlerPluginName: &AuthHandlerGRPCPlugin{Impl: impl},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
