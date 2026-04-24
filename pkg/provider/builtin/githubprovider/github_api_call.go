// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// allowedAPIMethods are the HTTP methods allowed for the api_call operation.
var allowedAPIMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// executeAPICall performs an arbitrary authenticated GitHub REST API request.
// The endpoint must be a relative path (e.g. "/repos/{owner}/{repo}/labels").
// The full URL is constructed as api_base + endpoint, reusing the provider's
// auth pipeline.
func (p *GitHubProvider) executeAPICall(ctx context.Context, client *httpc.Client, apiBase string, inputs map[string]any) (*provider.Output, error) {
	endpoint := getStringInput(inputs, "endpoint")
	if endpoint == "" {
		return nil, requiredInputError("api_call", "endpoint", inputs, "must be a relative path starting with /")
	}

	// Security: endpoint must be a relative path -- reject absolute URLs.
	if !strings.HasPrefix(endpoint, "/") {
		return nil, fmt.Errorf("api_call: endpoint must be a relative path starting with /, got %q", endpoint)
	}
	if strings.Contains(endpoint, "://") {
		return nil, fmt.Errorf("api_call: endpoint must be a relative path, not an absolute URL: %q", endpoint)
	}
	// Reject endpoints with query strings -- use query_params input instead.
	if strings.Contains(endpoint, "?") {
		return nil, fmt.Errorf("api_call: endpoint must not contain query string (?); use query_params input instead: %q", endpoint)
	}
	// Reject path traversal: decode percent-encoding, then check for '..' segments.
	decodedPath, err := url.PathUnescape(endpoint)
	if err != nil {
		return nil, fmt.Errorf("api_call: invalid endpoint encoding: %w", err)
	}
	// Check each segment for ".." to catch traversal in any form.
	for _, seg := range strings.Split(decodedPath, "/") {
		if seg == ".." {
			return nil, fmt.Errorf("api_call: endpoint must not contain path traversal (..): %q", endpoint)
		}
	}

	method := getStringInput(inputs, "method")
	if method == "" {
		method = http.MethodGet
	}
	if !allowedAPIMethods[method] {
		return nil, fmt.Errorf("api_call: unsupported HTTP method %q — allowed: GET, POST, PUT, PATCH, DELETE", method)
	}

	// Build query string from query_params if provided.
	queryParams := getMapInput(inputs, "query_params")
	requestURL := apiBase + endpoint
	if len(queryParams) > 0 {
		keys := make([]string, 0, len(queryParams))
		for k := range queryParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		vals := url.Values{}
		for _, k := range keys {
			vals.Set(k, fmt.Sprint(queryParams[k]))
		}
		requestURL += "?" + vals.Encode()
	}

	// Extract body for mutating methods.
	var body any
	if method != http.MethodGet && method != http.MethodDelete {
		if b, ok := inputs["request_body"]; ok {
			body = b
		}
	}

	result, err := p.doRESTRequest(ctx, client, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("api_call %s %s: %w", method, endpoint, err)
	}

	// For read methods return readOutput; for mutations return actionOutput.
	if method == http.MethodGet {
		return readOutput(result), nil
	}
	return actionOutput("api_call", result), nil
}
