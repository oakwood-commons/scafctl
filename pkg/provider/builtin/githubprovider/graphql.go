// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// graphqlRequest is the JSON body sent to the GitHub GraphQL API.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphqlResponse is the top-level JSON response from the GitHub GraphQL API.
type graphqlResponse struct {
	Data   map[string]any `json:"data"`
	Errors []graphqlError `json:"errors,omitempty"`
}

// graphqlError represents a single error returned by the GraphQL API.
type graphqlError struct {
	Message   string         `json:"message"`
	Type      string         `json:"type,omitempty"`
	Path      []any          `json:"path,omitempty"`
	Locations []gqlLocation  `json:"locations,omitempty"`
	Extra     map[string]any `json:"-"`
}

// gqlLocation is a line/column pair in a GraphQL query.
type gqlLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Error implements the error interface for graphqlError.
func (e graphqlError) Error() string {
	var sb strings.Builder
	sb.WriteString(e.Message)
	if e.Type != "" {
		sb.WriteString(" (type: ")
		sb.WriteString(e.Type)
		sb.WriteString(")")
	}
	return sb.String()
}

// GraphQLError is returned when the GraphQL API returns errors in the response body.
type GraphQLError struct {
	Errors []graphqlError
}

// Error implements the error interface.
func (e *GraphQLError) Error() string {
	if len(e.Errors) == 0 {
		return "unknown GraphQL error"
	}
	if len(e.Errors) == 1 {
		return fmt.Sprintf("GraphQL error: %s", e.Errors[0].Error())
	}
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("GraphQL errors: %s", strings.Join(msgs, "; "))
}

// graphqlEndpoint returns the GraphQL API endpoint for the given base URL.
func graphqlEndpoint(apiBase string) string {
	apiBase = strings.TrimRight(apiBase, "/")
	// GitHub.com API uses /graphql at the root; Enterprise uses /api/graphql
	if apiBase == "https://api.github.com" {
		return apiBase + "/graphql"
	}
	// For GitHub Enterprise Server: the base URL is typically https://ghe.example.com/api/v3
	// The GraphQL endpoint is at https://ghe.example.com/api/graphql
	// Strip /v3 suffix if present
	if strings.HasSuffix(apiBase, "/v3") {
		return strings.TrimSuffix(apiBase, "/v3") + "/graphql"
	}
	return apiBase + "/graphql"
}

// graphqlDo sends a GraphQL query or mutation to the GitHub API and returns the data portion.
// It handles auth header injection, response parsing, and GraphQL-level error checking.
func graphqlDo(ctx context.Context, client *httpc.Client, apiBase, query string, variables map[string]any) (map[string]any, error) {
	lgr := logger.FromContext(ctx)

	endpoint := graphqlEndpoint(apiBase)
	lgr.V(2).Info("graphql request", "endpoint", endpoint)

	reqBody := graphqlRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating GraphQL request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading GraphQL response: %w", err)
	}

	// HTTP-level errors (e.g., 401, 403, 500)
	if resp.StatusCode >= 400 {
		// Try to extract a useful message from the JSON body
		var errResp map[string]any
		if json.Unmarshal(respBody, &errResp) == nil {
			if msg, ok := errResp["message"].(string); ok {
				return nil, fmt.Errorf("GitHub GraphQL API error (HTTP %d): %s", resp.StatusCode, msg)
			}
		}
		return nil, fmt.Errorf("GitHub GraphQL API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse GraphQL response
	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("parsing GraphQL response: %w", err)
	}

	// Check for GraphQL-level errors
	if len(gqlResp.Errors) > 0 {
		// If there's data alongside errors, log the partial-data warning but still return the error
		if gqlResp.Data != nil {
			lgr.V(1).Info("GraphQL response contains partial data with errors", "errorCount", len(gqlResp.Errors))
		}
		return nil, &GraphQLError{Errors: gqlResp.Errors}
	}

	if gqlResp.Data == nil {
		return nil, fmt.Errorf("GraphQL response contains neither data nor errors")
	}

	return gqlResp.Data, nil
}

// extractNode drills into a nested GraphQL response to extract a specific node.
// path is a dot-separated list of keys (e.g., "repository.issue").
func extractNode(data map[string]any, path string) (any, error) {
	keys := strings.Split(path, ".")
	var current any = data
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object at %q, got %T", key, current)
		}
		current, ok = m[key]
		if !ok {
			return nil, fmt.Errorf("key %q not found in response", key)
		}
	}
	return current, nil
}

// extractNodeMap is like extractNode but asserts the result is a map.
func extractNodeMap(data map[string]any, path string) (map[string]any, error) {
	node, err := extractNode(data, path)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("node at %q is null", path)
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map at %q, got %T", path, node)
	}
	return m, nil
}

// extractNodes extracts a "nodes" array from a GraphQL connection at the given path.
func extractNodes(data map[string]any, path string) ([]any, error) {
	conn, err := extractNodeMap(data, path)
	if err != nil {
		return nil, err
	}
	nodes, ok := conn["nodes"]
	if !ok {
		return nil, fmt.Errorf("key %q has no 'nodes' field", path)
	}
	arr, ok := nodes.([]any)
	if !ok {
		return nil, fmt.Errorf("'nodes' at %q is not an array", path)
	}
	return arr, nil
}
