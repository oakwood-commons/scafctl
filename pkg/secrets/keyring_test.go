package secrets

import (
	"encoding/base64"
	"errors"
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

func TestFallbackKeyring_Get(t *testing.T) {
	t.Run("returns from primary when available", func(t *testing.T) {
		primary := newMockKeyringForTest()
		fallback := newMockKeyringForTest()

		_ = primary.Set("service", "account", "primary-value")
		_ = fallback.Set("service", "account", "fallback-value")

		kr := newFallbackKeyring(primary, fallback)
		val, err := kr.Get("service", "account")

		require.NoError(t, err)
		assert.Equal(t, "primary-value", val)
	})

	t.Run("falls back when primary fails", func(t *testing.T) {
		primary := newMockKeyringForTest()
		fallback := newMockKeyringForTest()

		// Only set in fallback
		_ = fallback.Set("service", "account", "fallback-value")

		kr := newFallbackKeyring(primary, fallback)
		val, err := kr.Get("service", "account")

		require.NoError(t, err)
		assert.Equal(t, "fallback-value", val)
	})

	t.Run("returns primary error when both fail", func(t *testing.T) {
		primary := newMockKeyringForTest()
		primary.getErr = NewKeyringError("get", errors.New("primary error"))
		fallback := newMockKeyringForTest()

		kr := newFallbackKeyring(primary, fallback)
		_, err := kr.Get("service", "account")

		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrKeyringAccess))
	})
}

func TestFallbackKeyring_Set(t *testing.T) {
	t.Run("sets in primary only", func(t *testing.T) {
		primary := newMockKeyringForTest()
		fallback := newMockKeyringForTest()

		kr := newFallbackKeyring(primary, fallback)
		err := kr.Set("service", "account", "value")

		require.NoError(t, err)

		// Verify set in primary
		val, err := primary.Get("service", "account")
		require.NoError(t, err)
		assert.Equal(t, "value", val)

		// Verify not set in fallback
		_, err = fallback.Get("service", "account")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("returns error from primary", func(t *testing.T) {
		primary := newMockKeyringForTest()
		primary.setErr = NewKeyringError("set", errors.New("primary error"))
		fallback := newMockKeyringForTest()

		kr := newFallbackKeyring(primary, fallback)
		err := kr.Set("service", "account", "value")

		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrKeyringAccess))
	})
}

func TestFallbackKeyring_Delete(t *testing.T) {
	t.Run("deletes from primary", func(t *testing.T) {
		primary := newMockKeyringForTest()
		fallback := newMockKeyringForTest()

		_ = primary.Set("service", "account", "value")

		kr := newFallbackKeyring(primary, fallback)
		err := kr.Delete("service", "account")

		require.NoError(t, err)

		// Verify deleted from primary
		_, err = primary.Get("service", "account")
		assert.ErrorIs(t, err, ErrKeyNotFound)
	})
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
