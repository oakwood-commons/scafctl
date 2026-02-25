// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secretcrypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	plaintext := []byte(`{"version":"scafctl-secrets-v1","secrets":[{"name":"test","value":"secret"}]}`)
	password := "test-password-123"

	encrypted, err := Encrypt(plaintext, password)
	require.NoError(t, err)
	assert.True(t, len(encrypted) > len(plaintext))

	// Verify header is present
	assert.Equal(t, EncryptedHeader, string(encrypted[:len(EncryptedHeader)]))

	// Decrypt
	decrypted, err := Decrypt(encrypted, password)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestDecrypt_WrongPassword(t *testing.T) {
	t.Parallel()

	plaintext := []byte("secret data")
	encrypted, err := Encrypt(plaintext, "correct-password")
	require.NoError(t, err)

	_, err = Decrypt(encrypted, "wrong-password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrong password")
}

func TestDecrypt_InvalidHeader(t *testing.T) {
	t.Parallel()

	_, err := Decrypt([]byte("not-encrypted-data"), "password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid encrypted format")
}

func TestDecrypt_TooShort(t *testing.T) {
	t.Parallel()

	data := []byte(EncryptedHeader + "short")
	_, err := Decrypt(data, "password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestEncryptProducesDifferentOutput(t *testing.T) {
	t.Parallel()

	plaintext := []byte("same data")
	password := "same-password"

	enc1, err := Encrypt(plaintext, password)
	require.NoError(t, err)

	enc2, err := Encrypt(plaintext, password)
	require.NoError(t, err)

	// Due to random salt and nonce, outputs should differ
	assert.NotEqual(t, enc1, enc2)
}
