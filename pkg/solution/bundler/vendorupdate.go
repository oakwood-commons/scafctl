// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Change status values recorded on UpdateEntry and PluginUpdateEntry.
const (
	UpdateStatusAdded     = "added"
	UpdateStatusUpdated   = "updated"
	UpdateStatusUnchanged = "unchanged"
	UpdateStatusPruned    = "pruned"
)

// Domain sentinel errors returned by vendor update helpers.
var (
	ErrInvalidUpdateTarget = errors.New("invalid update target")
	ErrSolutionRead        = errors.New("failed to read solution file")
)

// UpdateEntry tracks the change state of a single sub-solution dependency.
type UpdateEntry struct {
	Ref           string `json:"ref" yaml:"ref" doc:"Original catalog reference" maxLength:"255" example:"deploy-to-k8s@^2.0.0"`
	Constraint    string `json:"constraint,omitempty" yaml:"constraint,omitempty" doc:"Original version constraint" maxLength:"100" example:"^2.0.0"`
	Status        string `json:"status,omitempty" yaml:"status,omitempty" doc:"Change status (added, updated, unchanged, pruned)" maxLength:"20" example:"updated"`
	LockedVersion string `json:"lockedVersion" yaml:"lockedVersion" doc:"Version from the existing lock file" maxLength:"50" example:"2.0.0"`
	LockedDigest  string `json:"lockedDigest" yaml:"lockedDigest" doc:"Digest from the existing lock file" maxLength:"128" example:"sha256:abc123..."`
	LatestVersion string `json:"latestVersion" yaml:"latestVersion" doc:"Latest resolved version from catalog" maxLength:"50" example:"2.1.0"`
	LatestDigest  string `json:"latestDigest" yaml:"latestDigest" doc:"Digest of the latest content" maxLength:"128" example:"sha256:def456..."`
	NeedsUpdate   bool   `json:"needsUpdate" yaml:"needsUpdate" doc:"Whether this entry changed relative to the existing lock"`
}

// PluginUpdateEntry tracks the change state of a single plugin dependency.
type PluginUpdateEntry struct {
	Name          string `json:"name" yaml:"name" doc:"Plugin artifact name" maxLength:"100" example:"exec"`
	Alias         string `json:"alias,omitempty" yaml:"alias,omitempty" doc:"Solution-local plugin alias when aliased" maxLength:"100" example:"custom-exec"`
	Kind          string `json:"kind" yaml:"kind" doc:"Plugin kind" maxLength:"50" example:"provider"`
	Constraint    string `json:"constraint,omitempty" yaml:"constraint,omitempty" doc:"Requested version constraint" maxLength:"50" example:"^1.5.0"`
	Status        string `json:"status,omitempty" yaml:"status,omitempty" doc:"Change status (added, updated, unchanged, pruned)" maxLength:"20" example:"updated"`
	LockedVersion string `json:"lockedVersion,omitempty" yaml:"lockedVersion,omitempty" doc:"Version from the existing lock file" maxLength:"50" example:"1.5.0"`
	LockedDigest  string `json:"lockedDigest,omitempty" yaml:"lockedDigest,omitempty" doc:"Primary digest from the existing lock file" maxLength:"128" example:"sha256:abc123..."`
	LatestVersion string `json:"latestVersion" yaml:"latestVersion" doc:"Latest resolved version from catalog" maxLength:"50" example:"1.5.2"`
	LatestDigest  string `json:"latestDigest" yaml:"latestDigest" doc:"Primary digest of the latest resolved plugin" maxLength:"128" example:"sha256:def456..."`
	ResolvedFrom  string `json:"resolvedFrom,omitempty" yaml:"resolvedFrom,omitempty" doc:"Catalog alias that served the latest version" maxLength:"255" example:"official"`
	NeedsUpdate   bool   `json:"needsUpdate" yaml:"needsUpdate" doc:"Whether this plugin entry changed relative to the existing lock"`
}

// DisplayName returns "alias (name)" when the plugin is aliased, or Name otherwise.
func (e PluginUpdateEntry) DisplayName() string {
	if e.Alias != "" && e.Alias != e.Name {
		return fmt.Sprintf("%s (%s)", e.Alias, e.Name)
	}
	return e.Name
}

