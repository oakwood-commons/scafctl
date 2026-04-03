// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeCredentialStore_SetAndGet(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user", "pass", false)
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "user", cred.Username)
	assert.Equal(t, "pass", cred.Password)
	assert.False(t, cred.ContainerAuth)
}

func TestNativeCredentialStore_SetWithContainerAuth(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user", "pass", true)
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.True(t, cred.ContainerAuth)
}

func TestNativeCredentialStore_GetNotFound(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	assert.Nil(t, cred)
}

func TestNativeCredentialStore_Delete(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user", "pass", false)
	require.NoError(t, err)

	err = store.DeleteCredential("ghcr.io")
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	assert.Nil(t, cred)
}

func TestNativeCredentialStore_DeleteNonExistent(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.DeleteCredential("nonexistent.io")
	require.NoError(t, err)
}

func TestNativeCredentialStore_ListCredentials(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user1", "pass1", false)
	require.NoError(t, err)

	err = store.SetCredential("quay.io", "user2", "pass2", false)
	require.NoError(t, err)

	creds, err := store.ListCredentials()
	require.NoError(t, err)
	assert.Len(t, creds, 2)
	assert.Equal(t, "user1", creds["ghcr.io"])
	assert.Equal(t, "user2", creds["quay.io"])
}

func TestNativeCredentialStore_ListCredentials_Empty(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	creds, err := store.ListCredentials()
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestNativeCredentialStore_DeleteAll(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user1", "pass1", false)
	require.NoError(t, err)

	err = store.SetCredential("quay.io", "user2", "pass2", false)
	require.NoError(t, err)

	err = store.DeleteAll()
	require.NoError(t, err)

	creds, err := store.ListCredentials()
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestNativeCredentialStore_Overwrite(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	err := store.SetCredential("ghcr.io", "user1", "pass1", false)
	require.NoError(t, err)

	err = store.SetCredential("ghcr.io", "user2", "pass2", true)
	require.NoError(t, err)

	cred, err := store.GetCredential("ghcr.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "user2", cred.Username)
	assert.Equal(t, "pass2", cred.Password)
	assert.True(t, cred.ContainerAuth)
}

func TestNativeCredentialStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registries.json")
	store := NewNativeCredentialStoreWithPath(path)

	err := store.SetCredential("ghcr.io", "user", "pass", false)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(nativeCredentialFilePermissions), info.Mode().Perm())
}

func TestNativeCredentialStore_ConcurrentAccess(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			host := "registry" + string(rune('0'+idx)) + ".io"
			_ = store.SetCredential(host, "user", "pass", false)
			_, _ = store.GetCredential(host)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestNativeCredentialStore_Path(t *testing.T) {
	path := "/custom/path/registries.json"
	store := NewNativeCredentialStoreWithPath(path)
	assert.Equal(t, path, store.Path())
}

func TestNativeCredentialStore_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registries.json")
	err := os.WriteFile(path, []byte("not json"), 0o600)
	require.NoError(t, err)

	store := NewNativeCredentialStoreWithPath(path)
	_, err = store.GetCredential("ghcr.io")
	assert.Error(t, err)
}

func TestNativeCredentialStore_MultipleRegistries(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	registries := []struct {
		host     string
		username string
		password string
	}{
		{"ghcr.io", "github-user", "github-token"},
		{"quay.io", "quay-user", "quay-token"},
		{"us-docker.pkg.dev", "oauth2accesstoken", "gcp-token"},
		{"myacr.azurecr.io", "00000000-0000-0000-0000-000000000000", "entra-token"},
	}

	for _, r := range registries {
		err := store.SetCredential(r.host, r.username, r.password, false)
		require.NoError(t, err)
	}

	for _, r := range registries {
		cred, err := store.GetCredential(r.host)
		require.NoError(t, err)
		require.NotNil(t, cred)
		assert.Equal(t, r.username, cred.Username)
		assert.Equal(t, r.password, cred.Password)
	}
}

func TestEncodeBasicAuth(t *testing.T) {
	encoded := encodeBasicAuth("user", "pass")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	assert.Equal(t, "user:pass", string(decoded))
}

func TestDetectContainerAuthFile(t *testing.T) {
	// When REGISTRY_AUTH_FILE is set, it takes priority
	t.Setenv("REGISTRY_AUTH_FILE", "/custom/auth.json")
	path, err := detectContainerAuthFile()
	require.NoError(t, err)
	assert.Equal(t, "/custom/auth.json", path)
}

func TestWriteContainerAuthEntry(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	err := writeContainerAuthEntry(authFile, "ghcr.io", "user", "pass")
	require.NoError(t, err)

	data, err := os.ReadFile(authFile)
	require.NoError(t, err)

	var cfg map[string]json.RawMessage
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)

	var auths map[string]containerAuthEntry
	err = json.Unmarshal(cfg["auths"], &auths)
	require.NoError(t, err)

	entry, ok := auths["ghcr.io"]
	require.True(t, ok)

	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	require.NoError(t, err)
	assert.Equal(t, "user:pass", string(decoded))
}

