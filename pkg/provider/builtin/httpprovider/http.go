// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	ProviderName = "http"
	Version      = "1.0.0"
)

// retryConfig holds retry configuration for HTTP requests.
type retryConfig struct {
	MaxAttempts int           // Maximum number of attempts (default: 3)
	Backoff     string        // Backoff strategy: "none", "linear", "exponential" (default: "exponential")
	RetryOn     []int         // HTTP status codes to retry on (default: [429, 500, 502, 503, 504])
	InitialWait time.Duration // Initial wait duration between retries (default: 1s)
	MaxWait     time.Duration // Maximum wait duration between retries (default: 30s)
}

// defaultRetryConfig returns default retry configuration.
func defaultRetryConfig() retryConfig {
	return retryConfig{
		MaxAttempts: 3,
		Backoff:     "exponential",
		RetryOn:     []int{429, 500, 502, 503, 504},
		InitialWait: 1 * time.Second,
		MaxWait:     30 * time.Second,
	}
}

// parseRetryConfig parses retry configuration from inputs.
func parseRetryConfig(inputs map[string]any) *retryConfig {
	retryInput, ok := inputs["retry"]
	if !ok || retryInput == nil {
		return nil
	}

	retryMap, ok := retryInput.(map[string]any)
	if !ok {
		return nil
	}

	cfg := defaultRetryConfig()

	if maxAttempts, ok := retryMap["maxAttempts"].(int); ok && maxAttempts > 0 {
		cfg.MaxAttempts = maxAttempts
	}
	// Handle float64 from JSON/YAML unmarshaling
	if maxAttempts, ok := retryMap["maxAttempts"].(float64); ok && maxAttempts > 0 {
		cfg.MaxAttempts = int(maxAttempts)
	}

	if backoff, ok := retryMap["backoff"].(string); ok {
		switch backoff {
		case "none", "linear", "exponential":
			cfg.Backoff = backoff
		}
	}

	if retryOn, ok := retryMap["retryOn"].([]any); ok {
		codes := make([]int, 0, len(retryOn))
		for _, v := range retryOn {
			if code, ok := v.(int); ok {
				codes = append(codes, code)
			}
			if code, ok := v.(float64); ok {
				codes = append(codes, int(code))
			}
		}
		if len(codes) > 0 {
			cfg.RetryOn = codes
		}
	}

	if initialWait, ok := retryMap["initialWait"].(string); ok {
		if d, err := time.ParseDuration(initialWait); err == nil && d > 0 {
			cfg.InitialWait = d
		}
	}

	if maxWait, ok := retryMap["maxWait"].(string); ok {
		if d, err := time.ParseDuration(maxWait); err == nil && d > 0 {
			cfg.MaxWait = d
		}
	}

	return &cfg
}

// shouldRetry returns true if the status code should trigger a retry.
func shouldRetry(statusCode int, retryOn []int) bool {
	for _, code := range retryOn {
		if code == statusCode {
			return true
		}
	}
	return false
}

// calculateBackoff returns the wait duration for the given attempt.
func calculateBackoff(attempt int, cfg retryConfig) time.Duration {
	var wait time.Duration

	switch cfg.Backoff {
	case "none":
		wait = cfg.InitialWait
	case "linear":
		wait = cfg.InitialWait * time.Duration(attempt+1)
	case "exponential":
		// Cap at 10 to prevent overflow: 2^10 = 1024 is plenty
		exp := attempt
		if exp > 10 {
			exp = 10
		}
		wait = cfg.InitialWait * time.Duration(1<<exp)
	default:
		wait = cfg.InitialWait
	}

	if wait > cfg.MaxWait {
		wait = cfg.MaxWait
	}

	return wait
}

// HTTPProvider implements the Provider interface for making HTTP requests.
type HTTPProvider struct {
	descriptor *provider.Descriptor
	client     *http.Client
}

