// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRemoteCatalog_ParsesURL(t *testing.T) {
	t.Parallel()

	logger := logr.Discard()
	catCfg := config.CatalogConfig{
		Name: "test",
		Type: config.CatalogTypeOCI,
		URL:  "oci://ghcr.io/myorg",
	}

	remoteCat, err := buildRemoteCatalog(catCfg, nil, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, remoteCat)
	assert.Equal(t, "test", remoteCat.Name())
}

func TestBuildRemoteCatalogFromConfig_AuthProvider_UsesGetRegistered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		authProvider    string
		registerHandler bool
		expectAuth      bool
	}{
		{
			name:            "registered handler is wired",
			authProvider:    "entra",
			registerHandler: true,
			expectAuth:      true,
		},
		{
			name:            "unregistered handler is skipped without fallback",
			authProvider:    "missing",
			registerHandler: false,
			expectAuth:      false,
		},
		{
			name:            "empty auth provider skips lookup",
			authProvider:    "",
			registerHandler: false,
			expectAuth:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fallbackCalled := false
			reg := auth.NewRegistry(auth.WithFallbackResolver(func(_ context.Context, _ string) (auth.Handler, error) {
				fallbackCalled = true
				return nil, fmt.Errorf("fallback should not be called")
			}))

			if tc.registerHandler {
				require.NoError(t, reg.Register(auth.NewMockHandler(tc.authProvider)))
			}

			catCfg := config.CatalogConfig{
				Name:         "test-catalog",
				Type:         config.CatalogTypeOCI,
				URL:          "oci://ghcr.io/myorg",
				AuthProvider: tc.authProvider,
			}

			remoteCat, err := BuildRemoteCatalogFromConfig(catCfg, nil, reg, logr.Discard())
			require.NoError(t, err)
			require.NotNil(t, remoteCat)
			assert.False(t, fallbackCalled, "GetRegistered must not trigger the fallback resolver")
		})
	}
}