func TestWriteContainerAuthEntry_PreservesExistingFields(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "config.json")

	existing := `{"auths":{"quay.io":{"auth":"cXVheTpwYXNz"}},"credsStore":"osxkeychain"}`
	err := os.WriteFile(authFile, []byte(existing), 0o600)
	require.NoError(t, err)

	err = writeContainerAuthEntry(authFile, "ghcr.io", "user", "pass")
	require.NoError(t, err)

	data, err := os.ReadFile(authFile)
	require.NoError(t, err)

	var cfg map[string]json.RawMessage
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)

	// credsStore should be preserved
	assert.Contains(t, string(cfg["credsStore"]), "osxkeychain")

	var auths map[string]containerAuthEntry
	err = json.Unmarshal(cfg["auths"], &auths)
	require.NoError(t, err)

	// Both entries should exist
	assert.Contains(t, auths, "ghcr.io")
	assert.Contains(t, auths, "quay.io")
}

func TestDeleteContainerAuthEntry(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")

	err := writeContainerAuthEntry(authFile, "ghcr.io", "user", "pass")
	require.NoError(t, err)

	err = writeContainerAuthEntry(authFile, "quay.io", "user2", "pass2")
	require.NoError(t, err)

	err = deleteContainerAuthEntry(authFile, "ghcr.io")
	require.NoError(t, err)

	data, err := os.ReadFile(authFile)
	require.NoError(t, err)

	var cfg map[string]json.RawMessage
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)

	var auths map[string]json.RawMessage
	err = json.Unmarshal(cfg["auths"], &auths)
	require.NoError(t, err)

	assert.NotContains(t, auths, "ghcr.io")
	assert.Contains(t, auths, "quay.io")
}

func TestDeleteContainerAuthEntry_NonExistentFile(t *testing.T) {
	err := deleteContainerAuthEntry(filepath.Join(t.TempDir(), "nonexistent.json"), "ghcr.io")
	require.NoError(t, err)
}

// Benchmarks

func BenchmarkNativeCredentialStore_Get(b *testing.B) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(b.TempDir(), "registries.json"))
	_ = store.SetCredential("ghcr.io", "user", "pass", false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.GetCredential("ghcr.io")
	}
}

func BenchmarkNativeCredentialStore_Set(b *testing.B) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(b.TempDir(), "registries.json"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.SetCredential("ghcr.io", "user", "pass", false)
	}
}

func BenchmarkNativeCredentialStore_List(b *testing.B) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(b.TempDir(), "registries.json"))
	for i := 0; i < 10; i++ {
		_ = store.SetCredential("registry"+string(rune('0'+i))+".io", "user", "pass", false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.ListCredentials()
	}
}

func TestNativeCredentialStore_ListCredentialEntries(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))
	require.NoError(t, store.SetCredential("ghcr.io", "user1", "pass1", true))
	require.NoError(t, store.SetCredential("quay.io", "user2", "pass2", false))

	entries, err := store.ListCredentialEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	ghcr := entries["ghcr.io"]
	assert.Equal(t, "user1", ghcr.Username)
	assert.Equal(t, "pass1", ghcr.Password)
	assert.True(t, ghcr.ContainerAuth)

	quay := entries["quay.io"]
	assert.Equal(t, "user2", quay.Username)
	assert.Equal(t, "pass2", quay.Password)
	assert.False(t, quay.ContainerAuth)
}

func TestNativeCredentialStore_ListCredentialEntries_Empty(t *testing.T) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(t.TempDir(), "registries.json"))

	entries, err := store.ListCredentialEntries()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func BenchmarkNativeCredentialStore_ListEntries(b *testing.B) {
	store := NewNativeCredentialStoreWithPath(filepath.Join(b.TempDir(), "registries.json"))
	for i := 0; i < 10; i++ {
		_ = store.SetCredential("registry"+string(rune('0'+i))+".io", "user", "pass", false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.ListCredentialEntries()
	}
}
