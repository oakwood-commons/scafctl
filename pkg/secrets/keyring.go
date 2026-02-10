// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

const (
	// KeyringService is the service name used in the OS keyring.
	KeyringService = "scafctl"

	// KeyringMasterKeyAccount is the account name for the master encryption key.
	KeyringMasterKeyAccount = "master-key"

	// EnvSecretKey is the environment variable name for the fallback secret key.
	EnvSecretKey = "SCAFCTL_SECRET_KEY" //nolint:gosec // This is the env var name, not a credential
)

// ErrKeyNotFound is returned when a key is not found in the keyring.
var ErrKeyNotFound = errors.New("key not found in keyring")

// OSKeyring implements Keyring using the OS keychain via go-keyring.
type OSKeyring struct{}

// NewOSKeyring creates a new OS keyring wrapper.
func NewOSKeyring() *OSKeyring {
	return &OSKeyring{}
}

// Get retrieves a value from the OS keyring.
func (k *OSKeyring) Get(service, account string) (string, error) {
	value, err := keyring.Get(service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrKeyNotFound
		}
		return "", NewKeyringError("get", err)
	}
	return value, nil
}

// Set stores a value in the OS keyring.
func (k *OSKeyring) Set(service, account, value string) error {
	if err := keyring.Set(service, account, value); err != nil {
		return NewKeyringError("set", err)
	}
	return nil
}

// Delete removes a value from the OS keyring.
func (k *OSKeyring) Delete(service, account string) error {
	if err := keyring.Delete(service, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // Idempotent delete
		}
		return NewKeyringError("delete", err)
	}
	return nil
}

// envKeyring implements Keyring using environment variables.
// This is a fallback for environments where the OS keyring is not available.
// It only supports reading the master key from SCAFCTL_SECRET_KEY.
type envKeyring struct {
	envKey string
}

// newEnvKeyring creates a new environment variable keyring.
// envKey is the name of the environment variable to read from.
func newEnvKeyring(envKey string) *envKeyring {
	return &envKeyring{envKey: envKey}
}

// Get retrieves the secret key from the environment variable.
// Only supports the master key account; other accounts return ErrKeyNotFound.
func (k *envKeyring) Get(service, account string) (string, error) {
	// Only support the master key
	if service != KeyringService || account != KeyringMasterKeyAccount {
		return "", ErrKeyNotFound
	}

	value := os.Getenv(k.envKey)
	if value == "" {
		return "", ErrKeyNotFound
	}

	return value, nil
}

// Set is a no-op for envKeyring since we can't modify environment variables persistently.
// Returns an error indicating the operation is not supported.
func (k *envKeyring) Set(_, _, _ string) error {
	return NewKeyringError("set", errors.New("cannot set values in environment keyring"))
}

// Delete is a no-op for envKeyring.
// Returns an error indicating the operation is not supported.
func (k *envKeyring) Delete(_, _ string) error {
	return NewKeyringError("delete", errors.New("cannot delete values in environment keyring"))
}

// fallbackKeyring implements Keyring by trying an OS keyring first,
// then falling back to an environment-based keyring.
type fallbackKeyring struct {
	primary  Keyring
	fallback Keyring
}

// newFallbackKeyring creates a keyring that tries the primary keyring first,
// then falls back to the fallback keyring if the primary fails.
func newFallbackKeyring(primary, fallback Keyring) *fallbackKeyring {
	return &fallbackKeyring{
		primary:  primary,
		fallback: fallback,
	}
}

// Get tries to get a value from the primary keyring, falling back to the fallback.
func (k *fallbackKeyring) Get(service, account string) (string, error) {
	value, err := k.primary.Get(service, account)
	if err == nil {
		return value, nil
	}

	// Try fallback
	value, fallbackErr := k.fallback.Get(service, account)
	if fallbackErr == nil {
		return value, nil
	}

	// Return the primary error if both fail
	return "", err
}

// Set stores a value in the primary keyring.
// Does not fall back since we want to use the secure storage when available.
func (k *fallbackKeyring) Set(service, account, value string) error {
	return k.primary.Set(service, account, value)
}

// Delete removes a value from the primary keyring.
func (k *fallbackKeyring) Delete(service, account string) error {
	return k.primary.Delete(service, account)
}

// NewDefaultKeyring creates the default keyring with OS keyring primary
// and environment variable fallback.
func NewDefaultKeyring() Keyring {
	return newFallbackKeyring(
		NewOSKeyring(),
		newEnvKeyring(EnvSecretKey),
	)
}

// GetMasterKeyFromKeyring retrieves the master encryption key from the keyring.
// The key is stored as base64-encoded string in the keyring.
func GetMasterKeyFromKeyring(kr Keyring) ([]byte, error) {
	encoded, err := kr.Get(KeyringService, KeyringMasterKeyAccount)
	if err != nil {
		return nil, err
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode master key: %w", err)
	}

	if len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid master key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	return key, nil
}

// SetMasterKeyInKeyring stores the master encryption key in the keyring.
// The key is stored as base64-encoded string.
func SetMasterKeyInKeyring(kr Keyring, key []byte) error {
	if len(key) != masterKeySize {
		return fmt.Errorf("invalid master key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	return kr.Set(KeyringService, KeyringMasterKeyAccount, encoded)
}

// DeleteMasterKeyFromKeyring removes the master encryption key from the keyring.
func DeleteMasterKeyFromKeyring(kr Keyring) error {
	return kr.Delete(KeyringService, KeyringMasterKeyAccount)
}
