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
	CapFederatedToken       = sdkauth.CapFederatedToken
	CapCallbackPort         = sdkauth.CapCallbackPort
	CapFlowOverride         = sdkauth.CapFlowOverride
)

// HasCapability checks if a set of capabilities includes the specified capability.
func HasCapability(capabilities []Capability, capability Capability) bool {
	return sdkauth.HasCapability(capabilities, capability)
}
