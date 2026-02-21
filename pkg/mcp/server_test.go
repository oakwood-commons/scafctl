// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
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
		assert.True(t, toolNames["list_solutions"])
		assert.True(t, toolNames["inspect_solution"])
		assert.True(t, toolNames["lint_solution"])
		assert.True(t, toolNames["list_providers"])
		assert.True(t, toolNames["get_provider_schema"])
		assert.True(t, toolNames["list_cel_functions"])
		// Phase 3 tools
		assert.True(t, toolNames["evaluate_cel"])
		assert.True(t, toolNames["render_solution"])
		assert.True(t, toolNames["auth_status"])
		assert.True(t, toolNames["catalog_list"])
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
