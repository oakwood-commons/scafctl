// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	scafpath "github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

const (
	// VendorDirName is the directory name within the bundle for vendored artifacts.
	VendorDirName = ".scafctl/vendor"

	vendorDirPerm  = 0o700
	vendorFilePerm = 0o600
)

// VendorOptions configures dependency resolution.
type VendorOptions struct {
	BundleRoot     string
	ExistingLock   *LockFile
	CatalogFetcher CatalogFetcher
}

// CatalogFetcher fetches solution content from a catalog by reference.
type CatalogFetcher interface {
	FetchSolution(ctx context.Context, nameWithVersion string) (content []byte, info catalog.ArtifactInfo, err error)
	ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error)
}

// VendorResult describes the outcome of dependency resolution.
type VendorResult struct {
	VendoredFiles []string
	Fetched       map[string][]byte
	Replayed      map[string][]byte
	Lock          *LockFile
}

// Contents returns the content of every vendored file, fetched or
// replayed, keyed by its slash-separated path relative to the bundle root.
func (r *VendorResult) Contents() map[string][]byte {
	out := make(map[string][]byte, len(r.Fetched))
	maps.Copy(out, r.Replayed)
	maps.Copy(out, r.Fetched)
	return out
}

// ResolveDependencies resolves catalog dependencies, rewrites their source
// references in sol to vendored paths, and records them in a lock. It writes
// nothing to disk: persist the fetched content with WriteFiles and the lock
// with WriteLockFile once the caller decides to commit the result.
func ResolveDependencies(ctx context.Context, sol *solution.Solution, refs []CatalogRefEntry, opts VendorOptions) (*VendorResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}
	result := &VendorResult{
		Fetched:  make(map[string][]byte),
		Replayed: make(map[string][]byte),
		Lock: &LockFile{
			Version:      LockFileVersion,
			Dependencies: make([]LockDependency, 0, len(refs)),
		},
	}
	visited := make(map[string]string)
	for _, ref := range refs {
		if err := vendorRef(ctx, sol, ref, opts, result, visited); err != nil {
			return nil, fmt.Errorf("failed to vendor %s: %w", ref.Ref, err)
		}
	}
	return result, nil
}

func cleanVendorPath(p string) (slashPath, subRel string, ok bool) {
	if strings.TrimSpace(p) == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || scafpath.HasWindowsDrivePrefix(p) {
		return "", "", false
	}
	cleanRel := filepath.Clean(filepath.FromSlash(p))
	rel, err := filepath.Rel(filepath.Clean(VendorDirName), cleanRel)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") || filepath.IsAbs(rel) {
		return "", "", false
	}
	return filepath.ToSlash(cleanRel), rel, true
}

func openVendorSubRoot(bundleRoot string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(bundleRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var built string
	for _, seg := range strings.Split(VendorDirName, "/") {
		if built == "" {
			built = seg
		} else {
			built = filepath.Join(built, seg)
		}
		info, statErr := root.Lstat(built)
		switch {
		case statErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("refusing non-directory vendor segment %s", built)
			}
		case errors.Is(statErr, os.ErrNotExist) && create:
			if mkErr := root.Mkdir(built, vendorDirPerm); mkErr != nil {
				return nil, mkErr
			}
		default:
			return nil, statErr
		}
	}
	return root.OpenRoot(filepath.FromSlash(VendorDirName))
}

