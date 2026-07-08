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
	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
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

// maxBodyBytes caps the size of a request body (in bytes) after it has been
// serialized. This applies uniformly to string and structured (object/array)
// bodies to bound memory use during serialization and request replay.
const maxBodyBytes = 1048576

// Field name constants for input/output map keys.
const (
	fieldURL                   = "url"
	fieldMethod                = "method"
	fieldHeaders               = "headers"
	fieldBody                  = "body"
	fieldStatusCode            = "statusCode"
	fieldSuccess               = "success"
	fieldAcceptableStatusCodes = "acceptableStatusCodes"
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

// serializeBody converts a request body input into the string that is written to
// the wire. A string is returned verbatim. A nil or absent body yields an empty
// string. Byte slices (including json.RawMessage) are treated as an already
// serialized body and returned verbatim so they are not base64-encoded. Any
// other value (map, slice, etc.) is serialized to compact JSON and reported as
// structured so the caller can default the Content-Type header.
func serializeBody(body any) (content string, structured bool, err error) {
	switch v := body.(type) {
	case nil:
		return "", false, nil
	case string:
		return v, false, nil
	case json.RawMessage:
		return string(v), true, nil
	case []byte:
		return string(v), false, nil
	default:
		encoded, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			return "", false, fmt.Errorf("failed to serialize request body to JSON: %w", marshalErr)
		}
		return string(encoded), true, nil
	}
}

