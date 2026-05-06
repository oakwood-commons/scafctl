// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// maxResponseBody is the maximum size of API response bodies (10 MiB).
const maxResponseBody = 10 << 20

// registryEnumerator lists repository names for a specific registry type.
// Implementations return raw repository paths (e.g. "myorg/solutions/myapp")
// that parseRepositoryPath can interpret to extract artifact kinds and names.
type registryEnumerator interface {
	// enumerate returns all repository paths visible to this catalog.
	// Returns ErrEnumerationNotSupported if the registry cannot be enumerated.
	enumerate(ctx context.Context) ([]string, error)
}

// enumeratorConfig bundles values needed by selectEnumerator.
// Avoids coupling enumerators to RemoteCatalogConfig.
type enumeratorConfig struct {
	authHandlerName string
	authHandler     scafctlauth.Handler
	authScope       string
	registry        string
	repository      string
	client          *auth.Client
	insecure        bool
	logger          logr.Logger
}

// selectEnumerator returns the appropriate enumerator based on the auth handler
// name and registry URL. Falls back to OCI _catalog for unknown registries.
func selectEnumerator(cfg enumeratorConfig) registryEnumerator {
	// Try auth handler name first (explicit config)
	switch cfg.authHandlerName {
	case "gcp":
		if e, err := newGCPEnumerator(cfg); err == nil {
			cfg.logger.V(1).Info("using GCP Artifact Registry enumerator",
				"registry", cfg.registry)
			return e
		}
	case "ford-quay":
		cfg.logger.V(1).Info("using Quay API enumerator",
			"registry", cfg.registry)
		return newQuayEnumerator(cfg)
	case "github":
		if e, err := newGHCREnumerator(cfg); err == nil {
			cfg.logger.V(1).Info("using GHCR Packages API enumerator",
				"registry", cfg.registry)
			return e
		}
	}

	// Try hostname-based detection as fallback
	switch {
	case strings.HasSuffix(cfg.registry, "-docker.pkg.dev"):
		if e, err := newGCPEnumerator(cfg); err == nil {
			cfg.logger.V(1).Info("using GCP Artifact Registry enumerator (detected from hostname)",
				"registry", cfg.registry)
			return e
		}
	case strings.HasSuffix(cfg.registry, ".quay.io") || cfg.registry == "quay.io":
		cfg.logger.V(1).Info("using Quay API enumerator (detected from hostname)",
			"registry", cfg.registry)
		return newQuayEnumerator(cfg)
	case cfg.registry == "ghcr.io":
		if e, err := newGHCREnumerator(cfg); err == nil {
			cfg.logger.V(1).Info("using GHCR Packages API enumerator (detected from hostname)",
				"registry", cfg.registry)
			return e
		}
	}

	cfg.logger.V(1).Info("using OCI _catalog enumerator",
		"registry", cfg.registry)
	return newOCICatalogEnumerator(cfg)
}

// --- OCI _catalog enumerator (default/fallback) ---

// ociCatalogEnumerator uses the standard OCI Distribution _catalog endpoint.
// This is the fallback for registries without a known vendor-specific API.
type ociCatalogEnumerator struct {
	registry string
	client   *auth.Client
	insecure bool
	logger   logr.Logger
}

func newOCICatalogEnumerator(cfg enumeratorConfig) *ociCatalogEnumerator {
	return &ociCatalogEnumerator{
		registry: cfg.registry,
		client:   cfg.client,
		insecure: cfg.insecure,
		logger:   cfg.logger,
	}
}

func (e *ociCatalogEnumerator) setClient(client *auth.Client) {
	e.client = client
}

func (e *ociCatalogEnumerator) enumerate(ctx context.Context) ([]string, error) {
	// Try ORAS _catalog first
	repos, err := e.enumerateORAS(ctx)
	if err == nil {
		return repos, nil
	}

	// On 401/403, try direct HTTP (handles bare-401 registries)
	var errResp *errcode.ErrorResponse
	if errors.As(err, &errResp) {
		switch errResp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			e.logger.V(1).Info("ORAS _catalog auth failed, trying direct HTTP",
				"registry", e.registry, "status", errResp.StatusCode)

			repos, directErr := e.enumerateDirect(ctx)
			if directErr != nil {
				e.logger.V(1).Info("direct _catalog also failed",
					"registry", e.registry, "error", directErr.Error())
				// Check if credentials exist — if so, it's an enumeration
				// limitation, not an auth failure.
				if e.hasCredentials(ctx) {
					return nil, fmt.Errorf("oci _catalog for %s: %w", e.registry, ErrEnumerationNotSupported)
				}
				return nil, fmt.Errorf("oci _catalog for %s: %w", e.registry, err)
			}
			return repos, nil

		case http.StatusNotFound:
			return nil, fmt.Errorf("oci _catalog for %s: %w", e.registry, ErrEnumerationNotSupported)
		}
	}

	// Context timeout/cancellation
	if ctx.Err() != nil {
		return nil, fmt.Errorf("oci _catalog for %s (timed out): %w", e.registry, ErrEnumerationNotSupported)
	}

	return nil, fmt.Errorf("oci _catalog for %s: %w", e.registry, err)
}

