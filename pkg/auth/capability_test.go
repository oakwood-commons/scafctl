// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasCapability(t *testing.T) {
	caps := []Capability{CapScopesOnLogin, CapTenantID}

	assert.True(t, HasCapability(caps, CapScopesOnLogin))
	assert.True(t, HasCapability(caps, CapTenantID))
	assert.False(t, HasCapability(caps, CapScopesOnTokenRequest))
	assert.False(t, HasCapability(caps, CapHostname))
	assert.False(t, HasCapability(caps, CapFederatedToken))
}

func TestHasCapability_Empty(t *testing.T) {
	assert.False(t, HasCapability(nil, CapScopesOnLogin))
	assert.False(t, HasCapability([]Capability{}, CapScopesOnLogin))
}

func TestCapabilityConstants(t *testing.T) {
	// Verify capability constants are distinct
	caps := []Capability{
		CapScopesOnLogin,
		CapScopesOnTokenRequest,
		CapTenantID,
		CapHostname,
		CapFederatedToken,
	}

	seen := make(map[Capability]bool)
	for _, c := range caps {
		assert.False(t, seen[c], "duplicate capability: %s", c)
		seen[c] = true
	}
}
