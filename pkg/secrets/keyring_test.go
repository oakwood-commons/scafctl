// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockKeyringForTest is an in-memory keyring implementation for testing.
type mockKeyringForTest struct {
	data      map[string]string
	getErr    error
	setErr    error
	deleteErr error
}

func newMockKeyringForTest() *mockKeyringForTest {
	return &mockKeyringForTest{
		data: make(map[string]string),
	}
}

func (m *mockKeyringForTest) Get(service, account string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	key := service + ":" + account
	val, ok := m.data[key]
	if !ok {
		return "", ErrKeyNotFound
	}
	return val, nil
}

func (m *mockKeyringForTest) Set(service, account, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	key := service + ":" + account
	m.data[key] = value
	return nil
}

func (m *mockKeyringForTest) Delete(service, account string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	key := service + ":" + account
	delete(m.data, key)
	return nil
}

func TestMockKeyring_CRUD(t *testing.T) {
	kr := newMockKeyringForTest()

	// Test Get on non-existent key
	_, err := kr.Get("service", "account")
	assert.ErrorIs(t, err, ErrKeyNotFound)

	// Test Set
	err = kr.Set("service", "account", "secret-value")
	require.NoError(t, err)

	// Test Get after Set
	val, err := kr.Get("service", "account")
	require.NoError(t, err)
	assert.Equal(t, "secret-value", val)

	// Test overwrite
	err = kr.Set("service", "account", "new-value")
	require.NoError(t, err)

	val, err = kr.Get("service", "account")
	require.NoError(t, err)
	assert.Equal(t, "new-value", val)

	// Test Delete
	err = kr.Delete("service", "account")
	require.NoError(t, err)

	// Test Get after Delete
	_, err = kr.Get("service", "account")
	assert.ErrorIs(t, err, ErrKeyNotFound)

	// Test Delete non-existent (should not error in mock)
	err = kr.Delete("service", "nonexistent")
	require.NoError(t, err)
}

func TestEnvKeyring_Get(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		service  string
		account  string
		wantVal  string
		wantErr  error
	}{
		{
			name:     "master key found",
			envValue: "base64-encoded-key",
			service:  KeyringService,
			account:  KeyringMasterKeyAccount,
			wantVal:  "base64-encoded-key",
			wantErr:  nil,
		},
		{
			name:     "master key not set",
			envValue: "",
			service:  KeyringService,
			account:  KeyringMasterKeyAccount,
			wantVal:  "",
			wantErr:  ErrKeyNotFound,
		},
		{
			name:     "wrong service",
			envValue: "base64-encoded-key",
			service:  "other-service",
			account:  KeyringMasterKeyAccount,
			wantVal:  "",
			wantErr:  ErrKeyNotFound,
		},
		{
			name:     "wrong account",
			envValue: "base64-encoded-key",
			service:  KeyringService,
			account:  "other-account",
			wantVal:  "",
			wantErr:  ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a unique env var for each test
			envKey := "TEST_SECRET_KEY_" + tt.name
			if tt.envValue != "" {
				t.Setenv(envKey, tt.envValue)
			}

			kr := newEnvKeyring(envKey)
			val, err := kr.Get(tt.service, tt.account)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVal, val)
			}
		})
	}
}

func TestEnvKeyring_SetAndDelete(t *testing.T) {
	kr := newEnvKeyring("TEST_ENV_KEY")

	// Set should return error
	err := kr.Set("service", "account", "value")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrKeyringAccess))

	// Delete should return error
	err = kr.Delete("service", "account")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrKeyringAccess))
}

