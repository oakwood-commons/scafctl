// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"

// Serve is a helper function for plugin implementers to serve their provider plugins.
// This should be called from the plugin's main() function.
func Serve(impl ProviderPlugin) { sdkplugin.Serve(impl) }

// ServeAuthHandler is a helper function for plugin implementers to serve their
// auth handler plugins. This should be called from the plugin's main() function.
func ServeAuthHandler(impl AuthHandlerPlugin) { sdkplugin.ServeAuthHandler(impl) }
