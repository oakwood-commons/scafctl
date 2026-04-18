// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	// VendorDirName is the directory name within the bundle for vendored artifacts.
	VendorDirName = ".scafctl/vendor"
)

// VendorOptions configures the vendoring process.
type VendorOptions struct {
	// BundleRoot is the root directory of the solution bundle.
	BundleRoot string
	// VendorDir is the directory to store vendored artifacts.
	VendorDir string
	// LockPath is the path to the lock file.
	LockPath string
	// CatalogFetcher fetches artifacts from catalogs by name[@version].
	// If nil, vendoring will fail when catalog refs are discovered.
	CatalogFetcher CatalogFetcher
}

// CatalogFetcher fetches solution content from a catalog by reference.
type CatalogFetcher interface {
	// FetchSolution retrieves a solution by name[@version] and returns
	// the content bytes, the resolved reference info, and any error.
	FetchSolution(ctx context.Context, nameWithVersion string) (content []byte, info catalog.ArtifactInfo, err error)

	// ListSolutions returns all available versions for a named solution artifact.
	// Used for resolving semver constraints to the best matching version.
	ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error)
}

// VendorResult describes the outcome of a vendoring operation.
type VendorResult struct {
	// VendoredFiles contains relative paths (from bundle root) to vendored files.
	VendoredFiles []string
	// Lock is the updated lock file content.
	Lock *LockFile
}

// VendorDependencies fetches catalog dependencies, stores them locally,
// rewrites source references in the solution, and writes a lock file.
func VendorDependencies(ctx context.Context, sol *solution.Solution, refs []CatalogRefEntry, opts VendorOptions) (*VendorResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}

	lgr := logger.FromContext(ctx)

	// Load existing lock file for replay
	var existingLock *LockFile
	if opts.LockPath != "" {
		lf, err := LoadLockFile(opts.LockPath)
		if err == nil && lf != nil {
			existingLock = lf
			lgr.V(1).Info("loaded existing lock file", "path", opts.LockPath, "deps", len(lf.Dependencies))
		}
	}

	// Ensure vendor directory exists
	if err := os.MkdirAll(opts.VendorDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create vendor directory: %w", err)
	}

	result := &VendorResult{
		Lock: &LockFile{
			Version:      1,
			Dependencies: make([]LockDependency, 0, len(refs)),
		},
	}

	// Track visited refs by resolved name@version for proper dedup and conflict detection.
	// Maps resolved "name@version" → original ref string that resolved to it.
	visited := make(map[string]string)

	for _, ref := range refs {
		if err := vendorRef(ctx, sol, ref, opts, result, visited, existingLock); err != nil {
			return nil, fmt.Errorf("failed to vendor %s: %w", ref.Ref, err)
		}
	}

	// Write the lock file
	if opts.LockPath != "" && len(result.Lock.Dependencies) > 0 {
		if err := WriteLockFile(opts.LockPath, result.Lock); err != nil {
			return nil, fmt.Errorf("failed to write lock file: %w", err)
		}
		lgr.V(1).Info("wrote lock file", "path", opts.LockPath, "deps", len(result.Lock.Dependencies))
	}

	return result, nil
}

// resolvedRef holds the result of parsing and resolving a catalog reference.
type resolvedRef struct {
	// name is the artifact name (e.g., "deploy-to-k8s").
	name string
	// version is the resolved exact version (may be nil for bare names).
	version *semver.Version
	// constraint is the original constraint string if one was used (e.g., "^1.5.0").
	// Empty for exact version refs.
	constraint string
	// resolvedKey is the canonical key "name@version" for dedup/conflict detection.
	resolvedKey string
}

