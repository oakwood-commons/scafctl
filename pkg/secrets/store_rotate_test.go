// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_Rotate(t *testing.T) {
	t.Run("rotates empty store successfully", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Get original key
		originalKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)

		// Rotate
		err = store.Rotate(ctx)
		require.NoError(t, err)

		// Verify new key is different
		newKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)
		assert.NotEqual(t, originalKey, newKey, "key should have changed")
		assert.Len(t, newKey, masterKeySize, "new key should be correct size")
	})

	t.Run("rotates store with single secret", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store a secret
		secretName := "test-secret"
		secretValue := []byte("test-value")
		err = store.Set(ctx, secretName, secretValue)
		require.NoError(t, err)

		// Get original key
		originalKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)

		// Rotate
		err = store.Rotate(ctx)
		require.NoError(t, err)

		// Verify new key is different
		newKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)
		assert.NotEqual(t, originalKey, newKey)

		// Verify secret is still accessible with same value
		retrieved, err := store.Get(ctx, secretName)
		require.NoError(t, err)
		assert.Equal(t, secretValue, retrieved)
	})

	t.Run("rotates store with multiple secrets", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store multiple secrets
		secrets := map[string][]byte{
			"secret1": []byte("value1"),
			"secret2": []byte("value2"),
			"secret3": []byte("value3"),
		}

		for name, value := range secrets {
			err := store.Set(ctx, name, value)
			require.NoError(t, err)
		}

		// Rotate
		err = store.Rotate(ctx)
		require.NoError(t, err)

		// Verify all secrets are still accessible
		for name, expectedValue := range secrets {
			retrieved, err := store.Get(ctx, name)
			require.NoError(t, err, "should retrieve secret %s", name)
			assert.Equal(t, expectedValue, retrieved, "secret %s should have correct value", name)
		}
	})

	t.Run("rotates large secrets", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Create large secret (1MB)
		largeValue := make([]byte, 1024*1024)
		_, err = rand.Read(largeValue)
		require.NoError(t, err)

		secretName := "large-secret"
		err = store.Set(ctx, secretName, largeValue)
		require.NoError(t, err)

		// Rotate
		err = store.Rotate(ctx)
		require.NoError(t, err)

		// Verify secret is still accessible
		retrieved, err := store.Get(ctx, secretName)
		require.NoError(t, err)
		assert.Equal(t, largeValue, retrieved)
	})

	t.Run("rotates secrets with special characters", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store secrets with various special values
		secrets := map[string][]byte{
			"null-bytes": {0x00, 0x01, 0x02},
			"unicode":    []byte("Hello 世界 🔒"),
			"newlines":   []byte("line1\nline2\rline3\r\n"),
			"binary":     {0xFF, 0xFE, 0xFD, 0xFC},
		}

		// Add empty secret separately to handle nil vs empty slice comparison
		emptyName := "empty"
		err = store.Set(ctx, emptyName, []byte{})
		require.NoError(t, err)

		for name, value := range secrets {
			err := store.Set(ctx, name, value)
			require.NoError(t, err)
		}

		// Rotate
		err = store.Rotate(ctx)
		require.NoError(t, err)

		// Verify empty secret (allow nil or empty)
		retrieved, err := store.Get(ctx, emptyName)
		require.NoError(t, err)
		assert.Empty(t, retrieved, "empty secret should remain empty")

		// Verify all other secrets
		for name, expectedValue := range secrets {
			retrieved, err := store.Get(ctx, name)
			require.NoError(t, err)
			assert.Equal(t, expectedValue, retrieved)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Use cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = store.Rotate(ctx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("rolls back on keyring update failure", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store a secret
		secretName := "test-secret"
		secretValue := []byte("test-value")
		err = store.Set(ctx, secretName, secretValue)
		require.NoError(t, err)

		// Get original key
		originalKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)

		// Inject keyring error for Set operation
		keyring.SetErr = errors.New("keyring set failed")

		// Rotation should fail
		err = store.Rotate(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "updating keyring")

		// Reset keyring error
		keyring.SetErr = nil

		// Verify original key is still in keyring
		currentKey, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)
		assert.Equal(t, originalKey, currentKey, "original key should still be in keyring")

		// Verify secret is still accessible with original key
		retrieved, err := store.Get(ctx, secretName)
		require.NoError(t, err)
		assert.Equal(t, secretValue, retrieved)
	})

	t.Run("can rotate multiple times", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store a secret
		secretName := "test-secret"
		secretValue := []byte("test-value")
		err = store.Set(ctx, secretName, secretValue)
		require.NoError(t, err)

		// Get keys after each rotation
		keys := make([][]byte, 0, 4)
		key, err := GetMasterKeyFromKeyring(keyring)
		require.NoError(t, err)
		keys = append(keys, key)

		// Rotate multiple times
		for i := 0; i < 3; i++ {
			err := store.Rotate(ctx)
			require.NoError(t, err, "rotation %d should succeed", i+1)

			// Get new key
			key, err := GetMasterKeyFromKeyring(keyring)
			require.NoError(t, err)
			keys = append(keys, key)

			// Verify all keys are different
			for j := 0; j < len(keys)-1; j++ {
				assert.NotEqual(t, keys[j], keys[len(keys)-1],
					"key after rotation %d should differ from key %d", i+1, j)
			}

			// Verify secret is still accessible
			retrieved, err := store.Get(ctx, secretName)
			require.NoError(t, err, "should retrieve secret after rotation %d", i+1)
			assert.Equal(t, secretValue, retrieved, "secret value should be unchanged after rotation %d", i+1)
		}
	})

	t.Run("handles corrupted secret during rotation", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store a secret
		secretName := "test-secret"
		secretValue := []byte("test-value")
		err = store.Set(ctx, secretName, secretValue)
		require.NoError(t, err)

		// Corrupt the secret file
		err = writeSecret(tempDir, secretName, []byte("corrupted-data"))
		require.NoError(t, err)

		// Rotation should fail when trying to decrypt corrupted secret
		err = store.Rotate(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decrypting")

		// The corrupted file should still exist
		_, err = readSecret(tempDir, secretName)
		assert.NoError(t, err, "corrupted file should not be deleted by rotation")
	})

	t.Run("is thread-safe", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		keyring := NewMockKeyring()

		store, err := New(
			WithSecretsDir(tempDir),
			WithKeyring(keyring),
			WithLogger(logr.Discard()),
		)
		require.NoError(t, err)

		// Store initial secret
		err = store.Set(ctx, "secret", []byte("value"))
		require.NoError(t, err)

		// Run rotation and concurrent reads
		done := make(chan bool)
		errs := make(chan error, 1)

		go func() {
			err := store.Rotate(ctx)
			if err != nil {
				errs <- err
			}
			done <- true
		}()

		// Try concurrent operations (they should block or succeed)
		for i := 0; i < 10; i++ {
			go func() {
				_, _ = store.Get(ctx, "secret")
			}()
		}

		<-done
		select {
		case err := <-errs:
			t.Fatalf("rotation failed: %v", err)
		default:
		}

		// Verify secret is still accessible
		retrieved, err := store.Get(ctx, "secret")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), retrieved)
	})
}
