// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
)

// executeStateLoad loads state from a remote HTTP endpoint via GET.
func (p *HTTPProvider) executeStateLoad(ctx context.Context, client *httpc.Client, urlStr string, headers map[string]any) (*provider.Output, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("state load: create request: %w", err)
	}
	setHeaders(req, headers)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("state load: request failed: %w", err)
	}
	defer resp.Body.Close()

	limit := maxResponseBodySize(ctx)
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("state load: read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("state load: response body exceeds maximum size of %d bytes", limit)
	}

	// 404 means no state yet -- return empty state
	if resp.StatusCode == http.StatusNotFound {
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("state load: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var stateData state.Data
	if err := json.Unmarshal(body, &stateData); err != nil {
		return nil, fmt.Errorf("state load: unmarshal: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"data":    &stateData,
		},
	}, nil
}

// executeStateSave persists state to a remote HTTP endpoint via PUT.
func (p *HTTPProvider) executeStateSave(
	ctx context.Context, client *httpc.Client, urlStr string,
	headers, inputs map[string]any,
) (*provider.Output, error) {
	rawData, ok := inputs["data"]
	if !ok {
		return nil, fmt.Errorf("state save: data is required")
	}

	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		return nil, fmt.Errorf("state save: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, urlStr, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("state save: create request: %w", err)
	}
	setHeaders(req, headers)
	req.Header.Set("Content-Type", "application/json")

	// Provide GetBody for auth retry replays.
	capturedBody := jsonBytes
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(capturedBody)), nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("state save: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain and discard response body
	limit := maxResponseBodySize(ctx)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("state save: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDelete removes state from a remote HTTP endpoint via DELETE.
func (p *HTTPProvider) executeStateDelete(ctx context.Context, client *httpc.Client, urlStr string, headers map[string]any) (*provider.Output, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("state delete: create request: %w", err)
	}
	setHeaders(req, headers)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("state delete: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain and discard response body
	limit := maxResponseBodySize(ctx)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))

	// 404 is acceptable -- state was already absent
	if resp.StatusCode == http.StatusNotFound {
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("state delete: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return &provider.Output{
		Data: map[string]any{"success": true},
	}, nil
}

// executeStateDryRun handles dry-run mode for state operations.
func (p *HTTPProvider) executeStateDryRun(operation string) (*provider.Output, error) {
	switch operation {
	case "state_load":
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"data":    state.NewData(),
			},
		}, nil
	case "state_save", "state_delete":
		return &provider.Output{
			Data: map[string]any{"success": true},
		}, nil
	default:
		return nil, fmt.Errorf("unknown state operation: %s", operation)
	}
}

// dispatchStateOperation handles the state capability branch in Execute.
func (p *HTTPProvider) dispatchStateOperation(ctx context.Context, operation string, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("executing state operation", "provider", ProviderName, "operation", operation)

	urlStr, _ := inputs[fieldURL].(string)
	if urlStr == "" {
		return nil, fmt.Errorf("%s: url is required for state operations", ProviderName)
	}

	if provider.DryRunFromContext(ctx) {
		return p.executeStateDryRun(operation)
	}

	// SSRF protection
	if !privateIPsAllowed(ctx) {
		if err := validateURLNotPrivate(urlStr); err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
	}

	// Build timeout
	timeout := 30
	if t, ok := inputs["timeout"].(int); ok && t > 0 {
		timeout = t
	}
	if t, ok := inputs["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}
	timeoutDuration := time.Duration(timeout) * time.Second

	// Build headers
	headers := make(map[string]any)
	if h, ok := inputs[fieldHeaders].(map[string]any); ok {
		for k, v := range h {
			headers[k] = v
		}
	}

	// Handle authentication
	authProvider, _ := inputs["authProvider"].(string)
	scope, _ := inputs["scope"].(string)
	if authProvider != "" {
		if err := p.injectAuthHeader(ctx, authProvider, scope, timeoutDuration, headers); err != nil {
			return nil, err
		}
	}

	// Build httpc client
	retryCfg := parseRetryConfig(inputs)
	httpcCfg := buildHTTPClientConfig(timeoutDuration, retryCfg)

	// Wire 401 token-refresh when an auth provider is configured.
	if authProvider != "" {
		httpcCfg.OnUnauthorized = p.buildOnUnauthorized(ctx, authProvider, scope, timeoutDuration)
	}

	client := httpc.NewClient(httpcCfg)

	switch operation {
	case "state_load":
		return p.executeStateLoad(ctx, client, urlStr, headers)
	case "state_save":
		return p.executeStateSave(ctx, client, urlStr, headers, inputs)
	case "state_delete":
		return p.executeStateDelete(ctx, client, urlStr, headers)
	default:
		return nil, fmt.Errorf("%s: unsupported state operation: %s", ProviderName, operation)
	}
}

// setHeaders sets headers on an HTTP request from a map.
func setHeaders(req *http.Request, headers map[string]any) {
	for key, value := range headers {
		if strValue, ok := value.(string); ok {
			req.Header.Set(key, strValue)
		}
	}
}

// injectAuthHeader obtains an auth token and injects it into the headers map.
func (p *HTTPProvider) injectAuthHeader(ctx context.Context, authProviderName, scope string, timeoutDuration time.Duration, headers map[string]any) error {
	handler, err := auth.GetHandler(ctx, authProviderName)
	if err != nil {
		return fmt.Errorf("%s: %w", ProviderName, err)
	}

	requiresScope := auth.HasCapability(handler.Capabilities(), auth.CapScopesOnTokenRequest)
	if scope == "" && requiresScope {
		return fmt.Errorf("%s: scope is required when authProvider %q is set (handler supports per-request scopes)", ProviderName, authProviderName)
	}
	if scope != "" && !requiresScope {
		scope = ""
	}

	minValidFor := timeoutDuration + 60*time.Second
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:       scope,
		MinValidFor: minValidFor,
	})
	if err != nil {
		return fmt.Errorf("%s: failed to get auth token: %w", ProviderName, err)
	}

	headers["Authorization"] = fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
	return nil
}

// buildOnUnauthorized returns an httpc.OnUnauthorized callback for 401 retry.
func (p *HTTPProvider) buildOnUnauthorized(_ context.Context, authProviderName, scope string, timeoutDuration time.Duration) func(context.Context) (string, error) {
	return func(unauthCtx context.Context) (string, error) {
		handler, handlerErr := auth.GetHandler(unauthCtx, authProviderName)
		if handlerErr != nil {
			return "", handlerErr
		}
		token, tokenErr := handler.GetToken(unauthCtx, auth.TokenOptions{
			Scope:        scope,
			MinValidFor:  timeoutDuration + 60*time.Second,
			ForceRefresh: true,
		})
		if tokenErr != nil {
			return "", tokenErr
		}
		return fmt.Sprintf("%s %s", token.TokenType, token.AccessToken), nil
	}
}
