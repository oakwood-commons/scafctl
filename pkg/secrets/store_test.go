package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKeyring is an enhanced mock keyring for store tests that supports error injection.
type testKeyring struct {
	data      map[string]string
	getErr    error
	setErr    error
	deleteErr error
}

func newTestKeyring() *testKeyring {
	return &testKeyring{
		data: make(map[string]string),
	}
}

func (m *testKeyring) Get(service, account string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	key := service + ":" + account
	if value, ok := m.data[key]; ok {
		return value, nil
	}
	return "", ErrKeyNotFound
}

func (m *testKeyring) Set(service, account, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	key := service + ":" + account
	m.data[key] = value
	return nil
}

func (m *testKeyring) Delete(service, account string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	key := service + ":" + account
	delete(m.data, key)
	return nil
}

func TestNew(t *testing.T) {
	t.Run("creates store with default options", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)
		require.NotNil(t, store)

		// Verify master key was created
		_, err = keyring.Get(KeyringService, KeyringMasterKeyAccount)
		require.NoError(t, err)
	})

	t.Run("uses existing master key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		// Pre-populate master key
		key, err := generateMasterKey()
		require.NoError(t, err)
		err = SetMasterKeyInKeyring(keyring, key)
		require.NoError(t, err)

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)
		require.NotNil(t, store)
	})

	t.Run("deletes orphaned secrets when master key is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		// Create an "orphaned" secret file (encrypted with unknown key)
		fakeEncrypted := make([]byte, 50) // Doesn't matter, it will be deleted
		err := writeSecret(tmpDir, "orphaned-secret", fakeEncrypted)
		require.NoError(t, err)

		// Verify orphaned secret exists
		secrets, err := listSecrets(tmpDir)
		require.NoError(t, err)
		require.Len(t, secrets, 1)

		// Create store - should delete orphaned secrets
		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)
		require.NotNil(t, store)

		// Verify orphaned secrets were deleted
		secrets, err = listSecrets(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, secrets)
	})

	t.Run("returns error if keyring access fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()
		keyring.getErr = errors.New("keyring locked")
		keyring.setErr = errors.New("keyring locked")

		_, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "keyring")
	})
}

