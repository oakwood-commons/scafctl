// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
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
		return mcp.NewToolResultText("No cached plugins found. Use 'plugins install -f <solution>' to fetch plugins from catalogs, or copy plugin binaries to the plugin cache directory."), nil
	}

	return mcp.NewToolResultJSON(plugins)
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

	return mcp.NewToolResultJSON(items)
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
