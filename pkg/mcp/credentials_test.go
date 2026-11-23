// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPTools_CredentialsAndRedaction(t *testing.T) {
	store := secrets.NewMockStore()
	const rawSecret = "mcp-top-secret-value-334455"
	store.Data["mcp-secret"] = []byte(rawSecret)
	store.Data["mcp-short-pin"] = []byte("77")

	authReg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	const rawToken = "gho_mcp_live_token_998877"
	gh.GetTokenResult = &auth.Token{
		AccessToken: rawToken,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, authReg.Register(gh))

	// Verify context helper round-trip.
	ctxWithStore := NewContext(WithContextSecretStore(store))
	assert.Equal(t, store, secrets.FromContext(ctxWithStore))

	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)

	srv, err := NewServer(
		WithServerVersion("test"),
		WithServerRegistry(reg),
		WithSecretStore(store),
		WithServerAuthRegistry(authReg),
	)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "mcp-delivered.txt")
	solFile := filepath.Join(tmpDir, "solution.yaml")

	// Workflow actions reference ONLY rslvr: secret_header and numeric_pin (plus a
	// direct tkn: on notify) so both preview placeholder safety and run_solution
	// cross-phase redaction (including coerced int secrets) are exercised.
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-cred-test
  version: "1.0.0"
spec:
  resolvers:
    secret_header:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: mcp-secret
      transform:
        with:
          - provider: cel
            inputs:
              expression: '"Bearer " + __self'
    numeric_pin:
      type: int
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: mcp-short-pin
  workflow:
    actions:
      write_secret:
        provider: file
        inputs:
          operation: write
          path: ` + filepath.ToSlash(outFile) + `
          content:
            tmpl: "{{ .secret_header }}:{{ .numeric_pin }}"
      notify:
        dependsOn:
          - write_secret
        provider: message
        inputs:
          message:
            tkn: github
`
	require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))

	t.Run("preview_resolvers redacts sensitive resolvers and never touches live secret", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "preview_resolvers"
		req.Params.Arguments = map[string]any{"path": solFile}

		res, err := srv.handlePreviewResolvers(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, rawSecret)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		resolvers := parsed["resolvers"].(map[string]any)
		sh := resolvers["secret_header"].(map[string]any)
		assert.Equal(t, "[REDACTED]", sh["value"])
		np := resolvers["numeric_pin"].(map[string]any)
		assert.Equal(t, "[REDACTED]", np["value"])
	})

	t.Run("render_solution scrubs sensitive resolverData and materialized inputs", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "render_solution"
		req.Params.Arguments = map[string]any{"path": solFile}

		res, err := srv.handleRenderSolution(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, rawSecret)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		resData := parsed["resolverData"].(map[string]any)
		assert.Equal(t, "[REDACTED]", resData["secret_header"])
		assert.Equal(t, "[REDACTED]", resData["numeric_pin"])
		graphMap := parsed["graph"].(map[string]any)
		actionsMap := graphMap["actions"].(map[string]any)
		writeSecret := actionsMap["write_secret"].(map[string]any)
		writeInputs := writeSecret["inputs"].(map[string]any)
		assert.Equal(t, "[REDACTED]", writeInputs["content"])
	})

	t.Run("preview_action shows symbolic deferred inputs and placeholders", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "preview_action"
		req.Params.Arguments = map[string]any{"path": solFile}

		res, err := srv.handlePreviewAction(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "tkn:github")
		assert.NotContains(t, text, rawSecret)
		assert.NotContains(t, text, rawToken)
	})

	t.Run("dry_run_solution never touches live credentials or writes files", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "dry_run_solution"
		req.Params.Arguments = map[string]any{"path": solFile, "verbose": true}

		res, err := srv.handleDryRunSolution(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "tkn:github")
		assert.NotContains(t, text, rawSecret)
		assert.NotContains(t, text, rawToken)
		assert.NoFileExists(t, outFile)
	})

	t.Run("run_solution delivers secret to provider while redacting resolverData and action results", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "run_solution"
		req.Params.Arguments = map[string]any{
			"path":           solFile,
			"show_execution": true,
		}

		res, err := srv.handleRunSolution(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError, "unexpected error: %v", res.Content)
		text := res.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, rawSecret)
		assert.NotContains(t, text, rawToken)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		resData := parsed["resolverData"].(map[string]any)
		assert.Equal(t, "[REDACTED]", resData["secret_header"])
		assert.Equal(t, "[REDACTED]", resData["numeric_pin"])

		written, err := os.ReadFile(outFile) //nolint:gosec // test path
		require.NoError(t, err)
		assert.Equal(t, "Bearer "+rawSecret+":77", string(written))
	})

	t.Run("run_solution resolver-only masks sensitive coerced int and short secrets", func(t *testing.T) {
		resOnlyFile := filepath.Join(tmpDir, "resolver-only.yaml")
		resOnlyYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-res-only-cred
  version: "1.0.0"
spec:
  resolvers:
    numeric_pin:
      type: int
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: mcp-short-pin
`
		require.NoError(t, os.WriteFile(resOnlyFile, []byte(resOnlyYAML), 0o600))
		req := mcp.CallToolRequest{}
		req.Params.Name = "run_solution"
		req.Params.Arguments = map[string]any{"path": resOnlyFile}

		res, err := srv.handleRunSolution(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		resData := parsed["resolverData"].(map[string]any)
		assert.Equal(t, "[REDACTED]", resData["numeric_pin"])
	})

	t.Run("run_provider resolves scrt and scrubs returned data", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "run_provider"
		req.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs": map[string]any{
				"value": map[string]any{"scrt": "mcp-secret"},
			},
		}

		res, err := srv.handleRunProvider(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
		text := res.Content[0].(mcp.TextContent).Text
		assert.NotContains(t, text, rawSecret)
		assert.Contains(t, text, "[REDACTED]")
	})

	t.Run("MCP server isolates per-call trackers from parent root tracker and suppresses CI masks", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "true")
		parentTracker := redact.NewTracker()
		parentTracker.Track("parent_global_secret_do_not_bleed")
		parentCtx := redact.WithTracker(context.Background(), parentTracker)

		isolatedSrv, err := NewServer(
			WithServerContext(parentCtx),
			WithServerVersion("test"),
			WithServerRegistry(reg),
			WithSecretStore(store),
			WithServerAuthRegistry(authReg),
		)
		require.NoError(t, err)
		assert.True(t, redact.SuppressGitHubActionsMasksFromContext(isolatedSrv.ctx))

		// Call 1 resolves mcp-secret; parentTracker must NOT accumulate mcp-secret,
		// and Call 2 must NOT redact mcp-secret when passed as an ordinary literal!
		req1 := mcp.CallToolRequest{}
		req1.Params.Name = "run_provider"
		req1.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{"value": map[string]any{"scrt": "mcp-secret"}},
		}
		_, err = isolatedSrv.handleRunProvider(parentCtx, req1)
		require.NoError(t, err)
		assert.False(t, parentTracker.ContainsTracked(rawSecret), "parent root tracker must not accumulate MCP tool secrets")

		req2 := mcp.CallToolRequest{}
		req2.Params.Name = "run_provider"
		req2.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{"value": "harmless parent_global_secret_do_not_bleed literal"},
		}
		res2, err := isolatedSrv.handleRunProvider(parentCtx, req2)
		require.NoError(t, err)
		assert.Contains(t, res2.Content[0].(mcp.TextContent).Text, "parent_global_secret_do_not_bleed")
	})
}
