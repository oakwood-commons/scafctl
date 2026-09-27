// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHandlerSecretScopeWiring fails if either auth-handler registration
// path (the fetch fallback resolver and the cached-official lazy wrapper)
// stops scoping host secret access to the handler's own namespace.
func TestAuthHandlerSecretScopeWiring(t *testing.T) {
	t.Parallel()
	deps := &plugin.HostServiceDeps{}
	base := []plugin.ClientOption{plugin.WithHostDeps(deps)}

	// Fallback resolver path.
	got := plugin.ResolvedHostDeps(scopedAuthClientOpts(base, "entra")...)
	require.NotNil(t, got)
	assert.Equal(t, "scafctl.auth.entra.", got.AllowedSecretPrefix)

	// Cached-official lazy wrapper path.
	cfg := cachedAuthHandlerConfig("github", "/bin/fake", nil, base, nil)
	assert.Equal(t, "github", cfg.Name)
	got = plugin.ResolvedHostDeps(cfg.ClientOpts...)
	require.NotNil(t, got)
	assert.Equal(t, "scafctl.auth.github.", got.AllowedSecretPrefix)

	assert.Empty(t, deps.AllowedSecretPrefix, "shared base deps must not be mutated")
	assert.Len(t, base, 1, "base options must not be appended to in place")
}
