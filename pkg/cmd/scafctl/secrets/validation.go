// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// InternalSecretPrefix is the prefix used for internal scafctl secrets (e.g. auth tokens).
//
// Deprecated: Use secrets.InternalSecretPrefix from pkg/secrets instead.
const InternalSecretPrefix = secrets.InternalSecretPrefix

// IsInternalSecret returns true if the secret name belongs to the internal namespace.
//
// Deprecated: Use secrets.IsInternalSecret from pkg/secrets instead.
func IsInternalSecret(name string) bool {
	return secrets.IsInternalSecret(name)
}

// ValidateSecretName validates a secret name, optionally allowing internal secrets.
//
// Deprecated: Use secrets.ValidateSecretName from pkg/secrets instead.
func ValidateSecretName(name string, includeInternal bool) error {
	return secrets.ValidateSecretName(name, includeInternal)
}

// ValidateUserSecretName validates a secret name for user operations.
//
// Deprecated: Use secrets.ValidateUserSecretName from pkg/secrets instead.
func ValidateUserSecretName(name string) error {
	return secrets.ValidateUserSecretName(name)
}

// FilterSecrets filters secret names, optionally including internal secrets (scafctl.*).
//
// Deprecated: Use secrets.FilterSecrets from pkg/secrets instead.
func FilterSecrets(names []string, includeInternal bool) []string {
	return secrets.FilterSecrets(names, includeInternal)
}

// FilterUserSecrets filters out internal secrets (scafctl.*) from a list of secret names.
//
// Deprecated: Use secrets.FilterUserSecrets from pkg/secrets instead.
func FilterUserSecrets(names []string) []string {
	return secrets.FilterUserSecrets(names)
}

// SecretType returns "internal" or "user" based on the secret name prefix.
//
// Deprecated: Use secrets.SecretType from pkg/secrets instead.
func SecretType(name string) string {
	return secrets.SecretType(name)
}
