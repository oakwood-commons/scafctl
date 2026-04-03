// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCredentialStore(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestNormalizeRegistryHost_Auth(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"docker.io", "https://index.docker.io/v1/"},
		{"registry-1.docker.io", "https://index.docker.io/v1/"},
		{"index.docker.io", "https://index.docker.io/v1/"},
		{"ghcr.io", "ghcr.io"},
		{"myregistry.example.com", "myregistry.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeRegistryHost(tt.input))
		})
	}
}

func TestCredentialFromEnv_Empty(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	cred := store.credentialFromEnv()
	assert.Empty(t, cred.Username)
}

func TestCredentialFromEnv_Set(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "testuser")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "testpass")

	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	cred := store.credentialFromEnv()
	assert.Equal(t, "testuser", cred.Username)
	assert.Equal(t, "testpass", cred.Password)
}

func TestCredentialFromAuthEntry_IdentityToken(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	entry := dockerAuthEntry{IdentityToken: "my-token"}
	cred, err := store.credentialFromAuthEntry(entry)
	require.NoError(t, err)
	assert.Equal(t, "my-token", cred.RefreshToken)
}

func TestCredentialFromAuthEntry_ExplicitUsernamePassword(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	entry := dockerAuthEntry{Username: "user", Password: "pass"}
	cred, err := store.credentialFromAuthEntry(entry)
	require.NoError(t, err)
	assert.Equal(t, "user", cred.Username)
	assert.Equal(t, "pass", cred.Password)
}

func TestCredentialFromAuthEntry_Base64Auth(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	encoded := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	entry := dockerAuthEntry{Auth: encoded}
	cred, err := store.credentialFromAuthEntry(entry)
	require.NoError(t, err)
	assert.Equal(t, "alice", cred.Username)
	assert.Equal(t, "secret", cred.Password)
}

func TestCredentialFromAuthEntry_InvalidBase64(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	entry := dockerAuthEntry{Auth: "!!!not-base64!!!"}
	_, err = store.credentialFromAuthEntry(entry)
	require.Error(t, err)
}

func TestCredentialFromAuthEntry_NoCredentials(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	entry := dockerAuthEntry{}
	_, err = store.credentialFromAuthEntry(entry)
	require.Error(t, err)
}

func TestCredentialStore_Credential_AnonymousWhenNoConfig(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	// Point DOCKER_CONFIG to an empty temp dir so no real config is loaded
	emptyDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", emptyDir)

	// Create store directly with no config loaded
	store := &CredentialStore{
		configPath: "",
		config:     nil,
		logger:     logr.Discard(),
	}

	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Empty(t, cred.Username)
}

func TestCredentialStore_Credential_FromEnv(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "envuser")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "envpass")

	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Equal(t, "envuser", cred.Username)
}

func TestCredentialStore_CredentialFunc(t *testing.T) {
	store, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	fn := store.CredentialFunc()
	assert.NotNil(t, fn)
}

func TestCredentialStore_Credential_StaticAuthEntry(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	store := &CredentialStore{
		logger: logr.Discard(),
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{
				"ghcr.io": {Username: "staticuser", Password: "staticpass"},
			},
		},
	}

	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Equal(t, "staticuser", cred.Username)
	assert.Equal(t, "staticpass", cred.Password)
}

func TestCredentialStore_Credential_StaticAuthEntry_WithScheme(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	store := &CredentialStore{
		logger: logr.Discard(),
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{
				"https://myregistry.example.com": {Username: "schemeuser", Password: "schemepass"},
			},
		},
	}

	cred, err := store.Credential(context.Background(), "myregistry.example.com")
	require.NoError(t, err)
	assert.Equal(t, "schemeuser", cred.Username)
}

func TestCredentialStore_Credential_NotFound(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	store := &CredentialStore{
		logger: logr.Discard(),
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{},
		},
	}

	cred, err := store.Credential(context.Background(), "notshareable.example.com")
	require.NoError(t, err)
	assert.Empty(t, cred.Username)
}

func TestCredentialStore_Credential_FallbackToNativeStore(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	// Set up native credential store with a credential
	nativeStore := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	require.NoError(t, nativeStore.SetCredential("ghcr.io", "nativeuser", "nativepass", ""))

	store := &CredentialStore{
		logger:      logr.Discard(),
		nativeStore: nativeStore,
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{},
		},
	}

	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Equal(t, "nativeuser", cred.Username)
	assert.Equal(t, "nativepass", cred.Password)
}

func TestCredentialStore_Credential_DockerAuthTakesPriority(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	// Set up native credential store
	nativeStore := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	require.NoError(t, nativeStore.SetCredential("ghcr.io", "nativeuser", "nativepass", ""))

	store := &CredentialStore{
		logger:      logr.Discard(),
		nativeStore: nativeStore,
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{
				"ghcr.io": {Username: "dockeruser", Password: "dockerpass"},
			},
		},
	}

	// Docker auth should take priority over native store
	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Equal(t, "dockeruser", cred.Username)
	assert.Equal(t, "dockerpass", cred.Password)
}

func TestCredentialStore_Credential_NativeStoreFallbackWhenNoDockerConfig(t *testing.T) {
	t.Setenv("SCAFCTL_REGISTRY_USERNAME", "")
	t.Setenv("SCAFCTL_REGISTRY_PASSWORD", "")

	// Set up native credential store
	nativeStore := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	require.NoError(t, nativeStore.SetCredential("ghcr.io", "nativeuser", "nativepass", ""))

	// No Docker config at all
	store := &CredentialStore{
		logger:      logr.Discard(),
		nativeStore: nativeStore,
		config:      nil,
	}

	cred, err := store.Credential(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.Equal(t, "nativeuser", cred.Username)
	assert.Equal(t, "nativepass", cred.Password)
}

func TestFindDockerConfig_DockerConfigEnv(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0o600))
	t.Setenv("DOCKER_CONFIG", tmpDir)

	result := findDockerConfig()
	assert.Equal(t, configPath, result)
}

func TestFindDockerConfig_DockerConfigEnv_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", tmpDir)
	// No config.json in the dir, so should fall through to other paths.
	result := findDockerConfig()
	// Result may or may not be empty depending on machine state; just ensure no panic.
	_ = result
}

func TestFindDockerConfig_XDGRuntimeDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Clear DOCKER_CONFIG to ensure we fall through to XDG check
	t.Setenv("DOCKER_CONFIG", "")
	// Create the podman auth file
	podmanDir := tmpDir + "/containers"
	require.NoError(t, os.MkdirAll(podmanDir, 0o700))
	authPath := podmanDir + "/auth.json"
	require.NoError(t, os.WriteFile(authPath, []byte(`{}`), 0o600))
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	result := findDockerConfig()
	// The result depends on whether ~/.docker/config.json exists; if it does,
	// that takes precedence. We just verify no panic.
	_ = result
}
