// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)
		require.NotNil(t, srv)
		assert.Equal(t, "dev", srv.version)
		assert.NotNil(t, srv.mcpServer)
		assert.NotNil(t, srv.ctx)
	})

	t.Run("with all options", func(t *testing.T) {
		lgr := logr.Discard()
		reg := provider.NewRegistry()
		authReg := auth.NewRegistry()
		cfg := &config.Config{Version: 1}

		srv, err := NewServer(
			WithServerLogger(lgr),
			WithServerRegistry(reg),
			WithServerAuthRegistry(authReg),
			WithServerConfig(cfg),
			WithServerVersion("v1.2.3"),
			WithServerContext(context.Background()),
		)
		require.NoError(t, err)
		require.NotNil(t, srv)
		assert.Equal(t, "v1.2.3", srv.version)
		assert.Equal(t, reg, srv.registry)
		assert.Equal(t, authReg, srv.authReg)
		assert.Equal(t, cfg, srv.config)
	})

	t.Run("with version only", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("v0.1.0"))
		require.NoError(t, err)
		assert.Equal(t, "v0.1.0", srv.version)
	})

	t.Run("default server name", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)
		assert.Equal(t, "scafctl", srv.name)
	})

	t.Run("custom server name", func(t *testing.T) {
		srv, err := NewServer(WithServerName("mycli"))
		require.NoError(t, err)
		assert.Equal(t, "mycli", srv.name)
	})
}

func TestServerInfo_CustomName(t *testing.T) {
	srv, err := NewServer(WithServerName("mycli"), WithServerVersion("v1.0.0"))
	require.NoError(t, err)

	info, err := srv.Info()
	require.NoError(t, err)

	var parsed struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	err = json.Unmarshal(info, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "mycli", parsed.Name)
	assert.Equal(t, "v1.0.0", parsed.Version)
}

func TestServerInfo(t *testing.T) {
	t.Run("returns valid JSON", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("v1.0.0"))
		require.NoError(t, err)

		info, err := srv.Info()
		require.NoError(t, err)
		require.NotEmpty(t, info)

		var parsed struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Tools   []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		err = json.Unmarshal(info, &parsed)
		require.NoError(t, err)
		assert.Equal(t, "scafctl", parsed.Name)
		assert.Equal(t, "v1.0.0", parsed.Version)
	})

	t.Run("tools are registered", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		info, err := srv.Info()
		require.NoError(t, err)

		var parsed struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		err = json.Unmarshal(info, &parsed)
		require.NoError(t, err)
		// Phase 2 tools should be registered
		assert.NotEmpty(t, parsed.Tools)

		toolNames := make(map[string]bool)
		for _, t := range parsed.Tools {
			toolNames[t.Name] = true
		}
		// Always-visible tools
		assert.True(t, toolNames["inspect_solution"])
		assert.True(t, toolNames["lint_solution"])
		assert.True(t, toolNames["list_cel_functions"])
		// Phase 3 tools
		assert.True(t, toolNames["evaluate_cel"])
		assert.True(t, toolNames["render_solution"])
		// Phase 4 tools
		assert.True(t, toolNames["get_solution_schema"])
		assert.True(t, toolNames["explain_kind"])
		assert.True(t, toolNames["list_examples"])
		assert.True(t, toolNames["get_example"])
		// Template & expression tools
		assert.True(t, toolNames["evaluate_go_template"])
		assert.True(t, toolNames["validate_expression"])
		// Lint tools
		assert.True(t, toolNames["explain_lint_rule"])
		// Scaffold tools
		assert.True(t, toolNames["scaffold_solution"])
		// Action tools
		assert.True(t, toolNames["preview_action"])
		// Diff tools
		assert.True(t, toolNames["diff_solution"])

		// Contextually-filtered tools should NOT appear without deps
		assert.False(t, toolNames["list_solutions"], "list_solutions requires catalogs")
		assert.False(t, toolNames["catalog_list"], "catalog_list requires catalogs")
		assert.False(t, toolNames["catalog_inspect"], "catalog_inspect requires catalogs")
		assert.False(t, toolNames["auth_status"], "auth_status requires auth handlers")
		assert.False(t, toolNames["list_auth_handlers"], "list_auth_handlers requires auth handlers")
		assert.False(t, toolNames["list_providers"], "list_providers requires registry")
		assert.False(t, toolNames["get_provider_schema"], "get_provider_schema requires registry")
	})

	t.Run("filtered tools visible with dependencies", func(t *testing.T) {
		reg := provider.NewRegistry()
		authReg := auth.NewRegistry()
		cfg := &config.Config{
			Version:  1,
			Catalogs: []config.CatalogConfig{{Name: "test", URL: "https://example.com"}},
		}

		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerAuthRegistry(authReg),
			WithServerConfig(cfg),
		)
		require.NoError(t, err)

		info, err := srv.Info()
		require.NoError(t, err)

		var parsed struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		err = json.Unmarshal(info, &parsed)
		require.NoError(t, err)

		toolNames := make(map[string]bool)
		for _, tool := range parsed.Tools {
			toolNames[tool.Name] = true
		}
		assert.True(t, toolNames["list_solutions"], "list_solutions should be visible with catalogs")
		assert.True(t, toolNames["catalog_list"], "catalog_list should be visible with catalogs")
		assert.True(t, toolNames["catalog_inspect"], "catalog_inspect should be visible with catalogs")
		assert.True(t, toolNames["list_providers"], "list_providers should be visible with registry")
		assert.True(t, toolNames["get_provider_schema"], "get_provider_schema should be visible with registry")
	})
}

