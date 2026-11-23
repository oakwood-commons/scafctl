// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestBuildRemoteCatalogFromConfig_PropagatesInsecure(t *testing.T) {
	t.Parallel()

	catCfg := config.CatalogConfig{
		Name:     "local-test-registry",
		Type:     config.CatalogTypeOCI,
		URL:      "oci://127.0.0.1:5000/scafctl",
		Insecure: true,
	}

	remoteCat, err := BuildRemoteCatalogFromConfig(catCfg, nil, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, remoteCat)
	assert.True(t, remoteCat.insecure)
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
		officialURL = config.DefaultOfficialCatalogURL
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
			// The config merge omits the official entry entirely when
			// disableOfficialCatalog is set, so isOfficialByURL falls back to the
			// embedded default official URL. An aliased default pointing at that
			// URL must still be recognized as official and dropped when disabled.
			name: "official omitted + default URL == official URL + disabled -> empty",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "mirror", DisableOfficialCatalog: true},
				Catalogs: []config.CatalogConfig{
					localCat,
					{Name: "mirror", Type: config.CatalogTypeOCI, URL: officialURL},
				},
			},
			wantNames: []string{},
		},
		{
			// Same aliased-to-official default, but official not disabled: the
			// default is recognized as official and listed once (official is
			// absent from the config, so there is nothing to append/dedup after).
			name: "official omitted + default URL == official URL + enabled -> default only",
			cfg: &config.Config{
				Settings: config.Settings{DefaultCatalog: "mirror"},
				Catalogs: []config.CatalogConfig{
					localCat,
					{Name: "mirror", Type: config.CatalogTypeOCI, URL: officialURL},
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

// pluginFetcherTestConfig configures one embedder catalog plus the official
// catalog; neither is contacted by the builders.
func pluginFetcherTestConfig() *config.Config {
	return &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "embedder", Type: config.CatalogTypeOCI, URL: "oci://registry.example.com/embedder"},
			{Name: config.CatalogNameOfficial, Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/oakwood-commons"},
		},
	}
}

func catalogNames(cats []Catalog) []string {
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = c.Name()
	}
	return names
}

func TestBuildPluginFetcher_OrderAndCachedRemotes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard())
	require.NoError(t, err)

	cats := f.Catalogs()
	assert.Equal(t, []string{LocalCatalogName, "embedder", config.CatalogNameOfficial}, catalogNames(cats))
	assert.IsType(t, &LocalCatalog{}, cats[0])
	for _, c := range cats[1:] {
		assert.IsType(t, &CachedRemoteCatalog{}, c, "remotes are backed by the local cache")
	}
}

func TestBuildPluginFetcher_LocalNotAllowedStillCaches(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard(),
		WithAllowedCatalogs([]string{config.CatalogNameOfficial}))
	require.NoError(t, err)

	cats := f.Catalogs()
	require.Equal(t, []string{config.CatalogNameOfficial}, catalogNames(cats))
	assert.IsType(t, &CachedRemoteCatalog{}, cats[0])
}

func TestBuildPluginFetcher_WithLocalCatalog(t *testing.T) {
	t.Parallel()

	injected, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard(), WithLocalCatalog(injected))
	require.NoError(t, err)

	cats := f.Catalogs()
	require.Equal(t, []string{LocalCatalogName, "embedder", config.CatalogNameOfficial}, catalogNames(cats))
	assert.Same(t, injected, cats[0], "injected LocalCatalog instance must be reused directly")
	for _, c := range cats[1:] {
		cr, ok := c.(*CachedRemoteCatalog)
		require.True(t, ok)
		assert.Same(t, injected, cr.local, "CachedRemoteCatalog must share the injected LocalCatalog")
	}

	chain, err := BuildCatalogChain(pluginFetcherTestConfig(), nil, logr.Discard(), WithLocalCatalog(injected))
	require.NoError(t, err)
	assert.Same(t, injected, chain.Catalogs()[0])
}

func TestBuildPluginFetcher_PerCatalogArtifacts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard(),
		WithPerCatalogArtifacts(map[string]PluginPolicy{
			config.CatalogNameOfficial: {Plugins: []string{"exec"}},
		}))
	require.NoError(t, err)

	// A restricted local stays first in priority order.
	assert.Equal(t, []string{LocalCatalogName, "embedder", config.CatalogNameOfficial}, catalogNames(f.Catalogs()))

	// "git" is denied everywhere (local + embedder deny-all, official lists
	// only exec), so the allowlists answer not-found without network access.
	_, err = f.ResolvePlugin(t.Context(), "git", ArtifactKindProvider, "")
	require.ErrorIs(t, err, ErrArtifactNotFound)
}

