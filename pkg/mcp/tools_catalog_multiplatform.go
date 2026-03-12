// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// registerCatalogMultiPlatformTools registers multi-platform catalog MCP tools.
func (s *Server) registerCatalogMultiPlatformTools() {
	listPlatformsTool := mcp.NewTool("catalog_list_platforms",
		mcp.WithDescription("List available platforms for a multi-platform plugin artifact in the local catalog. Returns the platform list (e.g., linux/amd64, darwin/arm64) for OCI image index artifacts, or indicates the artifact is single-platform. Use 'catalog_list' first to find plugin names, then inspect platforms."),
		mcp.WithTitleAnnotation("Catalog List Platforms"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("reference",
			mcp.Required(),
			mcp.Description("Artifact reference in the format 'name' or 'name@version' (e.g., 'my-provider', 'my-provider@1.2.3')"),
		),
		mcp.WithString("kind",
			mcp.Required(),
			mcp.Description("Artifact kind: provider, auth-handler"),
			mcp.Enum("provider", "auth-handler"),
		),
	)
	s.mcpServer.AddTool(listPlatformsTool, s.handleCatalogListPlatforms)

	buildPluginTool := mcp.NewTool("build_plugin",
		mcp.WithDescription("Build a multi-platform plugin artifact into the local catalog as an OCI image index. Each platform entry maps an OS/architecture pair to a local binary path. Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64. Use this to package cross-compiled plugin binaries for distribution."),
		mcp.WithTitleAnnotation("Build Plugin"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Plugin name (e.g., 'my-provider')"),
		),
		mcp.WithString("kind",
			mcp.Required(),
			mcp.Description("Plugin kind: provider or auth-handler"),
			mcp.Enum("provider", "auth-handler"),
		),
		mcp.WithString("version",
			mcp.Required(),
			mcp.Description("Semantic version for the plugin (e.g., '1.0.0')"),
		),
		mcp.WithObject("platforms",
			mcp.Required(),
			mcp.Description("Map of platform to binary path. Keys are 'os/arch' (e.g., 'linux/amd64'), values are absolute file paths to the compiled binary."),
		),
		mcp.WithBoolean("force",
			mcp.Description("Overwrite an existing artifact with the same name and version (default: false)"),
		),
	)
	s.mcpServer.AddTool(buildPluginTool, s.handleBuildPlugin)
}

// handleCatalogListPlatforms lists platforms for a multi-platform catalog artifact.
func (s *Server) handleCatalogListPlatforms(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reference, err := request.RequireString("reference")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("reference"),
			WithSuggestion("Provide a catalog reference (e.g., 'my-provider@1.0.0')"),
			WithRelatedTools("catalog_list"),
		), nil
	}

	kindStr, err := request.RequireString("kind")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("kind"),
			WithSuggestion("Provide an artifact kind: 'provider' or 'auth-handler'"),
		), nil
	}

	artifactKind, ok := catalog.ParseArtifactKind(kindStr)
	if !ok {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid kind %q", kindStr),
			WithField("kind"),
			WithSuggestion("Valid kinds for plugins: provider, auth-handler"),
			WithRelatedTools("catalog_list"),
		), nil
	}

	ref, err := catalog.ParseReference(artifactKind, reference)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid reference %q: %v", reference, err),
			WithField("reference"),
			WithSuggestion("Use format 'name@version' or just 'name' for latest"),
			WithRelatedTools("catalog_list"),
		), nil
	}

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return newStructuredError(ErrCodeConfigError, fmt.Sprintf("failed to initialize local catalog: %v", err),
			WithSuggestion("Ensure the catalog directory exists and is accessible"),
		), nil
	}

	platforms, err := localCatalog.ListPlatforms(s.ctx, ref)
	if err != nil {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("artifact not found: %v", err),
			WithField("reference"),
			WithSuggestion("Use catalog_list to see available artifacts"),
			WithRelatedTools("catalog_list"),
		), nil
	}

	result := map[string]any{
		"reference":       reference,
		"kind":            kindStr,
		"isMultiPlatform": len(platforms) > 0,
	}
	if len(platforms) > 0 {
		result["platforms"] = platforms
		result["platformCount"] = len(platforms)
	}

	return mcp.NewToolResultJSON(result)
}

