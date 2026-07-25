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

func TestBuildRemoteCatalogChain_ExcludesLocal(t *testing.T) {
	t.Parallel()

	logger := logr.Discard()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name: "my-registry",
				Type: config.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	chain, err := BuildRemoteCatalogChain(cfg, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, chain)

	// Verify no catalog in the chain is the local catalog.
	for _, cat := range chain.Catalogs() {
		assert.NotEqual(t, LocalCatalogName, cat.Name(),
			"BuildRemoteCatalogChain must not include the local catalog")
	}
}

func TestBuildRemoteCatalogChain_NilConfig_ReturnsError(t *testing.T) {
	t.Parallel()

	chain, err := BuildRemoteCatalogChain(nil, nil, logr.Discard())
	assert.Nil(t, chain)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no remote catalogs available")
}

func TestBuildRemoteCatalogChain_NoCatalogs_ReturnsError(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Settings: config.Settings{
			DisableOfficialCatalog: true,
		},
	}

	chain, err := BuildRemoteCatalogChain(cfg, nil, logr.Discard())
	assert.Nil(t, chain)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no remote catalogs available")
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

func TestDefaultListCatalogs(t *testing.T) {
	t.Parallel()

	const (
		officialURL = "oci://ghcr.io/oakwood-commons"
		privateURL  = "oci://private.example.com/catalog"
	)

	officialCat := config.CatalogConfig{
		Name: config.CatalogNameOfficial,
		Type: config.CatalogTypeOCI,
		URL:  officialURL,
	}
	privateCat := config.CatalogConfig{
		Name: "corp",
		Type: config.CatalogTypeOCI,
		URL:  privateURL,
	}
	localCat := config.CatalogConfig{
		Name: config.CatalogNameLocal,
		Type: config.CatalogTypeFilesystem,
		Path: "/tmp/catalog",
	}

	tests := []struct {
		name      string
		cfg       *config.Config
		wantNames []string
	}{
		{
			name: "default and official distinct -> primary then official",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "corp"},
				Catalogs: []config.CatalogConfig{localCat, privateCat, officialCat},
			},
			wantNames: []string{"corp", config.CatalogNameOfficial},
		},
		{
			name: "disableOfficialCatalog -> default only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "corp", DisableOfficialCatalog: true},
				Catalogs: []config.CatalogConfig{localCat, privateCat, officialCat},
			},
			wantNames: []string{"corp"},
		},
		{
			name: "default IS official by name -> official once (deduped)",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: config.CatalogNameOfficial},
				Catalogs: []config.CatalogConfig{localCat, officialCat},
			},
			wantNames: []string{config.CatalogNameOfficial},
		},
		{
			name: "default IS official by name AND disabled -> empty",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: config.CatalogNameOfficial, DisableOfficialCatalog: true},
				Catalogs: []config.CatalogConfig{localCat, officialCat},
			},
			wantNames: []string{},
		},
		{
			name: "default IS official by URL AND disabled -> empty",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "mirror", DisableOfficialCatalog: true},
				Catalogs: []config.CatalogConfig{
					localCat,
					{Name: "mirror", Type: config.CatalogTypeOCI, URL: officialURL},
					officialCat,
				},
			},
			wantNames: []string{},
		},
		{
			name: "default has same URL as official -> deduped to primary",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "mirror"},
				Catalogs: []config.CatalogConfig{
					localCat,
					{Name: "mirror", Type: config.CatalogTypeOCI, URL: officialURL},
					officialCat,
				},
			},
			wantNames: []string{"mirror"},
		},
		{
			name: "no default configured -> official only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: ""},
				Catalogs: []config.CatalogConfig{localCat, officialCat},
			},
			wantNames: []string{config.CatalogNameOfficial},
		},
		{
			name: "default is non-OCI -> official only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: config.CatalogNameLocal},
				Catalogs: []config.CatalogConfig{localCat, officialCat},
			},
			wantNames: []string{config.CatalogNameOfficial},
		},
		{
			name: "official missing from catalogs -> default only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "corp"},
				Catalogs: []config.CatalogConfig{localCat, privateCat},
			},
			wantNames: []string{"corp"},
		},
		{
			name: "official present but non-OCI -> default only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "corp"},
				Catalogs: []config.CatalogConfig{
					localCat,
					privateCat,
					{Name: config.CatalogNameOfficial, Type: config.CatalogTypeHTTP, URL: officialURL},
				},
			},
			wantNames: []string{"corp"},
		},
		{
			// A non-OCI official entry whose URL matches an OCI default must NOT
			// make the default look "official by URL": isOfficialByURL is guarded
			// on the official catalog being OCI. The default is listed normally
			// and the non-OCI official is skipped.
			name: "official non-OCI with matching-URL default -> default listed, official skipped",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "mirror"},
				Catalogs: []config.CatalogConfig{
					localCat,
					{Name: "mirror", Type: config.CatalogTypeOCI, URL: officialURL},
					{Name: config.CatalogNameOfficial, Type: config.CatalogTypeHTTP, URL: officialURL},
				},
			},
			wantNames: []string{"mirror"},
		},
		{
			name:      "nil config -> nil",
			cfg:       nil,
			wantNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultListCatalogs(tt.cfg)
			gotNames := make([]string, len(got))
			for i, c := range got {
				gotNames[i] = c.Name
			}
			assert.Equal(t, tt.wantNames, gotNames, "ordered catalog names")
		})
	}
}