// hasContentType reports whether the headers map already contains a Content-Type
// header, matching the key case-insensitively.
func hasContentType(headers map[string]any) bool {
	for k := range headers {
		if strings.EqualFold(k, "Content-Type") {
			return true
		}
	}
	return false
}

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
				fieldBody: {
					Types:       []string{"string", "object", "array"},
					Description: "Request body for POST/PUT/PATCH requests. A string is sent verbatim. A structured value (object or array) is serialized to JSON, and Content-Type defaults to application/json when not otherwise set.",
					MaxLength:   ptrs.IntPtr(maxBodyBytes),
				},
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
					"pageSize": schemahelper.IntProp("Page size. Required for pageNumber strategy; also used as __pageSize in bodyTemplate for any strategy.",
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
					"bodyTemplate": schemahelper.StringProp("CEL expression evaluated per-page to generate the request body. Enables POST-based pagination (GraphQL, Elasticsearch). When set, overrides the top-level body field. Available variables: __page (1-indexed), __pageSize, __offset, __cursor.",
						schemahelper.WithExample(`'{"query":"{ items(page: ' + string(__page) + ', size: ' + string(__pageSize) + ') { results { id } } }"}'`),
						schemahelper.WithMaxLength(*ptrs.IntPtr(50000))),
				}),
				"authProvider": schemahelper.StringProp("Authentication provider to use for this request (e.g., 'entra'). When set, the provider will automatically obtain and inject an access token.",
					schemahelper.WithExample("entra"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(50))),
				"scope": schemahelper.StringProp("OAuth scope for authentication. Required for auth providers that support per-request scopes (e.g., Entra). Not used for providers with scopes fixed at login time (e.g., GitHub).",
					schemahelper.WithExample("https://graph.microsoft.com/.default"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(500))),
				"autoParseJson": schemahelper.BoolProp("When true and the response Content-Type is application/json, automatically parse the body into a structured object instead of returning it as a raw string. This allows direct field access in downstream CEL expressions (e.g., _.myApi.body.items) without manual parsing."),
				fieldAcceptableStatusCodes: schemahelper.ArrayProp("Status codes treated as successful. Each entry may be an integer (200), a class shorthand (\"2xx\"), or an inclusive range (\"200-204\"). When set, any response whose status is not acceptable causes the request to fail (which the source-level onError policy can turn into a fallback). When omitted, only 2xx responses are successful and non-2xx responses are returned without error.",
					schemahelper.WithItems(&jsonschema.Schema{Types: []string{"integer", "string"}}),
					schemahelper.WithExample([]any{200, 201, "2xx"})),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					fieldSuccess:    schemahelper.BoolProp("Whether the response status is considered successful. When acceptableStatusCodes is set, true only for those codes; otherwise true for any 2xx status."),
					fieldStatusCode: schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:       schemahelper.AnyProp("Response body. By default (or when the response Content-Type is not JSON, or the body is empty) this is the raw response string; an empty response yields an empty string (\"\"). When autoParseJson is true and the Content-Type is JSON and the body is non-empty, it is the parsed JSON object. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders:    schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":         schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems":    schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					fieldSuccess:    schemahelper.BoolProp("Whether the response status is considered successful. When acceptableStatusCodes is set, true only for those codes; otherwise true for any 2xx status."),
					fieldStatusCode: schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:       schemahelper.AnyProp("Response body. By default (or when the response Content-Type is not JSON, or the body is empty) this is the raw response string; an empty response yields an empty string (\"\"). When autoParseJson is true and the Content-Type is JSON and the body is non-empty, it is the parsed JSON object. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders:    schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":         schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems":    schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					fieldSuccess:    schemahelper.BoolProp("Whether the response status is considered successful. When acceptableStatusCodes is set, true only for those codes; otherwise true for any 2xx status."),
					fieldStatusCode: schemahelper.IntProp("HTTP response status code (last page when paginating)", schemahelper.WithExample(200)),
					fieldBody:       schemahelper.AnyProp("Response body. By default (or when the response Content-Type is not JSON, or the body is empty) this is the raw response string; an empty response yields an empty string (\"\"). When autoParseJson is true and the Content-Type is JSON and the body is non-empty, it is the parsed JSON object. When paginating with collectPath, contains the JSON array of all collected items"),
					fieldHeaders:    schemahelper.AnyProp("Response headers (last page when paginating)"),
					"pages":         schemahelper.IntProp("Number of pages fetched (only present when pagination is configured)", schemahelper.WithExample(3)),
					"totalItems":    schemahelper.IntProp("Total number of items collected across all pages (only present when pagination is configured)", schemahelper.WithExample(150)),
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
					Name:        "POST request with a structured JSON body",
					Description: "Pass an object as the body and let the provider serialize it to JSON. Content-Type defaults to application/json when not set. Use a CEL expression to build the body from resolver values without a separate serializer resolver.",
					YAML: `name: run-graph-query
provider: http
inputs:
  url: "https://api.example.com/graphql"
  method: POST
  body:
    expr: '{"query": _.graphQuery}'`,
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
					Name:        "Treat additional status codes as successful",
					Description: "Accept a 404 alongside 2xx responses so a missing resource is a normal result instead of an error. Entries may be integers (200), class shorthands (\"2xx\"), or ranges (\"200-204\"). Any status outside the set fails the request, which an onError fallback can handle.",
					YAML: `name: lookup-optional
provider: http
inputs:
  url: "https://api.example.com/users/maybe-missing"
  method: GET
  acceptableStatusCodes: [200, 404]`,
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
				fieldSuccess:    true,
				fieldStatusCode: 200,
				fieldBody:       "[DRY-RUN] Request not executed",
				fieldHeaders:    map[string]any{},
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

	// Get body content for potential retries. A string body is sent verbatim; a
	// structured body (object/array) is serialized to JSON.
	bodyContent, bodyStructured, err := serializeBody(inputs[fieldBody])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}
	if len(bodyContent) > maxBodyBytes {
		return nil, fmt.Errorf("%s: request body exceeds maximum size of %d bytes (got %d)", ProviderName, maxBodyBytes, len(bodyContent))
	}

	// Get headers (make a copy to avoid modifying input)
	headers := make(map[string]any)
	if h, ok := inputs[fieldHeaders].(map[string]any); ok {
		for k, v := range h {
			headers[k] = v
		}
	}

	// Default Content-Type to application/json when a structured body was
	// serialized and the caller did not set a Content-Type header.
	if bodyStructured && !hasContentType(headers) {
		headers["Content-Type"] = "application/json"
	}

	// Build httpc client with timeout and user-supplied retry configuration.
	authProvider, _ := inputs["authProvider"].(string)
	scope, _ := inputs["scope"].(string)
	retryCfg := parseRetryConfig(inputs)
	httpcCfg := buildHTTPClientConfig(timeoutDuration, retryCfg)

	if authProvider != "" {
		token, err := getToken(ctx, authProvider, scope, timeoutDuration, httpcCfg)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
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

	// Parse acceptable status codes. When configured, a response whose status is
	// not acceptable causes the request to fail; when unconfigured, only 2xx is
	// treated as successful and non-2xx responses are returned without error.
	acc, err := parseAcceptableStatusCodes(inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	httpClient := httpc.NewClient(httpcCfg)

	autoParseJSON, _ := inputs["autoParseJson"].(bool)

	// If pagination is configured, use the paginated execution path
	if pagCfg != nil {
		if pagCfg.BodyTemplate != "" && !methodSupportsBody(method) {
			return nil, fmt.Errorf("%s: pagination bodyTemplate requires method POST, PUT, or PATCH (got %q)", ProviderName, method)
		}
		return p.executePaginated(ctx, httpClient, method, urlStr, bodyContent, headers, pagCfg, autoParseJSON, acc)
	}

	// Build the execute function for potential polling
	executeFunc := func() (*provider.Output, error) {
		return p.execute(ctx, httpClient, method, urlStr, bodyContent, headers, autoParseJSON, acc)
	}

	// If polling is configured, wrap execution in a poll loop
	if pollCfg != nil {
		return p.executePoll(ctx, nil, method, urlStr, bodyContent, headers, pollCfg, autoParseJSON, executeFunc)
	}

	return executeFunc()
}

func handlerToken(ctx context.Context, authProvider, scope string, timeoutDuration time.Duration, httpcCfg *httpc.ClientConfig) (*auth.Token, error) {
	authRegistry := auth.RegistryFromContext(ctx)
	if authRegistry == nil {
		return nil, fmt.Errorf("%s: no auth registry in context for provider %q", ProviderName, authProvider)
	}
	handler, err := authRegistry.Get(authProvider)
	if err != nil {
		return nil, fmt.Errorf("%s: auth provider %q not found: %w", ProviderName, authProvider, err)
	}

	requiresScope := auth.HasCapability(handler.Capabilities(), auth.CapScopesOnTokenRequest)
	if scope == "" && requiresScope {
		return nil, fmt.Errorf("%s: scope is required when authProvider %q is set (handler supports per-request scopes)", ProviderName, authProvider)
	}
	if scope != "" && !requiresScope {
		scope = ""
	}

	tokenOpts := auth.TokenOptions{
		Scope:       scope,
		MinValidFor: timeoutDuration + 60*time.Second,
	}

	tokenOpts = tokenOptionChain(ctx, authProvider, tokenOpts, withServerContext, withAssertion, withCallerType)

	token, err := handler.GetToken(ctx, tokenOpts)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to get auth token for %q: %w", ProviderName, authProvider, err)
	}
	if token == nil || token.AccessToken == "" {
		return nil, fmt.Errorf("%s: auth handler %q returned empty token", ProviderName, authProvider)
	}

	// Only wire 401 refresh when the token came from a refreshable source.
	httpcCfg.OnUnauthorized = func(unauthCtx context.Context) (string, error) {
		refreshOpts := tokenOpts
		refreshOpts.ForceRefresh = true
		t, refreshErr := handler.GetToken(unauthCtx, refreshOpts)
		if refreshErr != nil {
			return "", refreshErr
		}
		if t == nil || t.AccessToken == "" {
			return "", fmt.Errorf("%s: auth handler %q returned empty token on refresh", ProviderName, authProvider)
		}
		return fmt.Sprintf("%s %s", t.TokenType, t.AccessToken), nil
	}

	return token, nil
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
	acc statusAcceptance,
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

	// Determine whether the status is acceptable. When acceptableStatusCodes is
	// configured and the status is not acceptable, fail so the source-level
	// onError policy can decide whether to fall back or propagate.
	success := acc.isSuccess(resp.StatusCode)
	if acc.configured && !success {
		return nil, fmt.Errorf("%s: response status %d is not in acceptableStatusCodes (%s)", ProviderName, resp.StatusCode, acc.describe())
	}

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
			fieldSuccess:    success,
			fieldStatusCode: resp.StatusCode,
			fieldBody:       bodyValue,
			fieldHeaders:    respHeaders,
		},
	}, nil
}