// WriteFiles writes the content fetched by ResolveDependencies under
// bundleRoot/.scafctl/vendor via a confined os.Root handle.
func (r *VendorResult) WriteFiles(bundleRoot string) error {
	if len(r.Fetched) == 0 {
		return nil
	}
	subPaths := make(map[string]string, len(r.Fetched))
	for relPath := range r.Fetched {
		_, subRel, ok := cleanVendorPath(relPath)
		if !ok {
			return fmt.Errorf("failed to write vendored file %s: path must be within %s", relPath, VendorDirName)
		}
		subPaths[relPath] = subRel
	}
	vRoot, err := openVendorSubRoot(bundleRoot, true)
	if err != nil {
		return fmt.Errorf("failed to create vendor directory: %w", err)
	}
	defer vRoot.Close()

	for relPath, content := range r.Fetched {
		subRel := subPaths[relPath]
		if dir := filepath.Dir(subRel); dir != "." {
			if err := vRoot.MkdirAll(dir, vendorDirPerm); err != nil {
				return fmt.Errorf("failed to create vendor directory for %s: %w", relPath, err)
			}
		}
		if info, statErr := vRoot.Lstat(subRel); statErr == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("failed to write vendored file %s: target is not a regular file", relPath)
		}
		f, openErr := vRoot.OpenFile(subRel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, vendorFilePerm)
		if openErr != nil {
			return fmt.Errorf("failed to write vendored file %s: %w", relPath, openErr)
		}
		_, writeErr := f.Write(content)
		chmodErr := f.Chmod(vendorFilePerm)
		closeErr := f.Close()
		if err := cmp.Or(writeErr, chmodErr, closeErr); err != nil {
			return fmt.Errorf("failed to write vendored file %s: %w", relPath, err)
		}
	}
	return nil
}

type resolvedRef struct {
	name        string
	version     *semver.Version
	constraint  string
	resolvedKey string
}

func pickBestSolutionVersion(ctx context.Context, artifacts []catalog.ArtifactInfo, constraint *semver.Constraints) *semver.Version {
	includePre := catalog.IncludePreReleaseFromContext(ctx)
	var best *semver.Version
	for _, a := range artifacts {
		v := a.Reference.Version
		if v == nil || (catalog.IsPreRelease(v) && !includePre) {
			continue
		}
		if constraint != nil && !constraint.Check(v) {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
		}
	}
	return best
}

func parseAndResolveRef(ctx context.Context, ref string, fetcher CatalogFetcher) (*resolvedRef, error) {
	name, versionPart := splitRef(ref)

	if versionPart == "" || strings.EqualFold(versionPart, "latest") {
		var constraint string
		if strings.EqualFold(versionPart, "latest") {
			constraint = "latest"
		}
		if fetcher != nil {
			if artifacts, err := fetcher.ListSolutions(ctx, name); err == nil {
				if best := pickBestSolutionVersion(ctx, artifacts, nil); best != nil {
					return &resolvedRef{
						name: name, version: best, constraint: constraint,
						resolvedKey: name + "@" + best.String(),
					}, nil
				}
			}
		}
		return &resolvedRef{name: name, constraint: constraint, resolvedKey: name}, nil
	}

	if v, err := semver.NewVersion(versionPart); err == nil {
		return &resolvedRef{name: name, version: v, resolvedKey: name + "@" + v.String()}, nil
	}

	constraint, err := semver.NewConstraint(versionPart)
	if err != nil {
		return nil, fmt.Errorf("invalid version or constraint %q in ref %q: %w", versionPart, ref, err)
	}
	constraint.IncludePrerelease = catalog.IncludePreReleaseFromContext(ctx)
	if fetcher == nil {
		return nil, fmt.Errorf("cannot resolve constraint %q for %q: no catalog fetcher configured", versionPart, name)
	}

	artifacts, err := fetcher.ListSolutions(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions for %q: %w", name, err)
	}
	bestVersion := pickBestSolutionVersion(ctx, artifacts, constraint)
	if bestVersion == nil {
		return nil, fmt.Errorf("no version of %q satisfies constraint %q (available: %s)",
			name, versionPart, formatAvailableVersions(artifacts))
	}

	logger.FromContext(ctx).V(1).Info("resolved version constraint",
		"name", name, "constraint", versionPart, "resolved", bestVersion.String(), "candidates", len(artifacts))
	return &resolvedRef{
		name: name, version: bestVersion, constraint: versionPart,
		resolvedKey: name + "@" + bestVersion.String(),
	}, nil
}

