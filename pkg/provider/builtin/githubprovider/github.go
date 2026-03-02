// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package githubprovider implements a provider for common GitHub API operations.
//
// It supports retrieving repository info, file content, releases, and pull requests
// via the GitHub REST API. Authentication is handled automatically using the
// configured GitHub auth handler.
package githubprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

// ProviderName is the registered name for this provider.
const ProviderName = "github"

// GitHubProvider implements common GitHub API operations as a resolver provider.
type GitHubProvider struct {
	descriptor *provider.Descriptor
	// httpClient can be overridden for testing
	httpClient *http.Client
}

// Option configures a GitHubProvider.
type Option func(*GitHubProvider)

// WithHTTPClient sets a custom HTTP client (useful for testing).
func WithHTTPClient(c *http.Client) Option {
	return func(p *GitHubProvider) {
		p.httpClient = c
	}
}

// NewGitHubProvider creates a new GitHub API provider.
func NewGitHubProvider(opts ...Option) *GitHubProvider {
	version, _ := semver.NewVersion("1.0.0")

	p := &GitHubProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "GitHub API",
			APIVersion:  "v1",
			Version:     version,
			Description: "Retrieve data from the GitHub REST API. Supports repository info, file content, releases, and pull requests. " +
				"Uses the configured GitHub auth handler automatically. " +
				"For arbitrary HTTP requests to GitHub, use the 'http' provider with authProvider: github.",
			Category:     "data",
			MockBehavior: "Returns mock data for the requested operation without making real API calls",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema(
				[]string{"operation", "owner", "repo"},
				map[string]*jsonschema.Schema{
					"operation": schemahelper.StringProp("GitHub API operation to perform",
						schemahelper.WithEnum(
							"get_repo",
							"get_file",
							"list_releases",
							"get_latest_release",
							"list_pull_requests",
							"get_pull_request",
						),
						schemahelper.WithExample("get_repo"),
					),
					"owner": schemahelper.StringProp("Repository owner (user or organization)",
						schemahelper.WithExample("octocat"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
					),
					"repo": schemahelper.StringProp("Repository name",
						schemahelper.WithExample("hello-world"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
					),
					"path": schemahelper.StringProp("File path within the repository (for get_file operation)",
						schemahelper.WithExample("README.md"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(1000)),
					),
					"ref": schemahelper.StringProp("Git reference (branch, tag, or commit SHA). Defaults to the repo's default branch.",
						schemahelper.WithExample("main"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(200)),
					),
					"number": schemahelper.IntProp("Pull request number (for get_pull_request operation)",
						schemahelper.WithMinimum(1),
						schemahelper.WithExample(42),
					),
					"state": schemahelper.StringProp("Filter by state for list operations",
						schemahelper.WithEnum("open", "closed", "all"),
						schemahelper.WithDefault("open"),
					),
					"per_page": schemahelper.IntProp("Number of results per page (max 100)",
						schemahelper.WithMinimum(1),
						schemahelper.WithMaximum(100),
						schemahelper.WithDefault(30),
					),
					"api_base": schemahelper.StringProp("GitHub API base URL. Defaults to https://api.github.com. Set for GitHub Enterprise.",
						schemahelper.WithDefault("https://api.github.com"),
						schemahelper.WithFormat("uri"),
					),
				},
			),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The API response data — structure varies by operation"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Get repository info",
					Description: "Fetch metadata for a GitHub repository",
					YAML: `operation: get_repo
owner: octocat
repo: hello-world`,
				},
				{
					Name:        "Get file content",
					Description: "Retrieve a file from a repository at a specific branch",
					YAML: `operation: get_file
owner: octocat
repo: hello-world
path: README.md
ref: main`,
				},
				{
					Name:        "Get latest release",
					Description: "Fetch the latest release of a repository",
					YAML: `operation: get_latest_release
owner: cli
repo: cli`,
				},
				{
					Name:        "List open pull requests",
					Description: "List open pull requests for a repository",
					YAML: `operation: list_pull_requests
owner: golang
repo: go
state: open
per_page: 10`,
				},
			},
			Links: []provider.Link{
				{Name: "GitHub REST API", URL: "https://docs.github.com/en/rest"},
			},
			Tags: []string{"github", "api", "data"},
		},
	}

	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Descriptor returns the provider metadata, schema, and capabilities.
