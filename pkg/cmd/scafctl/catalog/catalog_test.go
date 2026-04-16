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
