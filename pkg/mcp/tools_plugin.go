// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
)

// registerPluginTools registers all plugin-related MCP tools.
func (s *Server) registerPluginTools() {
	listPluginsTool := mcp.NewTool("list_plugins",
		mcp.WithDescription(fmt.Sprintf(
			"List cached plugin binaries in the %s plugin cache. Returns name, version, platform, path, and size for each cached plugin. Plugins are cached after being fetched from catalogs or installed locally.",
			s.name,
		)),
		mcp.WithTitleAnnotation("List Cached Plugins"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.addTool(listPluginsTool, s.handleListPlugins)

	catalogListPluginsTool := mcp.NewTool("catalog_list_plugins",
		mcp.WithDescription(
			"List plugin artifacts (providers and auth handlers) available in catalogs -- "+
				"the remote counterpart to 'list_plugins' (which shows only locally cached "+
				"binaries). Searches the local catalog and remote OCI catalog(s) and returns "+
				"each plugin's name, kind (provider or auth-handler), version, catalog, and digest. "+
				"Use this to discover what plugins exist, list all versions of a specific plugin "+
				"(set 'name'), or check whether a newer version is available (combine 'name' with a "+
				"'version' constraint). Results are deduplicated across catalogs."),
		mcp.WithTitleAnnotation("Catalog List Plugins"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("name",
			mcp.Description("Filter by plugin name (exact match). When set, all versions of that plugin are returned."),
		),
		mcp.WithString("version",
			mcp.Description("Semver constraint to filter versions (e.g. '>=1.2.0', '~1.4'). Requires 'name' to be meaningful; applied to the listed versions."),
		),
		mcp.WithString("catalog",
			mcp.Description("Catalog name to list from. Omit for the default catalog, use 'all' for every registered catalog, or 'local' for the local catalog only."),
		),
	)
	s.addTool(catalogListPluginsTool, s.handleCatalogListPlugins)

	pluginCachePathTool := mcp.NewTool("get_plugin_cache_path",
		mcp.WithDescription(fmt.Sprintf(
			"Get the path to the %s plugin cache directory. Useful for debugging plugin discovery and installation issues.",
			s.name,
		)),
		mcp.WithTitleAnnotation("Get Plugin Cache Path"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.addTool(pluginCachePathTool, s.handleGetPluginCachePath)

	// list_official_providers
	listOfficialProvidersTool := mcp.NewTool("list_official_providers",
		mcp.WithDescription(fmt.Sprintf(
			"List official first-party providers distributed as external plugins. "+
				"These providers are auto-fetched from the OCI catalog when a solution references them. "+
				"Returns name, catalog reference, and default version for each. "+
				"Use this to determine whether a provider is built-in (compiled into %s) or official "+
				"(auto-fetched as a plugin). Solutions should declare official providers in bundle.plugins "+
				"for reproducibility.",
			s.name,
		)),
		mcp.WithTitleAnnotation("List Official Providers"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.addTool(listOfficialProvidersTool, s.handleListOfficialProviders)

	// get_signature_policy
	getSignaturePolicyTool := mcp.NewTool("get_signature_policy",
		mcp.WithDescription(fmt.Sprintf(
			"Get the current plugin signature verification policy for %s. "+
				"Returns the effective signature mode (off, warn, enforce), "+
				"trusted OIDC issuers, trusted identity patterns, and whether "+
				"cosign verification is available in this build. "+
				"Use this to inspect how plugin binary signatures are verified.",
			s.name,
		)),
		mcp.WithTitleAnnotation("Get Plugin Signature Policy"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.addTool(getSignaturePolicyTool, s.handleGetSignaturePolicy)
}

// handleListPlugins lists cached plugin binaries.
func (s *Server) handleListPlugins(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cache := plugin.NewCache(paths.PluginCacheDir())
	plugins, err := cache.List()
	if err != nil {
		return newStructuredError(ErrCodeConfigError, fmt.Sprintf("failed to list plugins: %v", err),
			WithSuggestion("Check that the plugin cache directory exists and is readable"),
		), nil
	}

	if len(plugins) == 0 {
		// Return an empty envelope (not plain text) so strict MCP clients still
		// receive a valid structuredContent record.
		return mcp.NewToolResultJSON(map[string]any{
			"plugins": []any{},
			"message": "No cached plugins found. Use 'plugins install -f <solution>' to fetch plugins from catalogs, or copy plugin binaries to the plugin cache directory.",
		})
	}

	return mcp.NewToolResultJSON(map[string]any{"plugins": plugins})
}

// pluginCatalogEntry is the MCP output shape for a plugin artifact discovered in
// a catalog (as opposed to a locally cached binary, which handleListPlugins
// reports). It is intentionally flat and stable for AI consumers.
type pluginCatalogEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
	Catalog string `json:"catalog,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// handleCatalogListPlugins lists plugin artifacts (provider + auth-handler)
// available across the local and remote catalog(s). It is a thin wrapper: it
// gathers the catalogs to query and delegates the multi-kind listing,
// version-constraint filtering, and dedup to the pkg/catalog domain.
func (s *Server) handleCatalogListPlugins(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")
	versionConstraint := request.GetString("version", "")
	catalogFilter := request.GetString("catalog", "")

	// Gather the catalogs to query: local (unless a non-local filter excludes
	// it) and the requested remote scope.
	var catalogs []catalog.Catalog
	if catalogFilter == "" || strings.EqualFold(catalogFilter, "local") || strings.EqualFold(catalogFilter, "all") {
		if localCat, err := catalog.NewLocalCatalog(s.logger); err != nil {
			s.logger.V(1).Info("local catalog not available for plugin listing", "error", err)
		} else {
			catalogs = append(catalogs, localCat)
		}
	}
	if !strings.EqualFold(catalogFilter, "local") {
		for _, rc := range s.buildRemoteCatalogs(catalogFilter) {
			catalogs = append(catalogs, rc)
		}
	}

	// List provider + auth-handler artifacts from every catalog. Failures are
	// tolerated only when another catalog succeeds (aggregate 'all' search); if
	// no catalog can be queried, return a structured error rather than an empty
	// result so an unreachable/rejected catalog is not misread as "no plugins".
	// Enumeration-not-supported is benign (the registry simply cannot list) and
	// is never treated as a hard failure.
	var artifacts []catalog.ArtifactInfo
	var okCount int
	var lastErr error
	var failedCatalog string
	for _, cat := range catalogs {
		infos, err := catalog.ListAcrossKinds(ctx, cat, catalog.PluginKinds(), name, s.logger)
		if err != nil {
			if catalog.IsEnumerationNotSupported(err) {
				s.logger.V(1).Info("catalog does not support enumeration, skipping",
					"catalog", cat.Name())
				continue
			}
			s.logger.V(1).Info("plugin listing failed for catalog",
				"catalog", cat.Name(), "error", err)
			lastErr = err
			failedCatalog = cat.Name()
			continue
		}
		okCount++
		artifacts = append(artifacts, infos...)
	}

	// No catalog could be queried and at least one failed hard: surface it.
	if okCount == 0 && lastErr != nil {
		return newStructuredError(ErrCodeConfigError,
			fmt.Sprintf("failed to list plugins from catalog %q: %v", failedCatalog, lastErr),
			WithSuggestion("Check the catalog is reachable and your credentials are valid (e.g. 'auth login'). Use catalog='all' to search other configured catalogs."),
		), nil
	}

	// Apply the optional version constraint, then dedup across catalogs.
	filtered, err := catalog.FilterByVersionConstraint(artifacts, versionConstraint)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid version constraint: %v", err),
			WithSuggestion("Use a valid semver constraint such as '>=1.2.0' or '~1.4'."),
		), nil
	}
	filtered = catalog.DeduplicateArtifacts(filtered)

	// Deterministic ordering (name, then kind, then semver-descending version)
	// so repeated calls and cross-run diffs by AI consumers are stable
	// regardless of catalog iteration order. Version uses a semver-aware compare
	// (newest first) so 10.0.0 sorts before 2.0.0; entries without a parsed
	// version fall back to a stable string compare.
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.Reference.Name != b.Reference.Name {
			return a.Reference.Name < b.Reference.Name
		}
		if a.Reference.Kind != b.Reference.Kind {
			return a.Reference.Kind < b.Reference.Kind
		}
		vi, vj := a.Reference.Version, b.Reference.Version
		switch {
		case vi != nil && vj != nil:
			if vi.Equal(vj) {
				return false
			}
			return vi.GreaterThan(vj) // newest first
		case vi != nil:
			return true // parsed versions sort before unparsed
		case vj != nil:
			return false
		default:
			return false
		}
	})

	entries := make([]pluginCatalogEntry, 0, len(filtered))
	for _, a := range filtered {
		version := ""
		if a.Reference.Version != nil {
			version = a.Reference.Version.String()
		}
		entries = append(entries, pluginCatalogEntry{
			Name:    a.Reference.Name,
			Kind:    a.Reference.Kind.String(),
			Version: version,
			Catalog: a.Catalog,
			Digest:  a.Digest,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"plugins": entries,
		"count":   len(entries),
	})
}

// handleGetPluginCachePath returns the plugin cache directory path.
func (s *Server) handleGetPluginCachePath(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(paths.PluginCacheDir()), nil
}

// officialProviderItem is the response structure for list_official_providers.
type officialProviderItem struct {
	Name           string `json:"name"`
	CatalogRef     string `json:"catalogRef"`
	DefaultVersion string `json:"defaultVersion"`
}

// handleListOfficialProviders lists all official first-party providers that are
// distributed as external plugins and auto-fetched from the OCI catalog.
func (s *Server) handleListOfficialProviders(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reg := official.RegistryFromContext(s.ctx)
	if reg == nil {
		reg = official.NewRegistry()
	}

	names := reg.Names()
	items := make([]officialProviderItem, 0, len(names))
	for _, name := range names {
		p, _ := reg.Get(name)
		items = append(items, officialProviderItem{
			Name:           p.Name,
			CatalogRef:     p.CatalogRef,
			DefaultVersion: p.DefaultVersion,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{"providers": items})
}

// signaturePolicyResponse is the response structure for get_signature_policy.
type signaturePolicyResponse struct {
	Mode              string   `json:"mode"`
	TrustedIssuers    []string `json:"trustedIssuers,omitempty"`
	TrustedIdentities []string `json:"trustedIdentities,omitempty"`
	CosignAvailable   bool     `json:"cosignAvailable"`
}

// handleGetSignaturePolicy returns the effective plugin signature verification policy.
func (s *Server) handleGetSignaturePolicy(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check context-level override first.
	policy := plugin.SignaturePolicyFromContext(s.ctx)

	// Fall back to config.
	if policy == nil {
		cfg := s.resolveConfig()
		if cfg != nil {
			policy = signaturePolicyFromConfig(cfg)
		}
	}

	// Default to off.
	resp := signaturePolicyResponse{Mode: string(plugin.SignatureModeOff)}
	if policy != nil && policy.IsEnabled() {
		resp.Mode = string(policy.Mode)
		resp.TrustedIssuers = policy.TrustedIssuers
		resp.TrustedIdentities = policy.TrustedIdentities
	}

	// Probe cosign availability by attempting verification with an enabled
	// policy. The stub returns ErrCosignNotAvailable; the real implementation
	// returns a ref-parsing error (neither is ErrCosignNotAvailable == false).
	verifier := plugin.NewSignatureVerifier()
	probePolicy := &plugin.SignaturePolicy{Mode: plugin.SignatureModeWarn, TrustedIssuers: []string{"*"}, TrustedIdentities: []string{"*"}}
	_, err := verifier.VerifySignature(context.Background(), "probe://noop", probePolicy)
	resp.CosignAvailable = err == nil || !errors.Is(err, plugin.ErrCosignNotAvailable)

	return mcp.NewToolResultJSON(resp)
}

// signaturePolicyFromConfig converts config to a SignaturePolicy using the
// shared plugin.SignaturePolicyFromRaw helper.
// Returns nil when mode is off, empty, or invalid.
func signaturePolicyFromConfig(cfg *config.Config) *plugin.SignaturePolicy {
	if cfg == nil {
		return nil
	}
	sigCfg := cfg.Plugins.Signatures
	policy, err := plugin.SignaturePolicyFromRaw(sigCfg.Mode, sigCfg.TrustedIssuers, sigCfg.TrustedIdentities)
	if err != nil {
		return nil
	}
	return policy
}
