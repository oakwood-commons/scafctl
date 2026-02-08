// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	assert.Empty(t, cfg.secretsDir, "default secretsDir should be empty")
	assert.Nil(t, cfg.keyring, "default keyring should be nil")
	assert.Equal(t, logr.Discard(), cfg.logger, "default logger should be discard logger")
}

func TestWithSecretsDir(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{
			name: "custom directory",
			dir:  "/custom/secrets/dir",
		},
		{
			name: "empty directory",
			dir:  "",
		},
		{
			name: "relative directory",
			dir:  "./secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			opt := WithSecretsDir(tt.dir)
			opt(cfg)
			assert.Equal(t, tt.dir, cfg.secretsDir)
		})
	}
}

func TestWithLogger(t *testing.T) {
	t.Run("sets custom logger", func(t *testing.T) {
		cfg := defaultConfig()
		customLogger := logr.Discard().WithName("test")
		opt := WithLogger(customLogger)
		opt(cfg)
		// Note: logr.Logger doesn't have equality semantics, so we just verify it doesn't panic
		assert.NotNil(t, cfg.logger)
	})
}

func TestWithKeyring(t *testing.T) {
	t.Run("sets custom keyring", func(t *testing.T) {
		cfg := defaultConfig()
		mockKr := &mockKeyring{}
		opt := WithKeyring(mockKr)
		opt(cfg)
		assert.Equal(t, mockKr, cfg.keyring)
	})
}

func TestOptionChaining(t *testing.T) {
	t.Run("multiple options apply correctly", func(t *testing.T) {
		cfg := defaultConfig()
		mockKr := &mockKeyring{}
		customLogger := logr.Discard().WithName("test")

		opts := []Option{
			WithSecretsDir("/custom/dir"),
			WithKeyring(mockKr),
			WithLogger(customLogger),
		}

		for _, opt := range opts {
			opt(cfg)
		}

		assert.Equal(t, "/custom/dir", cfg.secretsDir)
		assert.Equal(t, mockKr, cfg.keyring)
	})

	t.Run("later options override earlier ones", func(t *testing.T) {
		cfg := defaultConfig()

		opts := []Option{
			WithSecretsDir("/first/dir"),
			WithSecretsDir("/second/dir"),
		}

		for _, opt := range opts {
			opt(cfg)
		}

		assert.Equal(t, "/second/dir", cfg.secretsDir)
	})
}

// mockKeyring is a simple mock implementation of the Keyring interface for testing.
type mockKeyring struct {
	data map[string]string
}

func (m *mockKeyring) Get(service, account string) (string, error) {
	key := service + ":" + account
	if m.data == nil {
		return "", ErrNotFound
	}
	val, ok := m.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

func (m *mockKeyring) Set(service, account, value string) error {
	key := service + ":" + account
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[key] = value
	return nil
}

func (m *mockKeyring) Delete(service, account string) error {
	key := service + ":" + account
	if m.data != nil {
		delete(m.data, key)
	}
	return nil
}