func (p *GitHubProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute runs the requested GitHub API operation.
func (p *GitHubProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map input, got %T", ProviderName, input)
	}

	operation, _ := inputs["operation"].(string)
	owner, _ := inputs["owner"].(string)
	repo, _ := inputs["repo"].(string)
	apiBase, _ := inputs["api_base"].(string)
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	// Build the API URL based on operation
	var apiURL string
	var queryParams []string

	switch operation {
	case "get_repo":
		apiURL = fmt.Sprintf("%s/repos/%s/%s", apiBase, owner, repo)

	case "get_file":
		path, _ := inputs["path"].(string)
		if path == "" {
			return nil, fmt.Errorf("%s: 'path' is required for get_file operation", ProviderName)
		}
		apiURL = fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, owner, repo, path)
		if ref, ok := inputs["ref"].(string); ok && ref != "" {
			queryParams = append(queryParams, "ref="+ref)
		}

	case "list_releases":
		apiURL = fmt.Sprintf("%s/repos/%s/%s/releases", apiBase, owner, repo)
		addPaginationParams(inputs, &queryParams)

	case "get_latest_release":
		apiURL = fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBase, owner, repo)

	case "list_pull_requests":
		apiURL = fmt.Sprintf("%s/repos/%s/%s/pulls", apiBase, owner, repo)
		if state, ok := inputs["state"].(string); ok && state != "" {
			queryParams = append(queryParams, "state="+state)
		}
		addPaginationParams(inputs, &queryParams)

	case "get_pull_request":
		num, _ := getIntInput(inputs, "number")
		if num == 0 {
			return nil, fmt.Errorf("%s: 'number' is required for get_pull_request operation", ProviderName)
		}
		apiURL = fmt.Sprintf("%s/repos/%s/%s/pulls/%d", apiBase, owner, repo, num)

	default:
		return nil, fmt.Errorf("%s: unknown operation %q — supported: get_repo, get_file, list_releases, get_latest_release, list_pull_requests, get_pull_request", ProviderName, operation)
	}

	if len(queryParams) > 0 {
		apiURL += "?" + strings.Join(queryParams, "&")
	}

	// Make the request
	result, err := p.doRequest(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	// For get_file, decode base64 content if present
	if operation == "get_file" {
		if m, ok := result.(map[string]any); ok {
			if encoded, ok := m["content"].(string); ok {
				decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\n", ""))
				if err == nil {
					m["decoded_content"] = string(decoded)
				}
			}
		}
	}

	return &provider.Output{
		Data: map[string]any{
			"result": result,
		},
	}, nil
}

// doRequest performs an authenticated GitHub API request.
func (p *GitHubProvider) doRequest(ctx context.Context, url string) (any, error) {
	lgr := logger.FromContext(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Try to get GitHub auth token
	handler, handlerErr := auth.GetHandler(ctx, "github")
	if handlerErr == nil {
		token, tokenErr := handler.GetToken(ctx, auth.TokenOptions{})
		if tokenErr == nil {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
			lgr.V(2).Info("using GitHub auth token for API request")
		} else {
			lgr.V(1).Info("GitHub auth token unavailable, making unauthenticated request", "error", tokenErr)
		}
	} else {
		lgr.V(1).Info("GitHub auth handler not available, making unauthenticated request", "error", handlerErr)
	}

	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // URL is intentionally user-provided via provider input
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Try to extract GitHub error message
		var ghErr map[string]any
		if json.Unmarshal(body, &ghErr) == nil {
			if msg, ok := ghErr["message"].(string); ok {
				return nil, fmt.Errorf("GitHub API error (HTTP %d): %s", resp.StatusCode, msg)
			}
		}
		return nil, fmt.Errorf("GitHub API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response JSON: %w", err)
	}

	return result, nil
}

// addPaginationParams adds per_page query parameter if provided.
func addPaginationParams(inputs map[string]any, params *[]string) {
	if perPage, _ := getIntInput(inputs, "per_page"); perPage > 0 {
		*params = append(*params, fmt.Sprintf("per_page=%d", perPage))
	}
}

// getIntInput extracts an integer from the input map, handling both float64 (from JSON) and int.
func getIntInput(inputs map[string]any, key string) (int, bool) {
	v, ok := inputs[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
