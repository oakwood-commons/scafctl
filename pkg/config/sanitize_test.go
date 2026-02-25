// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeConfig(t *testing.T) {
	t.Run("redacts GCP client secret", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				GCP: &GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "should-be-hidden",
					Project:      "my-proj",
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "visible-id", sanitized.Auth.GCP.ClientID)
		assert.Equal(t, RedactedValue, sanitized.Auth.GCP.GCPClientCredential)
		assert.Equal(t, "my-proj", sanitized.Auth.GCP.Project)
	})

	t.Run("empty secret not redacted", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				GCP: &GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "",
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "", sanitized.Auth.GCP.GCPClientCredential)
	})

	t.Run("nil auth sections", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Nil(t, sanitized.Auth.Entra)
		assert.Nil(t, sanitized.Auth.GitHub)
		assert.Nil(t, sanitized.Auth.GCP)
	})

	t.Run("catalogs with auth", func(t *testing.T) {
		cfg := &Config{
			Catalogs: []CatalogConfig{
				{
					Name: "remote",
					Type: "oci",
					URL:  "https://registry.example.com",
					Auth: &AuthConfig{
						Type:        "token",
						TokenEnvVar: "MY_TOKEN",
					},
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		require.Len(t, sanitized.Catalogs, 1)
		assert.Equal(t, "remote", sanitized.Catalogs[0].Name)
		require.NotNil(t, sanitized.Catalogs[0].Auth)
		assert.Equal(t, "token", sanitized.Catalogs[0].Auth.Type)
		assert.Equal(t, "MY_TOKEN", sanitized.Catalogs[0].Auth.TokenEnvVar)
	})
}
