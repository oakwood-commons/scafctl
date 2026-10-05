// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package builder provides the build pipeline for composing, discovering,
// vendoring, and bundling solution artifacts. This is the shared domain layer
// used by CLI, MCP, and future API consumers.
package builder

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// BuildResult holds the bundle produced for a bundled solution.
type BuildResult struct {
	// TarData is the traditional single tar archive (v1).
	TarData []byte

	// Dedup is the content-addressable dedup result (v2).
	Dedup *bundler.DedupeResult

	// CacheHit indicates the build was served from the build cache: the
	// artifact already exists in the catalog and nothing was stored.
	CacheHit bool

	// CacheEntry contains the cache metadata when CacheHit is true.
	CacheEntry *bundler.BuildCacheEntry

	// BuildFingerprint is the build-cache key of this build; empty when the
	// cache is skipped or the fingerprint could not be computed.
	BuildFingerprint string

	// InputFileCount is the number of input files that contributed to the fingerprint.
	InputFileCount int

	// LockData is the JSON-encoded lock file stored as a dedicated OCI layer
	// (MediaTypeSolutionLock). Empty when nothing was pinned or vendored.
	LockData []byte

	// Discovery holds the file discovery results.
	Discovery *bundler.DiscoveryResult
}

// collectPluginEntries converts solution bundle plugins to bundler entries.
func collectPluginEntries(sol *solution.Solution) []bundler.BundlePluginEntry {
	plugins := make([]bundler.BundlePluginEntry, 0, len(sol.Bundle.Plugins))
	for _, p := range sol.Bundle.Plugins {
		plugins = append(plugins, bundler.BundlePluginEntry{
			Name:     p.ArtifactName(),
			Kind:     string(p.Kind),
			Version:  p.Version,
			Registry: p.Registry(),
		})
	}
	return plugins
}

// ParseByteSize parses a human-readable byte size string (e.g., "50MB", "100KB").
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	// Check longer suffixes first to avoid "KB" matching "B"
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			numStr := strings.TrimSuffix(s, entry.suffix)
			numStr = strings.TrimSpace(numStr)
			if numStr == "" {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			var n int64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * entry.mult, nil
		}
	}

	// Plain number — treat as bytes
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}

// RegistryFetcherAdapter adapts a catalog.Registry to the bundler.CatalogFetcher interface.
// It supports both exact version fetches and listing all versions for constraint resolution.
type RegistryFetcherAdapter struct {
	Registry *catalog.Registry
}

// FetchSolution retrieves a solution by name[@version] from the registry.
func (a *RegistryFetcherAdapter) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, nameWithVersion)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("invalid reference %q: %w", nameWithVersion, err)
	}

	content, info, err := a.Registry.Fetch(ctx, ref)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}

	return content, info, nil
}

// ListSolutions returns all available versions of a named solution artifact.
func (a *RegistryFetcherAdapter) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	return a.Registry.List(ctx, catalog.ArtifactKindSolution, name)
}