func (e *ociCatalogEnumerator) enumerateORAS(ctx context.Context) ([]string, error) {
	reg, err := remote.NewRegistry(e.registry)
	if err != nil {
		return nil, fmt.Errorf("creating registry client: %w", err)
	}
	reg.Client = e.client

	var repos []string
	err = reg.Repositories(ctx, "", func(batch []string) error {
		repos = append(repos, batch...)
		return nil
	})
	return repos, err
}

func (e *ociCatalogEnumerator) enumerateDirect(ctx context.Context) ([]string, error) {
	scheme := "https"
	if e.insecure {
		scheme = "http"
	}

	var allRepos []string
	catalogURL := fmt.Sprintf("%s://%s/v2/_catalog", scheme, e.registry)

	for catalogURL != "" {
		repos, next, err := e.fetchCatalogPage(ctx, catalogURL)
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repos...)
		catalogURL = next
	}

	return allRepos, nil
}

func (e *ociCatalogEnumerator) fetchCatalogPage(ctx context.Context, catalogURL string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating _catalog request: %w", err)
	}

	// Inject credentials directly — the registry won't negotiate via WWW-Authenticate.
	if e.client.Credential != nil {
		cred, credErr := e.client.Credential(ctx, e.registry)
		if credErr == nil {
			switch {
			case cred.AccessToken != "":
				req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
			case cred.Username != "" && cred.Password != "":
				req.SetBasicAuth(cred.Username, cred.Password)
			}
		}
	}

	httpClient := e.client.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("_catalog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("_catalog returned status %d", resp.StatusCode)
	}

	var page struct {
		Repositories []string `json:"repositories"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, "", fmt.Errorf("reading _catalog response: %w", err)
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("decoding _catalog response: %w", err)
	}

	nextURL := parseLinkHeader(resp.Header.Get("Link"), catalogURL)
	return page.Repositories, nextURL, nil
}

func (e *ociCatalogEnumerator) hasCredentials(ctx context.Context) bool {
	if e.client.Credential == nil {
		return false
	}
	cred, err := e.client.Credential(ctx, e.registry)
	if err != nil {
		return false
	}
	return cred.AccessToken != "" || (cred.Username != "" && cred.Password != "")
}

// --- Quay enumerator ---

// quayEnumerator uses the Quay REST API /api/v1/repository endpoint.
type quayEnumerator struct {
	registry  string
	namespace string
	client    *auth.Client
	insecure  bool
	logger    logr.Logger
}

func (e *quayEnumerator) setClient(client *auth.Client) {
	e.client = client
}

func newQuayEnumerator(cfg enumeratorConfig) *quayEnumerator {
	// Quay's namespace is the top-level org/user that owns the repositories.
	// In catalog config this maps to the repository field (e.g. "ford-solutions").
	return &quayEnumerator{
		registry:  cfg.registry,
		namespace: cfg.repository,
		client:    cfg.client,
		insecure:  cfg.insecure,
		logger:    cfg.logger,
	}
}

func (e *quayEnumerator) enumerate(ctx context.Context) ([]string, error) {
	if e.namespace == "" {
		return nil, fmt.Errorf("quay enumeration for %s: no repository namespace configured: %w",
			e.registry, ErrEnumerationNotSupported)
	}

	scheme := "https"
	if e.insecure {
		scheme = "http"
	}

	apiURL := fmt.Sprintf("%s://%s/api/v1/repository?namespace=%s", scheme, e.registry, url.QueryEscape(e.namespace))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("quay enumeration for %s: creating request: %w", e.registry, err)
	}

	// Quay API accepts the OAuth token directly as Bearer.
	if e.client.Credential != nil {
		cred, credErr := e.client.Credential(ctx, e.registry)
		if credErr == nil && cred.Password != "" {
			req.Header.Set("Authorization", "Bearer "+cred.Password)
		}
	}

	httpClient := e.client.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("quay enumeration for %s: request failed: %w", e.registry, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quay enumeration for %s: status %d: %w",
			e.registry, resp.StatusCode, ErrEnumerationNotSupported)
	}

	var result struct {
		Repositories []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"repositories"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("quay enumeration for %s: reading response: %w", e.registry, err)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("quay enumeration for %s: decoding response: %w", e.registry, err)
	}

	repos := make([]string, 0, len(result.Repositories))
	for _, r := range result.Repositories {
		repos = append(repos, r.Namespace+"/"+r.Name)
	}

	e.logger.V(1).Info("Quay API returned repositories",
		"count", len(repos), "namespace", e.namespace)
	return repos, nil
}

// --- GCP Artifact Registry enumerator ---

// gcpEnumerator uses the Artifact Registry REST API to list packages.
// API: GET https://artifactregistry.googleapis.com/v1/projects/{project}/locations/{location}/repositories/{repo}/packages
type gcpEnumerator struct {
	project     string
	location    string
	gcpRepo     string // the AR repository name (not the OCI repository path)
	repository  string // the full OCI repository path (for path construction)
	authHandler scafctlauth.Handler
	authScope   string
	httpClient  *http.Client // HTTP client for API requests; falls back to http.DefaultClient
	apiBaseURL  string       // override for testing; defaults to gcpDefaultAPIBase
	insecure    bool
	logger      logr.Logger
}

// gcpDefaultAPIBase is the production Artifact Registry API endpoint.
const gcpDefaultAPIBase = "https://artifactregistry.googleapis.com"

// parseGCPRegistryURL extracts GCP Artifact Registry components from the
// registry and repository fields.
//
// Registry: "us-central1-docker.pkg.dev"
// Repository: "my-project/my-repo" or "my-project/my-repo/extra/path"
// Returns: location="us-central1", project="my-project", gcpRepo="my-repo"
func parseGCPRegistryURL(registry, repository string) (location, project, gcpRepo string, err error) {
	if !strings.HasSuffix(registry, "-docker.pkg.dev") {
		return "", "", "", fmt.Errorf("not a GCP Artifact Registry host: %s", registry)
	}
	location = strings.TrimSuffix(registry, "-docker.pkg.dev")

	parts := strings.SplitN(repository, "/", 3)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("GCP repository must be project/repo, got: %q", repository)
	}
	return location, parts[0], parts[1], nil
}

func newGCPEnumerator(cfg enumeratorConfig) (*gcpEnumerator, error) {
	location, project, gcpRepo, err := parseGCPRegistryURL(cfg.registry, cfg.repository)
	if err != nil {
		return nil, err
	}

	httpClient := cfg.client.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &gcpEnumerator{
		project:     project,
		location:    location,
		gcpRepo:     gcpRepo,
		repository:  cfg.repository,
		authHandler: cfg.authHandler,
		authScope:   cfg.authScope,
		httpClient:  httpClient,
		apiBaseURL:  gcpDefaultAPIBase,
		insecure:    cfg.insecure,
		logger:      cfg.logger,
	}, nil
}

func (e *gcpEnumerator) enumerate(ctx context.Context) ([]string, error) {
	token, err := e.getToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcp enumeration for %s/%s: %w", e.project, e.gcpRepo, err)
	}

	var allRepos []string
	pageToken := ""

	for {
		repos, nextToken, apiErr := e.fetchPage(ctx, token, pageToken)
		if apiErr != nil {
			return nil, fmt.Errorf("gcp enumeration for %s/%s: %w", e.project, e.gcpRepo, apiErr)
		}
		allRepos = append(allRepos, repos...)
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	e.logger.V(1).Info("GCP AR API returned packages",
		"count", len(allRepos), "project", e.project, "repository", e.gcpRepo)
	return allRepos, nil
}

func (e *gcpEnumerator) getToken(ctx context.Context) (string, error) {
	if e.authHandler == nil {
		return "", fmt.Errorf("no auth handler for GCP enumeration: %w", ErrEnumerationNotSupported)
	}

	opts := scafctlauth.TokenOptions{}
	if e.authScope != "" {
		opts.Scope = e.authScope
	}

	tok, err := e.authHandler.GetToken(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("getting GCP token: %w", err)
	}
	return tok.AccessToken, nil
}

func (e *gcpEnumerator) fetchPage(ctx context.Context, token, pageToken string) ([]string, string, error) {
	apiURL := fmt.Sprintf(
		"%s/v1/projects/%s/locations/%s/repositories/%s/packages?pageSize=1000",
		e.apiBaseURL,
		url.PathEscape(e.project), url.PathEscape(e.location), url.PathEscape(e.gcpRepo),
	)
	if pageToken != "" {
		apiURL += "&pageToken=" + url.QueryEscape(pageToken)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Packages []struct {
			Name string `json:"name"`
		} `json:"packages"`
		NextPageToken string `json:"nextPageToken"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}

	// Convert GCP package names to OCI repository paths.
	// GCP name: "projects/{p}/locations/{l}/repositories/{r}/packages/{path}"
	// We need: "{repository}/{path}" where {path} is like "solutions/my-solution"
	// and {repository} is the full OCI repo path from config.
	repos := make([]string, 0, len(result.Packages))
	prefix := fmt.Sprintf("projects/%s/locations/%s/repositories/%s/packages/",
		e.project, e.location, e.gcpRepo)

	for _, pkg := range result.Packages {
		pkgPath := strings.TrimPrefix(pkg.Name, prefix)
		if pkgPath == pkg.Name {
			// Unexpected format — skip
			continue
		}
		// GCP URL-encodes slashes in package names as %2F
		pkgPath = strings.ReplaceAll(pkgPath, "%2F", "/")
		// Build full OCI path: repository/kind-plural/artifact-name
		repos = append(repos, e.repository+"/"+pkgPath)
	}

	return repos, result.NextPageToken, nil
}

