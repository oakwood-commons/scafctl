// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	scafmcp "github.com/oakwood-commons/scafctl/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_MCPPreviewResolvers_RelativeToSolution exercises the full MCP
// dispatch stack (in-process client -> middleware -> preview_resolvers handler
// -> real file provider) and verifies that a resolver read using
// relativeTo: solution resolves against the solution file's directory rather
// than the MCP server's process CWD (issue #607). The solution lives in a temp
// directory that is not the test process CWD, so a successful read proves the
// solution directory is anchored on the execution context.
func TestIntegration_MCPPreviewResolvers_RelativeToSolution(t *testing.T) {
	const fileContent = "hello from solution dir"

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte(fileContent), 0o644))
	solFile := filepath.Join(tmpDir, "reads-relative.yaml")
	solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reads-relative
  version: 1.0.0
spec:
  resolvers:
    fileData:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: data.txt
              relativeTo: solution
`
	require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

	srv, err := scafmcp.NewServer(scafmcp.WithServerVersion("test"))
	require.NoError(t, err)

	c, err := mcpclient.NewInProcessClient(srv.MCPServer())
	require.NoError(t, err)
	defer c.Close()

	ctx := context.Background()
	_, err = c.Initialize(ctx, mcp.InitializeRequest{})
	require.NoError(t, err)

	req := mcp.CallToolRequest{}
	req.Params.Name = "preview_resolvers"
	req.Params.Arguments = map[string]any{
		"path": solFile,
	}

	result, err := c.CallTool(ctx, req)
	require.NoError(t, err)
	require.False(t, result.IsError, "read with relativeTo: solution should succeed regardless of server CWD")

	require.NotEmpty(t, result.Content, "tool result should include content")
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "first content item should be text, got %T", result.Content[0])
	text := textContent.Text
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &parsed))

	resolvers := parsed["resolvers"].(map[string]any)
	fileData := resolvers["fileData"].(map[string]any)
	assert.Equal(t, "resolved", fileData["status"])
	value := fileData["value"].(map[string]any)
	assert.Equal(t, fileContent, value["content"])
}
