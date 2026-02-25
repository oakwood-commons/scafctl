// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsInternalSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "internal auth refresh token", input: "scafctl.auth.entra.refresh_token", expected: true},
		{name: "internal auth metadata", input: "scafctl.auth.entra.metadata", expected: true},
		{name: "internal auth token prefix", input: "scafctl.auth.entra.token.abc123", expected: true},
		{name: "scafctl prefix only", input: "scafctl.", expected: true},
		{name: "user secret", input: "my-secret", expected: false},
		{name: "user secret with dots", input: "my.secret.name", expected: false},
		{name: "empty string", input: "", expected: false},
		{name: "partial prefix", input: "scafct.foo", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, IsInternalSecret(tt.input))
		})
	}
}

func TestSecretType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "internal", SecretType("scafctl.auth.entra.refresh_token"))
	assert.Equal(t, "internal", SecretType("scafctl.something"))
	assert.Equal(t, "user", SecretType("my-secret"))
	assert.Equal(t, "user", SecretType(""))
}

func TestValidateSecretName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		secretName      string
		includeInternal bool
		wantErr         bool
	}{
		{name: "user secret without all", secretName: "my-secret", includeInternal: false, wantErr: false},
		{name: "user secret with all", secretName: "my-secret", includeInternal: true, wantErr: false},
		{name: "internal secret without all", secretName: "scafctl.auth.token", includeInternal: false, wantErr: true},
		{name: "internal secret with all", secretName: "scafctl.auth.token", includeInternal: true, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSecretName(tt.secretName, tt.includeInternal)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "--all")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateUserSecretName(t *testing.T) {
	t.Parallel()

	err := ValidateUserSecretName("scafctl.auth.entra.refresh_token")
	assert.Error(t, err)

	err = ValidateUserSecretName("my-secret")
	assert.NoError(t, err)
}

func TestFilterSecrets(t *testing.T) {
	t.Parallel()

	names := []string{
		"my-secret",
		"scafctl.auth.entra.refresh_token",
		"another-secret",
		"scafctl.auth.entra.metadata",
	}

	filtered := FilterSecrets(names, false)
	assert.Equal(t, []string{"my-secret", "another-secret"}, filtered)

	filtered = FilterSecrets(names, true)
	assert.Equal(t, names, filtered)
}

func TestFilterUserSecrets(t *testing.T) {
	t.Parallel()

	names := []string{
		"my-secret",
		"scafctl.auth.entra.refresh_token",
		"another-secret",
		"scafctl.auth.entra.metadata",
	}

	filtered := FilterUserSecrets(names)
	assert.Equal(t, []string{"my-secret", "another-secret"}, filtered)
}

func TestFilterUserSecrets_EmptyList(t *testing.T) {
	t.Parallel()

	filtered := FilterUserSecrets([]string{})
	assert.Empty(t, filtered)
}

func TestFilterUserSecrets_AllInternal(t *testing.T) {
	t.Parallel()

	names := []string{
		"scafctl.auth.entra.refresh_token",
		"scafctl.auth.entra.metadata",
	}

	filtered := FilterUserSecrets(names)
	assert.Empty(t, filtered)
}
