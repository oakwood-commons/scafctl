// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCapabilities(t *testing.T) {
	t.Run("returns tools and prompts", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		caps := srv.ListCapabilities()
		require.NotEmpty(t, caps)

		var tools, prompts int
		for _, c := range caps {
			switch c.Kind {
			case CapabilityTool:
				tools++
			case CapabilityPrompt:
				prompts++
			}
		}

		assert.Greater(t, tools, 0, "should have at least one tool")
		assert.Greater(t, prompts, 0, "should have at least one prompt")
	})

	t.Run("all capabilities have source core", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		for _, c := range srv.ListCapabilities() {
			assert.Equal(t, SourceCore, c.Source, "capability %s should be core", c.Name)
		}
	})

	t.Run("sorted by kind then name", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		caps := srv.ListCapabilities()
		for i := 1; i < len(caps); i++ {
			if caps[i].Kind == caps[i-1].Kind {
				assert.LessOrEqual(t, caps[i-1].Name, caps[i].Name,
					"capabilities should be sorted by name within kind")
			} else {
				assert.Less(t, string(caps[i-1].Kind), string(caps[i].Kind),
					"tool kind should sort before prompt kind")
			}
		}
	})

	t.Run("prompts are tracked via addPrompt", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		caps := srv.ListCapabilities()
		var promptNames []string
		for _, c := range caps {
			if c.Kind == CapabilityPrompt {
				promptNames = append(promptNames, c.Name)
			}
		}

		assert.Contains(t, promptNames, "create_solution")
		assert.Contains(t, promptNames, "debug_solution")
		assert.Contains(t, promptNames, "add_resolver")
	})

	t.Run("tool annotations are populated", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		caps := srv.ListCapabilities()
		// Find a known read-only tool
		for _, c := range caps {
			if c.Kind == CapabilityTool && c.Name == "evaluate_cel" {
				assert.True(t, c.ReadOnly, "evaluate_cel should be read-only")
				assert.False(t, c.Destructive, "evaluate_cel should not be destructive")
				return
			}
		}
		t.Fatal("evaluate_cel tool not found")
	})

	t.Run("non-default binary name", func(t *testing.T) {
		srv, err := NewServer(WithServerName("mycli"))
		require.NoError(t, err)

		caps := srv.ListCapabilities()
		require.NotEmpty(t, caps, "should still list capabilities with custom binary name")
	})
}

func TestListCapabilities_EmbedderTools(t *testing.T) {
	t.Run("embedder tools via MCPServer are tagged as plugin", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		// Simulate an embedder adding a tool directly via MCPServer()
		srv.MCPServer().AddTool(mcp.NewTool("mycli_deploy",
			mcp.WithDescription("Deploy the application"),
		), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		})

		caps := srv.ListCapabilities()

		var found bool
		for _, c := range caps {
			if c.Name == "mycli_deploy" {
				found = true
				assert.Equal(t, SourcePlugin, c.Source, "embedder tool should be plugin")
				assert.Equal(t, CapabilityTool, c.Kind)
				break
			}
		}
		assert.True(t, found, "embedder tool should appear in capabilities")
	})

	t.Run("core tools remain tagged as core alongside embedder tools", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		srv.MCPServer().AddTool(mcp.NewTool("mycli_custom",
			mcp.WithDescription("Custom embedder tool"),
		), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("ok"), nil
		})

		caps := srv.ListCapabilities()

		var coreCount, pluginCount int
		for _, c := range caps {
			if c.Kind == CapabilityTool {
				switch c.Source {
				case SourceCore:
					coreCount++
				case SourcePlugin:
					pluginCount++
				}
			}
		}
		assert.Greater(t, coreCount, 0, "should still have core tools")
		assert.Equal(t, 1, pluginCount, "should have exactly one plugin tool")
	})
}

func TestListCapabilities_EmbedderPrompts(t *testing.T) {
	t.Run("embedder prompts via AddPrompt are tagged as plugin", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		// Simulate an embedder adding a prompt via the public API
		srv.AddPrompt(mcp.NewPrompt("mycli_onboard",
			mcp.WithPromptDescription("Onboard a new user"),
		), func(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{}, nil
		})

		caps := srv.ListCapabilities()

		var found bool
		for _, c := range caps {
			if c.Name == "mycli_onboard" {
				found = true
				assert.Equal(t, SourcePlugin, c.Source, "embedder prompt should be plugin")
				assert.Equal(t, CapabilityPrompt, c.Kind)
				break
			}
		}
		assert.True(t, found, "embedder prompt should appear in capabilities")
	})

	t.Run("core prompts remain tagged as core alongside embedder prompts", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		srv.AddPrompt(mcp.NewPrompt("mycli_custom_prompt",
			mcp.WithPromptDescription("Custom embedder prompt"),
		), func(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{}, nil
		})

		caps := srv.ListCapabilities()

		var corePrompts, pluginPrompts int
		for _, c := range caps {
			if c.Kind == CapabilityPrompt {
				switch c.Source {
				case SourceCore:
					corePrompts++
				case SourcePlugin:
					pluginPrompts++
				}
			}
		}
		assert.Greater(t, corePrompts, 0, "should still have core prompts")
		assert.Equal(t, 1, pluginPrompts, "should have exactly one plugin prompt")
	})
}
