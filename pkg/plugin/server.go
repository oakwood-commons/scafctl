// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"github.com/hashicorp/go-plugin"
)

// Serve is a helper function for plugin implementers to serve their plugins
// This should be called from the plugin's main() function
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
