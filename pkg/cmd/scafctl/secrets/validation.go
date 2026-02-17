// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
	// InternalSecretPrefix is the prefix used for internal scafctl secrets (e.g. auth tokens).
	InternalSecretPrefix = "scafctl." //nolint:gosec // Not a credential, just a naming prefix
)

// IsInternalSecret returns true if the secret name belongs to the internal namespace.
func IsInternalSecret(name string) bool {
	return strings.HasPrefix(name, InternalSecretPrefix)
}

// ValidateSecretName validates a secret name, optionally allowing internal secrets.
// When includeInternal is false, names starting with "scafctl." are rejected.
func ValidateSecretName(name string, includeInternal bool) error {
	if !includeInternal && IsInternalSecret(name) {
		return fmt.Errorf("%w: cannot operate on internal secrets (scafctl.*); use --all to include them", secrets.ErrInvalidName)
	}
	return secrets.ValidateName(name)
}

// ValidateUserSecretName validates a secret name for user operations.
// Returns an error if the name is invalid or uses the reserved "scafctl." prefix.
func ValidateUserSecretName(name string) error {
	return ValidateSecretName(name, false)
}

// FilterSecrets filters secret names, optionally including internal secrets (scafctl.*).
// When includeInternal is false, internal secrets are excluded.
func FilterSecrets(names []string, includeInternal bool) []string {
	if includeInternal {
		return names
	}
	return FilterUserSecrets(names)
}

// FilterUserSecrets filters out internal secrets (scafctl.*) from a list of secret names.
func FilterUserSecrets(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if !IsInternalSecret(name) {
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
