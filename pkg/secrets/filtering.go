// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"strings"
)

const (
	// InternalSecretPrefix is the default prefix used for internal secrets (e.g. auth tokens).
	InternalSecretPrefix = "scafctl." //nolint:gosec // Not a credential, just a naming prefix
)

// InternalSecretPrefixFor returns the internal-secret prefix for the given binary name.
// Falls back to the default prefix when binaryName is empty.
func InternalSecretPrefixFor(binaryName string) string {
	if binaryName == "" {
		return InternalSecretPrefix
	}
	return binaryName + "."
}

// IsInternalSecret returns true if the secret name belongs to the default internal namespace.
func IsInternalSecret(name string) bool {
	return IsInternalSecretFor(name, InternalSecretPrefix)
}

// IsInternalSecretFor returns true if the secret name starts with the given prefix.
func IsInternalSecretFor(name, prefix string) bool {
	return strings.HasPrefix(name, prefix)
}

// ValidateSecretName validates a secret name using the default internal prefix.
func ValidateSecretName(name string, includeInternal bool) error {
	return ValidateSecretNameFor(name, includeInternal, InternalSecretPrefix)
}

// ValidateSecretNameFor validates a secret name, optionally allowing internal secrets
// identified by the given prefix.
func ValidateSecretNameFor(name string, includeInternal bool, prefix string) error {
	if !includeInternal && IsInternalSecretFor(name, prefix) {
		return fmt.Errorf("%w: cannot operate on internal secrets (%s*); use --all to include them", ErrInvalidName, prefix)
	}
	return ValidateName(name)
}

// ValidateUserSecretName validates a secret name for user operations.
// Returns an error if the name is invalid or uses the reserved internal prefix.
func ValidateUserSecretName(name string) error {
	return ValidateSecretName(name, false)
}

// FilterSecrets filters secret names, optionally including internal secrets.
// When includeInternal is false, internal secrets are excluded using the default prefix.
func FilterSecrets(names []string, includeInternal bool) []string {
	return FilterSecretsFor(names, includeInternal, InternalSecretPrefix)
}

// FilterSecretsFor filters secret names using the given internal-secret prefix.
func FilterSecretsFor(names []string, includeInternal bool, prefix string) []string {
	if includeInternal {
		return names
	}
	return FilterUserSecretsFor(names, prefix)
}

// FilterUserSecrets filters out internal secrets from a list using the default prefix.
func FilterUserSecrets(names []string) []string {
	return FilterUserSecretsFor(names, InternalSecretPrefix)
}

// FilterUserSecretsFor filters out secrets matching the given prefix.
func FilterUserSecretsFor(names []string, prefix string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if !IsInternalSecretFor(name, prefix) {
			result = append(result, name)
		}
	}
	return result
}

// SecretType returns "internal" or "user" based on the secret name prefix.
func SecretType(name string) string {
	if IsInternalSecret(name) {
		return "internal"
	}
	return "user"
}
