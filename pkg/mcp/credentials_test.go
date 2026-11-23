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
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPTools_CredentialsAndRedaction(t *testing.T) {
	store := secrets.NewMockStore()
	const rawSecret = "mcp-top-secret-value-334455"
	store.Data["mcp-secret"] = []byte(rawSecret)

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

	// Workflow actions reference ONLY rslvr: secret_header (plus a direct scrt: on
	// notify) so both preview placeholder safety and run_solution cross-phase
	// redaction are exercised.
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
  workflow:
    actions:
      write_secret:
        provider: file
        inputs:
          operation: write
          path: ` + filepath.ToSlash(outFile) + `
          content:
            rslvr: secret_header
      notify:
        dependsOn:
          - write_secret
        provider: message
        inputs:
          message:
            tkn: github
`
	require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))

	t.Run("preview_resolvers uses placeholder and never leaks live secret", func(t *testing.T) {
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
		assert.Equal(t, "Bearer <redacted:scrt:mcp-secret>", sh["value"])
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

		written, err := os.ReadFile(outFile) //nolint:gosec // test path
		require.NoError(t, err)
		assert.Equal(t, "Bearer "+rawSecret, string(written))
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
}