// parseAndResolveRef parses a catalog ref string and resolves any version constraint
// to an exact version by querying the catalog.
func parseAndResolveRef(ctx context.Context, ref string, fetcher CatalogFetcher) (*resolvedRef, error) {
	lgr := logger.FromContext(ctx)

	name, versionPart := splitRef(ref)

	// No version specified — resolve to latest
	if versionPart == "" {
		return &resolvedRef{
			name:        name,
			resolvedKey: name,
		}, nil
	}

	// Try as exact semver version first
	if v, err := semver.NewVersion(versionPart); err == nil {
		return &resolvedRef{
			name:        name,
			version:     v,
			resolvedKey: name + "@" + v.String(),
		}, nil
	}

	// Try as semver constraint (e.g., ^1.5.0, >=2.0.0, ~1.0)
	constraint, err := semver.NewConstraint(versionPart)
	if err != nil {
		return nil, fmt.Errorf("invalid version or constraint %q in ref %q: %w", versionPart, ref, err)
	}

	if fetcher == nil {
		return nil, fmt.Errorf("cannot resolve constraint %q for %q: no catalog fetcher configured", versionPart, name)
	}

	// List all versions and find the highest match
	artifacts, err := fetcher.ListSolutions(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions for %q: %w", name, err)
	}

	var bestVersion *semver.Version
	for _, a := range artifacts {
		if a.Reference.Version != nil && constraint.Check(a.Reference.Version) {
			// Skip pre-release versions unless opted in via context
			if catalog.IsPreRelease(a.Reference.Version) && !catalog.IncludePreReleaseFromContext(ctx) {
				lgr.V(1).Info("skipping pre-release version for constraint",
					"name", name,
					"version", a.Reference.Version.String(),
					"constraint", versionPart)
				continue
			}
			if bestVersion == nil || a.Reference.Version.GreaterThan(bestVersion) {
				bestVersion = a.Reference.Version
			}
		}
	}

	if bestVersion == nil {
		return nil, fmt.Errorf("no version of %q satisfies constraint %q (available: %s)",
			name, versionPart, formatAvailableVersions(artifacts))
	}

	lgr.V(1).Info("resolved version constraint",
		"name", name,
		"constraint", versionPart,
		"resolved", bestVersion.String(),
		"candidates", len(artifacts))

	return &resolvedRef{
		name:        name,
		version:     bestVersion,
		constraint:  versionPart,
		resolvedKey: name + "@" + bestVersion.String(),
	}, nil
}

// splitRef splits a catalog reference into name and version/constraint parts.
func splitRef(ref string) (name, versionPart string) {
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

// formatAvailableVersions returns a comma-separated list of available versions.
func formatAvailableVersions(artifacts []catalog.ArtifactInfo) string {
	if len(artifacts) == 0 {
		return "none"
	}
	versions := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Reference.Version != nil {
			versions = append(versions, a.Reference.Version.String())
		}
	}
	if len(versions) == 0 {
		return "none"
	}
	return strings.Join(versions, ", ")
}

