package secrets

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{
			name:      "simple text",
			plaintext: []byte("hello world"),
		},
		{
			name:      "empty plaintext",
			plaintext: []byte{},
		},
		{
			name:      "single byte",
			plaintext: []byte{0x42},
		},
		{
			name:      "binary data",
			plaintext: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD},
		},
		{
			name:      "unicode text",
			plaintext: []byte("こんにちは世界 🔐"),
		},
		{
			name:      "large data (100KB)",
			plaintext: make([]byte, 100*1024),
		},
		{
			name:      "null bytes",
			plaintext: make([]byte, 100),
		},
	}

	// Fill large data with random bytes
	for i := range tests {
		if tests[i].name == "large data (100KB)" {
			_, err := rand.Read(tests[i].plaintext)
			require.NoError(t, err)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := encrypt(key, tt.plaintext)
			require.NoError(t, err)
			assert.NotNil(t, ciphertext)

			// Verify ciphertext structure
			assert.GreaterOrEqual(t, len(ciphertext), minCiphertextSize)
			assert.Equal(t, encryptionVersion, ciphertext[0], "version byte should be correct")

			// Ciphertext should be different from plaintext
			if len(tt.plaintext) > 0 {
				assert.NotEqual(t, tt.plaintext, ciphertext[versionSize+nonceSize:])
			}

			// Decrypt
			decrypted, err := decrypt(key, ciphertext)
			require.NoError(t, err)

			// Verify round-trip
			assert.True(t, bytes.Equal(tt.plaintext, decrypted), "decrypted data should match original")
		})
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)

	plaintext := []byte("same message")

	// Encrypt the same plaintext multiple times
	ciphertext1, err := encrypt(key, plaintext)
	require.NoError(t, err)

	ciphertext2, err := encrypt(key, plaintext)
	require.NoError(t, err)

	// Ciphertexts should be different due to random nonce
	assert.NotEqual(t, ciphertext1, ciphertext2, "each encryption should produce different ciphertext")

	// But both should decrypt to the same plaintext
	decrypted1, err := decrypt(key, ciphertext1)
	require.NoError(t, err)

	decrypted2, err := decrypt(key, ciphertext2)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted1)
	assert.Equal(t, plaintext, decrypted2)
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, err := generateMasterKey()
	require.NoError(t, err)

	key2, err := generateMasterKey()
	require.NoError(t, err)

	plaintext := []byte("secret message")

	// Encrypt with key1
	ciphertext, err := encrypt(key1, plaintext)
	require.NoError(t, err)

	// Try to decrypt with key2
	_, err = decrypt(key2, ciphertext)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decryption failed")
}

func TestDecryptCorruptedData(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)

	plaintext := []byte("secret message")
	ciphertext, err := encrypt(key, plaintext)
	require.NoError(t, err)

	tests := []struct {
		name        string
		corrupt     func([]byte) []byte
		errContains string
	}{
		{
			name: "modified ciphertext byte",
			corrupt: func(c []byte) []byte {
				corrupted := make([]byte, len(c))
				copy(corrupted, c)
				// Modify a byte in the encrypted data
				corrupted[len(corrupted)-5] ^= 0xFF
				return corrupted
			},
			errContains: "decryption failed",
		},
		{
			name: "modified nonce",
			corrupt: func(c []byte) []byte {
				corrupted := make([]byte, len(c))
				copy(corrupted, c)
				// Modify a byte in the nonce
				corrupted[versionSize+5] ^= 0xFF
				return corrupted
			},
			errContains: "decryption failed",
		},
		{
			name: "truncated ciphertext",
			corrupt: func(c []byte) []byte {
				return c[:len(c)-10]
			},
			errContains: "decryption failed",
		},
		{
			name: "appended data",
			corrupt: func(c []byte) []byte {
				return append(c, 0x00, 0x01, 0x02)
			},
			errContains: "decryption failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corrupted := tt.corrupt(ciphertext)
			_, err := decrypt(key, corrupted)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestDecryptInvalidVersion(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)

	plaintext := []byte("secret message")
	ciphertext, err := encrypt(key, plaintext)
	require.NoError(t, err)

	// Change version byte
	ciphertext[0] = 0x99

	_, err = decrypt(key, ciphertext)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported encryption version")
}

func TestDecryptTooShort(t *testing.T) {
	key, err := generateMasterKey()
	require.NoError(t, err)

	tests := []struct {
		name       string
		ciphertext []byte
	}{
		{
			name:       "empty",
			ciphertext: []byte{},
		},
		{
			name:       "just version",
			ciphertext: []byte{encryptionVersion},
		},
		{
			name:       "version + partial nonce",
			ciphertext: append([]byte{encryptionVersion}, make([]byte, 5)...),
		},
		{
			name:       "version + full nonce only",
			ciphertext: append([]byte{encryptionVersion}, make([]byte, nonceSize)...),
		},
		{
			name:       "one byte short of minimum",
			ciphertext: make([]byte, minCiphertextSize-1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decrypt(key, tt.ciphertext)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "ciphertext too short")
		})
	}
}

func TestEncryptInvalidKeySize(t *testing.T) {
	tests := []struct {
		name    string
		keySize int
	}{
		{
			name:    "empty key",
			keySize: 0,
		},
		{
			name:    "16-byte key (AES-128)",
			keySize: 16,
		},
		{
			name:    "24-byte key (AES-192)",
			keySize: 24,
		},
		{
			name:    "31-byte key (one short)",
			keySize: 31,
		},
		{
			name:    "33-byte key (one extra)",
			keySize: 33,
		},
		{
			name:    "64-byte key",
			keySize: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			plaintext := []byte("test")

			_, err := encrypt(key, plaintext)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid key size")
		})
	}
}

func TestDecryptInvalidKeySize(t *testing.T) {
	// First create valid ciphertext with valid key
	validKey, err := generateMasterKey()
	require.NoError(t, err)

	ciphertext, err := encrypt(validKey, []byte("test"))
	require.NoError(t, err)

	tests := []struct {
		name    string
		keySize int
	}{
		{
			name:    "empty key",
			keySize: 0,
		},
		{
			name:    "16-byte key",
			keySize: 16,
		},
		{
			name:    "31-byte key",
			keySize: 31,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			_, err := decrypt(key, ciphertext)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid key size")
		})
	}
}

func TestGenerateMasterKey(t *testing.T) {
	t.Run("generates correct size", func(t *testing.T) {
		key, err := generateMasterKey()
		require.NoError(t, err)
		assert.Len(t, key, masterKeySize)
	})

	t.Run("generates unique keys", func(t *testing.T) {
		keys := make(map[string]bool)
		for i := 0; i < 100; i++ {
			key, err := generateMasterKey()
			require.NoError(t, err)
			keyStr := string(key)
			assert.False(t, keys[keyStr], "generated duplicate key")
			keys[keyStr] = true
		}
	})

	t.Run("key is usable for encryption", func(t *testing.T) {
		key, err := generateMasterKey()
		require.NoError(t, err)

		plaintext := []byte("test encryption with generated key")
		ciphertext, err := encrypt(key, plaintext)
		require.NoError(t, err)

		decrypted, err := decrypt(key, ciphertext)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})
}

func TestEncryptionConstants(t *testing.T) {
	// Verify constants match expected values
	assert.Equal(t, byte(0x01), encryptionVersion)
	assert.Equal(t, 32, masterKeySize)
	assert.Equal(t, 12, nonceSize)
	assert.Equal(t, 1, versionSize)
	assert.Equal(t, 1+12+16, minCiphertextSize) // version + nonce + GCM tag
}

// Note: Benchmarks are in benchmark_test.go
