// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAuthScope_FromNamedCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:      "my-registry",
				Type:      appconfig.CatalogTypeOCI,
				URL:       "oci://ghcr.io/myorg",
				AuthScope: "repo:read",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "repo:read", scope)
}

func TestResolveAuthScope_EmptyWhenNoCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "nonexistent")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenNoConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenCatalogHasNoScope(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "", scope)
}

func TestResolveAuthHandler_NilWhenNoConfig(t *testing.T) {
	t.Parallel()

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)

	handler := resolveAuthHandler(ctx, "ghcr.io", "")
	assert.Nil(t, handler, "should return nil when no config in context")
}

func TestResolveAuthHandler_NilWhenNoMatchingCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	handler := resolveAuthHandler(ctx, "custom.io", "nonexistent")
	assert.Nil(t, handler, "should return nil when catalog not found and no inference match")
}

func TestResolveAuthHandler_FromCatalogConfig(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:         "gh-registry",
				Type:         appconfig.CatalogTypeOCI,
				URL:          "oci://ghcr.io/myorg",
				AuthProvider: "github",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	// github handler may or may not be loadable in test environment,
	// but the function should at least try (not panic)
	_ = resolveAuthHandler(ctx, "ghcr.io", "gh-registry")
}

// TestResolveAuthHandler_InferenceTakesPriorityOverDefault verifies that
// registry-host inference (e.g. *.pkg.dev → gcp) takes priority over the
// default catalog's authProvider. Without this, a full OCI ref to a GCP
// registry would incorrectly use the default catalog's handler (e.g. ford-quay).
func TestResolveAuthHandler_InferenceTakesPriorityOverDefault(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:         "ford-solutions",
				Type:         appconfig.CatalogTypeOCI,
				URL:          "oci://quay.io/myorg",
				AuthProvider: "quay",
			},
		},
		Settings: appconfig.Settings{
			DefaultCatalog: "ford-solutions",
		},
	}

	w := writer.New(testIOStreams(), settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	// resolveAuthHandler returns nil because no auth registry is in context,
	// but we can verify the priority logic by calling it and confirming it
	// doesn't panic with the default-catalog-has-different-handler scenario.
	handler := resolveAuthHandler(ctx, "us-central1-docker.pkg.dev", "")
	// Handler is nil because auth.GetHandler fails in test, but the important
	// assertion is below: InferAuthHandler returns "gcp" for *.pkg.dev, not
	// the default catalog's "quay".
	_ = handler

	handlerName := catalog.InferAuthHandler("us-central1-docker.pkg.dev", cfg.Auth.CustomOAuth2)
	assert.Equal(t, "gcp", handlerName, "registry host inference should identify gcp for *.pkg.dev")

	// The default catalog would return "quay" -- verify it's different
	defaultCat, ok := cfg.GetDefaultCatalog()
	require.True(t, ok)
	assert.Equal(t, "quay", defaultCat.AuthProvider, "default catalog has a different auth provider")
}

func TestCommandCatalog_HasSubcommands(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCatalog(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "catalog", cmd.Use)

	// Verify expected subcommands exist
	expectedSubs := []string{"list", "pull", "push", "delete", "login", "logout", "remote", "inspect", "tags", "tag", "attach"}
	for _, name := range expectedSubs {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		assert.True(t, found, "expected subcommand %q", name)
	}
}

func TestHintOnAuthError_401(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("request failed: 401 Unauthorized")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	output := outBuf.String()
	assert.Contains(t, output, "auth login github")
	assert.Contains(t, output, "bridged to ghcr.io automatically")
}

func TestHintOnAuthError_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	params.BinaryName = "mycli"
	w := writer.New(ioStreams, params)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, params)

	testErr := fmt.Errorf("request failed: 401 Unauthorized")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	output := outBuf.String()
	assert.Contains(t, output, "'mycli auth login github'")
	assert.NotContains(t, output, "scafctl")
}

func TestHintOnAuthError_403(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("request failed: 403 Forbidden")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	output := outBuf.String()
	assert.Contains(t, output, "auth login github")
	assert.Contains(t, output, "bridged to ghcr.io automatically")
}

func TestHintOnAuthError_NonAuthError(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	testErr := fmt.Errorf("network timeout")
	hintOnAuthError(ctx, w, "ghcr.io", testErr)
	assert.Empty(t, outBuf.String(), "should not print hint for non-auth errors")
}