func TestGetMasterKeyFromKeyring(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		kr := newMockKeyringForTest()
		originalKey := make([]byte, masterKeySize)
		for i := range originalKey {
			originalKey[i] = byte(i)
		}
		encoded := base64.StdEncoding.EncodeToString(originalKey)
		_ = kr.Set(KeyringService, KeyringMasterKeyAccount, encoded)

		key, err := GetMasterKeyFromKeyring(kr)
		require.NoError(t, err)
		assert.Equal(t, originalKey, key)
	})

	t.Run("key not found", func(t *testing.T) {
		kr := newMockKeyringForTest()

		_, err := GetMasterKeyFromKeyring(kr)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("invalid base64", func(t *testing.T) {
		kr := newMockKeyringForTest()
		_ = kr.Set(KeyringService, KeyringMasterKeyAccount, "not-valid-base64!!!")

		_, err := GetMasterKeyFromKeyring(kr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode master key")
	})

	t.Run("wrong key size", func(t *testing.T) {
		kr := newMockKeyringForTest()
		shortKey := make([]byte, 16) // Wrong size
		encoded := base64.StdEncoding.EncodeToString(shortKey)
		_ = kr.Set(KeyringService, KeyringMasterKeyAccount, encoded)

		_, err := GetMasterKeyFromKeyring(kr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid master key size")
	})
}

func TestSetMasterKeyInKeyring(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		kr := newMockKeyringForTest()
		key := make([]byte, masterKeySize)
		for i := range key {
			key[i] = byte(i)
		}

		err := SetMasterKeyInKeyring(kr, key)
		require.NoError(t, err)

		// Verify it can be retrieved
		retrieved, err := GetMasterKeyFromKeyring(kr)
		require.NoError(t, err)
		assert.Equal(t, key, retrieved)
	})

	t.Run("wrong key size", func(t *testing.T) {
		kr := newMockKeyringForTest()
		shortKey := make([]byte, 16)

		err := SetMasterKeyInKeyring(kr, shortKey)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid master key size")
	})

	t.Run("keyring error", func(t *testing.T) {
		kr := newMockKeyringForTest()
		kr.setErr = NewKeyringError("set", errors.New("keyring error"))
		key := make([]byte, masterKeySize)

		err := SetMasterKeyInKeyring(kr, key)
		assert.Error(t, err)
	})
}

func TestDeleteMasterKeyFromKeyring(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		kr := newMockKeyringForTest()
		key := make([]byte, masterKeySize)
		_ = SetMasterKeyInKeyring(kr, key)

		err := DeleteMasterKeyFromKeyring(kr)
		require.NoError(t, err)

		// Verify it's deleted
		_, err = GetMasterKeyFromKeyring(kr)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("non-existent key", func(t *testing.T) {
		kr := newMockKeyringForTest()

		// Should not error on non-existent
		err := DeleteMasterKeyFromKeyring(kr)
		require.NoError(t, err)
	})
}

func TestKeyringConstants(t *testing.T) {
	assert.Equal(t, "scafctl", KeyringService)
	assert.Equal(t, "master-key", KeyringMasterKeyAccount)
	assert.Equal(t, "SCAFCTL_SECRET_KEY", EnvSecretKey)
}

func TestFileKeyring_Get(t *testing.T) {
	t.Run("returns key from file", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		// Write a key file
		err := os.WriteFile(filepath.Join(dir, masterKeyFileName), []byte("my-secret-key"), 0o600)
		require.NoError(t, err)

		val, err := kr.Get(KeyringService, KeyringMasterKeyAccount)
		require.NoError(t, err)
		assert.Equal(t, "my-secret-key", val)
	})

	t.Run("returns ErrKeyNotFound when file missing", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		_, err := kr.Get(KeyringService, KeyringMasterKeyAccount)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("returns ErrKeyNotFound for wrong service", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		_, err := kr.Get("other-service", KeyringMasterKeyAccount)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("returns ErrKeyNotFound for wrong account", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		_, err := kr.Get(KeyringService, "other-account")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("returns ErrKeyNotFound for empty file", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := os.WriteFile(filepath.Join(dir, masterKeyFileName), []byte(""), 0o600)
		require.NoError(t, err)

		_, err = kr.Get(KeyringService, KeyringMasterKeyAccount)
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})
}

func TestFileKeyring_Set(t *testing.T) {
	t.Run("writes key to file with correct permissions", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := kr.Set(KeyringService, KeyringMasterKeyAccount, "my-secret-value")
		require.NoError(t, err)

		// Verify file contents
		data, err := os.ReadFile(filepath.Join(dir, masterKeyFileName))
		require.NoError(t, err)
		assert.Equal(t, "my-secret-value", string(data))

		// Verify permissions
		info, err := os.Stat(filepath.Join(dir, masterKeyFileName))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("creates directory if missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "dir")
		kr := newFileKeyring(dir)

		err := kr.Set(KeyringService, KeyringMasterKeyAccount, "my-secret-value")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, masterKeyFileName))
		require.NoError(t, err)
		assert.Equal(t, "my-secret-value", string(data))
	})

	t.Run("rejects non-master-key accounts", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := kr.Set("other-service", "other-account", "value")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrKeyringAccess))
	})

	t.Run("overwrites existing key", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := kr.Set(KeyringService, KeyringMasterKeyAccount, "first-value")
		require.NoError(t, err)

		err = kr.Set(KeyringService, KeyringMasterKeyAccount, "second-value")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, masterKeyFileName))
		require.NoError(t, err)
		assert.Equal(t, "second-value", string(data))
	})
}

