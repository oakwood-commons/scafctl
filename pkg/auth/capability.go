// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

// Capability represents a feature or behavior that an auth handler supports.
// Capabilities allow CLI commands to dynamically adapt their flags and validation
// based on what each handler supports, enabling plugin-loaded handlers to work
// without hardcoded knowledge of their features.
type Capability string

const (
	// CapScopesOnLogin indicates the handler supports specifying OAuth scopes at login time.
	// Both GitHub (device code) and Entra (device code/SP/WI) support this.
	CapScopesOnLogin Capability = "scopes_on_login"

	// CapScopesOnTokenRequest indicates the handler supports specifying per-request scopes
	// when acquiring tokens. Entra supports this (different resource scopes per request),
	// but GitHub does not (scopes are fixed at login time).
	CapScopesOnTokenRequest Capability = "scopes_on_token_request"

	// CapTenantID indicates the handler supports a tenant ID parameter.
	// Entra uses this for Azure AD tenant selection.
	CapTenantID Capability = "tenant_id"

	// CapHostname indicates the handler supports a hostname parameter.
	// GitHub uses this for GitHub Enterprise Server (GHES) support.
	CapHostname Capability = "hostname"

	// CapFederatedToken indicates the handler supports federated token input.
	// Entra uses this for workload identity (Kubernetes) authentication.
	CapFederatedToken Capability = "federated_token"

	// CapCallbackPort indicates the handler supports binding the OAuth callback
	// server to a specific port via --callback-port. Handlers that use the
	// authorization code + PKCE flow (Entra, GCP) advertise this capability.
	CapCallbackPort Capability = "callback_port"

	// CapFlowOverride indicates the handler supports runtime flow selection via --flow.
	CapFlowOverride Capability = "flow_override"
)

// HasCapability checks if a set of capabilities includes the specified capability.
func HasCapability(capabilities []Capability, capability Capability) bool {
	for _, c := range capabilities {
		if c == capability {
			return true
		}
	}
	return false
}