// NewHTTPProvider creates a new HTTP provider instance.
func NewHTTPProvider() *HTTPProvider {
	version, _ := semver.NewVersion(Version)

	return &HTTPProvider{
		descriptor: &provider.Descriptor{
			Name:         ProviderName,
			DisplayName:  "HTTP Client",
			APIVersion:   "v1",
			Description:  "Makes HTTP/HTTPS requests to APIs and web services",
			Version:      version,
			Category:     "network",
			MockBehavior: "Returns mock HTTP response with status 200 and placeholder body without making actual network request",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityAction,
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
				"url": schemahelper.StringProp("The URL to request",
					schemahelper.WithExample("https://api.example.com/users"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(2048)),
					schemahelper.WithPattern(`^https?://.*`)),
				"method": schemahelper.StringProp("HTTP method",
					schemahelper.WithExample("GET"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"headers": schemahelper.AnyProp("HTTP headers as key-value pairs"),
				"body": schemahelper.StringProp("Request body for POST/PUT/PATCH requests",
					schemahelper.WithMaxLength(*ptrs.IntPtr(1048576))),
				"timeout": schemahelper.IntProp("Request timeout in seconds",
					schemahelper.WithExample(30),
					schemahelper.WithMaximum(*ptrs.Float64Ptr(300.0))),
				"retry": schemahelper.AnyProp("Retry configuration for transient failures"),
				"authProvider": schemahelper.StringProp("Authentication provider to use for this request (e.g., 'entra'). When set, the provider will automatically obtain and inject an access token.",
					schemahelper.WithExample("entra"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(50))),
				"scope": schemahelper.StringProp("OAuth scope for authentication. Required for auth providers that support per-request scopes (e.g., Entra). Not used for providers with scopes fixed at login time (e.g., GitHub).",
					schemahelper.WithExample("https://graph.microsoft.com/.default"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"statusCode": schemahelper.IntProp("HTTP response status code", schemahelper.WithExample(200)),
					"body":       schemahelper.StringProp("Response body as string"),
					"headers":    schemahelper.AnyProp("Response headers"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"statusCode": schemahelper.IntProp("HTTP response status code", schemahelper.WithExample(200)),
					"body":       schemahelper.StringProp("Response body as string"),
					"headers":    schemahelper.AnyProp("Response headers"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":    schemahelper.BoolProp("Whether the HTTP request completed successfully (2xx status)"),
					"statusCode": schemahelper.IntProp("HTTP response status code", schemahelper.WithExample(200)),
					"body":       schemahelper.StringProp("Response body as string"),
					"headers":    schemahelper.AnyProp("Response headers"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Simple GET request",
					Description: "Fetch data from an API endpoint using HTTP GET",
					YAML: `name: fetch-users
provider: http
inputs:
  url: "https://api.example.com/users"
  method: GET`,
				},
				{
					Name:        "POST request with JSON body",
					Description: "Send JSON data to an API endpoint using HTTP POST",
					YAML: `name: create-user
provider: http
inputs:
  url: "https://api.example.com/users"
  method: POST
  headers:
    Content-Type: "application/json"
  body: '{"name": "John Doe", "email": "john@example.com"}'`,
				},
				{
					Name:        "Request with authentication header",
					Description: "Make an authenticated API request with custom headers",
					YAML: `name: fetch-protected-data
provider: http
inputs:
  url: "https://api.example.com/protected"
  method: GET
  headers:
    Authorization: "Bearer your-token-here"`,
				},
				{
					Name:        "Request with custom timeout",
					Description: "Make an HTTP request with a specific timeout to prevent long waits",
					YAML: `name: quick-check
provider: http
inputs:
  url: "https://api.example.com/health"
  method: GET
  timeout: 5`,
				},
				{
					Name:        "Request with Entra authentication",
					Description: "Make an authenticated request using Microsoft Entra ID (formerly Azure AD). The provider automatically obtains and injects an access token.",
					YAML: `name: fetch-azure-data
provider: http
inputs:
  url: "https://graph.microsoft.com/v1.0/me"
  method: GET
  authProvider: entra
  scope: "https://graph.microsoft.com/.default"`,
				},
			},
		},
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Descriptor returns the provider's metadata and schema.
func (p *HTTPProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the HTTP request.
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "url", inputs["url"])

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return &provider.Output{
			Data: map[string]any{
				"statusCode": 200,
				"body":       "[DRY-RUN] Request not executed",
				"headers":    map[string]any{},
			},
		}, nil
	}

	// Extract inputs
	urlStr, _ := inputs["url"].(string)
	method, _ := inputs["method"].(string)
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	// Get timeout (handle both int and float64 from JSON/YAML unmarshaling)
	timeout := 30
	if t, ok := inputs["timeout"].(int); ok && t > 0 {
		timeout = t
	}
	if t, ok := inputs["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}
	timeoutDuration := time.Duration(timeout) * time.Second

	// Get body content for potential retries
	bodyContent, _ := inputs["body"].(string)

	// Get headers (make a copy to avoid modifying input)
	headers := make(map[string]any)
	if h, ok := inputs["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = v
		}
	}

	// Handle authentication
	authProvider, _ := inputs["authProvider"].(string)
	scope, _ := inputs["scope"].(string)

	if authProvider != "" {
		// Get auth handler from context
		handler, err := auth.GetHandler(ctx, authProvider)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}

		// Validate scope requirement based on handler capabilities
		requiresScope := auth.HasCapability(handler.Capabilities(), auth.CapScopesOnTokenRequest)
		if scope == "" && requiresScope {
			return nil, fmt.Errorf("%s: scope is required when authProvider %q is set (handler supports per-request scopes)", ProviderName, authProvider)
		}
		if scope != "" && !requiresScope {
			lgr.V(1).Info("ignoring scope for auth provider that does not support per-request scopes",
				"authProvider", authProvider,
				"scope", scope,
			)
			scope = ""
		}

		// Calculate minimum token validity: request timeout + 60 second buffer
		minValidFor := timeoutDuration + 60*time.Second

		// Get token with sufficient validity
		token, err := handler.GetToken(ctx, auth.TokenOptions{
			Scope:       scope,
			MinValidFor: minValidFor,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: failed to get auth token: %w", ProviderName, err)
		}

		// Inject authorization header
		headers["Authorization"] = fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
		lgr.V(1).Info("injected auth header",
			"authProvider", authProvider,
			"scope", scope,
			"tokenExpiresAt", token.ExpiresAt,
			"minValidFor", minValidFor,
		)
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: timeoutDuration,
	}

	// Parse retry configuration
	retryCfg := parseRetryConfig(inputs)

	// Execute request (with or without retry)
	return p.executeWithRetry(ctx, lgr, client, method, urlStr, bodyContent, headers, retryCfg, authProvider, scope)
}

// executeWithRetry performs an HTTP request with optional retry logic.
// Supports automatic token refresh on 401 responses when authProvider is set.
func (p *HTTPProvider) executeWithRetry(
	ctx context.Context,
	lgr *logr.Logger,
	client *http.Client,
	method, url, bodyContent string,
	headers map[string]any,
	retryCfg *retryConfig,
	authProvider, scope string,
) (*provider.Output, error) {
	maxAttempts := 1
	if retryCfg != nil {
		maxAttempts = retryCfg.MaxAttempts
	}

	var lastErr error
	var lastStatusCode int
	authRetried := false // Track if we've already retried with fresh token

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%s: request cancelled: %w", ProviderName, ctx.Err())
		default:
		}

		// Create request body for this attempt
		var bodyReader io.Reader
		if bodyContent != "" {
			bodyReader = strings.NewReader(bodyContent)
		}

		// Create request
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to create request: %w", ProviderName, err)
		}

		// Set headers
		for key, value := range headers {
			if strValue, ok := value.(string); ok {
				req.Header.Set(key, strValue)
			}
		}

		// Execute request
		resp, err := client.Do(req) //nolint:gosec // G704: URL is from provider configuration
		if err != nil {
			lastErr = err
			// Network errors are retryable
			if retryCfg != nil && attempt < maxAttempts-1 {
				wait := calculateBackoff(attempt, *retryCfg)
				lgr.V(1).Info("request failed, retrying", "provider", ProviderName, "attempt", attempt+1, "maxAttempts", maxAttempts, "wait", wait, "error", err)
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("%s: request cancelled during retry: %w", ProviderName, ctx.Err())
				case <-time.After(wait):
				}
				continue
			}
			return nil, fmt.Errorf("%s: request failed: %w", ProviderName, err)
		}

		// Read response body
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: failed to read response body: %w", ProviderName, err)
		}

		lastStatusCode = resp.StatusCode

		// Handle 401 Unauthorized with automatic token refresh
		if resp.StatusCode == http.StatusUnauthorized && authProvider != "" && !authRetried {
			lgr.V(1).Info("received 401, attempting token refresh", "provider", ProviderName, "authProvider", authProvider)
			authRetried = true

			// Get fresh token with ForceRefresh
			handler, err := auth.GetHandler(ctx, authProvider)
			if err != nil {
				lgr.V(1).Info("failed to get auth handler for retry", "error", err)
			} else {
				minValidFor := client.Timeout + 60*time.Second
				token, err := handler.GetToken(ctx, auth.TokenOptions{
					Scope:        scope,
					MinValidFor:  minValidFor,
					ForceRefresh: true,
				})
				if err != nil {
					lgr.V(1).Info("failed to refresh token", "error", err)
				} else {
					// Update authorization header and retry
					headers["Authorization"] = fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
					lgr.V(1).Info("token refreshed, retrying request",
						"authProvider", authProvider,
						"tokenExpiresAt", token.ExpiresAt,
					)
					// Don't count this as a regular retry attempt
					attempt--
					continue
				}
			}
		}

		// Check if we should retry based on status code
		if retryCfg != nil && shouldRetry(resp.StatusCode, retryCfg.RetryOn) && attempt < maxAttempts-1 {
			wait := calculateBackoff(attempt, *retryCfg)
			lgr.V(1).Info("received retryable status code, retrying", "provider", ProviderName, "statusCode", resp.StatusCode, "attempt", attempt+1, "maxAttempts", maxAttempts, "wait", wait)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%s: request cancelled during retry: %w", ProviderName, ctx.Err())
			case <-time.After(wait):
			}
			continue
		}

		// Build response headers map
		respHeaders := make(map[string]any)
		for key, values := range resp.Header {
			if len(values) == 1 {
				respHeaders[key] = values[0]
			} else {
				respHeaders[key] = values
			}
		}

		lgr.V(1).Info("provider execution completed", "provider", ProviderName, "statusCode", resp.StatusCode, "attempts", attempt+1)

		// Return output
		return &provider.Output{
			Data: map[string]any{
				"statusCode": resp.StatusCode,
				"body":       string(respBody),
				"headers":    respHeaders,
			},
		}, nil
	}

	// All retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("%s: max retries exceeded: %w", ProviderName, lastErr)
	}
	return nil, fmt.Errorf("%s: max retries exceeded, last status code: %d", ProviderName, lastStatusCode)
}
