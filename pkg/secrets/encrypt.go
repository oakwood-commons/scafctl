package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const (
	// encryptionVersion is the current version of the encryption format.
	// This allows for future format changes while maintaining backward compatibility.
	encryptionVersion byte = 0x01

	// masterKeySize is the size of the master key in bytes (256 bits).
	masterKeySize = 32

	// nonceSize is the size of the GCM nonce in bytes.
	nonceSize = 12

	// versionSize is the size of the version byte.
	versionSize = 1

	// minCiphertextSize is the minimum valid ciphertext size:
	// version (1) + nonce (12) + at least empty ciphertext with tag (16)
	minCiphertextSize = versionSize + nonceSize + 16
)

// encrypt encrypts plaintext using AES-256-GCM with the provided key.
// The returned ciphertext has the format: [version:1][nonce:12][ciphertext+tag:N]
//
// Parameters:
//   - key: 32-byte AES-256 key
//   - plaintext: data to encrypt (can be empty)
//
// Returns encrypted data or an error if encryption fails.
func encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Build output: [version][nonce][ciphertext+tag]
	result := make([]byte, versionSize+nonceSize+len(ciphertext))
	result[0] = encryptionVersion
	copy(result[versionSize:], nonce)
	copy(result[versionSize+nonceSize:], ciphertext)

	return result, nil
}

// decrypt decrypts ciphertext that was encrypted with encrypt().
// It validates the version byte and authenticates the ciphertext.
//
// Parameters:
//   - key: 32-byte AES-256 key (must match the key used for encryption)
//   - ciphertext: encrypted data in format [version:1][nonce:12][ciphertext+tag:N]
//
// Returns decrypted plaintext or an error if decryption fails.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	if len(ciphertext) < minCiphertextSize {
		return nil, fmt.Errorf("ciphertext too short: minimum %d bytes required", minCiphertextSize)
	}

	// Parse version
	version := ciphertext[0]
	if version != encryptionVersion {
		return nil, fmt.Errorf("unsupported encryption version: %d (expected %d)", version, encryptionVersion)
	}

	// Extract nonce and encrypted data
	nonce := ciphertext[versionSize : versionSize+nonceSize]
	encryptedData := ciphertext[versionSize+nonceSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt and authenticate
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (data may be corrupted or wrong key): %w", err)
	}

	return plaintext, nil
}

// generateMasterKey generates a cryptographically secure random 256-bit key
// suitable for use as an AES-256 master encryption key.
//
// Returns a 32-byte key or an error if random generation fails.
func generateMasterKey() ([]byte, error) {
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}
	return key, nil
}
