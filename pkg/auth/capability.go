// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"

// Capability represents a feature or behavior that an auth handler supports.
type Capability = sdkauth.Capability

const (
	CapScopesOnLogin        = sdkauth.CapScopesOnLogin
	CapScopesOnTokenRequest = sdkauth.CapScopesOnTokenRequest
	CapTenantID             = sdkauth.CapTenantID
	CapHostname             = sdkauth.CapHostname
	CapTokenHostname        = sdkauth.CapTokenHostname
	CapInstanceHostname     = sdkauth.CapInstanceHostname
	CapFederatedToken       = sdkauth.CapFederatedToken
	CapCallbackPort         = sdkauth.CapCallbackPort
	CapFlowOverride         = sdkauth.CapFlowOverride
)

// Callback port bounds for the interactive OAuth loopback callback server.
// Zero means "unset" (ephemeral/OS-assigned); any non-zero value must be an
// unprivileged, in-range TCP port within [MinCallbackPort, MaxCallbackPort].
// These are the single source of truth shared by CLI flag validation and the
// host->plugin wire clamp so the two layers cannot drift.
const (
	MinCallbackPort = 1024
	MaxCallbackPort = 65535
)

// HasCapability checks if a set of capabilities includes the specified capability.
func HasCapability(capabilities []Capability, capability Capability) bool {
	return sdkauth.HasCapability(capabilities, capability)
}
