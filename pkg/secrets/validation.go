// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"regexp"
)

const (
	// MinNameLength is the minimum length for a secret name.
	MinNameLength = 1
	// MaxNameLength is the maximum length for a secret name.
	MaxNameLength = 255
)

// secretNameRegex validates secret names.
// Allowed characters: a-z, A-Z, 0-9, -, _, .
// Rules:
//   - Length: 1-255 characters
//   - Cannot start with '.' or '-'
//   - Cannot contain '..'
//   - Case-sensitive
var secretNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)

// doubleDotRegex detects consecutive dots in a name.
var doubleDotRegex = regexp.MustCompile(`\.\.`)

// ValidateName checks if a secret name is valid according to the naming rules.
// Returns nil if valid, or an InvalidNameError with details if invalid.
func ValidateName(name string) error {
	if name == "" {
		return NewInvalidNameError(name, "name cannot be empty")
	}

	if len(name) > MaxNameLength {
		return NewInvalidNameError(name, "name exceeds maximum length of 255 characters")
	}

	// Check for leading dot or dash
	if name[0] == '.' {
		return NewInvalidNameError(name, "name cannot start with '.'")
	}
	if name[0] == '-' {
		return NewInvalidNameError(name, "name cannot start with '-'")
	}

	// Check for consecutive dots
	if doubleDotRegex.MatchString(name) {
		return NewInvalidNameError(name, "name cannot contain '..'")
	}

	// Check overall pattern
	if !secretNameRegex.MatchString(name) {
		return NewInvalidNameError(name, "name contains invalid characters (allowed: a-z, A-Z, 0-9, -, _, .)")
	}

	return nil
}

// IsValidName returns true if the secret name is valid.
func IsValidName(name string) bool {
	return ValidateName(name) == nil
}