func TestMergeContext(t *testing.T) {
	t.Run("values from values context are accessible", func(t *testing.T) {
		type key struct{}
		values := context.WithValue(context.Background(), key{}, "hello")
		parent := context.Background()

		merged := mergeContext(parent, values)
		assert.Equal(t, "hello", merged.Value(key{}))
	})

	t.Run("parent cancellation propagates", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		values := context.Background()

		merged := mergeContext(parent, values)

		cancel()
		select {
		case <-merged.Done():
			// expected
		default:
			t.Error("expected merged context to be cancelled")
		}
	})

	t.Run("values context does not override parent cancellation", func(t *testing.T) {
		parent := context.Background()
		type key struct{}
		values := context.WithValue(context.Background(), key{}, "value")

		merged := mergeContext(parent, values)
		assert.Equal(t, "value", merged.Value(key{}))
		// Parent is not cancelled, so merged should not be done
		select {
		case <-merged.Done():
			t.Error("context should not be done")
		default:
			// expected
		}
	})
}

func TestBuildInstructions(t *testing.T) {
	t.Run("empty supplemental returns base only", func(t *testing.T) {
		result := buildInstructions(settings.CliBinaryName, "")
		assert.Equal(t, serverInstructionsTemplate, result)
	})

	t.Run("supplemental is appended", func(t *testing.T) {
		supplemental := "Use domain tools for legacy files."
		result := buildInstructions(settings.CliBinaryName, supplemental)
		assert.Contains(t, result, serverInstructionsTemplate)
		assert.Contains(t, result, supplemental)
		assert.True(t, strings.HasSuffix(result, supplemental))
		assert.Contains(t, result, "\n\n"+supplemental)
	})

	t.Run("binary name substitution in supplemental", func(t *testing.T) {
		supplemental := "Run " + settings.CliBinaryName + " solve to execute."
		result := buildInstructions("mycli", supplemental)
		assert.Contains(t, result, "Run mycli solve to execute.")
		assert.NotContains(t, result, "Run "+settings.CliBinaryName+" solve to execute.")
	})

	t.Run("no substitution when using default name", func(t *testing.T) {
		supplemental := "Run " + settings.CliBinaryName + " solve to execute."
		result := buildInstructions(settings.CliBinaryName, supplemental)
		assert.Contains(t, result, "Run "+settings.CliBinaryName+" solve to execute.")
	})
}

func TestWithSupplementalInstructions(t *testing.T) {
	t.Run("sets field on config", func(t *testing.T) {
		cfg := &serverConfig{}
		opt := WithSupplementalInstructions("extra guidance")
		opt(cfg)
		assert.Equal(t, "extra guidance", cfg.supplementalInstructions)
	})

	t.Run("server created with supplemental instructions", func(t *testing.T) {
		supplemental := "Use migration tools for legacy solutions."
		srv, err := NewServer(
			WithServerName("mycli"),
			WithSupplementalInstructions(supplemental),
		)
		require.NoError(t, err)
		require.NotNil(t, srv)
		assert.Equal(t, "mycli", srv.name)
	})
}