// parseLinkHeader extracts the next page URL from a Link header.
// Format: <url>; rel="next"
func parseLinkHeader(header, baseURL string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end < 0 || end <= start {
			continue
		}
		link := part[start+1 : end]
		// Handle relative URLs
		if strings.HasPrefix(link, "/") {
			idx := strings.Index(baseURL, "://")
			if idx >= 0 {
				hostEnd := strings.Index(baseURL[idx+3:], "/")
				if hostEnd >= 0 {
					link = baseURL[:idx+3+hostEnd] + link
				}
			}
		}
		return link
	}
	return ""
}

// --- GHCR (GitHub Container Registry) enumerator ---

// ghcrEnumerator uses the GitHub Packages REST API to list container packages.
// API: GET https://api.github.com/orgs/{org}/packages?package_type=container
// This works for public packages without authentication.
type ghcrEnumerator struct {
	org         string // GitHub organization (derived from repository field)
	repository  string // OCI repository path (e.g. "oakwood-commons")
	client      *auth.Client
	httpClient  *http.Client
	apiBaseURL  string // override for testing; defaults to ghcrDefaultAPIBase
	authHandler scafctlauth.Handler
	authScope   string
	logger      logr.Logger
}

func (e *ghcrEnumerator) setClient(client *auth.Client) {
	e.client = client
}