// vendorRef fetches a single catalog reference, stores it, and rewrites the solution.
func vendorRef(ctx context.Context, sol *solution.Solution, ref CatalogRefEntry, opts VendorOptions, result *VendorResult, visited map[string]string, existingLock *LockFile) error {
	lgr := logger.FromContext(ctx)

	// Pre-resolution lock check: if the ref looks like a constraint and we have a lock
	// file, try to replay from lock before requiring a catalog fetcher for resolution.
	name, versionPart := splitRef(ref.Ref)
	if existingLock != nil && versionPart != "" {
		// If it's not a valid exact semver, it might be a constraint
		if _, exactErr := semver.NewVersion(versionPart); exactErr != nil {
			if dep := existingLock.FindDependencyByName(name); dep != nil && dep.ResolvedVersion != "" {
				satisfies, checkErr := CheckVersionConstraint(versionPart, dep.ResolvedVersion)
				if checkErr == nil && satisfies {
					// Build a resolvedRef from the lock data
					lockedVer, _ := semver.NewVersion(dep.ResolvedVersion)
					resolved := &resolvedRef{
						name:        name,
						version:     lockedVer,
						constraint:  versionPart,
						resolvedKey: name + "@" + dep.ResolvedVersion,
					}
					if _, seen := visited[resolved.resolvedKey]; !seen {
						visited[resolved.resolvedKey] = ref.Ref
						if replayed := tryReplayLock(ctx, sol, ref.Ref, dep, opts, result); replayed {
							return nil
						}
					}
				}
			}
		}
	}

	// Parse and resolve the reference (handles constraints, exact versions, bare names)
	resolved, err := parseAndResolveRef(ctx, ref.Ref, opts.CatalogFetcher)
	if err != nil {
		return err
	}

	// Version conflict detection and dedup
	if previousRef, seen := visited[resolved.resolvedKey]; seen {
		lgr.V(1).Info("skipping already-vendored reference", "ref", ref.Ref, "resolvedKey", resolved.resolvedKey)
		// Still need to rewrite sources if the original ref string differs
		if ref.Ref != previousRef {
			// Find the vendored path from existing result
			for _, dep := range result.Lock.Dependencies {
				if refName(dep.Ref) == resolved.name {
					rewriteSolutionSources(sol, ref.Ref, dep.VendoredAt)
					break
				}
			}
		}
		return nil
	}

	// Check for version conflict: same name but different resolved version
	for resolvedKey, prevRef := range visited {
		prevName, _ := splitRef(resolvedKey)
		if prevName == resolved.name && resolvedKey != resolved.resolvedKey {
			return fmt.Errorf("version conflict for %q: %q resolves to %s, but %q already resolved to %s",
				resolved.name, ref.Ref, resolved.resolvedKey, prevRef, resolvedKey)
		}
	}

	visited[resolved.resolvedKey] = ref.Ref

	// Build the exact ref string for fetching (name@resolvedVersion)
	fetchRef := resolved.name
	if resolved.version != nil {
		fetchRef = resolved.name + "@" + resolved.version.String()
	}

	// Check if we can replay from lock file
	if existingLock != nil {
		if dep := existingLock.FindDependencyByName(resolved.name); dep != nil {
			// If there's a constraint, verify the locked version still satisfies it
			if resolved.constraint != "" && dep.ResolvedVersion != "" {
				satisfies, checkErr := CheckVersionConstraint(resolved.constraint, dep.ResolvedVersion)
				switch {
				case checkErr != nil:
					lgr.V(1).Info("lock file constraint check failed, re-fetching",
						"ref", ref.Ref, "constraint", resolved.constraint, "error", checkErr)
				case !satisfies:
					lgr.V(1).Info("lock file entry no longer satisfies constraint, re-fetching",
						"ref", ref.Ref, "constraint", resolved.constraint, "lockedVersion", dep.ResolvedVersion)
				default:
					// Constraint still satisfied — try replay
					if replayed := tryReplayLock(ctx, sol, ref.Ref, dep, opts, result); replayed {
						return nil
					}
				}
			} else {
				// Exact version or no constraint — try replay directly
				if replayed := tryReplayLock(ctx, sol, ref.Ref, dep, opts, result); replayed {
					return nil
				}
			}
		}
	}

	// Fetch from catalog
	if opts.CatalogFetcher == nil {
		return fmt.Errorf("no catalog fetcher configured; cannot vendor %s", ref.Ref)
	}

	content, info, err := opts.CatalogFetcher.FetchSolution(ctx, fetchRef)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", fetchRef, err)
	}

	// Determine vendor path using the resolved exact version
	vendoredName := vendorFileName(fetchRef, info)
	vendorRelPath := filepath.ToSlash(filepath.Join(VendorDirName, vendoredName))
	absVendorPath := filepath.Join(opts.BundleRoot, vendorRelPath)

	// Write vendored content
	if err := os.MkdirAll(filepath.Dir(absVendorPath), 0o755); err != nil {
		return fmt.Errorf("failed to create vendor directory for %s: %w", ref.Ref, err)
	}
	if err := os.WriteFile(absVendorPath, content, 0o600); err != nil {
		return fmt.Errorf("failed to write vendored file for %s: %w", ref.Ref, err)
	}

	// Compute digest
	contentDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))

	// Determine resolved version string
	resolvedVersion := ""
	if resolved.version != nil {
		resolvedVersion = resolved.version.String()
	} else if info.Reference.Version != nil {
		resolvedVersion = info.Reference.Version.String()
	}

	lgr.V(1).Info("vendored dependency",
		"ref", ref.Ref,
		"resolvedVersion", resolvedVersion,
		"vendoredAt", vendorRelPath,
		"digest", contentDigest,
		"catalog", info.Catalog)

	// Rewrite solution sources for both the original ref and the resolved ref
	rewriteSolutionSources(sol, ref.Ref, vendorRelPath)
	if fetchRef != ref.Ref {
		rewriteSolutionSources(sol, fetchRef, vendorRelPath)
	}

	// Record in result
	result.VendoredFiles = append(result.VendoredFiles, vendorRelPath)
	result.Lock.Dependencies = append(result.Lock.Dependencies, LockDependency{
		Ref:             ref.Ref,
		ResolvedVersion: resolvedVersion,
		Constraint:      resolved.constraint,
		Digest:          contentDigest,
		ResolvedFrom:    info.Catalog,
		VendoredAt:      vendorRelPath,
	})

	// Recursive vendoring: parse the vendored solution and check for its own catalog refs
	var vendoredSol solution.Solution
	if unmarshalErr := vendoredSol.UnmarshalFromBytes(content); unmarshalErr != nil {
		// Not a valid solution — skip recursive vendoring
		lgr.V(1).Info("vendored content is not a valid solution, skipping recursive vendoring", "ref", ref.Ref, "error", unmarshalErr)
		return nil
	}

	subDiscovery, err := DiscoverFiles(&vendoredSol, filepath.Dir(absVendorPath))
	if err != nil {
		lgr.V(1).Info("failed to discover sub-dependencies, skipping", "ref", ref.Ref, "error", err)
		return nil
	}

	for _, subRef := range subDiscovery.CatalogRefs {
		if err := vendorRef(ctx, sol, subRef, opts, result, visited, existingLock); err != nil {
			return fmt.Errorf("recursive vendoring of %s (from %s): %w", subRef.Ref, ref.Ref, err)
		}
	}

	return nil
}

