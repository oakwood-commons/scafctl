// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package secrets provides secure secret storage operations using AES-256-GCM encryption
// with OS keychain integration for master key management.
//
// The package uses a hybrid approach for secret storage:
//   - Master encryption key is stored in the OS keychain (or env var fallback)
//   - Secrets are encrypted and stored as individual files
//
// This allows for storing large secrets (e.g., auth tokens) that wouldn't fit
// in the OS keychain directly, while still leveraging secure key storage.
//
// Basic usage:
//
//	store, err := secrets.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Store a secret
//	err = store.Set(ctx, "my-api-key", []byte("secret-value"))
//
//	// Retrieve a secret
//	value, err := store.Get(ctx, "my-api-key")
//
//	// List all secrets
//	names, err := store.List(ctx)
//
//	// Delete a secret
//	err = store.Delete(ctx, "my-api-key")
package secrets

import "context"

// Store provides secure secret storage operations.
// All operations are safe for concurrent use.
type Store interface {
	// Get retrieves a secret by name.
	// Returns ErrNotFound if the secret does not exist.
	// Returns ErrCorrupted if the secret file is corrupted and cannot be decrypted.
	// Returns ErrInvalidName if the name is invalid.
	Get(ctx context.Context, name string) ([]byte, error)

	// Set stores a secret. Creates or overwrites existing.
	// Returns ErrInvalidName if the name is invalid.
	Set(ctx context.Context, name string, value []byte) error

	// Delete removes a secret.
	// No error is returned if the secret does not exist.
	// Returns ErrInvalidName if the name is invalid.
	Delete(ctx context.Context, name string) error

	// List returns the names of all stored secrets.
	List(ctx context.Context) ([]string, error)

	// Exists checks if a secret exists.
	// Returns ErrInvalidName if the name is invalid.
	Exists(ctx context.Context, name string) (bool, error)

	// Rotate rotates the master encryption key by re-encrypting all secrets.
	// This operation is atomic - if any step fails, all secrets are preserved
	// with the original key. The new key is generated, all secrets are
	// re-encrypted, and the keyring is updated.
	Rotate(ctx context.Context) error

	// KeyringBackend returns the identifier of the keyring backend used for
	// master key storage. Possible values: "os", "env", "file", or "" if unknown.
	KeyringBackend() string
}

// New creates a new Store with the given options.
// If no keyring is provided, the default keyring (OS keychain with env var fallback) is used.
// If no secrets directory is provided, the platform-specific default is used.
//
// The master encryption key is retrieved from the keyring on initialization.
// If no master key exists:
//   - If there are existing secrets, they are deleted (orphaned) and a new key is generated
//   - If there are no existing secrets, a new key is generated
//
// Options:
//   - WithSecretsDir: Override the secrets directory
//   - WithKeyring: Provide a custom keyring implementation
//   - WithLogger: Set a logger for diagnostic output
func New(opts ...Option) (Store, error) {
	return newStore(opts...)
}