func splitRef(ref string) (name, versionPart string) {
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

func formatAvailableVersions(artifacts []catalog.ArtifactInfo) string {
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

func lockedDepMatchesRef(dep *LockDependency, rawRef string) (resolvedKey, constraint string, ok bool) {
	if dep == nil {
		return "", "", false
	}
	name, versionPart := splitRef(rawRef)
	lockedVer := dep.ResolvedVersion
	if lockedVer == "" {
		if _, dv := splitRef(dep.Ref); dv != "" {
			if pv, err := semver.NewVersion(dv); err == nil {
				lockedVer = pv.String()
			}
		}
	}
	switch {
	case versionPart == "":
		if lockedVer != "" {
			return name + "@" + lockedVer, "", true
		}
		return name, "", dep.Ref == rawRef && dep.VendoredAt != ""
	case strings.EqualFold(versionPart, "latest"):
		return name + "@" + lockedVer, "latest", lockedVer != ""
	default:
		if exactVer, err := semver.NewVersion(versionPart); err == nil {
			return name + "@" + exactVer.String(), "", lockedVer == exactVer.String() || (lockedVer == "" && dep.Ref == rawRef)
		}
		parsedLocked, verErr := semver.NewVersion(lockedVer)
		c, checkErr := semver.NewConstraint(versionPart)
		if verErr != nil || checkErr != nil {
			return "", "", false
		}
		c.IncludePrerelease = catalog.IsPreRelease(parsedLocked)
		return name + "@" + lockedVer, versionPart, c.Check(parsedLocked)
	}
}

func checkVisitedKey(sol *solution.Solution, rawRef, name, resolvedKey string, result *VendorResult, visited map[string]string) (bool, error) {
	if previousRef, seen := visited[resolvedKey]; seen {
		if rawRef != previousRef {
			for _, dep := range result.Lock.Dependencies {
				if refName(dep.Ref) == name {
					rewriteSolutionSources(sol, rawRef, dep.VendoredAt)
					break
				}
			}
		}
		return true, nil
	}
	for prevKey, prevRef := range visited {
		prevName, _ := splitRef(prevKey)
		if prevName == name && prevKey != resolvedKey {
			return false, fmt.Errorf("version conflict for %q: %q resolves to %s, but %q already resolved to %s",
				name, rawRef, resolvedKey, prevRef, prevKey)
		}
	}
	return false, nil
}

func vendorRef(ctx context.Context, sol *solution.Solution, ref CatalogRefEntry, opts VendorOptions, result *VendorResult, visited map[string]string) error {
	lgr := logger.FromContext(ctx)
	name, _ := splitRef(ref.Ref)

	if opts.ExistingLock != nil {
		if dep := opts.ExistingLock.FindDependencyByName(name); dep != nil {
			if lockedKey, constraint, ok := lockedDepMatchesRef(dep, ref.Ref); ok {
				seen, conflictErr := checkVisitedKey(sol, ref.Ref, name, lockedKey, result, visited)
				if conflictErr != nil || seen {
					return conflictErr
				}
				replayed, missingOnDisk, replayErr := tryReplayLock(ctx, sol, ref.Ref, lockedKey, constraint, dep, opts, result, visited)
				if replayErr != nil || replayed {
					return replayErr
				}
				if missingOnDisk && dep.ResolvedVersion != "" && opts.CatalogFetcher != nil {
					return tryMaterializeLockedDep(ctx, sol, ref.Ref, lockedKey, constraint, dep, opts, result, visited)
				}
			}
		}
	}

	resolved, err := parseAndResolveRef(ctx, ref.Ref, opts.CatalogFetcher)
	if err != nil {
		return err
	}
	seen, conflictErr := checkVisitedKey(sol, ref.Ref, resolved.name, resolved.resolvedKey, result, visited)
	if conflictErr != nil || seen {
		return conflictErr
	}
	if opts.CatalogFetcher == nil {
		return fmt.Errorf("no catalog fetcher configured; cannot vendor %s", ref.Ref)
	}

	fetchRef := resolved.name
	if resolved.version != nil {
		fetchRef = resolved.name + "@" + resolved.version.String()
	}
	content, info, err := opts.CatalogFetcher.FetchSolution(ctx, fetchRef)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", fetchRef, err)
	}

	visited[resolved.resolvedKey] = ref.Ref
	vendorRelPath := filepath.ToSlash(filepath.Join(VendorDirName, vendorFileName(fetchRef, info)))
	result.Fetched[vendorRelPath] = content

	contentDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	resolvedVersion := ""
	if resolved.version != nil {
		resolvedVersion = resolved.version.String()
	} else if info.Reference.Version != nil {
		resolvedVersion = info.Reference.Version.String()
	}

	lgr.V(1).Info("vendored dependency",
		"ref", ref.Ref, "resolvedVersion", resolvedVersion, "vendoredAt", vendorRelPath, "digest", contentDigest, "catalog", info.Catalog)

	rewriteSolutionSources(sol, ref.Ref, vendorRelPath)
	if fetchRef != ref.Ref {
		rewriteSolutionSources(sol, fetchRef, vendorRelPath)
	}
	result.VendoredFiles = append(result.VendoredFiles, vendorRelPath)
	result.Lock.Dependencies = append(result.Lock.Dependencies, LockDependency{
		Ref:             ref.Ref,
		ResolvedVersion: resolvedVersion,
		Constraint:      resolved.constraint,
		Digest:          contentDigest,
		ResolvedFrom:    info.Catalog,
		VendoredAt:      vendorRelPath,
	})
	return vendorSubRefs(ctx, sol, ref.Ref, content, opts, result, visited)
}

func vendorSubRefs(ctx context.Context, sol *solution.Solution, parentRef string, content []byte, opts VendorOptions, result *VendorResult, visited map[string]string) error {
	var vendoredSol solution.Solution
	if err := vendoredSol.UnmarshalFromBytes(content); err != nil {
		logger.FromContext(ctx).V(1).Info("vendored content is not a valid solution, skipping recursive vendoring", "ref", parentRef, "error", err)
		return nil
	}
	_, subRefs := analyzeProviderInputs(&vendoredSol)
	for _, subRef := range subRefs {
		if err := vendorRef(ctx, sol, subRef, opts, result, visited); err != nil {
			return fmt.Errorf("recursive vendoring of %s (from %s): %w", subRef.Ref, parentRef, err)
		}
	}
	return nil
}

func tryReplayLock(
	ctx context.Context,
	sol *solution.Solution,
	originalRef, resolvedKey, constraint string,
	dep *LockDependency,
	opts VendorOptions,
	result *VendorResult,
	visited map[string]string,
) (replayed, missingOnDisk bool, err error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("replaying from lock file", "ref", originalRef, "digest", dep.Digest)

	if dep.VendoredAt == "" || opts.BundleRoot == "" {
		return false, true, nil
	}
	vendorPath, subRel, ok := cleanVendorPath(dep.VendoredAt)
	if !ok {
		return false, false, fmt.Errorf("invalid vendoredAt path %q in lock file: must be within %s", dep.VendoredAt, VendorDirName)
	}
	vRoot, openErr := openVendorSubRoot(opts.BundleRoot, false)
	if openErr != nil {
		return false, errors.Is(openErr, os.ErrNotExist), nil
	}
	defer vRoot.Close()

	info, statErr := vRoot.Lstat(subRel)
	if statErr != nil {
		lgr.V(1).Info("lock file entry stale (file missing), re-fetching", "ref", originalRef)
		return false, errors.Is(statErr, os.ErrNotExist), nil
	}
	if !info.Mode().IsRegular() {
		return false, false, fmt.Errorf("refusing non-regular vendored file %s", vendorPath)
	}
	content, readErr := vRoot.ReadFile(subRel)
	if readErr != nil {
		return false, false, nil
	}
	if fmt.Sprintf("sha256:%x", sha256.Sum256(content)) != dep.Digest {
		lgr.V(1).Info("lock file entry stale (digest mismatch), re-fetching", "ref", originalRef)
		return false, false, nil
	}

	visited[resolvedKey] = originalRef
	rewriteSolutionSources(sol, originalRef, vendorPath)
	result.Replayed[vendorPath] = content
	result.VendoredFiles = append(result.VendoredFiles, vendorPath)
	replayedDep := *dep
	replayedDep.Ref, replayedDep.VendoredAt = originalRef, vendorPath
	if originalRef != dep.Ref {
		replayedDep.Constraint = constraint
	}
	result.Lock.Dependencies = append(result.Lock.Dependencies, replayedDep)

	if err := vendorSubRefs(ctx, sol, originalRef, content, opts, result, visited); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func tryMaterializeLockedDep(
	ctx context.Context,
	sol *solution.Solution,
	originalRef, resolvedKey, constraint string,
	dep *LockDependency,
	opts VendorOptions,
	result *VendorResult,
	visited map[string]string,
) error {
	var explicitVendorPath string
	if dep.VendoredAt != "" {
		cleanRel, _, ok := cleanVendorPath(dep.VendoredAt)
		if !ok {
			return fmt.Errorf("invalid vendoredAt path %q in lock file: must be within %s", dep.VendoredAt, VendorDirName)
		}
		explicitVendorPath = cleanRel
	}
	name, _ := splitRef(originalRef)
	fetchRef := name + "@" + dep.ResolvedVersion
	content, info, err := opts.CatalogFetcher.FetchSolution(ctx, fetchRef)
	if err != nil {
		return fmt.Errorf("failed to fetch locked dependency %s: %w", fetchRef, err)
	}
	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	if dep.Digest != "" && actualDigest != dep.Digest {
		return fmt.Errorf("locked dependency %s digest mismatch: expected %s, got %s", fetchRef, dep.Digest, actualDigest)
	}

	vendorRelPath := cmp.Or(explicitVendorPath, filepath.ToSlash(filepath.Join(VendorDirName, vendorFileName(fetchRef, info))))
	effConstraint := constraint
	if effConstraint == "" && originalRef == dep.Ref {
		effConstraint = dep.Constraint
	}

	visited[resolvedKey] = originalRef
	result.Fetched[vendorRelPath] = content
	result.VendoredFiles = append(result.VendoredFiles, vendorRelPath)
	rewriteSolutionSources(sol, originalRef, vendorRelPath)
	if fetchRef != originalRef {
		rewriteSolutionSources(sol, fetchRef, vendorRelPath)
	}
	result.Lock.Dependencies = append(result.Lock.Dependencies, LockDependency{
		Ref:             originalRef,
		ResolvedVersion: dep.ResolvedVersion,
		Constraint:      effConstraint,
		Digest:          actualDigest,
		ResolvedFrom:    cmp.Or(info.Catalog, dep.ResolvedFrom),
		VendoredAt:      vendorRelPath,
	})
	return vendorSubRefs(ctx, sol, originalRef, content, opts, result, visited)
}

func rewriteSolutionSources(sol *solution.Solution, catalogRef, vendorPath string) {
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
	if sol.Spec.Workflow != nil {
		rewriteActionSources(sol.Spec.Workflow.Actions, catalogRef, vendorPath)
		rewriteActionSources(sol.Spec.Workflow.Finally, catalogRef, vendorPath)
	}
}

func rewriteActionSources(actions map[string]*actionpkg.Action, catalogRef, vendorPath string) {
	for _, a := range actions {
		if a != nil && a.Provider == "solution" {
			rewriteSourceInput(a.Inputs, catalogRef, vendorPath)
		}
	}
}

func rewriteSourceInput(inputs map[string]*spec.ValueRef, catalogRef, vendorPath string) {
	if inputs == nil {
		return
	}
	vr := inputs["source"]
	if vr == nil || vr.Expr != nil || vr.Tmpl != nil || vr.Resolver != nil || vr.Secret != nil || vr.Token != nil {
		return
	}
	if s, ok := vr.Literal.(string); ok && s == catalogRef {
		vr.Literal = vendorPath
	}
}

func vendorFileName(ref string, info catalog.ArtifactInfo) string {
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(ref)
	if !strings.Contains(ref, "@") && info.Reference.Version != nil {
		name += "@" + info.Reference.Version.String()
	}
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}
	return name
}

// VendorFileNameFromRef generates the file name for a vendored artifact (exported).
func VendorFileNameFromRef(ref string, info catalog.ArtifactInfo) string {
	return vendorFileName(ref, info)
}
