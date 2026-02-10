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

	// Track visited refs for circular reference detection
	visited := make(map[string]bool)

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

// vendorRef fetches a single catalog reference, stores it, and rewrites the solution.
func vendorRef(ctx context.Context, sol *solution.Solution, ref CatalogRefEntry, opts VendorOptions, result *VendorResult, visited map[string]bool, existingLock *LockFile) error {
	lgr := logger.FromContext(ctx)

	// Circular reference detection
	if visited[ref.Ref] {
		lgr.V(1).Info("skipping already-vendored reference", "ref", ref.Ref)
		return nil
	}
	visited[ref.Ref] = true

	// Check if we can replay from lock file
	if existingLock != nil {
		if dep := existingLock.FindDependency(ref.Ref); dep != nil {
			lgr.V(1).Info("replaying from lock file", "ref", ref.Ref, "digest", dep.Digest)

			// Verify the vendored file still exists
			vendorPath := dep.VendoredAt
			absVendorPath := filepath.Join(opts.BundleRoot, vendorPath)
			if _, err := os.Stat(absVendorPath); err == nil {
				// File exists, verify digest
				content, err := os.ReadFile(absVendorPath)
				if err == nil {
					actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
					if actualDigest == dep.Digest {
						// Replay: rewrite sources and add to result
						rewriteSolutionSources(sol, ref.Ref, vendorPath)
						result.VendoredFiles = append(result.VendoredFiles, vendorPath)
						result.Lock.Dependencies = append(result.Lock.Dependencies, *dep)
						return nil
					}
				}
			}
			lgr.V(1).Info("lock file entry stale, re-fetching", "ref", ref.Ref)
		}
	}

	// Fetch from catalog
	if opts.CatalogFetcher == nil {
		return fmt.Errorf("no catalog fetcher configured; cannot vendor %s", ref.Ref)
	}

	content, info, err := opts.CatalogFetcher.FetchSolution(ctx, ref.Ref)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", ref.Ref, err)
	}

	// Determine vendor path
	vendoredName := vendorFileName(ref.Ref, info)
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

	lgr.V(1).Info("vendored dependency",
		"ref", ref.Ref,
		"vendoredAt", vendorRelPath,
		"digest", contentDigest,
		"catalog", info.Catalog)

	// Rewrite solution sources
	rewriteSolutionSources(sol, ref.Ref, vendorRelPath)

	// Record in result
	result.VendoredFiles = append(result.VendoredFiles, vendorRelPath)
	result.Lock.Dependencies = append(result.Lock.Dependencies, LockDependency{
		Ref:          ref.Ref,
		Digest:       contentDigest,
		ResolvedFrom: info.Catalog,
		VendoredAt:   vendorRelPath,
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
