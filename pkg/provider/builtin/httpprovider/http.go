// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	ProviderName = "http"
	Version      = "1.0.0"
)

// Field name constants for input/output map keys.
const (
	fieldURL     = "url"
	fieldMethod  = "method"
	fieldHeaders = "headers"
	fieldBody    = "body"
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

// shouldRetry and calculateBackoff have been removed — httpc.BuildStatusCodeCheckRetry
// and httpc.BuildNamedBackoff now provide equivalent logic via retryablehttp.

// HTTPProvider implements the Provider interface for making HTTP requests.
type HTTPProvider struct {
	descriptor *provider.Descriptor
}

// NewHTTPProvider creates a new HTTP provider instance.
func NewHTTPProvider() *HTTPProvider {
	version, _ := semver.NewVersion(Version)

	return &HTTPProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "HTTP Client",
			APIVersion:  "v1",
			Description: "Makes HTTP/HTTPS requests to APIs and web services",
			Version:     version,
			Category:    "network",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				operation, _ := inputs["operation"].(string)
				switch operation {
				case "state_load":
					return fmt.Sprintf("Would load state from %s", inputs[fieldURL]), nil
				case "state_save":
					return fmt.Sprintf("Would save state to %s", inputs[fieldURL]), nil
				case "state_delete":
					return fmt.Sprintf("Would delete state at %s", inputs[fieldURL]), nil
				}
				method, _ := inputs[fieldMethod].(string)
				if method == "" {
					method = "GET"
				}
				url, _ := inputs[fieldURL].(string)
				return fmt.Sprintf("Would send %s request to %s", method, url), nil
			},
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityAction,
				provider.CapabilityTransform,
				provider.CapabilityState,
			},
			Schema: schemahelper.ObjectSchema([]string{fieldURL}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform. Only used for state operations.",
					schemahelper.WithEnum("state_load", "state_save", "state_delete")),
				"data": schemahelper.AnyProp("The full StateData object to persist (required for state_save operation)"),
				fieldURL: schemahelper.StringProp("The URL to request",
					schemahelper.WithExample("https://api.example.com/users"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(2048)),
					schemahelper.WithPattern(`^https?://.*`)),
				fieldMethod: schemahelper.StringProp("HTTP method",
					schemahelper.WithExample("GET"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				fieldHeaders: schemahelper.AnyProp("HTTP headers as key-value pairs"),
				fieldBody: schemahelper.StringProp("Request body for POST/PUT/PATCH requests",
					schemahelper.WithMaxLength(*ptrs.IntPtr(1048576))),
				"timeout": schemahelper.IntProp("Request timeout in seconds",
					schemahelper.WithExample(30),
					schemahelper.WithMaximum(*ptrs.Float64Ptr(300.0))),
				"retry": schemahelper.AnyProp("Retry configuration for transient failures"),
				"poll": schemahelper.ObjectProp("Polling configuration for re-executing the request until a condition is met. Use this for waiting on async operations (e.g., deployment status). Different from retry: retry handles transient failures, poll re-executes until the response content matches a condition.", []string{"until"}, map[string]*jsonschema.Schema{
					"until": schemahelper.StringProp("CEL expression evaluated against {statusCode, body, headers}. Polling stops when this returns true.",
						schemahelper.WithExample("_.body.status == 'succeeded'"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
					"failWhen": schemahelper.StringProp("CEL expression for early exit with error. If true, polling stops immediately with a failure.",
						schemahelper.WithExample("_.body.status == 'failed'"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
					"interval": schemahelper.StringProp("Duration between poll attempts (Go duration format or integer seconds, default: 10s)",
						schemahelper.WithDefault("10s"),
						schemahelper.WithExample("5s")),
					"maxAttempts": schemahelper.IntProp("Maximum number of poll attempts before giving up (default: 30)",
						schemahelper.WithDefault(30),
						schemahelper.WithExample(30),
						schemahelper.WithMaximum(*ptrs.Float64Ptr(1000))),
				}),
				"pagination": schemahelper.ObjectProp("Pagination configuration for automatically following paginated API responses", []string{"strategy", "maxPages"}, map[string]*jsonschema.Schema{
					"strategy": schemahelper.StringProp("Pagination strategy to use",
						schemahelper.WithEnum("offset", "pageNumber", "cursor", "linkHeader", "custom"),
						schemahelper.WithExample("cursor")),
					"maxPages": schemahelper.IntProp("Maximum number of pages to fetch (safety limit to prevent infinite loops)",
						schemahelper.WithExample(10),
						schemahelper.WithMaximum(*ptrs.Float64Ptr(10000)),
						schemahelper.WithDefault(100)),
					"offsetParam": schemahelper.StringProp("Query parameter name for offset (offset strategy, default: 'offset')",
						schemahelper.WithExample("offset"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(100))),
					"limitParam": schemahelper.StringProp("Query parameter name for limit (offset strategy, default: 'limit')",
						schemahelper.WithExample("limit"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(100))),
					"limit": schemahelper.IntProp("Page size for offset strategy",
						schemahelper.WithExample(50),
						schemahelper.WithMaximum(*ptrs.Float64Ptr(10000))),
					"pageParam": schemahelper.StringProp("Query parameter name for page number (pageNumber strategy, default: 'page')",
						schemahelper.WithExample("page"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(100))),
					"pageSizeParam": schemahelper.StringProp("Query parameter name for page size (pageNumber strategy, default: 'pageSize')",
						schemahelper.WithExample("pageSize"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(100))),
					"pageSize": schemahelper.IntProp("Page size for pageNumber strategy",
						schemahelper.WithExample(50),
						schemahelper.WithMaximum(*ptrs.Float64Ptr(10000))),
					"startPage": schemahelper.IntProp("Starting page number for pageNumber strategy (default: 1)",
						schemahelper.WithExample(1),
						schemahelper.WithDefault(1)),
					"nextTokenPath": schemahelper.StringProp("CEL expression to extract the next cursor/token from the response body (cursor strategy)",
						schemahelper.WithExample("body.nextToken"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
					"nextTokenParam": schemahelper.StringProp("Query parameter name to set the cursor/token on the next request (cursor strategy)",
						schemahelper.WithExample("cursor"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(100))),
					"nextURLPath": schemahelper.StringProp("CEL expression to extract the full next page URL from the response body (cursor strategy)",
						schemahelper.WithExample("body['@odata.nextLink']"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
					"nextURL": schemahelper.StringProp("CEL expression that returns the full URL for the next request; null/empty stops pagination (custom strategy)",
						schemahelper.WithMaxLength(*ptrs.IntPtr(1000))),
					"nextParams": schemahelper.StringProp("CEL expression that returns a map of query params for the next request (custom strategy)",
						schemahelper.WithMaxLength(*ptrs.IntPtr(1000))),
					"stopWhen": schemahelper.StringProp("CEL expression evaluated against each response; if true, pagination stops. Available variables: statusCode, body, rawBody, headers, page",
						schemahelper.WithExample("size(body.items) == 0"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
					"collectPath": schemahelper.StringProp("CEL expression to extract items from each page's response body. Items are accumulated across pages into a single array.",
						schemahelper.WithExample("body.items"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
				}),
				"authProvider": schemahelper.StringProp("Authentication provider to use for this request (e.g., 'entra'). When set, the provider will automatically obtain and inject an access token.",
					schemahelper.WithExample("entra"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(50))),
				"scope": schemahelper.StringProp("OAuth scope for authentication. Required for auth providers that support per-request scopes (e.g., Entra). Not used for providers with scopes fixed at login time (e.g., GitHub).",
					schemahelper.WithExample("https://graph.microsoft.com/.default"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
				"autoParseJson": schemahelper.BoolProp("When true and the response Content-Type is application/json, automatically parse the body into a structured object instead of returning it as a raw string. This allows direct field access in downstream CEL expressions (e.g., _.myApi.body.items) without manual parsing."),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"statusCode": schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:    schemahelper.AnyProp("Response body as string (default) or parsed JSON object when autoParseJson is true. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders: schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":      schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems": schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"statusCode": schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:    schemahelper.AnyProp("Response body as string (default) or parsed JSON object when autoParseJson is true. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders: schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":      schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems": schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":    schemahelper.BoolProp("Whether the HTTP request completed successfully (2xx status)"),
					"statusCode": schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:    schemahelper.AnyProp("Response body as string (default) or parsed JSON object when autoParseJson is true. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders: schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":      schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems": schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
				}),
				provider.CapabilityState: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the state operation succeeded"),
					"data":    schemahelper.AnyProp("The loaded state data (for state_load operation)"),
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
				{
					Name:        "Cursor-based pagination",
					Description: "Fetch all pages from an API using cursor-based pagination with a token extracted from the response body",
					YAML: `name: fetch-all-items
provider: http
inputs:
  url: "https://api.example.com/items"
  method: GET
  pagination:
    strategy: cursor
    maxPages: 10
    nextTokenPath: "body.nextCursor"
    nextTokenParam: "cursor"
    collectPath: "body.items"
    stopWhen: "body.nextCursor == null"`,
				},
				{
					Name:        "Cursor-based pagination with nextURL",
					Description: "Fetch all pages from a Microsoft Graph or OData API where the response body contains the full next page URL",
					YAML: `name: fetch-graph-users
provider: http
inputs:
  url: "https://graph.microsoft.com/v1.0/users?$top=100"
  method: GET
  authProvider: entra
  scope: "https://graph.microsoft.com/.default"
  pagination:
    strategy: cursor
    maxPages: 50
    nextURLPath: "body['@odata.nextLink']"
    collectPath: "body.value"`,
				},
				{
					Name:        "Link header pagination",
					Description: "Follow RFC 8288 Link header pagination (used by GitHub, GitLab, and other REST APIs)",
					YAML: `name: fetch-github-repos
provider: http
inputs:
  url: "https://api.github.com/users/octocat/repos?per_page=30"
  method: GET
  headers:
    Accept: "application/vnd.github+json"
  pagination:
    strategy: linkHeader
    maxPages: 5
    collectPath: "body"`,
				},
				{
					Name:        "Offset-based pagination",
					Description: "Paginate through results using offset and limit query parameters",
					YAML: `name: fetch-all-records
provider: http
inputs:
  url: "https://api.example.com/records"
  method: GET
  pagination:
    strategy: offset
    maxPages: 20
    limit: 50
    offsetParam: "offset"
    limitParam: "limit"
    collectPath: "body.records"
    stopWhen: "size(body.records) < 50"`,
				},
				{
					Name:        "Page number pagination",
					Description: "Paginate through results using page number and page size query parameters",
					YAML: `name: fetch-paginated
provider: http
inputs:
  url: "https://api.example.com/products"
  method: GET
  pagination:
    strategy: pageNumber
    maxPages: 10
    pageSize: 25
    pageParam: "page"
    pageSizeParam: "per_page"
    collectPath: "body.products"
    stopWhen: "size(body.products) == 0"`,
				},
				{
					Name:        "Custom pagination with CEL",
					Description: "Use custom CEL expressions for full control over pagination logic",
					YAML: `name: fetch-custom-paginated
provider: http
inputs:
  url: "https://api.example.com/search?q=test"
  method: GET
  pagination:
    strategy: custom
    maxPages: 10
    nextURL: "has(body.links) && has(body.links.next) ? body.links.next : ''"
    collectPath: "body.results"
    stopWhen: "!has(body.links) || !has(body.links.next)"`,
				},
				{
					Name:        "Auto-parse JSON response",
					Description: "Automatically parse JSON response body for direct field access in downstream expressions",
					YAML: `name: fetch-user-parsed
provider: http
inputs:
  url: "https://api.example.com/users/1"
  method: GET
  autoParseJson: true`,
				},
				{
					Name:        "Poll until condition is met",
					Description: "Keep checking a deployment status API until the deployment succeeds or fails",
					YAML: `name: wait-for-deployment
provider: http
inputs:
  url: "https://api.example.com/deployments/123"
  method: GET
  autoParseJson: true
  poll:
    until: "_.body.status == 'succeeded'"
    failWhen: "_.body.status == 'failed'"
    interval: "10s"
    maxAttempts: 30`,
				},
			},
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

	lgr.V(1).Info("executing provider", "provider", ProviderName, fieldURL, inputs[fieldURL])

	// State operations use a dedicated dispatch path
	if operation, _ := inputs["operation"].(string); strings.HasPrefix(operation, "state_") {
		return p.dispatchStateOperation(ctx, operation, inputs)
	}

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return &provider.Output{
			Data: map[string]any{
				"statusCode": 200,
				fieldBody:    "[DRY-RUN] Request not executed",
				fieldHeaders: map[string]any{},
			},
		}, nil
	}

	// Extract inputs
	urlStr, _ := inputs[fieldURL].(string)
	method, _ := inputs[fieldMethod].(string)
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
	bodyContent, _ := inputs[fieldBody].(string)

	// Get headers (make a copy to avoid modifying input)
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

	// Build httpc client with timeout and user-supplied retry configuration.
	retryCfg := parseRetryConfig(inputs)
	httpcCfg := buildHTTPClientConfig(timeoutDuration, retryCfg)

	// Wire 401 token-refresh via the httpc OnUnauthorized hook when an auth provider is configured.
	// The initial token was already injected into headers above; this hook handles silent re-auth on 401.
	if authProvider != "" {
		capturedScope := scope
		capturedTimeout := timeoutDuration
		httpcCfg.OnUnauthorized = func(unauthCtx context.Context) (string, error) {
			handler, handlerErr := auth.GetHandler(unauthCtx, authProvider)
			if handlerErr != nil {
				return "", handlerErr
			}
			token, tokenErr := handler.GetToken(unauthCtx, auth.TokenOptions{
				Scope:        capturedScope,
				MinValidFor:  capturedTimeout + 60*time.Second,
				ForceRefresh: true,
			})
			if tokenErr != nil {
				return "", tokenErr
			}
			return fmt.Sprintf("%s %s", token.TokenType, token.AccessToken), nil
		}
	}

	// Parse pagination configuration
	pagCfg, err := parsePaginationConfig(inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	// Parse poll configuration
	pollCfg, err := parsePollConfig(inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	httpClient := httpc.NewClient(httpcCfg)

	autoParseJSON, _ := inputs["autoParseJson"].(bool)

	// If pagination is configured, use the paginated execution path
	if pagCfg != nil {
		return p.executePaginated(ctx, httpClient, method, urlStr, bodyContent, headers, pagCfg)
	}

	// Build the execute function for potential polling
	executeFunc := func() (*provider.Output, error) {
		return p.execute(ctx, httpClient, method, urlStr, bodyContent, headers, autoParseJSON)
	}

	// If polling is configured, wrap execution in a poll loop
	if pollCfg != nil {
		return p.executePoll(ctx, nil, method, urlStr, bodyContent, headers, pollCfg, autoParseJSON, executeFunc)
	}

	return executeFunc()
}

// buildHTTPClientConfig translates provider timeout and retry config into an httpc.ClientConfig.
// When retryCfg is nil a single attempt is made and all HTTP responses are returned as-is
// (matching the original http.Client behaviour).
// When retryCfg is provided, retries are configured and the last HTTP response is always
// returned to the caller even after retries are exhausted (network errors are still propagated).
func buildHTTPClientConfig(timeout time.Duration, retryCfg *retryConfig) *httpc.ClientConfig {
	cfg := &httpc.ClientConfig{
		Timeout:              timeout,
		RetryMax:             0,
		EnableCache:          false,
		EnableCompression:    true,
		EnableCircuitBreaker: false,
	}
	if retryCfg == nil {
		// No retry: block — single attempt, never retry on any HTTP status.
		// This preserves the original http.Client behaviour where every HTTP response
		// (including 4xx/5xx) is returned without error; only network failures error.
		cfg.CheckRetry = func(_ context.Context, _ *http.Response, err error) (bool, error) {
			return false, err
		}
		// Without an ErrorHandler, retryablehttp wraps any non-nil error in a
		// "giving up after N attempt(s)" message even when shouldRetry is false.
		// Pass the underlying error through unchanged so the caller gets the
		// original net/http error (e.g. context deadline exceeded).
		cfg.ErrorHandler = func(resp *http.Response, err error, _ int) (*http.Response, error) {
			return resp, err
		}
		return cfg
	}

	cfg.RetryMax = retryCfg.MaxAttempts - 1
	cfg.RetryWaitMin = retryCfg.InitialWait
	cfg.RetryWaitMax = retryCfg.MaxWait
	cfg.CheckRetry = httpc.BuildStatusCodeCheckRetry(retryCfg.RetryOn)
	cfg.Backoff = httpc.BuildNamedBackoff(retryCfg.Backoff, retryCfg.InitialWait, retryCfg.MaxWait)
	// After all retries are exhausted, return the last HTTP response instead of
	// an error so callers can inspect the final status code (matches old behaviour).
	cfg.ErrorHandler = func(resp *http.Response, err error, _ int) (*http.Response, error) {
		if resp != nil {
			return resp, nil
		}
		return nil, err
	}
	return cfg
}

// execute performs the HTTP request using the given httpc.Client.
// Retries and 401 token-refresh are handled transparently by the httpc layer.
func (p *HTTPProvider) execute(
	ctx context.Context,
	client *httpc.Client,
	method, urlStr, bodyContent string,
	headers map[string]any,
	autoParseJSON bool,
) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	if !privateIPsAllowed(ctx) {
		if err := validateURLNotPrivate(urlStr); err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
	}

	var bodyReader io.Reader
	if bodyContent != "" {
		bodyReader = strings.NewReader(bodyContent)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create request: %w", ProviderName, err)
	}

	// Provide GetBody so the httpc OnUnauthorized hook can replay the body on an auth retry.
	if bodyContent != "" {
		capturedBody := bodyContent
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(capturedBody)), nil
		}
	}

	for key, value := range headers {
		if strValue, ok := value.(string); ok {
			req.Header.Set(key, strValue)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", ProviderName, err)
	}
	defer resp.Body.Close()

	// Limit the response body size to prevent denial-of-service via unbounded
	// responses. The limit is configurable via httpClient.maxResponseBodySize.
	limit := maxResponseBodySize(ctx)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: failed to read response body: %w", ProviderName, err)
	}
	if int64(len(respBody)) > limit {
		return nil, fmt.Errorf("%s: response body exceeds maximum size of %d bytes", ProviderName, limit)
	}

	respHeaders := make(map[string]any)
	for key, values := range resp.Header {
		if len(values) == 1 {
			respHeaders[key] = values[0]
		} else {
			respHeaders[key] = values
		}
	}

	lgr.V(1).Info("provider execution completed", "provider", ProviderName, "statusCode", resp.StatusCode)

	// Determine response body value
	var bodyValue any = string(respBody)
	if autoParseJSON && isJSONContentType(resp.Header.Get("Content-Type")) && len(respBody) > 0 {
		var parsed any
		if err := json.Unmarshal(respBody, &parsed); err == nil {
			bodyValue = parsed
		}
		// If JSON parse fails, fall back to raw string silently
	}

	return &provider.Output{
		Data: map[string]any{
			"statusCode": resp.StatusCode,
			fieldBody:    bodyValue,
			fieldHeaders: respHeaders,
		},
	}, nil
}

// isJSONContentType returns true if the Content-Type header indicates a JSON response.
func isJSONContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(ct, "application/json") ||
		strings.HasSuffix(ct, "+json")
}