func TestBuildPluginFetcher_WithoutLocalBuilds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard(), WithoutLocalBuilds())
	require.NoError(t, err)

	cats := f.Catalogs()
	require.Equal(t, []string{"embedder", config.CatalogNameOfficial}, catalogNames(cats))
	for _, c := range cats {
		assert.IsType(t, &CachedRemoteCatalog{}, c, "remotes are still backed by the local cache")
	}

	_, err = f.ResolvePlugin(t.Context(), "my-plugin", ArtifactKindProvider, "", WithCatalog(LocalCatalogName))
	require.ErrorContains(t, err, "not configured")
}

func TestBuildPluginFetcher_NoSources(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	f, err := BuildPluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard(),
		WithAllowedCatalogs([]string{"nonexistent"}))
	assert.Nil(t, f)
	require.ErrorContains(t, err, "no catalogs available for plugin resolution")
}

func TestBuildRemotePluginFetcher(t *testing.T) {
	t.Parallel()

	f, err := BuildRemotePluginFetcher(pluginFetcherTestConfig(), nil, logr.Discard())
	require.NoError(t, err)

	cats := f.Catalogs()
	assert.Equal(t, []string{"embedder", config.CatalogNameOfficial}, catalogNames(cats))
	for _, c := range cats {
		assert.IsType(t, &RemoteCatalog{}, c, "remote-only fetcher has no cache fallback")
	}
}

func TestBuildRemotePluginFetcher_NoCatalogs(t *testing.T) {
	t.Parallel()

	f, err := BuildRemotePluginFetcher(nil, nil, logr.Discard())
	assert.Nil(t, f)
	require.ErrorContains(t, err, "no remote catalogs available")
}

func TestArtifactAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		perCatalog   map[string]PluginPolicy
		wantAllowed  []string
		wantRestrict bool
	}{
		{name: "no policy map is unrestricted", perCatalog: nil},
		{name: "AllowAll is unrestricted", perCatalog: map[string]PluginPolicy{"c": {AllowAll: true}}},
		{
			name:         "explicit list restricts to it",
			perCatalog:   map[string]PluginPolicy{"c": {Plugins: []string{"exec"}}},
			wantAllowed:  []string{"exec"},
			wantRestrict: true,
		},
		{
			name:         "policy without plugins denies all",
			perCatalog:   map[string]PluginPolicy{"c": {}},
			wantAllowed:  []string{},
			wantRestrict: true,
		},
		{
			name:         "catalog absent from active map denies all",
			perCatalog:   map[string]PluginPolicy{"other": {AllowAll: true}},
			wantAllowed:  []string{},
			wantRestrict: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			allowed, restrict := artifactAllowlist("c", tc.perCatalog)
			assert.Equal(t, tc.wantRestrict, restrict)
			assert.Equal(t, tc.wantAllowed, allowed)
		})
	}
}

func TestBuildPluginFetcher_ArtifactKindsSkipsSolutionOnlyCatalog(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:          "solutions-only",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://private.example.com/solutions",
				ArtifactKinds: []string{config.CatalogArtifactKindSolution},
			},
			{
				Name: config.CatalogNameOfficial,
				Type: config.CatalogTypeOCI,
				URL:  "oci://ghcr.io/oakwood-commons",
			},
		},
	}

	f, err := BuildPluginFetcher(cfg, nil, logr.Discard())
	require.NoError(t, err)
	assert.Equal(t, []string{LocalCatalogName, config.CatalogNameOfficial}, catalogNames(f.Catalogs()),
		"solution-only catalog must be omitted from plugin fetcher")

	rf, err := BuildRemotePluginFetcher(cfg, nil, logr.Discard())
	require.NoError(t, err)
	assert.Equal(t, []string{config.CatalogNameOfficial}, catalogNames(rf.Catalogs()),
		"solution-only catalog must be omitted from remote plugin fetcher")
}

func TestBuildPluginFetcher_ArtifactKindsSinglePluginKind(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Plain HTTP server on 127.0.0.1 fails HTTPS handshake immediately (<1ms) as
	// UnreachableError when contacted, whereas an unserved kind (auth-handler)
	// returns ArtifactNotFoundError locally without dialing at all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	cfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{
				Name:          "providers-only",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://" + host + "/providers",
				Insecure:      true,
				ArtifactKinds: []string{config.CatalogArtifactKindProvider},
			},
		},
	}

	f, err := BuildPluginFetcher(cfg, nil, logr.Discard(), WithoutLocalBuilds())
	require.NoError(t, err)
	assert.Equal(t, []string{"providers-only"}, catalogNames(f.Catalogs()))

	// Auth-handler lookup is filtered locally as not-found with zero network calls.
	_, err = f.ResolvePlugin(t.Context(), "github", ArtifactKindAuthHandler, "")
	require.ErrorIs(t, err, ErrArtifactNotFound)

	// Provider lookup reaches the remote catalog and reports unreachable.
	_, err = f.ResolvePlugin(t.Context(), "exec", ArtifactKindProvider, "")
	_, unreachable := IsCatalogUnreachable(err)
	assert.True(t, unreachable, "expected provider lookup to contact catalog, got %v", err)
}

