// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"os"
	"strings"

	"github.com/go-logr/logr"
)

// config holds the configuration for the secrets Store.
type config struct {
	secretsDir           string
	keyring              Keyring
	logger               logr.Logger
	requireSecureKeyring bool
}

// defaultConfig returns a config with default values.
func defaultConfig() *config {
	return &config{
		secretsDir:           "", // Will be determined by platform if empty
		keyring:              nil,
		logger:               logr.Discard(),
		requireSecureKeyring: strings.EqualFold(os.Getenv("SCAFCTL_REQUIRE_SECURE_KEYRING"), "true"),
	}
}

// Option configures the Store.
type Option func(*config)

// WithSecretsDir overrides the default secrets directory.
// If empty, the XDG-compliant default will be used:
//   - Linux: ~/.local/share/scafctl/secrets/ (XDG_DATA_HOME)
//   - macOS: ~/.local/share/scafctl/secrets/
//   - Windows: %LOCALAPPDATA%\scafctl\secrets\
//
// This can also be overridden by the SCAFCTL_SECRETS_DIR environment variable.
func WithSecretsDir(dir string) Option {
	return func(c *config) {
		c.secretsDir = dir
	}
}

// WithKeyring sets a custom keyring implementation.
// This is primarily useful for testing or for environments where
// the OS keyring is not available.
func WithKeyring(kr Keyring) Option {
	return func(c *config) {
		c.keyring = kr
	}
}

// WithLogger sets the logger for the Store.
// If not set, a discard logger is used.
func WithLogger(logger logr.Logger) Option {
	return func(c *config) {
		c.logger = logger
	}
}

// WithRequireSecureKeyring makes store initialization fail if the OS keyring
// is unavailable and scafctl would have to fall back to an insecure file-based
// or environment-variable-based master key. Enable this in production or shared
// environments to prevent silent degradation of secret protection.
func WithRequireSecureKeyring(require bool) Option {
	return func(c *config) {
		c.requireSecureKeyring = require
	}
}

// Keyring defines the interface for keyring operations.
// This interface abstracts the OS keychain to allow for testing
// and alternative implementations.
type Keyring interface {
	// Get retrieves a value from the keyring.
	// Returns an error if the key does not exist or cannot be accessed.
	Get(service, account string) (string, error)

	// Set stores a value in the keyring.
	// Creates or updates the existing value.
	Set(service, account, value string) error

	// Delete removes a value from the keyring.
	// Returns an error if the key does not exist or cannot be deleted.
	Delete(service, account string) error
}
