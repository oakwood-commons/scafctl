// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// ValidateUserSecretName validates a secret name for user operations.
// Returns an error if the name is invalid or uses the reserved "scafctl." prefix.
func ValidateUserSecretName(name string) error {
	if strings.HasPrefix(name, "scafctl.") {
		return fmt.Errorf("%w: cannot operate on internal secrets (scafctl.*)", secrets.ErrInvalidName)
	}
	return secrets.ValidateName(name)
}

// FilterUserSecrets filters out internal secrets (scafctl.*) from a list of secret names.
func FilterUserSecrets(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.HasPrefix(name, "scafctl.") {
			result = append(result, name)
		}
	}
	return result
}
