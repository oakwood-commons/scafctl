// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetConfig(t *testing.T) {
	cfg := &config.Config{
		Version: 1,
		Settings: config.Settings{
			DefaultCatalog: "local",
			NoColor:        false,
			Quiet:          false,
		},
		Logging: config.LoggingConfig{
			Level:  "none",
			Format: "console",
		},
		Catalogs: []config.CatalogConfig{
			{
				Name: "local",
				Type: "filesystem",
				Path: "/tmp/catalogs",
			},
		},
		Resolver: config.ResolverConfig{
			Timeout:      "30s",
			PhaseTimeout: "5m",
		},
		Auth: config.GlobalAuthConfig{
			GCP: &config.GCPAuthConfig{
				ClientID:     "my-client-id",
				ClientSecret: "super-secret-value",
				Project:      "my-project",
			},
		},
	}

	srv, err := NewServer(
		WithServerVersion("test"),
		WithServerConfig(cfg),
	)
	require.NoError(t, err)

	t.Run("full config", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_config"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetConfig(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output sanitizedConfig
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		assert.Equal(t, 1, output.Version)
		assert.Equal(t, "local", output.Settings.DefaultCatalog)
		assert.Len(t, output.Catalogs, 1)
		assert.Equal(t, "local", output.Catalogs[0].Name)
	})

	t.Run("specific section", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_config"
		request.Params.Arguments = map[string]any{
			"section": "settings",
		}

		result, err := srv.handleGetConfig(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "settings", output["section"])

		data := output["data"].(map[string]any)
		assert.Equal(t, "local", data["defaultCatalog"])
	})

	t.Run("invalid section", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_config"
		request.Params.Arguments = map[string]any{
			"section": "nonexistent",
		}

		result, err := srv.handleGetConfig(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "nonexistent")
	})

	t.Run("sensitive fields are redacted", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_config"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetConfig(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, "super-secret-value")
		assert.Contains(t, text, redactedValue)
		// Client ID should still be visible
		assert.Contains(t, text, "my-client-id")
	})

	t.Run("no config available", func(t *testing.T) {
		noConfigSrv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_config"
		request.Params.Arguments = map[string]any{}

		// This may succeed with global or fail gracefully
		result, err := noConfigSrv.handleGetConfig(context.Background(), request)
		require.NoError(t, err)
		// Either way, it should not panic
		assert.NotNil(t, result)
	})
}

func TestSanitizeConfig(t *testing.T) {
	t.Run("redacts GCP client secret", func(t *testing.T) {
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GCP: &config.GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "should-be-hidden",
					Project:      "my-proj",
				},
			},
		}

		sanitized := sanitizeConfig(cfg)
		assert.Equal(t, "visible-id", sanitized.Auth.GCP.ClientID)
		assert.Equal(t, redactedValue, sanitized.Auth.GCP.GCPClientCredential)
		assert.Equal(t, "my-proj", sanitized.Auth.GCP.Project)
	})

	t.Run("empty secret not redacted", func(t *testing.T) {
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GCP: &config.GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "",
				},
			},
		}

		sanitized := sanitizeConfig(cfg)
		assert.Equal(t, "", sanitized.Auth.GCP.GCPClientCredential)
	})

	t.Run("nil auth sections", func(t *testing.T) {
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{},
		}

		sanitized := sanitizeConfig(cfg)
		assert.Nil(t, sanitized.Auth.Entra)
		assert.Nil(t, sanitized.Auth.GitHub)
		assert.Nil(t, sanitized.Auth.GCP)
	})

	t.Run("catalogs with auth", func(t *testing.T) {
		cfg := &config.Config{
			Catalogs: []config.CatalogConfig{
				{
					Name: "remote",
					Type: "oci",
					URL:  "https://registry.example.com",
					Auth: &config.AuthConfig{
						Type:        "token",
						TokenEnvVar: "MY_TOKEN",
					},
				},
			},
		}

		sanitized := sanitizeConfig(cfg)
		require.Len(t, sanitized.Catalogs, 1)
		assert.Equal(t, "remote", sanitized.Catalogs[0].Name)
		require.NotNil(t, sanitized.Catalogs[0].Auth)
		assert.Equal(t, "token", sanitized.Catalogs[0].Auth.Type)
		assert.Equal(t, "MY_TOKEN", sanitized.Catalogs[0].Auth.TokenEnvVar)
	})
}