// isJSONContentType returns true if the Content-Type header indicates a JSON response.
func isJSONContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(ct, "application/json") ||
		strings.HasSuffix(ct, "+json")
}

func extractHeaderToken(ctx context.Context, authProvider string) (bool, string) {
	canonicalProvider := http.CanonicalHeaderKey(authProvider)
	headerTokens := middleware.TokensFromContext(ctx)
	if headerTokens == nil {
		return false, ""
	}

	token, ok := headerTokens[canonicalProvider]
	if !ok {
		return false, ""
	}
	return true, token
}

func getToken(ctx context.Context, authProvider, scope string, timeoutDuration time.Duration, httpcCfg *httpc.ClientConfig) (*auth.Token, error) {
	// First, check if the token is provided in the request headers (e.g., via middleware).
	if ok, token := extractHeaderToken(ctx, authProvider); ok {
		if token == "" {
			return nil, fmt.Errorf("%s: authProvider %q token is empty in request headers", ProviderName, authProvider)
		}
		return &auth.Token{AccessToken: token, TokenType: "Bearer"}, nil
	}
	// If not in headers, obtain the token from the auth provider.
	return handlerToken(ctx, authProvider, scope, timeoutDuration, httpcCfg)
}

func getAssertion(ctx context.Context, authProvider string) string {
	if authProvider != middleware.OIDCProviderFromContext(ctx) {
		return ""
	}
	middlewareAssertion := middleware.AccessTokenFromContext(ctx)
	return middlewareAssertion
}