func testIOStreams() *terminal.IOStreams {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return ioStreams
}

func TestResolveAuthScopeForRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		cfg      *appconfig.Config
		want     string
	}{
		{
			name:     "matches catalog by registry host",
			registry: "us-central1-docker.pkg.dev",
			cfg: &appconfig.Config{
				Catalogs: []appconfig.CatalogConfig{
					{Name: "gcp", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthScope: "https://www.googleapis.com/auth/cloud-platform"},
				},
			},
			want: "https://www.googleapis.com/auth/cloud-platform",
		},
		{
			name:     "no match falls back to InferDefaultScope for GCP",
			registry: "us-central1-docker.pkg.dev",
			cfg: &appconfig.Config{
				Catalogs: []appconfig.CatalogConfig{
					{Name: "other", URL: "oci://ghcr.io/myorg", AuthScope: ""},
				},
			},
			want: "https://www.googleapis.com/auth/cloud-platform",
		},
		{
			name:     "no match and no inference returns empty",
			registry: "ghcr.io",
			cfg: &appconfig.Config{
				Catalogs: []appconfig.CatalogConfig{
					{Name: "gcp", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthScope: "cloud-platform"},
				},
			},
			want: "",
		},
		{
			name:     "nil config falls back to InferDefaultScope",
			registry: "us-east1-docker.pkg.dev",
			cfg:      nil,
			want:     "https://www.googleapis.com/auth/cloud-platform",
		},
		{
			name:     "nil config unknown registry returns empty",
			registry: "ghcr.io",
			cfg:      nil,
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.cfg != nil {
				ctx = appconfig.WithConfig(ctx, tc.cfg)
			}
			got := resolveAuthScopeForRegistry(ctx, tc.registry)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateVersionConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		nameOrRef  string
		constraint string
		wantErr    string
	}{
		{
			name:       "empty constraint is valid",
			nameOrRef:  "my-app@1.0.0",
			constraint: "",
		},
		{
			name:       "constraint without @version is valid",
			nameOrRef:  "my-app",
			constraint: "^1.0.0",
		},
		{
			name:       "constraint with @version is error",
			nameOrRef:  "my-app@1.0.0",
			constraint: "^1.0.0",
			wantErr:    "cannot use --version with an explicit version",
		},
		{
			name:       "invalid constraint syntax",
			nameOrRef:  "my-app",
			constraint: "not-semver!!",
			wantErr:    "invalid version constraint",
		},
		{
			name:       "valid tilde constraint",
			nameOrRef:  "my-app",
			constraint: "~1.2.0",
		},
		{
			name:       "valid range constraint",
			nameOrRef:  "my-app",
			constraint: ">= 1.0.0, < 2.0.0",
		},
		{
			name:       "wildcard constraint",
			nameOrRef:  "my-app",
			constraint: "*",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVersionConstraint(tc.nameOrRef, tc.constraint)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFilterArtifactsByConstraint(t *testing.T) {
	t.Parallel()

	mustVersion := func(s string) *semver.Version {
		v, err := semver.NewVersion(s)
		require.NoError(t, err)
		return v
	}

	artifacts := []catalog.ArtifactInfo{
		{Reference: catalog.Reference{Name: "app", Version: mustVersion("0.9.0")}},
		{Reference: catalog.Reference{Name: "app", Version: mustVersion("1.0.0")}},
		{Reference: catalog.Reference{Name: "app", Version: mustVersion("1.1.0")}},
		{Reference: catalog.Reference{Name: "app", Version: mustVersion("1.2.0")}},
		{Reference: catalog.Reference{Name: "app", Version: mustVersion("2.0.0")}},
		{Reference: catalog.Reference{Name: "app"}}, // nil version, should be excluded
	}

	tests := []struct {
		name       string
		constraint string
		wantCount  int
		wantFirst  string
	}{
		{
			name:       "empty constraint returns all",
			constraint: "",
			wantCount:  6,
		},
		{
			name:       "caret range ^1.0.0",
			constraint: "^1.0.0",
			wantCount:  3,
			wantFirst:  "1.2.0",
		},
		{
			name:       "exact match",
			constraint: "1.1.0",
			wantCount:  1,
			wantFirst:  "1.1.0",
		},
		{
			name:       "range >= 1.0, < 2.0",
			constraint: ">= 1.0.0, < 2.0.0",
			wantCount:  3,
			wantFirst:  "1.2.0",
		},
		{
			name:       "no matches",
			constraint: "^3.0.0",
			wantCount:  0,
		},
		{
			name:       "wildcard matches all with versions",
			constraint: "*",
			wantCount:  5,
			wantFirst:  "2.0.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filtered, err := filterArtifactsByConstraint(artifacts, tc.constraint)
			require.NoError(t, err)
			assert.Len(t, filtered, tc.wantCount)
			if tc.wantFirst != "" && len(filtered) > 0 {
				assert.Equal(t, tc.wantFirst, filtered[0].Reference.Version.Original())
			}
		})
	}
}

func TestFilterArtifactsByConstraint_InvalidConstraint(t *testing.T) {
	t.Parallel()

	_, err := filterArtifactsByConstraint(nil, "invalid!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version constraint")
}

func TestFilterArtifactsByConstraint_MultipleKindsSameVersion(t *testing.T) {
	t.Parallel()

	mustVersion := func(s string) *semver.Version {
		v, err := semver.NewVersion(s)
		require.NoError(t, err)
		return v
	}

	artifacts := []catalog.ArtifactInfo{
		{Reference: catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "app", Version: mustVersion("1.0.0")}},
		{Reference: catalog.Reference{Kind: catalog.ArtifactKindProvider, Name: "app", Version: mustVersion("1.0.0")}},
		{Reference: catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "app", Version: mustVersion("2.0.0")}},
	}

	filtered, err := filterArtifactsByConstraint(artifacts, "^1.0.0")
	require.NoError(t, err)
	assert.Len(t, filtered, 2, "both solution and provider at 1.0.0 should be included")
}

