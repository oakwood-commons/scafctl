// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
)

// store implements the Store interface.
type store struct {
	secretsDir     string
	keyring        Keyring
	masterKey      []byte
	logger         logr.Logger
	keyringBackend string
	mu             sync.RWMutex
}

// newStore creates a new store with the given options.
func newStore(opts ...Option) (*store, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Determine secrets directory
	secretsDir := cfg.secretsDir
	if secretsDir == "" {
		var err error
		secretsDir, err = getSecretsDir()
		if err != nil {
			return nil, fmt.Errorf("determining secrets directory: %w", err)
		}
	}

	// Ensure secrets directory exists
	if err := ensureSecretsDir(secretsDir); err != nil {
		return nil, fmt.Errorf("ensuring secrets directory: %w", err)
	}

	// Set up keyring
	keyring := cfg.keyring
	if keyring == nil {
		keyring = NewDefaultKeyring()
	}

	s := &store{
		secretsDir: secretsDir,
		keyring:    keyring,
		logger:     cfg.logger,
	}

	// Initialize master key
	if err := s.initMasterKey(); err != nil {
		return nil, fmt.Errorf("initializing master key: %w", err)
	}

	// Capture which keyring backend was used
	if ck, ok := keyring.(*chainKeyring); ok {
		s.keyringBackend = ck.Backend()
	}

	// Warn when falling back to an insecure backend — the master encryption key is stored
	// in a plaintext file or environment variable rather than the OS keychain.
	if s.keyringBackend == KeyringBackendFile || s.keyringBackend == KeyringBackendEnv {
		if cfg.requireSecureKeyring {
			return nil, fmt.Errorf(
				"OS keyring is unavailable and settings.requireSecureKeyring is enabled; "+
					"insecure keyring backend %q would be used — refusing to proceed. "+
					"Ensure the OS keychain (Keychain/Credential Manager/Secret Service) is accessible, "+
					"or set settings.requireSecureKeyring: false to allow insecure fallback",
				s.keyringBackend,
			)
		}
		s.logger.Info("WARNING: using insecure keyring backend; master key is not protected by OS keychain",
			"backend", s.keyringBackend,
			"recommendation", "ensure the OS keyring (Keychain/Credential Manager/Secret Service) is available for production use",
		)
	}

	return s, nil
}

// initMasterKey retrieves or creates the master encryption key.
// If the key doesn't exist in the keyring:
//   - If there are existing secrets, they are orphaned and deleted
//   - A new key is generated and stored in the keyring
func (s *store) initMasterKey() error {
	// Try to get existing master key
	key, err := GetMasterKeyFromKeyring(s.keyring)
	if err == nil {
		s.masterKey = key
		s.logger.V(1).Info("loaded existing master key from keyring")
		return nil
	}

	// Key not found - need to generate a new one
	if !errors.Is(err, ErrKeyNotFound) {
		// Some other error occurred
		return fmt.Errorf("accessing keyring: %w", err)
	}

	s.logger.V(1).Info("master key not found in keyring, checking for orphaned secrets")

	// Check if there are existing secrets (orphaned due to lost key)
	existingSecrets, err := listSecrets(s.secretsDir)
	if err != nil {
		return fmt.Errorf("listing existing secrets: %w", err)
	}

	if len(existingSecrets) > 0 {
		s.logger.Info("WARNING: found orphaned secrets due to missing master key — these secrets "+
			"are no longer decryptable and will be deleted. This typically happens after an OS "+
			"keychain reset or reinstall. Use 'scafctl secrets export' before clearing the keychain "+
			"to create a recoverable backup.",
			"count", len(existingSecrets),
			"secretsDir", s.secretsDir,
		)

		if err := deleteAllSecrets(s.secretsDir); err != nil {
			return fmt.Errorf("deleting orphaned secrets: %w", err)
		}
	}

	// Generate new master key
	newKey, err := generateMasterKey()
	if err != nil {
		return fmt.Errorf("generating master key: %w", err)
	}

	// Store in keyring
	if err := SetMasterKeyInKeyring(s.keyring, newKey); err != nil {
		return fmt.Errorf("storing master key in keyring: %w", err)
	}

	s.masterKey = newKey
	s.logger.V(1).Info("generated and stored new master key")

	return nil
}

// KeyringBackend returns the identifier of the keyring backend used.
func (s *store) KeyringBackend() string {
	return s.keyringBackend
}

// Get retrieves a secret by name.
func (s *store) Get(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := ValidateName(name); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Read encrypted data
	encryptedData, err := readSecret(s.secretsDir, name)
	if err != nil {
		return nil, err
	}

	// Decrypt
	plaintext, err := decrypt(s.masterKey, encryptedData)
	if err != nil {
		s.logger.Error(err, "failed to decrypt secret, file may be corrupted", "name", name)

		// Delete corrupted file
		if delErr := deleteSecret(s.secretsDir, name); delErr != nil {
			s.logger.Error(delErr, "failed to delete corrupted secret file", "name", name)
		}

		return nil, NewCorruptedSecretError(name, "decryption failed", err)
	}

	return plaintext, nil
}