func TestStore_Get(t *testing.T) {
	setup := func(t *testing.T) (Store, string) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		return store, tmpDir
	}

	t.Run("retrieves stored secret", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		// Store a secret
		secretValue := []byte("my-secret-value")
		err := store.Set(ctx, "test-secret", secretValue)
		require.NoError(t, err)

		// Retrieve it
		value, err := store.Get(ctx, "test-secret")
		require.NoError(t, err)
		assert.Equal(t, secretValue, value)
	})

	t.Run("returns ErrNotFound for missing secret", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		_, err := store.Get(ctx, "non-existent")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("returns ErrInvalidName for invalid name", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		_, err := store.Get(ctx, ".invalid")
		assert.ErrorIs(t, err, ErrInvalidName)
	})

	t.Run("returns ErrCorrupted for corrupted file", func(t *testing.T) {
		store, tmpDir := setup(t)
		ctx := context.Background()

		// Write garbage to a secret file
		err := writeSecret(tmpDir, "corrupted", []byte("not-valid-encrypted-data"))
		require.NoError(t, err)

		_, err = store.Get(ctx, "corrupted")
		assert.ErrorIs(t, err, ErrCorrupted)

		// Verify corrupted file was deleted
		exists, err := secretExists(tmpDir, "corrupted")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		store, _ := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := store.Get(ctx, "test-secret")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStore_Set(t *testing.T) {
	setup := func(t *testing.T) (Store, string) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		return store, tmpDir
	}

	t.Run("stores secret successfully", func(t *testing.T) {
		store, tmpDir := setup(t)
		ctx := context.Background()

		err := store.Set(ctx, "test-secret", []byte("secret-value"))
		require.NoError(t, err)

		// Verify file was created
		exists, err := secretExists(tmpDir, "test-secret")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("overwrites existing secret", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		// Store original
		err := store.Set(ctx, "test-secret", []byte("original"))
		require.NoError(t, err)

		// Overwrite
		err = store.Set(ctx, "test-secret", []byte("updated"))
		require.NoError(t, err)

		// Verify updated value
		value, err := store.Get(ctx, "test-secret")
		require.NoError(t, err)
		assert.Equal(t, []byte("updated"), value)
	})

	t.Run("returns ErrInvalidName for invalid name", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		err := store.Set(ctx, "-invalid", []byte("value"))
		assert.ErrorIs(t, err, ErrInvalidName)
	})

	t.Run("handles empty value", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		err := store.Set(ctx, "empty-secret", []byte{})
		require.NoError(t, err)

		value, err := store.Get(ctx, "empty-secret")
		require.NoError(t, err)
		assert.Empty(t, value)
	})

	t.Run("handles large value", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		// Create 100KB of data
		largeValue := make([]byte, 100*1024)
		for i := range largeValue {
			largeValue[i] = byte(i % 256)
		}

		err := store.Set(ctx, "large-secret", largeValue)
		require.NoError(t, err)

		value, err := store.Get(ctx, "large-secret")
		require.NoError(t, err)
		assert.Equal(t, largeValue, value)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		store, _ := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := store.Set(ctx, "test-secret", []byte("value"))
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStore_Delete(t *testing.T) {
	setup := func(t *testing.T) (Store, string) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		return store, tmpDir
	}

	t.Run("deletes existing secret", func(t *testing.T) {
		store, tmpDir := setup(t)
		ctx := context.Background()

		// Create secret
		err := store.Set(ctx, "test-secret", []byte("value"))
		require.NoError(t, err)

		// Delete it
		err = store.Delete(ctx, "test-secret")
		require.NoError(t, err)

		// Verify deleted
		exists, err := secretExists(tmpDir, "test-secret")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("succeeds for non-existent secret", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		err := store.Delete(ctx, "non-existent")
		assert.NoError(t, err)
	})

	t.Run("returns ErrInvalidName for invalid name", func(t *testing.T) {
		store, _ := setup(t)
		ctx := context.Background()

		err := store.Delete(ctx, "..invalid")
		assert.ErrorIs(t, err, ErrInvalidName)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		store, _ := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := store.Delete(ctx, "test-secret")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStore_List(t *testing.T) {
	setup := func(t *testing.T) Store {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		return store
	}

	t.Run("lists all secrets", func(t *testing.T) {
		store := setup(t)
		ctx := context.Background()

		// Create some secrets
		secrets := []string{"secret1", "secret2", "api-key"}
		for _, name := range secrets {
			err := store.Set(ctx, name, []byte("value"))
			require.NoError(t, err)
		}

		// List
		names, err := store.List(ctx)
		require.NoError(t, err)
		assert.Len(t, names, len(secrets))

		// Check all secrets are present (order may vary)
		for _, name := range secrets {
			assert.Contains(t, names, name)
		}
	})

	t.Run("returns empty list when no secrets", func(t *testing.T) {
		store := setup(t)
		ctx := context.Background()

		names, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		store := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := store.List(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStore_Exists(t *testing.T) {
	setup := func(t *testing.T) Store {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		return store
	}

	t.Run("returns true for existing secret", func(t *testing.T) {
		store := setup(t)
		ctx := context.Background()

		err := store.Set(ctx, "test-secret", []byte("value"))
		require.NoError(t, err)

		exists, err := store.Exists(ctx, "test-secret")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent secret", func(t *testing.T) {
		store := setup(t)
		ctx := context.Background()

		exists, err := store.Exists(ctx, "non-existent")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("returns ErrInvalidName for invalid name", func(t *testing.T) {
		store := setup(t)
		ctx := context.Background()

		_, err := store.Exists(ctx, ".invalid")
		assert.ErrorIs(t, err, ErrInvalidName)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		store := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := store.Exists(ctx, "test-secret")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	keyring := newTestKeyring()

	store, err := New(
		WithSecretsDir(tmpDir),
		WithKeyring(keyring),
	)
	require.NoError(t, err)

	ctx := context.Background()
	const numGoroutines = 10
	const numOperations = 50

	// Run concurrent operations
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
				name := "secret"
				value := []byte("value")

				// Mix of operations
				switch j % 4 {
				case 0:
					_ = store.Set(ctx, name, value)
				case 1:
					_, _ = store.Get(ctx, name)
				case 2:
					_, _ = store.Exists(ctx, name)
				case 3:
					_, _ = store.List(ctx)
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should complete without panic or data race
}

func TestStore_RoundTrip(t *testing.T) {
	t.Run("stores and retrieves binary data correctly", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyring := newTestKeyring()

		store, err := New(
			WithSecretsDir(tmpDir),
			WithKeyring(keyring),
		)
		require.NoError(t, err)

		ctx := context.Background()

		// Test various data types
		testCases := []struct {
			name  string
			value []byte
		}{
			{"text", []byte("hello world")},
			{"empty", []byte{}},
			{"binary", []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}},
			{"unicode", []byte("日本語 🎉 emoji")},
			{"newlines", []byte("line1\nline2\r\nline3")},
			{"large", make([]byte, 100*1024)}, // 100KB
		}

		// Initialize large test case
		for i := range testCases[len(testCases)-1].value {
			testCases[len(testCases)-1].value[i] = byte(i % 256)
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := store.Set(ctx, tc.name, tc.value)
				require.NoError(t, err)

				retrieved, err := store.Get(ctx, tc.name)
				require.NoError(t, err)

				// For empty slice, check length instead of equality
				// because nil and empty slice are semantically equivalent
				if len(tc.value) == 0 {
					assert.Len(t, retrieved, 0)
				} else {
					assert.Equal(t, tc.value, retrieved)
				}
			})
		}
	})
}
