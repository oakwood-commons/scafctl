package httpprovider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	ProviderName = "http"
	Version      = "1.0.0"
)

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
			Name:        ProviderName,
			DisplayName: "HTTP Client",
			Description: "Makes HTTP/HTTPS requests to APIs and web services",
			Version:     version,
			Category:    "network",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityAction,
				provider.CapabilityTransform,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"url": {
						Type:        provider.PropertyTypeString,
						Description: "The URL to request",
						Required:    true,
						Example:     "https://api.example.com/users",
						MaxLength:   ptrs.IntPtr(2048),
						Pattern:     `^https?://.*`,
					},
					"method": {
						Type:        provider.PropertyTypeString,
						Description: "HTTP method",
						Required:    false,
						Example:     "GET",
						MaxLength:   ptrs.IntPtr(10),
					},
					"headers": {
						Type:        provider.PropertyTypeAny,
						Description: "HTTP headers as key-value pairs",
						Required:    false,
					},
					"body": {
						Type:        provider.PropertyTypeString,
						Description: "Request body for POST/PUT/PATCH requests",
						Required:    false,
						MaxLength:   ptrs.IntPtr(1048576),
					},
					"timeout": {
						Type:        provider.PropertyTypeInt,
						Description: "Request timeout in seconds",
						Required:    false,
						Example:     30,
						Maximum:     ptrs.Float64Ptr(300.0),
					},
				},
			},
			OutputSchema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"statusCode": {
						Type:        provider.PropertyTypeInt,
						Description: "HTTP response status code",
						Example:     200,
					},
					"body": {
						Type:        provider.PropertyTypeString,
						Description: "Response body as string",
					},
					"headers": {
						Type:        provider.PropertyTypeAny,
						Description: "Response headers",
					},
				},
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
func (p *HTTPProvider) Execute(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
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
	url, _ := inputs["url"].(string)
	method, _ := inputs["method"].(string)
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	// Get timeout
	timeout := 30
	if t, ok := inputs["timeout"].(int); ok && t > 0 {
		timeout = t
	}

	// Create request
	var bodyReader io.Reader
	if body, ok := inputs["body"].(string); ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	if headers, ok := inputs["headers"].(map[string]any); ok {
		for key, value := range headers {
			if strValue, ok := value.(string); ok {
				req.Header.Set(key, strValue)
			}
		}
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
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

	// Return output
	return &provider.Output{
		Data: map[string]any{
			"statusCode": resp.StatusCode,
			"body":       string(respBody),
			"headers":    respHeaders,
		},
	}, nil
}
