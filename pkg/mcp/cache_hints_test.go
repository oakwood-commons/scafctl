// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// testHTTPClient is shared by the cache-hint tests; a single client is fine
// because each test targets its own ephemeral httptest server.
var testHTTPClient = &http.Client{}

// postStatelessRequest issues a stateless JSON-RPC request against the
// Streamable HTTP handler, tagging the request with the given protocol
// version the way modern-era clients do (via _meta). extraParams, when
// non-nil, is merged into the request params.
func postStatelessRequest(t *testing.T, url string, method mcp.MCPMethod, protocolVersion string, extraParams map[string]any) map[string]any {
	t.Helper()

	params := map[string]any{}
	for k, v := range extraParams {
		params[k] = v
	}
	params["_meta"] = map[string]any{
		mcp.MetaKeyProtocolVersion: protocolVersion,
		mcp.MetaKeyClientInfo: map[string]any{
			"name":    "cache-hints-test",
			"version": "1.0.0",
		},
		mcp.MetaKeyClientCapabilities: map[string]any{},
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcp.HeaderProtocolVersion, protocolVersion)
	req.Header.Set(mcp.HeaderMethod, string(method))
	// SEP-2516: stateless requests address the target (e.g. a resource URI)
	// via the Mcp-Name header instead of a session.
	rawParams, err := json.Marshal(params)
	require.NoError(t, err)
	if name, ok := mcp.ExtractHeaderName(method, rawParams); ok {
		encoded, ok := mcp.EncodeHeaderValue(name)
		require.True(t, ok)
		req.Header.Set(mcp.HeaderName, encoded)
	}

	resp, err := testHTTPClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var message map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&message))
	return message
}

// resultOf extracts and returns the result object of a decoded JSON-RPC
// response, failing the test if the response carries an error instead.
func resultOf(t *testing.T, message map[string]any) map[string]any {
	t.Helper()

	require.Nil(t, message["error"], "unexpected JSON-RPC error in response: %v", message)
	result, ok := message["result"].(map[string]any)
	require.True(t, ok, "expected a result, got %v", message)
	return result
}

// TestMethodCacheHints_Modern verifies that list responses to modern-era
// clients carry the SEP-2549 cache hints configured in NewServer: a 5-minute
// freshness hint with private scope, telling clients they may reuse the
// static catalog. resources/read must keep the SDK's fail-closed default
// (ttlMs 0 = revalidate every time) because resource contents change on
// disk.
func TestMethodCacheHints_Modern(t *testing.T) {
	srv, err := NewServer(
		WithServerLogger(logr.Discard()),
		WithServerRegistry(provider.NewRegistry()),
	)
	require.NoError(t, err)
	defer srv.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tests := []struct {
		name        string
		method      mcp.MCPMethod
		expectedTTL float64
		params      map[string]any
	}{
		{"tools/list", mcp.MethodToolsList, float64(listCacheTTLMs), nil},
		{"prompts/list", mcp.MethodPromptsList, float64(listCacheTTLMs), nil},
		{"resources/list", mcp.MethodResourcesList, float64(listCacheTTLMs), nil},
		{"resources/templates/list", mcp.MethodResourcesTemplatesList, float64(listCacheTTLMs), nil},
		{"resources/read keeps fail-closed default", mcp.MethodResourcesRead, 0, map[string]any{"uri": "provider://reference"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := postStatelessRequest(t, ts.URL, tt.method, mcp.ProtocolVersion20260728, tt.params)

			result := resultOf(t, message)
			require.Contains(t, result, "ttlMs", "SEP-2549 requires ttlMs on modern-era results")
			assert.Equal(t, tt.expectedTTL, result["ttlMs"])
			assert.Equal(t, string(mcp.CacheScopePrivate), result["cacheScope"])
		})
	}
}

// TestMethodCacheHints_LegacyAbsent verifies that a legacy-era session
// (initialize handshake, 2025-11-25 protocol version) gets undecorated
// results: cache hints must not leak into legacy-era responses.
func TestMethodCacheHints_LegacyAbsent(t *testing.T) {
	srv, err := NewServer(WithServerLogger(logr.Discard()))
	require.NoError(t, err)
	defer srv.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// initialize handshake, the way a pre-2026-07-28 client opens a session.
	initBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_LEGACY_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "cache-hints-test",
				"version": "1.0.0",
			},
		},
	})
	require.NoError(t, err)

	initReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL, bytes.NewReader(initBody))
	require.NoError(t, err)
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")

	initResp, err := testHTTPClient.Do(initReq)
	require.NoError(t, err)
	t.Cleanup(func() { _ = initResp.Body.Close() })
	require.Equal(t, http.StatusOK, initResp.StatusCode)

	sessionID := initResp.Header.Get(mcp.HeaderSessionID)
	require.NotEmpty(t, sessionID, "legacy session must be assigned a session ID")

	var initMessage map[string]any
	require.NoError(t, json.NewDecoder(initResp.Body).Decode(&initMessage))
	resultOf(t, initMessage) // assert initialize succeeded, not a JSON-RPC error

	// tools/list over the legacy session (no _meta, no protocol headers).
	listBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  mcp.MethodToolsList,
		"params":  map[string]any{},
	})
	require.NoError(t, err)

	listReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL, bytes.NewReader(listBody))
	require.NoError(t, err)
	listReq.Header.Set("Content-Type", "application/json")
	listReq.Header.Set("Accept", "application/json, text/event-stream")
	listReq.Header.Set(mcp.HeaderSessionID, sessionID)

	listResp, err := testHTTPClient.Do(listReq)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listResp.Body.Close() })
	require.Equal(t, http.StatusOK, listResp.StatusCode)

	var listMessage map[string]any
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&listMessage))
	result := resultOf(t, listMessage)
	assert.NotContains(t, result, "ttlMs")
	assert.NotContains(t, result, "cacheScope")
}
