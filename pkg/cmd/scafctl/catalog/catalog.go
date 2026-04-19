// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package catalog provides commands for inspecting and managing the local catalog.
package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	catversion "github.com/oakwood-commons/scafctl/pkg/catalog/version"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandCatalog creates the catalog command group.
func CommandCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "catalog",
		Aliases:      []string{"cat"},
		Short:        "Manage the local artifact catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Manage the local artifact catalog.

			The catalog stores solutions, providers, and auth handlers as versioned
			OCI artifacts for later execution with 'scafctl run'.

			The local catalog is stored at:
			  - Linux: ~/.local/share/scafctl/catalog/
			  - macOS: ~/.local/share/scafctl/catalog/
			  - Windows: %LOCALAPPDATA%\scafctl\catalog\
		`),
	}

	cmd.AddCommand(CommandList(cliParams, ioStreams, path))
	cmd.AddCommand(CommandInspect(cliParams, ioStreams, path))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPrune(cliParams, ioStreams, path))
	cmd.AddCommand(CommandSave(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLoad(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPush(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPull(cliParams, ioStreams, path))
	cmd.AddCommand(CommandTag(cliParams, ioStreams, path))
	cmd.AddCommand(CommandTags(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLogin(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLogout(cliParams, ioStreams, path))
	cmd.AddCommand(CommandRemote(cliParams, ioStreams, path))
	cmd.AddCommand(CommandAttach(cliParams, ioStreams, path))

	return cmd
}

// hintOnAuthError checks if the error looks like a 401/403 and prints a
// helpful hint suggesting 'catalog login'. This keeps the hint logic in one
// place for push, pull, and delete.
func hintOnAuthError(ctx context.Context, w *writer.Writer, registry string, err error) {
	errStr := err.Error()
	if !strings.Contains(errStr, "401") && !strings.Contains(errStr, "403") &&
		!strings.Contains(errStr, "Unauthorized") && !strings.Contains(errStr, "Forbidden") {
		return
	}

	bin := settings.BinaryNameFromContext(ctx)

	var customHandlers []config.CustomOAuth2Config
	if cfg := config.FromContext(ctx); cfg != nil {
		customHandlers = cfg.Auth.CustomOAuth2
	}

	if handler := catalog.InferAuthHandler(registry, customHandlers); handler != "" {
		w.Infof("Hint: run '%s auth login %s' to authenticate (credentials are bridged to %s automatically)",
			bin, handler, registry)
	} else {
		w.Infof("Hint: run '%s catalog login %s' to authenticate", bin, registry)
	}
}

// resolveAuthHandler attempts to find and return the auth handler for a registry.
// It checks:
//  1. Catalog config authProvider field (when catalogFlag resolves to a named catalog)
//  2. InferAuthHandler (built-in + custom OAuth2 mappings)
//
// Returns nil if no handler can be resolved or the handler cannot be loaded.
// This is best-effort — push/pull/delete still work via credential store alone.
func resolveAuthHandler(ctx context.Context, registry, catalogFlag string) auth.Handler {
	var handlerName string
	cfg := config.FromContext(ctx)

	// Try catalog config first (has explicit authProvider field)
	if catalogFlag != "" && !catalog.LooksLikeCatalogURL(catalogFlag) && cfg != nil {
		if cat, ok := cfg.GetCatalog(catalogFlag); ok && cat.AuthProvider != "" {
			handlerName = cat.AuthProvider
		}
	}

	// Try inference from registry host (e.g. *.pkg.dev → gcp, ghcr.io → github).
	// This must run before the default-catalog fallback so that a full OCI ref
	// like "us-central1-docker.pkg.dev/..." picks up the correct handler even
	// when the default catalog points to a different provider.
	if handlerName == "" {
		var customHandlers []config.CustomOAuth2Config
		if cfg != nil {
			customHandlers = cfg.Auth.CustomOAuth2
		}
		handlerName = catalog.InferAuthHandler(registry, customHandlers)
	}

	// Fall back to default catalog's authProvider (last resort).
	if handlerName == "" && cfg != nil {
		if cat, ok := cfg.GetDefaultCatalog(); ok && cat.AuthProvider != "" {
			handlerName = cat.AuthProvider
		}
	}

	if handlerName == "" {
		return nil
	}

	handler, err := auth.GetHandler(ctx, handlerName)
	if err != nil {
		return nil
	}

	return handler
}

// resolveAuthScope returns the authScope configured for the named catalog, or
// by matching the registry host against configured catalogs.
func resolveAuthScope(ctx context.Context, catalogFlag string) string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ""
	}

	// Try named catalog first.
	if catalogFlag != "" {
		if cat, ok := cfg.GetCatalog(catalogFlag); ok && cat.AuthScope != "" {
			return cat.AuthScope
		}
	}

	// Fall back: match by registry host from catalogFlag (when it's a URL).
	if catalogFlag != "" && catalog.LooksLikeCatalogURL(catalogFlag) {
		host, _ := catalog.ParseCatalogURL(catalogFlag)
		for _, cat := range cfg.Catalogs {
			if cat.URL == "" || cat.AuthScope == "" {
				continue
			}
			catHost, _ := catalog.ParseCatalogURL(cat.URL)
			if catHost == host {
				return cat.AuthScope
			}
		}
	}

	return ""
}

// resolveAuthScopeForRegistry returns the authScope by matching a registry host
// against configured catalogs. Used when a full OCI reference is provided and
// there is no --catalog flag to look up. Falls back to InferDefaultScope for
// well-known registries (e.g. GCP Artifact Registry).
func resolveAuthScopeForRegistry(ctx context.Context, registry string) string {
	cfg := config.FromContext(ctx)
	if cfg != nil {
		for _, cat := range cfg.Catalogs {
			if cat.URL == "" || cat.AuthScope == "" {
				continue
			}
			catHost, _ := catalog.ParseCatalogURL(cat.URL)
			if catHost == registry {
				return cat.AuthScope
			}
		}
	}

	return catalog.InferDefaultScope(registry)
}

// verboseRemoteInfo logs user-facing verbose diagnostics about how a remote
// catalog operation was resolved: registry, repository, auth handler, scope,
// credential source, and handler auth status.
// Safe to call even when --verbose is off (Writer.Verbosef short-circuits).
func verboseRemoteInfo(ctx context.Context, w *writer.Writer, registry, repository string, handler auth.Handler, scope string) {
	if !w.VerboseEnabled() {
		return
	}

	w.Verbosef("Registry: %s", registry)
	w.Verbosef("Repository: %s", repository)

	if handler != nil {
		w.Verbosef("Auth handler: %s", handler.Name())
		if scope != "" {
			w.Verbosef("Auth scope: %s", scope)
		}

		// Check handler auth status for troubleshooting.
		status, err := handler.Status(ctx)
		switch {
		case err != nil:
			w.Verbosef("Auth status: unknown (check failed: %v)", err)
		case !status.Authenticated:
			reason := status.Reason
			if reason == "" {
				reason = "not logged in"
			}
			w.Verbosef("Auth status: NOT AUTHENTICATED (%s)", reason)
			w.Verbosef("Credential bridging will fail -- run 'auth login %s' first", handler.Name())
		default:
			identity := ""
			if status.Claims != nil {
				if status.Claims.Email != "" {
					identity = status.Claims.Email
				} else if status.Claims.Username != "" {
					identity = status.Claims.Username
				}
			}
			if identity != "" {
				w.Verbosef("Auth status: authenticated as %s", identity)
			} else {
				w.Verbose("Auth status: authenticated")
			}
			if !status.ExpiresAt.IsZero() {
				remaining := time.Until(status.ExpiresAt)
				if remaining <= 0 {
					w.Verbose("Auth expiry: EXPIRED -- credentials may fail, re-authenticate if needed")
				} else {
					w.Verbosef("Auth expiry: %s remaining", remaining.Truncate(time.Second))
				}
			}
		}
		w.Verbose("Credentials: bridged from auth handler on-the-fly")
	} else {
		w.Verbose("Auth handler: none (using stored registry credentials)")
		w.Verbosef("Credential source: container auth config or native credential store for %s", registry)
	}
}

// verboseRefInfo logs user-facing verbose diagnostics about how a reference
// was parsed.
func verboseRefInfo(w *writer.Writer, name, kind, version string) {
	msg := "Reference: name=" + name
	if kind != "" {
		msg += " kind=" + kind
	}
	if version != "" {
		msg += " version=" + version
	}
	w.Verbose(msg)
}

// validateVersionConstraint checks for conflicts between an explicit @version
// in the reference name and a --version constraint flag. Returns an error if
// both are set, which would be ambiguous.
func validateVersionConstraint(nameOrRef, versionConstraint string) error {
	if versionConstraint == "" {
		return nil
	}

	// Check for @version in the reference
	if idx := strings.LastIndex(nameOrRef, "@"); idx > 0 {
		return fmt.Errorf("cannot use --version with an explicit version in reference %q; use one or the other", nameOrRef)
	}

	return catversion.ValidateConstraint(versionConstraint)
}

// resolveVersionConstraint lists all versions of the artifact in the
// catalog, applies the constraint, and returns the best (highest) match.
// The returned Reference has Kind, Name, and Version populated.
func resolveVersionConstraint(ctx context.Context, cat catalog.Catalog, ref catalog.Reference, constraint string) (catalog.Reference, error) {
	artifacts, err := cat.List(ctx, ref.Kind, ref.Name)
	if err != nil {
		return ref, fmt.Errorf("failed to list versions for constraint resolution: %w", err)
	}

	filtered, err := filterArtifactsByConstraint(artifacts, constraint)
	if err != nil {
		return ref, err
	}

	if len(filtered) == 0 {
		return ref, fmt.Errorf("no versions of %q match constraint %q", ref.Name, constraint)
	}

	// filtered is sorted descending (newest first) by filterArtifactsByConstraint
	best := filtered[0].Reference
	return best, nil
}

// filterArtifactsByConstraint filters a list of catalog artifacts by a semver
// version constraint string. When a constraint is provided, artifacts with nil
// versions are excluded from the result. When constraint is empty, the input
// is returned unchanged. Results are sorted descending (newest first).
func filterArtifactsByConstraint(artifacts []catalog.ArtifactInfo, constraint string) ([]catalog.ArtifactInfo, error) {
	if constraint == "" {
		return artifacts, nil
	}

	var versions []*semver.Version
	for _, a := range artifacts {
		versions = append(versions, a.Reference.Version)
	}

	matched, err := catversion.FilterSemver(versions, constraint)
	if err != nil {
		return nil, err
	}

	// Build a set of matching version strings for O(1) lookup.
	matchedVersions := make(map[string]struct{}, len(matched))
	for _, v := range matched {
		matchedVersions[v.Original()] = struct{}{}
	}

	// Collect all artifacts whose version matches, preserving all kinds.
	// Then sort by version descending (matching FilterSemver order).
	result := make([]catalog.ArtifactInfo, 0, len(matched))
	for _, a := range artifacts {
		if a.Reference.Version != nil {
			if _, ok := matchedVersions[a.Reference.Version.Original()]; ok {
				result = append(result, a)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		vi := result[i].Reference.Version
		vj := result[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vj.LessThan(vi) // descending
	})
	return result, nil
}

// filterPreReleaseArtifacts removes artifacts with pre-release versions
// (e.g. 1.0.0-beta.1). If all artifacts are pre-release, returns the
// original slice unchanged to avoid returning empty results.
func filterPreReleaseArtifacts(artifacts []catalog.ArtifactInfo) []catalog.ArtifactInfo {
	stable := make([]catalog.ArtifactInfo, 0, len(artifacts))
	for _, a := range artifacts {
		if !catalog.IsPreRelease(a.Reference.Version) {
			stable = append(stable, a)
		}
	}
	if len(stable) == 0 {
		return artifacts
	}
	return stable
}