// ghcrDefaultAPIBase is the production GitHub API endpoint.
const ghcrDefaultAPIBase = "https://api.github.com"

// ghcrMaxPages caps pagination to avoid unbounded loops.
const ghcrMaxPages = 50

// newGHCREnumerator creates a GHCR enumerator from the config.
// The repository field is used as the GitHub org/user name.
func newGHCREnumerator(cfg enumeratorConfig) (*ghcrEnumerator, error) {
	if cfg.repository == "" {
		return nil, fmt.Errorf("ghcr enumeration requires a repository (org name)")
	}

	// The org is the first segment of the repository path.
	org := cfg.repository
	if idx := strings.Index(org, "/"); idx > 0 {
		org = org[:idx]
	}

	httpClient := cfg.client.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &ghcrEnumerator{
		org:         org,
		repository:  cfg.repository,
		client:      cfg.client,
		httpClient:  httpClient,
		apiBaseURL:  ghcrDefaultAPIBase,
		authHandler: cfg.authHandler,
		authScope:   cfg.authScope,
		logger:      cfg.logger,
	}, nil
}

func (e *ghcrEnumerator) enumerate(ctx context.Context) ([]string, error) {
	// Try the org endpoint first. If it returns 404, the namespace is likely
	// a user account, so fall back to the users endpoint.
	repos, err := e.enumerateWithOwnerType(ctx, "orgs")
	if err != nil {
		// 404 from /orgs/ means this is a user namespace -- retry with /users/.
		if strings.Contains(err.Error(), "API returned status 404") {
			e.logger.V(1).Info("org endpoint returned 404, trying user endpoint",
				"org", e.org)
			return e.enumerateWithOwnerType(ctx, "users")
		}
		return nil, err
	}
	return repos, nil
}