// VendorUpdateRunOptions configures UpdateVendoredDependencies.
type VendorUpdateRunOptions struct {
	BundleRoot, LockPath string
	Dependencies         []string
	DryRun, LockOnly     bool
	SolutionFetcher      CatalogFetcher
	PluginOptions        *VendorAllPluginsOptions
}

// VendorUpdateResult describes the outcome of a vendor update operation.
type VendorUpdateResult struct {
	Entries        []UpdateEntry       `json:"entries" yaml:"entries" doc:"Resolved sub-solution update entries" maxItems:"1000"`
	PluginEntries  []PluginUpdateEntry `json:"pluginEntries" yaml:"pluginEntries" doc:"Resolved plugin update entries" maxItems:"100"`
	UpdateCount    int                 `json:"updateCount" yaml:"updateCount" doc:"Number of entries changed" maximum:"1100" example:"3"`
	DryRun         bool                `json:"dryRun" yaml:"dryRun" doc:"Whether the operation ran in dry-run mode"`
	Messages       []string            `json:"messages,omitempty" yaml:"messages,omitempty" doc:"Informational messages" maxItems:"500"`
	PluginMessages []string            `json:"pluginMessages,omitempty" yaml:"pluginMessages,omitempty" doc:"Plugin status messages" maxItems:"100"`
}

// LoadComposedSolution reads, composes, applies defaults to, and validates solutionPath via os.Root.
func LoadComposedSolution(solutionPath string) (*solution.Solution, string, error) {
	cleanPath := filepath.Clean(solutionPath)
	bundleRoot, base := filepath.Dir(cleanPath), filepath.Base(cleanPath)
	root, err := os.OpenRoot(bundleRoot)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrSolutionRead, err)
	}
	defer root.Close()
	if info, sErr := root.Lstat(base); sErr != nil || !info.Mode().IsRegular() {
		if sErr == nil {
			sErr = fmt.Errorf("refusing non-regular solution file %s", base)
		}
		return nil, "", fmt.Errorf("%w: %w", ErrSolutionRead, sErr)
	}
	raw, err := root.ReadFile(base)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrSolutionRead, err)
	}
	var sol solution.Solution
	sol.SetPath(cleanPath)
	if err := sol.UnmarshalFromBytes(raw); err != nil {
		return nil, "", fmt.Errorf("failed to parse solution: %w", err)
	}
	composed := &sol
	if len(sol.Compose) > 0 {
		cleanBundle := filepath.Clean(bundleRoot)
		composed, err = Compose(&sol, bundleRoot, WithReadFileFunc(func(absTarget string) ([]byte, error) {
			rel, relErr := filepath.Rel(cleanBundle, filepath.Clean(absTarget))
			if relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") || filepath.IsAbs(rel) {
				return nil, fmt.Errorf("compose path %q escapes bundle root", absTarget)
			}
			return root.ReadFile(rel)
		}))
		if err != nil {
			return nil, "", fmt.Errorf("composing solution: %w", err)
		}
	}
	composed.ApplyDefaults()
	if err := composed.Validate(); err != nil {
		return nil, "", fmt.Errorf("solution validation failed: %w", err)
	}
	if err := ValidatePlugins(composed); err != nil {
		return nil, "", fmt.Errorf("plugin validation failed: %w", err)
	}
	return composed, bundleRoot, nil
}