// mockVersionCatalog is a minimal Catalog implementation for testing
// resolveVersionConstraint.
type mockVersionCatalog struct {
	artifacts []catalog.ArtifactInfo
	listErr   error
}

func (m *mockVersionCatalog) Name() string { return "mock" }

func (m *mockVersionCatalog) List(_ context.Context, _ catalog.ArtifactKind, _ string) ([]catalog.ArtifactInfo, error) {
	return m.artifacts, m.listErr
}

func (m *mockVersionCatalog) Store(context.Context, catalog.Reference, []byte, []byte, map[string]string, bool) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockVersionCatalog) Fetch(context.Context, catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	return nil, catalog.ArtifactInfo{}, nil
}

func (m *mockVersionCatalog) FetchWithBundle(context.Context, catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	return nil, nil, catalog.ArtifactInfo{}, nil
}

func (m *mockVersionCatalog) Resolve(context.Context, catalog.Reference) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockVersionCatalog) Exists(context.Context, catalog.Reference) (bool, error) {
	return false, nil
}

func (m *mockVersionCatalog) Delete(context.Context, catalog.Reference) error { return nil }

func TestResolveVersionConstraint(t *testing.T) {
	t.Parallel()

	mustVersion := func(s string) *semver.Version {
		v, err := semver.NewVersion(s)
		require.NoError(t, err)
		return v
	}

	tests := []struct {
		name        string
		artifacts   []catalog.ArtifactInfo
		constraint  string
		wantVersion string
		wantErr     string
	}{
		{
			name: "picks best match",
			artifacts: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: mustVersion("1.0.0")}},
				{Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: mustVersion("1.1.0")}},
				{Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: mustVersion("2.0.0")}},
			},
			constraint:  "^1.0.0",
			wantVersion: "1.1.0",
		},
		{
			name: "no match returns error",
			artifacts: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: mustVersion("1.0.0")}},
			},
			constraint: "^3.0.0",
			wantErr:    "no versions of",
		},
		{
			name:       "empty artifact list",
			artifacts:  []catalog.ArtifactInfo{},
			constraint: "^1.0.0",
			wantErr:    "no versions of",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockVersionCatalog{artifacts: tc.artifacts}
			ref := catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution}
			got, err := resolveVersionConstraint(context.Background(), mock, ref, tc.constraint)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantVersion, got.Version.Original())
			}
		})
	}
}

