// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretRef_Validate_ValidEnv(t *testing.T) {
	t.Parallel()
	s := SecretRef("env://MY_SECRET")
	assert.NoError(t, s.Validate())
}

func TestSecretRef_Validate_ValidFile(t *testing.T) {
	t.Parallel()
	s := SecretRef("file:///run/secrets/token")
	assert.NoError(t, s.Validate())
}

func TestSecretRef_Validate_EmptyEnvName(t *testing.T) {
	t.Parallel()
	s := SecretRef("env://")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a variable name")
}

func TestSecretRef_Validate_EmptyFilePath(t *testing.T) {
	t.Parallel()
	s := SecretRef("file://")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a path")
}

func TestSecretRef_Validate_UnknownScheme(t *testing.T) {
	t.Parallel()
	s := SecretRef("vault://secret/data/foo")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported secret ref scheme")
}

func TestSecretRef_Validate_NoScheme(t *testing.T) {
	t.Parallel()
	s := SecretRef("just-a-string")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported secret ref scheme")
}

func TestSecretRef_Resolve_Env(t *testing.T) {
	t.Setenv("TEST_SECRET_REF_VAL", "s3cr3t")
	s := SecretRef("env://TEST_SECRET_REF_VAL")
	val, err := s.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", val)
}

func TestSecretRef_Resolve_EnvEmpty(t *testing.T) {
	t.Setenv("TEST_SECRET_REF_EMPTY", "")
	s := SecretRef("env://TEST_SECRET_REF_EMPTY")
	_, err := s.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty or not set")
}

func TestSecretRef_Resolve_FileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(path, []byte("file-secret\n"), 0o600))
	s := SecretRef("file://" + path)
	val, err := s.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "file-secret", val)
}

func TestSecretRef_Resolve_FileMissing(t *testing.T) {
	t.Parallel()
	s := SecretRef("file:///nonexistent/path/secret")
	_, err := s.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading secret file")
}

func TestSecretRef_Scheme(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "env", SecretRef("env://FOO").Scheme())
	assert.Equal(t, "file", SecretRef("file:///bar").Scheme())
	assert.Equal(t, "", SecretRef("noscheme").Scheme())
}