func TestServerContextPropagation(t *testing.T) {
	t.Run("mergeContext propagates values from server context", func(t *testing.T) {
		authReg := auth.NewRegistry()
		cfg := &config.Config{Version: 42}

		srv, err := NewServer(
			WithServerAuthRegistry(authReg),
			WithServerConfig(cfg),
			WithServerName("testcli"),
		)
		require.NoError(t, err)

		// Simulate what the transport context func does: merge a fresh
		// background context (as mcp-go would create) with the server context.
		transportCtx := context.Background()
		merged := mergeContext(transportCtx, srv.ctx)

		// Auth registry must be available — this is the core bug fix.
		gotAuth := auth.RegistryFromContext(merged)
		require.NotNil(t, gotAuth, "auth registry must be present in merged context")
		assert.Equal(t, authReg, gotAuth)

		// Config must be available.
		gotCfg := config.FromContext(merged)
		require.NotNil(t, gotCfg)
		assert.Equal(t, 42, gotCfg.Version)

		// Settings must be available with the configured binary name.
		gotSettings, ok := settings.FromContext(merged)
		require.True(t, ok)
		assert.Equal(t, "testcli", gotSettings.BinaryName)
	})

	t.Run("mergeContext prefers values context over parent", func(t *testing.T) {
		parentReg := auth.NewRegistry()
		valuesReg := auth.NewRegistry()

		parentCtx := auth.WithRegistry(context.Background(), parentReg)
		valuesCtx := auth.WithRegistry(context.Background(), valuesReg)

		merged := mergeContext(parentCtx, valuesCtx)
		got := auth.RegistryFromContext(merged)
		assert.Equal(t, valuesReg, got, "values context should take precedence")
	})

	t.Run("mergeContext falls back to parent for missing values", func(t *testing.T) {
		parentReg := auth.NewRegistry()
		parentCtx := auth.WithRegistry(context.Background(), parentReg)
		valuesCtx := context.Background() // no auth registry

		merged := mergeContext(parentCtx, valuesCtx)
		got := auth.RegistryFromContext(merged)
		assert.Equal(t, parentReg, got, "should fall back to parent context")
	})

	t.Run("embedder tool receives auth from merged context", func(t *testing.T) {
		authReg := auth.NewRegistry()

		srv, err := NewServer(
			WithServerAuthRegistry(authReg),
			WithServerName("embeddercli"),
		)
		require.NoError(t, err)

		// Simulate an embedder-registered tool handler receiving the
		// transport context after mergeContext is applied.
		handlerCtx := mergeContext(context.Background(), srv.ctx)
		got := auth.RegistryFromContext(handlerCtx)
		require.NotNil(t, got, "embedder tool must see auth registry in handler ctx")
		assert.Equal(t, authReg, got)
	})
}

// ── Server option coverage ──────────────────────────────────────────────────

func TestWithPaginationLimit(t *testing.T) {
	srv, err := NewServer(WithPaginationLimit(50))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestWithWorkerPoolSize(t *testing.T) {
	srv, err := NewServer(WithWorkerPoolSize(4))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestWithQueueSize(t *testing.T) {
	srv, err := NewServer(WithQueueSize(100))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestWithErrorLog(t *testing.T) {
	lgr := log.Default()
	srv, err := NewServer(WithErrorLog(lgr))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// ── MCPServer accessor ──────────────────────────────────────────────────────

func TestMCPServer(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	assert.NotNil(t, srv.MCPServer(), "MCPServer() should return the underlying server")
	assert.Equal(t, srv.mcpServer, srv.MCPServer())
}

// ── Handler tests ────────────────────────────────────────────────────────────

func TestHandler_ReturnsHTTPHandler(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	h := srv.Handler()
	require.NotNil(t, h, "Handler() should return an http.Handler")
	assert.NotNil(t, srv.httpServer, "httpServer should be initialized after Handler()")
}

func TestHandler_ReturnsCachedInstance(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	h1 := srv.Handler()
	h2 := srv.Handler()
	assert.Equal(t, h1, h2, "Handler() should return the same instance on subsequent calls")
}