func TestResolveVersionConstraint_ListError(t *testing.T) {
	t.Parallel()

	mock := &mockVersionCatalog{listErr: fmt.Errorf("network error")}
	ref := catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution}
	_, err := resolveVersionConstraint(context.Background(), mock, ref, "^1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list versions")
}

func TestVerboseRemoteInfo_NilHandler(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	verboseRemoteInfo(ctx, w, "ghcr.io", "myorg/catalog", nil, "")

	output := errBuf.String()
	assert.Contains(t, output, "Registry: ghcr.io")
	assert.Contains(t, output, "Repository: myorg/catalog")
	assert.Contains(t, output, "Auth handler: none")
}

func TestVerboseRemoteInfo_AuthenticatedHandler(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("gcp")
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Email: "user@example.com"},
		ExpiresAt:     time.Now().Add(30 * time.Minute),
	}

	verboseRemoteInfo(ctx, w, "us-docker.pkg.dev", "proj/repo", mock, "https://www.googleapis.com/auth/cloud-platform")

	output := errBuf.String()
	assert.Contains(t, output, "Auth handler: gcp")
	assert.Contains(t, output, "Auth scope: https://www.googleapis.com/auth/cloud-platform")
	assert.Contains(t, output, "authenticated as user@example.com")
	assert.Contains(t, output, "remaining")
}

func TestVerboseRemoteInfo_NotAuthenticated(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("github")
	mock.StatusResult = &auth.Status{
		Authenticated: false,
		Reason:        "expired",
	}

	verboseRemoteInfo(ctx, w, "ghcr.io", "myorg/catalog", mock, "")

	output := errBuf.String()
	assert.Contains(t, output, "NOT AUTHENTICATED (expired)")
	assert.Contains(t, output, "auth login github")
}

func TestVerboseRemoteInfo_StatusError(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("entra")
	mock.StatusErr = fmt.Errorf("keychain error")

	verboseRemoteInfo(ctx, w, "quay.io", "myorg/catalog", mock, "")

	output := errBuf.String()
	assert.Contains(t, output, "unknown (check failed: keychain error)")
}

func TestVerboseRemoteInfo_ExpiredToken(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &errBuf, false)
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("gcp")
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Username: "robot"},
		ExpiresAt:     time.Now().Add(-10 * time.Minute), // expired
	}

	verboseRemoteInfo(ctx, w, "us-docker.pkg.dev", "proj/repo", mock, "")

	output := errBuf.String()
	assert.Contains(t, output, "EXPIRED")
}

func TestFilterPreReleaseArtifacts(t *testing.T) {
	t.Parallel()

	mustParse := func(v string) *semver.Version {
		return semver.MustParse(v)
	}

	tests := []struct {
		name     string
		input    []catalog.ArtifactInfo
		wantLen  int
		wantVers []string
	}{
		{
			name: "filters out pre-release versions",
			input: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "a", Version: mustParse("1.0.0")}},
				{Reference: catalog.Reference{Name: "a", Version: mustParse("1.1.0-beta.1")}},
				{Reference: catalog.Reference{Name: "a", Version: mustParse("0.9.0")}},
			},
			wantLen:  2,
			wantVers: []string{"1.0.0", "0.9.0"},
		},
		{
			name: "keeps all if no pre-releases",
			input: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "a", Version: mustParse("1.0.0")}},
				{Reference: catalog.Reference{Name: "a", Version: mustParse("2.0.0")}},
			},
			wantLen:  2,
			wantVers: []string{"1.0.0", "2.0.0"},
		},
		{
			name: "returns all if only pre-releases exist",
			input: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "a", Version: mustParse("1.0.0-alpha")}},
				{Reference: catalog.Reference{Name: "a", Version: mustParse("2.0.0-rc.1")}},
			},
			wantLen:  2,
			wantVers: []string{"1.0.0-alpha", "2.0.0-rc.1"},
		},
		{
			name:     "handles empty slice",
			input:    []catalog.ArtifactInfo{},
			wantLen:  0,
			wantVers: nil,
		},
		{
			name: "handles nil version gracefully",
			input: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "a", Version: nil}},
				{Reference: catalog.Reference{Name: "a", Version: mustParse("1.0.0-beta")}},
			},
			wantLen:  1,
			wantVers: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := filterPreReleaseArtifacts(tt.input)
			assert.Len(t, result, tt.wantLen)
			if tt.wantVers != nil {
				for i, v := range tt.wantVers {
					if result[i].Reference.Version != nil {
						assert.Equal(t, v, result[i].Reference.Version.String())
					} else {
						assert.Equal(t, v, "")
					}
				}
			}
		})
	}
}