func TestFileKeyring_Delete(t *testing.T) {
	t.Run("removes key file", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := os.WriteFile(filepath.Join(dir, masterKeyFileName), []byte("value"), 0o600)
		require.NoError(t, err)

		err = kr.Delete(KeyringService, KeyringMasterKeyAccount)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dir, masterKeyFileName))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("idempotent delete", func(t *testing.T) {
		dir := t.TempDir()
		kr := newFileKeyring(dir)

		err := kr.Delete(KeyringService, KeyringMasterKeyAccount)
		require.NoError(t, err)
	})
}

func TestFileKeyring_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	kr := newFileKeyring(dir)

	// Set
	err := kr.Set(KeyringService, KeyringMasterKeyAccount, "round-trip-value")
	require.NoError(t, err)

	// Get
	val, err := kr.Get(KeyringService, KeyringMasterKeyAccount)
	require.NoError(t, err)
	assert.Equal(t, "round-trip-value", val)

	// Delete
	err = kr.Delete(KeyringService, KeyringMasterKeyAccount)
	require.NoError(t, err)

	// Get after delete
	_, err = kr.Get(KeyringService, KeyringMasterKeyAccount)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

func TestChainKeyring_Get(t *testing.T) {
	t.Run("returns from first keyring that succeeds", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr2 := newMockKeyringForTest()
		kr3 := newMockKeyringForTest()

		_ = kr2.Set("service", "account", "from-second")
		_ = kr3.Set("service", "account", "from-third")

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: "first"},
			keyringEntry{keyring: kr2, backend: "second"},
			keyringEntry{keyring: kr3, backend: "third"},
		)

		val, err := chain.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "from-second", val)
		assert.Equal(t, "second", chain.Backend())
	})

	t.Run("returns from primary when available", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr2 := newMockKeyringForTest()

		_ = kr1.Set("service", "account", "primary-value")
		_ = kr2.Set("service", "account", "fallback-value")

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: KeyringBackendOS},
			keyringEntry{keyring: kr2, backend: KeyringBackendEnv},
		)

		val, err := chain.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "primary-value", val)
		assert.Equal(t, KeyringBackendOS, chain.Backend())
	})

	t.Run("returns first error when all fail", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr1.getErr = NewKeyringError("get", errors.New("os keyring error"))
		kr2 := newMockKeyringForTest()

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: KeyringBackendOS},
			keyringEntry{keyring: kr2, backend: KeyringBackendEnv},
		)

		_, err := chain.Get("service", "account")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrKeyringAccess))
	})

	t.Run("tracks backend correctly", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr1.getErr = NewKeyringError("get", errors.New("os keyring unavailable"))
		kr2 := newMockKeyringForTest()
		kr3 := newMockKeyringForTest()

		_ = kr3.Set("service", "account", "file-value")

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: KeyringBackendOS},
			keyringEntry{keyring: kr2, backend: KeyringBackendEnv},
			keyringEntry{keyring: kr3, backend: KeyringBackendFile},
		)

		val, err := chain.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "file-value", val)
		assert.Equal(t, KeyringBackendFile, chain.Backend())
	})

	t.Run("empty backend before Get", func(t *testing.T) {
		chain := newChainKeyring()
		assert.Equal(t, "", chain.Backend())
	})
}

func TestChainKeyring_Set(t *testing.T) {
	t.Run("sets in first keyring that accepts", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr2 := newMockKeyringForTest()

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: KeyringBackendOS},
			keyringEntry{keyring: kr2, backend: KeyringBackendEnv},
		)

		err := chain.Set("service", "account", "value")
		require.NoError(t, err)

		val, err := kr1.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "value", val)
	})
}

func TestChainKeyring_Delete(t *testing.T) {
	t.Run("deletes from all keyrings", func(t *testing.T) {
		kr1 := newMockKeyringForTest()
		kr2 := newMockKeyringForTest()

		_ = kr1.Set("service", "account", "val1")
		_ = kr2.Set("service", "account", "val2")

		chain := newChainKeyring(
			keyringEntry{keyring: kr1, backend: KeyringBackendOS},
			keyringEntry{keyring: kr2, backend: KeyringBackendEnv},
		)

		err := chain.Delete("service", "account")
		require.NoError(t, err)

		_, err = kr1.Get("service", "account")
		assert.ErrorIs(t, err, ErrKeyNotFound)

		_, err = kr2.Get("service", "account")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})
}

func TestNewDefaultKeyring(t *testing.T) {
	t.Run("returns a chainKeyring", func(t *testing.T) {
		kr := NewDefaultKeyring()
		_, ok := kr.(*chainKeyring)
		assert.True(t, ok, "NewDefaultKeyring should return a *chainKeyring")
	})
}
