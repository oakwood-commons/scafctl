// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package secretcrypto provides password-based encryption and decryption for secret
// export/import operations using PBKDF2 key derivation and AES-256-GCM.
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// ExportVersion is the version identifier written into export files.
	ExportVersion = "scafctl-secrets-v1"

	// EncryptedHeader is the magic prefix prepended to encrypted export data.
	EncryptedHeader = "SCAFCTL-ENC-V1\n"

	// PBKDF2Iterations is the number of PBKDF2 iterations used for key derivation.
	PBKDF2Iterations = 100000

	// PBKDF2KeySize is the derived key length in bytes (256 bits).
	PBKDF2KeySize = 32

	// PBKDF2SaltSize is the random salt length in bytes.
	PBKDF2SaltSize = 16
)

// ExportFormat represents the format for exported secrets.
type ExportFormat struct {
	Version    string           `json:"version" yaml:"version"`
	ExportedAt string           `json:"exported_at" yaml:"exported_at"`
	Secrets    []ExportedSecret `json:"secrets" yaml:"secrets"`
}

// ExportedSecret represents a single exported secret.
type ExportedSecret struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

// Encrypt encrypts export data with a password using PBKDF2 + AES-256-GCM.
func Encrypt(data []byte, password string) ([]byte, error) {
	// Generate random salt
	salt := make([]byte, PBKDF2SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from password
	key := pbkdf2.Key([]byte(password), salt, PBKDF2Iterations, PBKDF2KeySize, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	// Format: header + salt + iterations + ciphertext
	iterationsBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(iterationsBytes, PBKDF2Iterations)

	result := []byte(EncryptedHeader)
	result = append(result, salt...)
	result = append(result, iterationsBytes...)
	result = append(result, []byte(base64.StdEncoding.EncodeToString(ciphertext))...)

	return result, nil
}

// Decrypt decrypts an encrypted export file.
func Decrypt(data []byte, password string) ([]byte, error) {
	// Remove header
	header := []byte(EncryptedHeader)
	if len(data) < len(header) || string(data[:len(header)]) != EncryptedHeader {
		return nil, fmt.Errorf("invalid encrypted format")
	}
	data = data[len(header):]

	// Parse: salt + iterations + base64-ciphertext
	if len(data) < PBKDF2SaltSize+4 {
		return nil, fmt.Errorf("invalid encrypted data: too short")
	}

	salt := data[:PBKDF2SaltSize]
	iterationsBytes := data[PBKDF2SaltSize : PBKDF2SaltSize+4]
	iterations := binary.BigEndian.Uint32(iterationsBytes)
	ciphertextB64 := data[PBKDF2SaltSize+4:]

	// Decode base64
	ciphertext, err := base64.StdEncoding.DecodeString(string(ciphertextB64))
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Derive key
	key := pbkdf2.Key([]byte(password), salt, int(iterations), PBKDF2KeySize, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong password?): %w", err)
	}

	return plaintext, nil
}