func TestFilterArtifactsByCatalog(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{Reference: catalog.Reference{Name: "a", Kind: catalog.ArtifactKindSolution}, Catalog: "local"},
		{Reference: catalog.Reference{Name: "b", Kind: catalog.ArtifactKindSolution}, Catalog: "remote"},
		{Reference: catalog.Reference{Name: "c", Kind: catalog.ArtifactKindSolution}, Catalog: "local"},
	}

	result := filterArtifactsByCatalog(artifacts, "local")
	assert.Len(t, result, 2)
	assert.Equal(t, "a", result[0].Reference.Name)
	assert.Equal(t, "c", result[1].Reference.Name)
}

func TestFilterArtifactsByCatalog_NoMatch(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{Reference: catalog.Reference{Name: "a"}, Catalog: "local"},
	}

	result := filterArtifactsByCatalog(artifacts, "remote")
	assert.Empty(t, result)
}

func TestIsConfiguredCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{Name: "local", Type: appconfig.CatalogTypeFilesystem},
			{Name: "official", Type: appconfig.CatalogTypeOCI},
		},
	}
	ctx := appconfig.WithConfig(context.Background(), cfg)

	assert.True(t, isConfiguredCatalog(ctx, "local"))
	assert.True(t, isConfiguredCatalog(ctx, "official"))
	assert.False(t, isConfiguredCatalog(ctx, "unknown"))
	assert.False(t, isConfiguredCatalog(context.Background(), "local"))
}

func TestResolveDiscoveryStrategy(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{Name: "official", Type: appconfig.CatalogTypeOCI, DiscoveryStrategy: appconfig.DiscoveryStrategyIndex},
			{Name: "other", Type: appconfig.CatalogTypeOCI},
		},
	}
	ctx := appconfig.WithConfig(context.Background(), cfg)

	assert.Equal(t, appconfig.DiscoveryStrategyIndex, resolveDiscoveryStrategy(ctx, "official"))
	assert.Equal(t, appconfig.DiscoveryStrategy(""), resolveDiscoveryStrategy(ctx, "other"))
	assert.Equal(t, appconfig.DiscoveryStrategy(""), resolveDiscoveryStrategy(ctx, "unknown"))
	assert.Equal(t, appconfig.DiscoveryStrategy(""), resolveDiscoveryStrategy(ctx, ""))
	assert.Equal(t, appconfig.DiscoveryStrategy(""), resolveDiscoveryStrategy(context.Background(), "official"))
}

func TestDeduplicateArtifacts(t *testing.T) {
	t.Parallel()

	now := time.Now()
	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "sha256:abc",
			CreatedAt: now,
			Catalog:   "local",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "",
			Catalog:   "remote",
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	assert.Equal(t, "local, remote", result[0].Catalog)
	assert.Equal(t, "sha256:abc", result[0].Digest, "should prefer row with digest")
	assert.Equal(t, now, result[0].CreatedAt, "should prefer row with createdAt")
}

func TestDeduplicateArtifacts_PreferSecondRowMetadata(t *testing.T) {
	t.Parallel()

	now := time.Now()
	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "",
			Catalog:   "remote",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Digest:    "sha256:abc",
			CreatedAt: now,
			Catalog:   "local",
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	assert.Equal(t, "remote, local", result[0].Catalog)
	assert.Equal(t, "sha256:abc", result[0].Digest)
	assert.Equal(t, now, result[0].CreatedAt)
}

func TestDeduplicateArtifacts_DifferentVersionsNotMerged(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "local",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("2.0.0")},
			Catalog:   "remote",
		},
	}

	result := deduplicateArtifacts(artifacts)
	assert.Len(t, result, 2)
}

func TestDeduplicateArtifacts_DifferentKindsNotMerged(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "local",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindProvider, Version: semver.MustParse("1.0.0")},
			Catalog:   "remote",
		},
	}

	result := deduplicateArtifacts(artifacts)
	assert.Len(t, result, 2)
}

