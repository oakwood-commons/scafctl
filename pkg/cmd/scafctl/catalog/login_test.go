// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "login <registry>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandLogin_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name         string
		defaultValue string
	}{
		{"auth-provider", ""},
		{"scope", ""},
		{"username", ""},
		{"password-stdin", "false"},
		{"password-env", ""},
		{"write-registry-auth", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defaultValue, f.DefValue)
		})
	}
}

func TestCommandLogin_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestReadPassword_BothStdinAndEnv(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordStdin: true,
		PasswordEnv:   "SOME_VAR",
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestReadPassword_NeitherStdinNorEnv(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordStdin: false,
		PasswordEnv:   "",
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--password-stdin or --password-env is required")
}

func TestReadPassword_EmptyEnvVar(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordEnv: "SCAFCTL_TEST_EMPTY_VAR_" + t.Name(),
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty or not set")
}

func TestReadPassword_FromEnvVar(t *testing.T) {
	envKey := "SCAFCTL_TEST_PASSWORD_" + t.Name()
	t.Setenv(envKey, "my-secret-token")

	opts := &LoginOptions{
		PasswordEnv: envKey,
	}
	password, err := readPassword(opts)
	require.NoError(t, err)
	assert.Equal(t, "my-secret-token", password)
}

func BenchmarkCommandLogin(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for b.Loop() {
		_ = CommandLogin(cliParams, ioStreams, "scafctl/catalog")
	}
}

// TestRunCatalogLogin_DirectCredentials tests the direct credential path via runCatalogLogin.
func TestRunCatalogLogin_DirectCredentials(t *testing.T) {
	envKey := "SCAFCTL_TEST_CATALOG_PASS_" + t.Name()
	t.Setenv(envKey, "mypassword")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := newCatalogTestCtx(t)

	opts := &LoginOptions{
		Registry:    "ghcr.io",
		Username:    "myuser",
		PasswordEnv: envKey,
	}

	err := runCatalogLogin(ctx, opts)
	require.NoError(t, err)

	// Verify credential was stored. Use the same secrets backend to retrieve it.
	ss, err := secrets.New()
	require.NoError(t, err)
	store := catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "myuser", cred.Username)
	assert.Equal(t, "mypassword", cred.Password)
}

// TestRunCatalogLogin_NoUsernameNoHandler tests runCatalogLogin when no
// username and no auto-detectable handler exist.
func TestRunCatalogLogin_NoUsernameNoHandler(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	opts := &LoginOptions{
		Registry: "private.example.io",
	}

	err := runCatalogLogin(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handler found")
}

// TestRunDirectCredentialLogin_Success tests successful direct credential login.
func TestRunDirectCredentialLogin_Success(t *testing.T) {
	envKey := "SCAFCTL_TEST_DIRECT_PASS_" + t.Name()
	t.Setenv(envKey, "secrettoken")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := newCatalogTestCtx(t)
	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:    "quay.io",
		Username:    "robot+deployer",
		PasswordEnv: envKey,
	}

	err := runDirectCredentialLogin(ctx, w, opts)
	require.NoError(t, err)

	store := catalog.NewNativeCredentialStore()
	cred, err := store.GetCredential("quay.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "robot+deployer", cred.Username)
}

// TestRunDirectCredentialLogin_BadPasswordConfig tests error when password options are misconfigured.
func TestRunDirectCredentialLogin_BadPasswordConfig(t *testing.T) {
	ctx := newCatalogTestCtx(t)
	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry: "quay.io",
		Username: "user",
		// Neither PasswordStdin nor PasswordEnv set
	}

	err := runDirectCredentialLogin(ctx, w, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--password-stdin or --password-env is required")
}

// TestRunAuthHandlerLogin_NoHandlerInferred tests the error path when no handler
// can be inferred for the registry and none is explicitly provided.
func TestRunAuthHandlerLogin_NoHandlerInferred(t *testing.T) {
	ctx := newCatalogTestCtx(t)
	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:     "private.unknown-vendor.io",
		AuthProvider: "",
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handler found")
}

// TestRunAuthHandlerLogin_ExplicitHandlerNotRegistered tests the error path when a
// handler name is provided but the handler is not registered in the auth registry.
func TestRunAuthHandlerLogin_ExplicitHandlerNotRegistered(t *testing.T) {
	ctx := newCatalogTestCtx(t)
	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:     "quay.io",
		AuthProvider: "quay",
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.Error(t, err)
	// Should report that the auth handler is not available
	assert.Contains(t, err.Error(), "auth handler")
}

// TestRunAuthHandlerLogin_WithMockHandler tests successful auth handler login using
// an auth handler that is registered in the context via the catalog.
func TestRunAuthHandlerLogin_WithMockHandler(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := newCatalogTestCtx(t)

	// Register a mock handler in the auth registry
	mock := auth.NewMockHandler("quay")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "fake-access-token",
		TokenType:   "Bearer",
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:     "quay.io",
		AuthProvider: "quay",
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.NoError(t, err)

	// Verify credential stored
	store := catalog.NewNativeCredentialStore()
	cred, err := store.GetCredential("quay.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
}

// TestRunAuthHandlerLogin_InferredRegistryGHCR tests auto-detection of the github handler
// for ghcr.io. The handler is registered but returns an error when fetching the token.
func TestRunAuthHandlerLogin_InferredRegistryGHCR(t *testing.T) {
	ctx := newCatalogTestCtx(t)

	mock := auth.NewMockHandler("github")
	mock.GetTokenErr = fmt.Errorf("token expired")

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry: "ghcr.io",
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to bridge auth to registry")
}

func TestRunAuthHandlerLogin_ScopeAutoDetectedFromConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := newCatalogTestCtx(t)

	// Add catalog config with authScope to context
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:      "gcp-catalog",
				Type:      "oci",
				URL:       "oci://us-docker.pkg.dev/my-project/scafctl",
				AuthScope: "https://www.googleapis.com/auth/cloud-platform",
			},
		},
	}
	ctx = config.WithConfig(ctx, cfg)

	mock := auth.NewMockHandler("gcp")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "fake-gcp-token",
		TokenType:   "Bearer",
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:     "us-docker.pkg.dev",
		AuthProvider: "gcp",
		// Scope intentionally empty -- should be auto-detected from config
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.NoError(t, err)

	// Verify the scope was auto-detected and passed to the handler
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", mock.GetTokenCalls[0].Scope)
}

func TestRunAuthHandlerLogin_ExplicitScopeOverridesConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx := newCatalogTestCtx(t)

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:      "gcp-catalog",
				Type:      "oci",
				URL:       "oci://us-docker.pkg.dev/my-project/scafctl",
				AuthScope: "https://www.googleapis.com/auth/cloud-platform",
			},
		},
	}
	ctx = config.WithConfig(ctx, cfg)

	mock := auth.NewMockHandler("gcp")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "fake-gcp-token",
		TokenType:   "Bearer",
	}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	w := writerFromCtx(ctx)

	opts := &LoginOptions{
		Registry:     "us-docker.pkg.dev",
		AuthProvider: "gcp",
		Scope:        "https://custom.scope/override",
	}

	err := runAuthHandlerLogin(ctx, w, opts)
	require.NoError(t, err)

	// Explicit --scope should take precedence over config authScope
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://custom.scope/override", mock.GetTokenCalls[0].Scope)
}
