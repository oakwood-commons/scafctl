// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetOpenAPISpec(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_openapi_spec"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleGetOpenAPISpec(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text

	var spec map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &spec))
	assert.Contains(t, spec, "openapi")
	assert.Contains(t, spec, "paths")
	assert.Contains(t, spec, "info")
}

func TestHandleListAPIEndpoints(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "list_api_endpoints"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleListAPIEndpoints(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text

	var body struct {
		Count     int `json:"count"`
		Endpoints []struct {
			Method  string   `json:"method"`
			Path    string   `json:"path"`
			Summary string   `json:"summary"`
			Tags    []string `json:"tags,omitempty"`
		} `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &body))
	assert.Greater(t, body.Count, 0, "expected at least one endpoint")
	assert.Len(t, body.Endpoints, body.Count)
}

func TestHandleListAPIEndpoints_HasExpectedPaths(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "list_api_endpoints"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleListAPIEndpoints(context.Background(), request)
	require.NoError(t, err)

	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "/health")
	assert.Contains(t, text, "providers")
	assert.Contains(t, text, "solutions")
}

func BenchmarkHandleGetOpenAPISpec(b *testing.B) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(b, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_openapi_spec"
	request.Params.Arguments = map[string]any{}

	for b.Loop() {
		_, _ = srv.handleGetOpenAPISpec(context.Background(), request)
	}
}

func BenchmarkHandleListAPIEndpoints(b *testing.B) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(b, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "list_api_endpoints"
	request.Params.Arguments = map[string]any{}

	for b.Loop() {
		_, _ = srv.handleListAPIEndpoints(context.Background(), request)
	}
}