func TestBuildPluginFetcher_ChainModeBestEffortVsStrict(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Plain HTTP server on 127.0.0.1 fails HTTPS handshake immediately as UnreachableError ("unavailable").
	unavailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(unavailSrv.Close)
	unavailHost := strings.TrimPrefix(unavailSrv.URL, "http://")

	// TLS server rejecting bearer credentials produces errCredentialsRejected ("unauthenticated").
	unauthSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`))
	}))
	t.Cleanup(unauthSrv.Close)
	unauthHost := strings.TrimPrefix(unauthSrv.URL, "https://")

	authReg := auth.NewRegistry()
	mockAuth := auth.NewMockHandler("corp-sso")
	mockAuth.SetToken(&auth.Token{AccessToken: "expired-token"})
	require.NoError(t, authReg.Register(mockAuth))

	cfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{
				Name:     "broken-private",
				Type:     config.CatalogTypeOCI,
				URL:      "oci://" + unavailHost + "/private",
				Insecure: true,
			},
			{
				Name:         "expired-auth",
				Type:         config.CatalogTypeOCI,
				URL:          "oci://" + unauthHost + "/corp",
				AuthProvider: "corp-sso",
				Insecure:     true,
			},
			{
				Name:     "mirror",
				Type:     config.CatalogTypeOCI,
				URL:      "oci://" + unavailHost + "/mirror",
				Insecure: true,
			},
		},
	}

	var bestEffortWarnings []string
	bf, err := BuildPluginFetcher(cfg, authReg, logr.Discard(),
		WithoutLocalBuilds(),
		WithPluginWarningHandler(func(msg string) {
			bestEffortWarnings = append(bestEffortWarnings, msg)
		}),
	)
	require.NoError(t, err)
	_, err = bf.ResolvePlugin(t.Context(), "exec", ArtifactKindProvider, "")
	require.Error(t, err)
	assert.Equal(t, []string{
		`catalog "broken-private" unavailable, trying next catalog...`,
		`catalog "expired-auth" unauthenticated, trying next catalog...`,
	}, bestEffortWarnings,
		"best-effort mode warns with unavailable/unauthenticated and skips trailing warning on last source")

	var strictWarnings []string
	sf, err := BuildPluginFetcher(cfg, authReg, logr.Discard(),
		WithoutLocalBuilds(),
		WithPluginChainMode(ChainModeStrict),
		WithPluginWarningHandler(func(msg string) {
			strictWarnings = append(strictWarnings, msg)
		}),
	)
	require.NoError(t, err)
	_, err = sf.ResolvePlugin(t.Context(), "exec", ArtifactKindProvider, "")
	require.Error(t, err)
	assert.Empty(t, strictWarnings, "strict mode halts immediately without fallback warnings")
}

func TestBuildCatalogChain_ArtifactKindsUnwrapsCleanly(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{
				Name:          "providers-only",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://127.0.0.1:1/providers",
				Insecure:      true,
				ArtifactKinds: []string{config.CatalogArtifactKindProvider},
			},
		},
	}

	chain, err := BuildCatalogChain(cfg, nil, logr.Discard(), WithAllowedCatalogs([]string{"providers-only"}))
	require.NoError(t, err)
	require.Len(t, chain.Catalogs(), 1)

	kf, ok := chain.Catalogs()[0].(*KindFilterCatalog)
	require.True(t, ok, "catalog with artifactKinds must be wrapped in KindFilterCatalog")
	assert.IsType(t, &RemoteCatalog{}, kf.Inner())

	// Unserved solution kind returns not-found without dialing 127.0.0.1:1.
	_, err = chain.Resolve(t.Context(), Reference{Name: "starter", Kind: ArtifactKindSolution})
	require.ErrorIs(t, err, ErrArtifactNotFound)

	// BuildRemoteCatalogChain also wraps kind-restricted catalogs, and fails closed
	// when ArtifactKinds contains only unrecognized values.
	badCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{
				Name:          "invalid-kinds",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://127.0.0.1:1/bad",
				Insecure:      true,
				ArtifactKinds: []string{"bogus"},
			},
		},
	}
	remoteChain, err := BuildRemoteCatalogChain(badCfg, nil, logr.Discard())
	require.NoError(t, err)
	_, err = remoteChain.Resolve(t.Context(), Reference{Name: "starter", Kind: ArtifactKindSolution})
	require.ErrorIs(t, err, ErrArtifactNotFound, "unrecognized non-empty artifactKinds must fail closed")

	// BuildRemotePluginFetcher wraps single-plugin-kind catalogs and applies allowlists.
	rpf, err := BuildRemotePluginFetcher(cfg, nil, logr.Discard(),
		WithPerCatalogArtifacts(map[string]PluginPolicy{
			"providers-only": {Plugins: []string{"allowed-prov"}},
		}),
	)
	require.NoError(t, err)
	_, err = rpf.ResolvePlugin(t.Context(), "github", ArtifactKindAuthHandler, "")
	require.ErrorIs(t, err, ErrArtifactNotFound)
	_, err = rpf.ResolvePlugin(t.Context(), "blocked-prov", ArtifactKindProvider, "")
	require.ErrorIs(t, err, ErrArtifactNotFound)

	// SelectSolutionRemoteCatalogs & SelectPluginRemoteCatalogs honor artifactKinds across scopes.
	mixedCfg := &config.Config{
		Settings: config.Settings{
			DefaultCatalog:         "solutions-only",
			DisableOfficialCatalog: false,
		},
		Catalogs: []config.CatalogConfig{
			{
				Name:          "solutions-only",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://registry.example.com/solutions",
				ArtifactKinds: []string{config.CatalogArtifactKindSolution},
			},
			{
				Name:          "providers-only",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://registry.example.com/providers",
				ArtifactKinds: []string{config.CatalogArtifactKindProvider},
			},
			{
				Name: config.CatalogNameOfficial,
				Type: config.CatalogTypeOCI,
				URL:  config.DefaultOfficialCatalogURL,
			},
		},
	}
	assert.Nil(t, SelectSolutionRemoteCatalogs(nil, "", nil, logr.Discard()))
	nilCats, nilExcl := SelectPluginRemoteCatalogs(nil, "", nil, logr.Discard())
	assert.Nil(t, nilCats)
	assert.False(t, nilExcl)

	assert.Len(t, SelectSolutionRemoteCatalogs(mixedCfg, "", nil, logr.Discard()), 1)
	assert.Len(t, SelectSolutionRemoteCatalogs(mixedCfg, "all", nil, logr.Discard()), 2)
	assert.Len(t, SelectSolutionRemoteCatalogs(mixedCfg, "solutions-only", nil, logr.Discard()), 1)
	assert.Empty(t, SelectSolutionRemoteCatalogs(mixedCfg, "providers-only", nil, logr.Discard()))

	defPluginCats, excluded := SelectPluginRemoteCatalogs(mixedCfg, "", nil, logr.Discard())
	assert.False(t, excluded)
	require.Len(t, defPluginCats, 1)
	assert.Equal(t, config.CatalogNameOfficial, defPluginCats[0].Name())

	allPluginCats, excluded := SelectPluginRemoteCatalogs(mixedCfg, "all", nil, logr.Discard())
	assert.False(t, excluded)
	require.Len(t, allPluginCats, 2)

	namedPluginCats, excluded := SelectPluginRemoteCatalogs(mixedCfg, "providers-only", nil, logr.Discard())
	assert.False(t, excluded)
	require.Len(t, namedPluginCats, 1)
	assert.IsType(t, &KindFilterCatalog{}, namedPluginCats[0])

	unknownCats, excluded := SelectPluginRemoteCatalogs(mixedCfg, "nonexistent", nil, logr.Discard())
	assert.False(t, excluded)
	assert.Nil(t, unknownCats)

	exclCats, excluded := SelectPluginRemoteCatalogs(mixedCfg, "solutions-only", nil, logr.Discard())
	assert.True(t, excluded)
	assert.Empty(t, exclCats)

	// DisableOfficialCatalog prevents official from being returned even by name or default.
	disabledOfficialCfg := *mixedCfg
	disabledOfficialCfg.Settings.DefaultCatalog = config.CatalogNameOfficial
	disabledOfficialCfg.Settings.DisableOfficialCatalog = true
	assert.Nil(t, SelectSolutionRemoteCatalogs(&disabledOfficialCfg, "", nil, logr.Discard()))
	assert.Nil(t, SelectSolutionRemoteCatalogs(&disabledOfficialCfg, config.CatalogNameOfficial, nil, logr.Discard()))
	offCats, offExcl := SelectPluginRemoteCatalogs(&disabledOfficialCfg, config.CatalogNameOfficial, nil, logr.Discard())
	assert.Nil(t, offCats)
	assert.False(t, offExcl)
}