func (e *ghcrEnumerator) enumerateWithOwnerType(ctx context.Context, ownerType string) ([]string, error) {
	var allRepos []string

	apiURL := fmt.Sprintf("%s/%s/%s/packages?package_type=container&per_page=100",
		e.apiBaseURL, ownerType, url.PathEscape(e.org))

	for page := 0; apiURL != "" && page < ghcrMaxPages; page++ {
		repos, nextURL, err := e.fetchPage(ctx, apiURL)
		if err != nil {
			return nil, fmt.Errorf("ghcr enumeration for %s: %w", e.org, err)
		}
		allRepos = append(allRepos, repos...)
		apiURL = nextURL
	}

	e.logger.V(1).Info("GHCR Packages API returned repositories",
		"count", len(allRepos), "org", e.org)
	return allRepos, nil
}

func (e *ghcrEnumerator) fetchPage(ctx context.Context, apiURL string) ([]string, string, error) {
	// Try unauthenticated first (works for public orgs). If 401/403,
	// retry with credentials — the OCI credential password may be a
	// GitHub PAT that also works for the Packages API.
	resp, err := e.doRequest(ctx, apiURL, "")
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		resp, err = e.retryWithCredentials(ctx, apiURL)
		if err != nil {
			return nil, "", err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			bin := settings.BinaryNameFromContext(ctx)
			return nil, "", fmt.Errorf(
				"GHCR Packages API returned %d — your GitHub token may lack the read:packages scope; "+
					"run: %s auth login github: %w",
				resp.StatusCode, bin, ErrEnumerationNotSupported)
		}
		return nil, "", fmt.Errorf("API returned status %d: %w",
			resp.StatusCode, ErrEnumerationNotSupported)
	}

	var packages []struct {
		Name string `json:"name"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(body, &packages); err != nil {
		return nil, "", fmt.Errorf("decoding response: %w", err)
	}

	// Convert package names to OCI repository paths.
	// GHCR package names use "/" separators (e.g. "solutions/starter-kit").
	// Full OCI path: "{repository}/{package-name}" → "oakwood-commons/solutions/starter-kit"
	repos := make([]string, 0, len(packages))
	for _, pkg := range packages {
		repos = append(repos, e.repository+"/"+pkg.Name)
	}

	nextURL := parseLinkHeader(resp.Header.Get("Link"), apiURL)
	return repos, nextURL, nil
}

// retryWithCredentials tries OCI credential store first, then falls back to
// auth handler bridge. This handles the case where the native credential store
// has a token that works for OCI pulls but not for the GitHub REST API.
func (e *ghcrEnumerator) retryWithCredentials(ctx context.Context, apiURL string) (*http.Response, error) {
	// Try OCI credential store (native store / docker config).
	if e.client.Credential != nil {
		cred, credErr := e.client.Credential(ctx, "ghcr.io")
		if credErr == nil && cred.Password != "" {
			e.logger.V(1).Info("retrying GHCR API with stored credentials")
			resp, err := e.doRequest(ctx, apiURL, cred.Password)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
				return resp, nil
			}
			resp.Body.Close()
			e.logger.V(1).Info("stored credentials rejected by GHCR API",
				"status", resp.StatusCode)
		}
	}

	// Fall back to auth handler bridge (e.g. GitHub OAuth token).
	if e.authHandler != nil {
		_, password, err := BridgeAuthToRegistry(ctx, e.authHandler, "ghcr.io", e.authScope)
		if err == nil && password != "" {
			e.logger.V(1).Info("retrying GHCR API with auth handler bridge",
				"handler", e.authHandler.Name())
			resp, reqErr := e.doRequest(ctx, apiURL, password)
			if reqErr != nil {
				return nil, reqErr
			}
			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
				return resp, nil
			}
			resp.Body.Close()
			e.logger.V(1).Info("auth handler bridge credentials also rejected by GHCR API",
				"handler", e.authHandler.Name(), "status", resp.StatusCode)
		}
	}

	// No credentials available — return an unauthorized response so the
	// caller can produce an appropriate error.
	return e.doRequest(ctx, apiURL, "")
}

// doRequest sends a GET request to the GitHub Packages API.
// If token is non-empty it is sent as a Bearer token.
func (e *ghcrEnumerator) doRequest(ctx context.Context, apiURL, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}