// Set stores a secret.
func (s *store) Set(ctx context.Context, name string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ValidateName(name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Encrypt
	encryptedData, err := encrypt(s.masterKey, value)
	if err != nil {
		return fmt.Errorf("encrypting secret: %w", err)
	}

	// Write to file
	if err := writeSecret(s.secretsDir, name, encryptedData); err != nil {
		return fmt.Errorf("writing secret: %w", err)
	}

	s.logger.V(1).Info("stored secret", "name", name, "size", len(value))

	return nil
}

// Delete removes a secret.
func (s *store) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ValidateName(name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := deleteSecret(s.secretsDir, name); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}

	s.logger.V(1).Info("deleted secret", "name", name)

	return nil
}

// List returns the names of all stored secrets.
func (s *store) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	names, err := listSecrets(s.secretsDir)
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}

	return names, nil
}

// Exists checks if a secret exists.
func (s *store) Exists(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if err := ValidateName(name); err != nil {
		return false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	exists, err := secretExists(s.secretsDir, name)
	if err != nil {
		return false, fmt.Errorf("checking secret existence: %w", err)
	}

	return exists, nil
}

// Rotate rotates the master encryption key by re-encrypting all secrets.
// This operation is atomic - if any step fails, all secrets are preserved
// with the original key and the keyring is unchanged.
func (s *store) Rotate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("starting master key rotation")

	// Step 1: List all secrets
	names, err := listSecrets(s.secretsDir)
	if err != nil {
		return fmt.Errorf("listing secrets: %w", err)
	}

	if len(names) == 0 {
		s.logger.Info("no secrets to rotate")
		// Still rotate the key even if no secrets exist
	}

	// Step 2: Read and decrypt all secrets with current key
	type secretData struct {
		name      string
		plaintext []byte
	}
	secrets := make([]secretData, 0, len(names))

	for _, name := range names {
		// Read encrypted data
		encryptedData, err := readSecret(s.secretsDir, name)
		if err != nil {
			return fmt.Errorf("reading secret %s: %w", name, err)
		}

		// Decrypt with current master key
		plaintext, err := decrypt(s.masterKey, encryptedData)
		if err != nil {
			s.logger.Error(err, "failed to decrypt secret during rotation", "name", name)
			return fmt.Errorf("decrypting secret %s: %w", name, err)
		}

		secrets = append(secrets, secretData{
			name:      name,
			plaintext: plaintext,
		})
	}

	s.logger.V(1).Info("decrypted all secrets with current key", "count", len(secrets))

	// Step 3: Generate new master key
	newKey, err := generateMasterKey()
	if err != nil {
		return fmt.Errorf("generating new master key: %w", err)
	}

	s.logger.V(1).Info("generated new master key")

	// Step 4: Re-encrypt all secrets with new key
	// We'll collect all encrypted data before writing to maintain atomicity
	type encryptedSecret struct {
		name          string
		encryptedData []byte
	}
	reencrypted := make([]encryptedSecret, 0, len(secrets))

	for _, secret := range secrets {
		encryptedData, err := encrypt(newKey, secret.plaintext)
		if err != nil {
			return fmt.Errorf("re-encrypting secret %s: %w", secret.name, err)
		}

		reencrypted = append(reencrypted, encryptedSecret{
			name:          secret.name,
			encryptedData: encryptedData,
		})
	}

	s.logger.V(1).Info("re-encrypted all secrets with new key", "count", len(reencrypted))

	// Step 5: Write re-encrypted secrets atomically
	// If any write fails, we need to rollback all changes
	var written []string
	var writeErr error

	for _, encrypted := range reencrypted {
		if err := writeSecret(s.secretsDir, encrypted.name, encrypted.encryptedData); err != nil {
			writeErr = fmt.Errorf("writing re-encrypted secret %s: %w", encrypted.name, err)
			break
		}
		written = append(written, encrypted.name)
	}

	// If write failed, rollback by re-encrypting with old key
	if writeErr != nil {
		s.logger.Error(writeErr, "failed to write re-encrypted secret, rolling back")

		for _, name := range written {
			// Find original plaintext
			var plaintext []byte
			for _, secret := range secrets {
				if secret.name == name {
					plaintext = secret.plaintext
					break
				}
			}

			// Re-encrypt with old key
			encryptedData, err := encrypt(s.masterKey, plaintext)
			if err != nil {
				s.logger.Error(err, "failed to re-encrypt during rollback", "name", name)
				continue
			}

			// Write back
			if err := writeSecret(s.secretsDir, name, encryptedData); err != nil {
				s.logger.Error(err, "failed to write during rollback", "name", name)
			}
		}

		return writeErr
	}

	// Step 6: Update keyring with new key
	if err := SetMasterKeyInKeyring(s.keyring, newKey); err != nil {
		s.logger.Error(err, "failed to update keyring, rolling back all secrets")

		// Rollback all secrets to old key
		for _, secret := range secrets {
			encryptedData, err := encrypt(s.masterKey, secret.plaintext)
			if err != nil {
				s.logger.Error(err, "failed to re-encrypt during keyring rollback", "name", secret.name)
				continue
			}

			if err := writeSecret(s.secretsDir, secret.name, encryptedData); err != nil {
				s.logger.Error(err, "failed to write during keyring rollback", "name", secret.name)
			}
		}

		return fmt.Errorf("updating keyring: %w", err)
	}

	s.logger.V(1).Info("updated keyring with new master key")

	// Step 7: Update in-memory master key
	s.masterKey = newKey

	s.logger.Info("master key rotation completed successfully", "secrets_rotated", len(secrets))

	return nil
}