// UpdateVendoredDependencies re-resolves targeted sub-solutions via ResolveDependencies,
// updates targeted plugins via VendorAllPlugins, and commits changes atomically.
func UpdateVendoredDependencies(ctx context.Context, sol *solution.Solution, existingLock *LockFile, opts VendorUpdateRunOptions) (*VendorUpdateResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}
	if existingLock == nil {
		existingLock = &LockFile{Version: LockFileVersion}
	}
	var discOpts []DiscoverOption
	if opts.BundleRoot != "" {
		if ic, icErr := LoadScafctlIgnore(opts.BundleRoot); icErr == nil {
			discOpts = append(discOpts, WithIgnoreChecker(ic))
		}
	}
	discovery, err := DiscoverFiles(sol, opts.BundleRoot, discOpts...)
	if err != nil {
		return nil, fmt.Errorf("discovering dependencies: %w", err)
	}
	sel, err := matchUpdateFilters(discovery.CatalogRefs, sol.Bundle.Plugins, existingLock, opts.Dependencies)
	if err != nil {
		return nil, err
	}

	var vendorRes *VendorResult
	newDeps := slices.Clone(existingLock.Dependencies)
	if len(opts.Dependencies) == 0 || len(sel.deps) > 0 {
		var filteredLock *LockFile
		if len(opts.Dependencies) > 0 {
			deps := slices.DeleteFunc(slices.Clone(existingLock.Dependencies), func(d LockDependency) bool { return sel.deps[refName(d.Ref)] })
			filteredLock = &LockFile{Version: LockFileVersion, Dependencies: deps}
		}
		solBytes, cloneErr := sol.ToYAML()
		var solCopy solution.Solution
		if cloneErr == nil {
			cloneErr = solCopy.UnmarshalFromBytes(solBytes)
		}
		if cloneErr != nil {
			return nil, fmt.Errorf("cloning solution: %w", cloneErr)
		}
		vOpts := VendorOptions{BundleRoot: opts.BundleRoot, ExistingLock: filteredLock, CatalogFetcher: opts.SolutionFetcher}
		if vendorRes, err = ResolveDependencies(ctx, &solCopy, discovery.CatalogRefs, vOpts); err != nil {
			return nil, err
		}
		newDeps = vendorRes.Lock.Dependencies
	}

	entries := diffLockDependencies(existingLock.Dependencies, newDeps)
	newPlugins := slices.Clone(existingLock.Plugins)
	pluginEntries := []PluginUpdateEntry{}
	var pluginMessages []string
	if opts.PluginOptions != nil {
		if newPlugins, pluginEntries, err = resolveAndDiffPlugins(ctx, sol.Bundle.Plugins, existingLock, sel, len(opts.Dependencies) == 0, *opts.PluginOptions); err != nil {
			return nil, err
		}
	} else if len(opts.Dependencies) == 0 {
		pluginMessages = CheckPluginUpdates(existingLock)
	}

	res := buildVendorUpdateResult(entries, pluginEntries, pluginMessages, opts.DryRun)
	if opts.DryRun || res.UpdateCount == 0 {
		return res, nil
	}
	if !opts.LockOnly && opts.BundleRoot != "" {
		if err := syncVendorDir(opts.BundleRoot, vendorRes, existingLock.Dependencies, newDeps); err != nil {
			return nil, err
		}
	}
	if opts.LockPath != "" {
		if err := WriteLockFile(opts.LockPath, &LockFile{Version: LockFileVersion, Dependencies: newDeps, Plugins: newPlugins}); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func formatChangeMsg(label, status, lockedVer, latestVer, unchangedSuffix string, dryRun bool) string {
	lv := cmp.Or(lockedVer, "(unlocked)")
	switch {
	case status == UpdateStatusPruned && dryRun:
		return fmt.Sprintf("%s: would prune (%s)", label, lv)
	case status == UpdateStatusPruned:
		return fmt.Sprintf("%s: pruned (%s)", label, lv)
	case status == UpdateStatusUnchanged:
		return fmt.Sprintf("%s: %s", label, unchangedSuffix)
	case dryRun:
		return fmt.Sprintf("%s: locked %s -> latest %s (would update)", label, lv, latestVer)
	default:
		return fmt.Sprintf("%s: updated %s -> %s", label, lv, latestVer)
	}
}

func buildVendorUpdateResult(entries []UpdateEntry, pluginEntries []PluginUpdateEntry, fallbackPluginMsgs []string, dryRun bool) *VendorUpdateResult {
	res := &VendorUpdateResult{Entries: entries, PluginEntries: pluginEntries, DryRun: dryRun, PluginMessages: fallbackPluginMsgs}
	for _, e := range entries {
		if e.NeedsUpdate {
			res.UpdateCount++
		}
		res.Messages = append(res.Messages, formatChangeMsg(e.Ref, e.Status, e.LockedVersion, e.LatestVersion, "up to date ("+e.LatestVersion+")", dryRun))
	}
	for _, pe := range pluginEntries {
		if pe.NeedsUpdate {
			res.UpdateCount++
		}
		res.PluginMessages = append(res.PluginMessages, formatChangeMsg(pe.DisplayName()+" ("+pe.Kind+")", pe.Status, pe.LockedVersion, pe.LatestVersion, pe.LatestVersion+" up to date", dryRun))
	}
	return res
}

type filterTargets struct {
	deps               map[string]bool
	plugins, lockPlugs map[int]bool
}

func matchUpdateFilters(refs []CatalogRefEntry, plugins []solution.PluginDependency, lock *LockFile, filters []string) (filterTargets, error) {
	if len(filters) == 0 {
		return filterTargets{}, nil
	}
	sel := filterTargets{deps: make(map[string]bool), plugins: make(map[int]bool), lockPlugs: make(map[int]bool)}
	var unmatched []string
	for _, f := range filters {
		matched := false
		for _, cr := range refs {
			if cr.Ref == f || refName(cr.Ref) == f {
				sel.deps[refName(cr.Ref)], matched = true, true
			}
		}
		for _, dep := range lock.Dependencies {
			if dep.Ref == f || refName(dep.Ref) == f {
				sel.deps[refName(dep.Ref)], matched = true, true
			}
		}
		for i, p := range plugins {
			kp := string(p.Kind) + "/"
			if p.LocalName() == f || p.ArtifactName() == f || kp+p.LocalName() == f || kp+p.ArtifactName() == f {
				sel.plugins[i], matched = true, true
			}
		}
		for li, lp := range lock.Plugins {
			if lp.Name == f || lp.Kind+"/"+lp.Name == f {
				sel.lockPlugs[li], matched = true, true
			}
		}
		if !matched && !slices.Contains(unmatched, f) {
			unmatched = append(unmatched, f)
		}
	}
	if len(unmatched) == 1 {
		return filterTargets{}, fmt.Errorf("%w: dependency %q not found in solution or lock file", ErrInvalidUpdateTarget, unmatched[0])
	}
	if len(unmatched) > 1 {
		return filterTargets{}, fmt.Errorf("%w: dependencies not found in solution or lock file: %s", ErrInvalidUpdateTarget, strings.Join(unmatched, ", "))
	}
	return sel, nil
}

func updateStatusFor(changed bool) string {
	if changed {
		return UpdateStatusUpdated
	}
	return UpdateStatusUnchanged
}

func diffLockDependencies(oldDeps, newDeps []LockDependency) []UpdateEntry {
	entries := make([]UpdateEntry, 0, len(newDeps))
	usedOld := make(map[int]bool, len(oldDeps))
	for _, nd := range newDeps {
		idx := -1
		for i, od := range oldDeps {
			if !usedOld[i] && (od.Ref == nd.Ref || (idx < 0 && refName(od.Ref) == refName(nd.Ref))) {
				idx = i
				if od.Ref == nd.Ref {
					break
				}
			}
		}
		latestVer := cmp.Or(nd.ResolvedVersion, ExtractVersionFromRef(nd.Ref))
		if idx < 0 {
			entries = append(entries, UpdateEntry{Ref: nd.Ref, Constraint: nd.Constraint, Status: UpdateStatusAdded, LatestVersion: latestVer, LatestDigest: nd.Digest, NeedsUpdate: true})
			continue
		}
		usedOld[idx] = true
		od := oldDeps[idx]
		changed := od.Digest != nd.Digest || od.ResolvedVersion != nd.ResolvedVersion || od.Ref != nd.Ref || od.Constraint != nd.Constraint
		entries = append(entries, UpdateEntry{
			Ref: nd.Ref, Constraint: nd.Constraint, Status: updateStatusFor(changed), NeedsUpdate: changed,
			LockedVersion: cmp.Or(od.ResolvedVersion, ExtractVersionFromRef(od.Ref)), LockedDigest: od.Digest,
			LatestVersion: latestVer, LatestDigest: nd.Digest,
		})
	}
	for i, od := range oldDeps {
		if !usedOld[i] {
			lv := cmp.Or(od.ResolvedVersion, ExtractVersionFromRef(od.Ref))
			entries = append(entries, UpdateEntry{Ref: od.Ref, Constraint: od.Constraint, Status: UpdateStatusPruned, LockedVersion: lv, LockedDigest: od.Digest, NeedsUpdate: true})
		}
	}
	return entries
}

func lockedPluginMatchesConstraint(constraint, lockedVer string) bool {
	verStr := strings.TrimSpace(constraint)
	if verStr == "" || strings.EqualFold(verStr, "latest") {
		return lockedVer != ""
	}
	_, _, ok := lockedDepMatchesRef(&LockDependency{ResolvedVersion: lockedVer}, "p@"+verStr)
	return ok
}

func resolveAndDiffPlugins(ctx context.Context, declared []solution.PluginDependency, existingLock *LockFile, sel filterTargets, updateAll bool, opts VendorAllPluginsOptions) ([]LockPlugin, []PluginUpdateEntry, error) {
	if opts.Platform == "" {
		opts.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	resolved := make([]LockPlugin, 0, len(declared))
	entries := make([]PluginUpdateEntry, 0, len(declared))
	usedOld := make(map[int]bool, len(existingLock.Plugins))
	claimOld := func(p solution.PluginDependency) *LockPlugin {
		for exact := range 2 {
			for i := range existingLock.Plugins {
				single := &LockFile{Plugins: existingLock.Plugins[i : i+1]}
				if !usedOld[i] && ((exact == 0 && single.FindPluginByVersionConstraint(p) != nil) || (exact == 1 && single.FindPluginByDep(p) != nil)) {
					usedOld[i] = true
					return &existingLock.Plugins[i]
				}
			}
		}
		return nil
	}

	for i, p := range declared {
		locked := claimOld(p)
		var latest LockPlugin
		if !updateAll && !sel.plugins[i] && locked != nil && lockedPluginMatchesConstraint(p.Version, locked.Version) {
			latest = cloneLockPlugin(*locked)
			latest.Constraint = p.Version
		} else {
			vr, err := VendorAllPlugins(ctx, []solution.PluginDependency{p}, nil, opts)
			if err != nil {
				return nil, nil, err
			}
			latest = cloneLockPlugin(vr.ResolvedPlugins[0])
			if locked != nil && locked.Version == latest.Version && lockedDigestsMatch(locked, latest.Digests, latest.Digest) &&
				locked.IsBuilt() == latest.IsBuilt() && locked.ResolvedCanonical == latest.ResolvedCanonical &&
				(locked.ResolvedCanonical != "" || locked.ResolvedFrom == latest.ResolvedFrom) {
				latest.Digest = locked.Digest
				if locked.ResolvedCanonical != "" && locked.ResolvedFrom != "" {
					latest.ResolvedFrom = locked.ResolvedFrom
				}
				if opts.VerifySignature == nil && locked.Signature != nil && !latest.IsBuilt() && latest.ResolvedCanonical != "" {
					sig := *locked.Signature
					latest.Signature = &sig
				}
			}
		}
		resolved = append(resolved, latest)

		pe := PluginUpdateEntry{
			Name: latest.Name, Kind: latest.Kind, Constraint: latest.Constraint,
			LatestVersion: latest.Version, LatestDigest: latest.Digest, ResolvedFrom: latest.ResolvedFrom,
		}
		if p.IsAliased() {
			pe.Alias = p.LocalName()
		}
		if locked == nil {
			pe.Status, pe.NeedsUpdate = UpdateStatusAdded, true
		} else {
			pe.LockedVersion, pe.LockedDigest = locked.Version, locked.Digest
			pe.NeedsUpdate = !lockPluginsEqual(*locked, latest)
			pe.Status = updateStatusFor(pe.NeedsUpdate)
		}
		entries = append(entries, pe)
	}

	for i, lp := range existingLock.Plugins {
		switch {
		case usedOld[i]:
		case updateAll || sel.lockPlugs[i]:
			entries = append(entries, PluginUpdateEntry{Name: lp.Name, Kind: lp.Kind, Constraint: lp.Constraint, Status: UpdateStatusPruned, LockedVersion: lp.Version, LockedDigest: lp.Digest, NeedsUpdate: true})
		default:
			resolved = append(resolved, cloneLockPlugin(lp))
		}
	}
	if len(resolved) == 0 {
		resolved = nil
	}
	return resolved, entries, nil
}

func lockPluginsEqual(a, b LockPlugin) bool {
	srcEq := (a.Source == nil && b.Source == nil) || (a.Source != nil && b.Source != nil && *a.Source == *b.Source)
	sigEq := (a.Signature == nil && b.Signature == nil) || (a.Signature != nil && b.Signature != nil && *a.Signature == *b.Signature)
	return a.Name == b.Name && a.Kind == b.Kind && a.Version == b.Version && a.Constraint == b.Constraint &&
		a.Digest == b.Digest && len(a.Digests) == len(b.Digests) && maps.Equal(a.Digests, b.Digests) &&
		a.ResolvedFrom == b.ResolvedFrom && a.ResolvedCanonical == b.ResolvedCanonical && srcEq && sigEq
}

func syncVendorDir(bundleRoot string, vendorRes *VendorResult, oldDeps, newDeps []LockDependency) error {
	if vendorRes != nil {
		if err := vendorRes.WriteFiles(bundleRoot); err != nil {
			return err
		}
	}
	active := make(map[string]bool, len(newDeps))
	for _, nd := range newDeps {
		if slashPath, _, ok := cleanVendorPath(nd.VendoredAt); ok {
			active[slashPath] = true
		}
	}
	var toRemove []string
	for _, od := range oldDeps {
		if slashPath, subRel, ok := cleanVendorPath(od.VendoredAt); ok && !active[slashPath] {
			toRemove = append(toRemove, subRel)
		}
	}
	if len(toRemove) == 0 {
		return nil
	}
	vRoot, err := openVendorSubRoot(bundleRoot, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("opening vendor root: %w", err)
	}
	defer vRoot.Close()
	for _, subRel := range toRemove {
		if rmErr := vRoot.Remove(subRel); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("removing pruned vendored file %s: %w", subRel, rmErr)
		}
	}
	return nil
}

// CatalogChainAdapter adapts any catalog.Catalog to the CatalogFetcher interface.
type CatalogChainAdapter struct {
	Catalog      catalog.Catalog
	LocalCatalog *catalog.LocalCatalog
}

// FetchSolution retrieves a solution by name[@version] from the catalog chain.
func (a *CatalogChainAdapter) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	if a == nil || a.Catalog == nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("catalog is nil")
	}
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, nameWithVersion)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("invalid reference %q: %w", nameWithVersion, err)
	}
	content, info, err := a.Catalog.Fetch(ctx, ref)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}
	if canonical := cmp.Or(info.Canonical, info.Reference.Remote); a.LocalCatalog != nil &&
		info.Reference.Version != nil && canonical != "" && info.Catalog != "" && !strings.EqualFold(info.Catalog, a.LocalCatalog.Name()) {
		cacheRef := info.Reference
		cacheRef.Remote, cacheRef.Built = canonical, false
		ann := catalog.NewAnnotationBuilder().Set(catalog.AnnotationSource, "auto-cached").
			Set(catalog.AnnotationOrigin, "auto-cached from "+info.Catalog).
			Set(catalog.AnnotationSourceCanonical, canonical).Build()
		if _, sErr := a.LocalCatalog.Store(ctx, cacheRef, content, nil, ann, true); sErr != nil {
			logger.FromContext(ctx).V(1).Info("failed to auto-cache remote solution locally (non-fatal)", "name", cacheRef.Name, "error", sErr)
		}
	}
	return content, info, nil
}

// ListSolutions returns all available versions of a named solution artifact from the catalog chain.
func (a *CatalogChainAdapter) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	if a == nil || a.Catalog == nil {
		return nil, fmt.Errorf("catalog is nil")
	}
	return a.Catalog.List(ctx, catalog.ArtifactKindSolution, name)
}

// ExtractVersionFromRef extracts the version part after the last '@' in a catalog reference.
func ExtractVersionFromRef(ref string) string {
	if idx := strings.LastIndex(ref, "@"); idx >= 0 {
		return ref[idx+1:]
	}
	return "latest"
}

// TruncateDigest truncates a digest string for display purposes.
func TruncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}

// CheckPluginUpdates returns formatted status messages for locked plugin entries.
func CheckPluginUpdates(lock *LockFile) []string {
	if lock == nil {
		return nil
	}
	messages := make([]string, 0, len(lock.Plugins))
	for _, p := range lock.Plugins {
		messages = append(messages, fmt.Sprintf("  - %s (%s): %s (locked at %s)", p.Name, p.Kind, p.Version, TruncateDigest(p.Digest)))
	}
	return messages
}