// tryReplayLock attempts to replay a vendored dependency from the lock file.
// Returns true if replay succeeded, false if the caller should re-fetch.
func tryReplayLock(ctx context.Context, sol *solution.Solution, originalRef string, dep *LockDependency, opts VendorOptions, result *VendorResult) bool {
	lgr := logger.FromContext(ctx)

	lgr.V(1).Info("replaying from lock file", "ref", originalRef, "digest", dep.Digest)

	vendorPath := dep.VendoredAt
	absVendorPath := filepath.Join(opts.BundleRoot, vendorPath)
	content, err := os.ReadFile(absVendorPath)
	if err != nil {
		lgr.V(1).Info("lock file entry stale (file missing), re-fetching", "ref", originalRef)
		return false
	}

	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	if actualDigest != dep.Digest {
		lgr.V(1).Info("lock file entry stale (digest mismatch), re-fetching", "ref", originalRef)
		return false
	}

	// Replay: rewrite sources and add to result
	rewriteSolutionSources(sol, originalRef, vendorPath)
	result.VendoredFiles = append(result.VendoredFiles, vendorPath)
	result.Lock.Dependencies = append(result.Lock.Dependencies, *dep)
	return true
}

// rewriteSolutionSources rewrites catalog references to vendored paths in the solution.
func rewriteSolutionSources(sol *solution.Solution, catalogRef, vendorPath string) {
	// Walk resolver inputs
	for _, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil {
			continue
		}
		for _, ps := range r.Resolve.With {
			if ps.Provider == "solution" {
				rewriteSourceInput(ps.Inputs, catalogRef, vendorPath)
			}
		}
	}

	// Walk action inputs
	if sol.Spec.Workflow != nil {
		rewriteActionSources(sol.Spec.Workflow.Actions, catalogRef, vendorPath)
		rewriteActionSources(sol.Spec.Workflow.Finally, catalogRef, vendorPath)
	}
}

// rewriteActionSources rewrites catalog references in action inputs.
func rewriteActionSources(actions map[string]*actionpkg.Action, catalogRef, vendorPath string) {
	for _, a := range actions {
		if a == nil {
			continue
		}
		if a.Provider == "solution" {
			rewriteSourceInput(a.Inputs, catalogRef, vendorPath)
		}
	}
}

// rewriteSourceInput replaces a catalog ref literal with a vendored path.
func rewriteSourceInput(inputs map[string]*spec.ValueRef, catalogRef, vendorPath string) {
	if inputs == nil {
		return
	}
	vr := inputs["source"]
	if vr == nil {
		return
	}
	// Only rewrite literal string values
	if vr.Expr != nil || vr.Tmpl != nil || vr.Resolver != nil {
		return
	}
	s, ok := vr.Literal.(string)
	if !ok || s != catalogRef {
		return
	}
	vr.Literal = vendorPath
}

// vendorFileName generates the file name for a vendored artifact.
func vendorFileName(ref string, info catalog.ArtifactInfo) string {
	name := ref
	// Replace / and @ with safe characters
	name = strings.ReplaceAll(name, "/", "_")
	// If the ref doesn't have a version but info does, append it
	if !strings.Contains(ref, "@") && info.Reference.Version != nil {
		name += "@" + info.Reference.Version.String()
	}
	// Ensure .yaml extension
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}
	return name
}

// VendorFileNameFromRef generates the file name for a vendored artifact (exported).
func VendorFileNameFromRef(ref string, info catalog.ArtifactInfo) string {
	return vendorFileName(ref, info)
}