func determineCallerIdentity(ctx context.Context, authProvider string) sdkauth.CallerType {
	if authProvider != middleware.OIDCProviderFromContext(ctx) {
		return ""
	}
	claims := middleware.ClaimsFromContext(ctx)
	if claims == nil {
		return ""
	}
	callerType := claims.CallerType()

	switch callerType {
	case middleware.IdentityTypeApp:
		return sdkauth.CallerMachine
	case middleware.IdentityTypeUser:
		return sdkauth.CallerUser
	case middleware.IdentityTypeUnknown:
		return sdkauth.CallerUser
	}
	return sdkauth.CallerUser
}

type tokenOptionFunc func(context.Context, string, auth.TokenOptions) auth.TokenOptions

func withAssertion(ctx context.Context, authProvider string, opts auth.TokenOptions) auth.TokenOptions {
	assertions := getAssertion(ctx, authProvider)
	if assertions != "" {
		opts.Assertion = assertions
	}
	return opts
}

func withCallerType(ctx context.Context, authProvider string, opts auth.TokenOptions) auth.TokenOptions {
	callerType := determineCallerIdentity(ctx, authProvider)
	if callerType != "" {
		opts.Caller = callerType
	}
	return opts
}

func withServerContext(_ context.Context, _ string, opts auth.TokenOptions) auth.TokenOptions {
	opts.ServerContext = auth.Delegate
	return opts
}

func tokenOptionChain(ctx context.Context, authProvider string, opts auth.TokenOptions, f ...tokenOptionFunc) auth.TokenOptions {
	for _, fn := range f {
		opts = fn(ctx, authProvider, opts)
	}
	return opts
}