// handleBuildPlugin builds a multi-platform plugin into the local catalog.
func (s *Server) handleBuildPlugin(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("name"),
			WithSuggestion("Provide a plugin name"),
		), nil
	}

	kindStr, err := request.RequireString("kind")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("kind"),
			WithSuggestion("Provide a plugin kind: 'provider' or 'auth-handler'"),
		), nil
	}

	artifactKind, err := catalog.ValidatePluginKind(kindStr)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("kind"),
			WithSuggestion("Use 'provider' or 'auth-handler'"),
		), nil
	}

	versionStr, err := request.RequireString("version")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("version"),
			WithSuggestion("Provide a semantic version (e.g., '1.0.0')"),
		), nil
	}

	ver, err := semver.NewVersion(versionStr)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid semantic version %q: %v", versionStr, err),
			WithField("version"),
			WithSuggestion("Use semantic versioning (e.g., '1.0.0', '2.1.0-beta.1')"),
		), nil
	}

	// Extract platforms map
	args := request.GetArguments()
	platformsRaw, ok := args["platforms"]
	if !ok {
		return newStructuredError(ErrCodeInvalidInput, "missing required parameter 'platforms'",
			WithField("platforms"),
			WithSuggestion("Provide a map of platform to binary path, e.g., {\"linux/amd64\": \"/path/to/binary\"}"),
		), nil
	}

	platformsMap, ok := platformsRaw.(map[string]any)
	if !ok || len(platformsMap) == 0 {
		return newStructuredError(ErrCodeInvalidInput, "platforms must be a non-empty object mapping platform strings to file paths",
			WithField("platforms"),
			WithSuggestion("Example: {\"linux/amd64\": \"/path/to/binary\", \"darwin/arm64\": \"/path/to/binary\"}"),
		), nil
	}

	force := request.GetBool("force", false)

	// Convert platforms map to string paths and validate/read binaries
	platformPaths := make(map[string]string, len(platformsMap))
	for platform, pathRaw := range platformsMap {
		binPath, ok := pathRaw.(string)
		if !ok || binPath == "" {
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("platform %q: binary path must be a non-empty string", platform),
				WithField("platforms"),
			), nil
		}
		platformPaths[platform] = binPath
	}

	platformBinaries, err := catalog.ReadPlatformBinaries(platformPaths)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("platforms"),
			WithSuggestion("Ensure binary paths exist and are valid files"),
		), nil
	}

	platformList := make([]string, 0, len(platformBinaries))
	for _, pb := range platformBinaries {
		platformList = append(platformList, pb.Platform)
	}

	ref := catalog.Reference{
		Kind:    artifactKind,
		Name:    name,
		Version: ver,
	}

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return newStructuredError(ErrCodeConfigError, fmt.Sprintf("failed to initialize local catalog: %v", err),
			WithSuggestion("Ensure the catalog directory exists and is accessible"),
		), nil
	}

	info, err := localCatalog.StoreMultiPlatform(s.ctx, ref, platformBinaries, nil, force)
	if err != nil {
		if catalog.IsExists(err) {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("artifact %s@%s already exists", name, versionStr),
				WithSuggestion("Use force: true to overwrite, or choose a different version"),
				WithRelatedTools("catalog_list_platforms"),
			), nil
		}
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to build plugin: %v", err),
			WithSuggestion("Check file paths and catalog permissions"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"name":          name,
		"kind":          kindStr,
		"version":       versionStr,
		"platforms":     platformList,
		"platformCount": len(platformList),
		"digest":        info.Digest,
		"catalog":       info.Catalog,
	})
}