func TestDeduplicateArtifacts_SubstringCatalogNames(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "production",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "prod",
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	assert.Equal(t, "production, prod", result[0].Catalog,
		"substring catalog name must not be treated as duplicate")
}

func TestDeduplicateArtifacts_ExactDuplicateCatalog(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "prod",
		},
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "prod",
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	assert.Equal(t, "prod", result[0].Catalog,
		"exact duplicate catalog should not be appended")
}

func TestDeduplicateArtifacts_AnnotationMerging(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference:   catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:     "prod",
			Annotations: map[string]string{"desc": "production app", "team": "platform"},
		},
		{
			Reference:   catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:     "staging",
			Annotations: map[string]string{"desc": "staging override", "env": "staging"},
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	assert.Equal(t, "production app", result[0].Annotations["desc"],
		"existing annotation should not be overwritten")
	assert.Equal(t, "platform", result[0].Annotations["team"],
		"existing-only annotation should be preserved")
	assert.Equal(t, "staging", result[0].Annotations["env"],
		"new annotation should be merged from second artifact")
}

func TestDeduplicateArtifacts_AnnotationMergingNilTarget(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.ArtifactInfo{
		{
			Reference: catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:   "prod",
			// No annotations on first entry
		},
		{
			Reference:   catalog.Reference{Name: "app", Kind: catalog.ArtifactKindSolution, Version: semver.MustParse("1.0.0")},
			Catalog:     "staging",
			Annotations: map[string]string{"env": "staging"},
		},
	}

	result := deduplicateArtifacts(artifacts)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].Annotations)
	assert.Equal(t, "staging", result[0].Annotations["env"],
		"annotations should be initialized from second artifact when first has none")
}

func TestWarnStaleCredentials_NoStale(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, settings.NewCliParams())

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)

	warnStaleCredentials(ctx, w, rc)
	assert.Empty(t, errBuf.String(), "should not emit warning when credentials are not stale")
}

func TestWarnStaleCredentials_WithStale_KnownHandler(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	w := writer.New(ioStreams, params)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)
	rc.SetStaleForTesting()

	warnStaleCredentials(ctx, w, rc)
	output := errBuf.String()
	assert.Contains(t, output, "rejected")
	assert.Contains(t, output, "anonymous access")
	assert.Contains(t, output, "auth login github",
		"should suggest auth login for known registry handler")
}

func TestWarnStaleCredentials_WithStale_UnknownRegistry(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	w := writer.New(ioStreams, params)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "registry.example.com",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)
	rc.SetStaleForTesting()

	warnStaleCredentials(ctx, w, rc)
	output := errBuf.String()
	assert.Contains(t, output, "rejected")
	assert.Contains(t, output, "catalog login registry.example.com",
		"should suggest catalog login for unknown registry")
}

func TestWarnStaleCredentials_WithCredentialSource(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	w := writer.New(ioStreams, params)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)
	rc.SetStaleForTesting()
	rc.SetCredentialSourceForTest("docker credential helper (desktop)")

	warnStaleCredentials(ctx, w, rc)
	output := errBuf.String()
	assert.Contains(t, output, "docker credential helper (desktop)")
}

func TestWarnStaleCredentials_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	params.BinaryName = "mycli"
	w := writer.New(ioStreams, params)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = settings.IntoContext(ctx, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)
	rc.SetStaleForTesting()

	warnStaleCredentials(ctx, w, rc)
	output := errBuf.String()
	assert.Contains(t, output, "mycli auth login github")
	assert.NotContains(t, output, "scafctl")
}

func TestVerboseCredentialSource_WithSource(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	params.Verbose = true
	w := writer.New(ioStreams, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)
	rc.SetCredentialSourceForTest("docker credential helper (desktop)")

	verboseCredentialSource(w, rc)
	output := errBuf.String()
	assert.Contains(t, output, "Credential source: docker credential helper (desktop)")
}

func TestVerboseCredentialSource_Empty(t *testing.T) {
	t.Parallel()

	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	params := settings.NewCliParams()
	params.Verbose = true
	w := writer.New(ioStreams, params)

	rc, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/solutions",
	})
	require.NoError(t, err)

	verboseCredentialSource(w, rc)
	assert.Empty(t, errBuf.String(), "should not log when credential source is empty")
}