// validateProviderCoverage checks that every provider referenced in the
// solution's resolvers, actions, and calls is either a builtin (compiled into
// the binary) or explicitly declared in bundle.plugins. Returns an actionable
// error listing missing providers and suggesting the YAML to add.
func validateProviderCoverage(sol *solution.Solution, builtins []string, officialReg *official.Registry) error {
	referenced := sol.ReferencedProviderNames()
	if len(referenced) == 0 {
		return nil
	}

	builtinSet := make(map[string]struct{}, len(builtins))
	for _, name := range builtins {
		builtinSet[name] = struct{}{}
	}

	declared := make(map[string]struct{}, len(sol.Bundle.Plugins))
	for _, p := range sol.Bundle.Plugins {
		declared[p.LocalName()] = struct{}{}
	}

	var missing []string
	for _, name := range referenced {
		if _, isBuiltin := builtinSet[name]; isBuiltin {
			continue
		}
		if _, isDeclared := declared[name]; isDeclared {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return nil
	}

	// Build actionable suggestion.
	var suggestions []string
	for _, name := range missing {
		version := "<version constraint>"
		if officialReg != nil {
			if p, ok := officialReg.Get(name); ok {
				version = p.DefaultVersion
			}
		}
		suggestions = append(suggestions, fmt.Sprintf("    - name: %s\n      kind: provider\n      version: %s", name, version))
	}

	return fmt.Errorf(
		"providers %v are referenced in the solution but not declared in bundle.plugins;\n"+
			"add to your solution:\n\n"+
			"bundle:\n  plugins:\n%s",
		missing, strings.Join(suggestions, "\n"),
	)
}

// buildLockSignatureVerifier returns a VerifySignature callback for the bundler
// when a signature policy is active. It checks the context for an explicit
// policy override first, then falls back to the app configuration
// (config.Plugins.Signatures). Returns nil when signatures are disabled,
// which causes VendorAllPlugins to skip verification.
func buildLockSignatureVerifier(ctx context.Context, lgr logr.Logger) func(context.Context, string) (*bundler.LockPluginSignature, error) {
	policy := plugin.SignaturePolicyFromContext(ctx)
	if policy == nil {
		policy = signaturePolicyFromAppConfig(config.FromContext(ctx), lgr)
	}
	if !policy.IsEnabled() {
		return nil
	}
	verifier := plugin.NewSignatureVerifier()
	return func(ctx context.Context, imageRef string) (*bundler.LockPluginSignature, error) {
		result, err := verifier.VerifySignature(ctx, imageRef, policy)
		if err != nil {
			if handleErr := plugin.HandleVerificationError(policy, err, lgr,
				"imageRef", imageRef); handleErr != nil {
				return nil, handleErr
			}
			return nil, nil
		}
		if result == nil || !result.Verified {
			return nil, nil
		}
		return &bundler.LockPluginSignature{
			Issuer:   result.Issuer,
			Identity: result.Identity,
			SignedAt: result.SignedAt,
		}, nil
	}
}

// signaturePolicyFromAppConfig derives a SignaturePolicy from the app
// configuration using the shared plugin.SignaturePolicyFromRaw helper.
// Returns nil when the config is nil, mode is "off", empty, or invalid.
func signaturePolicyFromAppConfig(appCfg *config.Config, lgr logr.Logger) *plugin.SignaturePolicy {
	if appCfg == nil {
		return nil
	}
	sigCfg := appCfg.Plugins.Signatures
	policy, err := plugin.SignaturePolicyFromRaw(sigCfg.Mode, sigCfg.TrustedIssuers, sigCfg.TrustedIdentities)
	if err != nil {
		lgr.Info("invalid plugin signature mode in config, defaulting to off",
			"mode", sigCfg.Mode, "error", err)
		return nil
	}
	return policy
}

// buildSourcedCatalogs constructs one remote catalog per URL-bearing configured
// catalog, keyed by the lowercased catalog alias. It mirrors
// catalogindex.FromConfig's remote enumeration exactly -- only URL-bearing
// (remote/OCI) catalogs, keyed by strings.ToLower(catCfg.Name) -- so the
// alias->origin map (used to bind a sourced plugin's origin to an alias)
// and this alias->catalog map (used to resolve that alias to a live catalog)
// stay in lockstep. Filesystem catalogs are skipped: they have no registry
// origin and cannot serve a fully-qualified source.
//
// Each catalog is built via catalog.BuildRemoteCatalogFromConfig, which wires
// config-matched auth (the catalog's authProvider handler + authScope) when
// available and otherwise falls back to anonymous/Docker credentials. A single
// shared CredentialStore is threaded through all catalogs. A catalog that fails
// to build is skipped with a debug log rather than failing the whole build --
// an unrelated misconfigured catalog must not block vendoring a plugin sourced
// from a different, healthy catalog.
//
// Returns nil when the config is absent or no remote catalogs are configured,
// so a sourced plugin fails with "no catalog available for alias".
func buildSourcedCatalogs(ctx context.Context, lgr logr.Logger) map[string]bundler.ArtifactResolver {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return nil
	}

	authReg := auth.RegistryFromContext(ctx)
	credStore, credErr := catalog.NewCredentialStore(lgr)
	if credErr != nil {
		// Non-fatal: BuildRemoteCatalogFromConfig tolerates a nil store and
		// falls back to anonymous access.
		lgr.V(1).Info("credential store unavailable for sourced plugin catalogs", "error", credErr)
	}

	out := make(map[string]bundler.ArtifactResolver)
	for _, catCfg := range cfg.Catalogs {
		if catCfg.URL == "" {
			continue // filesystem catalog: no registry origin
		}
		alias := strings.ToLower(catCfg.Name)
		if _, exists := out[alias]; exists {
			// Mirror catalogindex.FromConfig's "first alias wins" behavior so
			// the two maps agree on which catalog an alias binds to.
			continue
		}
		rc, buildErr := catalog.BuildRemoteCatalogFromConfig(catCfg, credStore, authReg, lgr)
		if buildErr != nil {
			lgr.V(1).Info("skipping catalog for sourced plugin vendoring",
				"catalog", catCfg.Name, "error", buildErr)
			continue
		}
		out[alias] = rc
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
