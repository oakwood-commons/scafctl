// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/contextvars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextVarsResponse struct {
	Variables []contextvars.ContextVariable `json:"variables"`
	Phases    []string                      `json:"phases"`
	Phase     string                        `json:"phase"`
	Hint      string                        `json:"hint"`
}

func newConceptTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	return srv
}

func TestHandleListContextVariables(t *testing.T) {
	t.Run("returns all variables when no phase given", func(t *testing.T) {
		srv := newConceptTestServer(t)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_context_variables"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListContextVariables(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		require.NotEmpty(t, result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var resp contextVarsResponse
		require.NoError(t, json.Unmarshal([]byte(text), &resp))

		assert.Equal(t, len(contextvars.List()), len(resp.Variables))
		assert.NotEmpty(t, resp.Phases)
		assert.Empty(t, resp.Phase)
		assert.Contains(t, resp.Hint, "context-variables")
	})

	t.Run("filters by phase", func(t *testing.T) {
		srv := newConceptTestServer(t)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_context_variables"
		request.Params.Arguments = map[string]any{"phase": contextvars.PhaseAction}

		result, err := srv.handleListContextVariables(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var resp contextVarsResponse
		require.NoError(t, json.Unmarshal([]byte(text), &resp))

		require.NotEmpty(t, resp.Variables)
		assert.Equal(t, contextvars.PhaseAction, resp.Phase)
		names := make(map[string]bool)
		for _, v := range resp.Variables {
			assert.Contains(t, v.Phases, contextvars.PhaseAction)
			names[v.Name] = true
		}
		// __execution/__actions/__cwd are action-scoped; __plan (resolve) is not.
		assert.True(t, names["__execution"])
		assert.True(t, names["__actions"])
		assert.True(t, names["__cwd"])
		assert.False(t, names["__plan"], "__plan is resolve-phase, not action")
	})

	t.Run("unknown phase returns structured error", func(t *testing.T) {
		srv := newConceptTestServer(t)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_context_variables"
		request.Params.Arguments = map[string]any{"phase": "no-such-phase"}

		result, err := srv.handleListContextVariables(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

// TestListContextVariables_EnumMatchesRegistry guards against the tool's
// hardcoded phase enum diverging from the phases the registry actually uses.
// Every phase a variable declares must be an accepted enum value, and every
// enum value must return at least one variable.
func TestListContextVariables_EnumMatchesRegistry(t *testing.T) {
	// The enum values accepted by the list_context_variables "phase" argument.
	enumPhases := []string{
		contextvars.PhaseResolve,
		contextvars.PhaseTransform,
		contextvars.PhaseValidate,
		contextvars.PhaseForEach,
		contextvars.PhaseAction,
		contextvars.PhaseStateBackend,
		contextvars.PhaseError,
		contextvars.PhaseTemplateFile,
	}
	enumSet := make(map[string]bool, len(enumPhases))
	for _, p := range enumPhases {
		enumSet[p] = true
	}

	// Every phase the registry actually uses must be an accepted enum value.
	for _, p := range contextvars.Phases() {
		assert.True(t, enumSet[p], "registry phase %q is not an accepted enum value in list_context_variables", p)
	}

	// Every enum value must resolve to at least one variable (no dead options).
	for _, p := range enumPhases {
		assert.NotEmpty(t, contextvars.ByPhase(p), "enum phase %q returns no variables", p)
	}
}

func TestHandleExplainConcepts_ContextCategory(t *testing.T) {
	srv := newConceptTestServer(t)

	request := mcp.CallToolRequest{}
	request.Params.Name = "explain_concepts"
	request.Params.Arguments = map[string]any{"category": "context"}

	result, err := srv.handleExplainConcepts(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text
	for _, name := range []string{
		"context-variables",
		"phase-execution",
		"cel-cost-model",
		"template-dependency-inference",
		"snapshot-masking",
		"authoring-workflow",
	} {
		assert.Contains(t, text, name)
	}
}
