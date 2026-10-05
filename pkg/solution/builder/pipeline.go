// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// pipelineState carries values between stages. Each stage reads what earlier
// stages produced and records its own output; the comment on each field names
// the stage that sets it.
type pipelineState struct {
	// bundle is true when the bundle stages run (SolutionSource.ShouldBundle).
	// Set by newState.
	bundle bool
	// discovery holds the local files and catalog references to bundle.
	// Set by discover; resolve catalog dependencies adds vendored files.
	discovery *bundler.DiscoveryResult
	// existingLock is the solution.lock read at load time; replayed so
	// pinned versions stay stable. Set by read lock.
	existingLock *bundler.LockFile
	// plugins are the pinned plugins. Set by resolve plugins.
	plugins []bundler.LockPlugin
	// deps are the vendored catalog dependencies, held in memory until
	// written. Set by resolve catalog dependencies.
	deps *bundler.VendorResult
	// lock is the lock assembled from plugins and deps; nil when empty.
	// Set by assemble lock.
	lock *bundler.LockFile
	// content is the solution document to store. Set by render.
	content []byte
	// files holds the content of the files to bundle, read once and shared
	// by the build fingerprint and the archive. Set by read files.
	files *bundler.FileSnapshot
	// staged is the artifact held in memory until it is verified.
	// Set by stage.
	staged *stagedArtifact
	// result is returned by Do. Every stage may add to it.
	result *PackageResult
}

// stage is one step of the pipeline. run returns done to finish the
// pipeline early without error.
type stage struct {
	name string
	when func(st *pipelineState) bool
	run  func(ctx context.Context, st *pipelineState) (done bool, err error)
}

// newState returns the initial pipeline state.
func (p *Packager) newState() *pipelineState {
	bundle, _ := p.src.ShouldBundle()
	return &pipelineState{bundle: bundle, result: &PackageResult{}}
}

// stages returns the pipeline in execution order. Apart from the requested
// --composed-out file, nothing is written before the dry-run stage, and
// nothing reaches the catalog before verify.
func (p *Packager) stages() []stage {
	return []stage{
		{name: "preflight", when: p.preflightEnabled, run: p.runPreflight},
		{name: "compose", when: p.composed, run: p.reportCompose},
		{name: "discover", when: bundling, run: p.discover},
		{name: "check plugins", when: bundling, run: p.checkPlugins},
		{name: "read lock", when: p.vendoring, run: p.readLock},
		{name: "resolve plugins", when: p.pluginsToVendor, run: p.resolvePlugins},
		{name: "resolve catalog dependencies", when: p.depsToVendor, run: p.resolveCatalogDeps},
		{name: "assemble lock", when: p.vendoring, run: p.assembleLock},
		{name: "composed out", when: p.composedOutEnabled, run: p.writeComposedOut},
		{name: "dry run", when: p.isDryRun, run: p.stopDryRun},
		{name: "write vendored files", when: hasDeps, run: p.writeVendoredFiles},
		{name: "write lock", when: hasLock, run: p.writeLock},
		{name: "render", when: always, run: p.render},
		{name: "read files", when: hasLocalFiles, run: p.readFiles},
		{name: "build cache lookup", when: p.caching, run: p.lookupBuildCache},
		{name: "archive", when: hasLocalFiles, run: p.archiveBundle},
		{name: "stage", when: always, run: p.stageInMemory},
		{name: "verify", when: p.verifying, run: p.verifyStaged},
		{name: "publish", when: always, run: p.publish},
		{name: "build cache write", when: p.cacheWritable, run: p.writeBuildCache},
	}
}

// cacheDir returns the build cache directory.
func (p *Packager) cacheDir() string {
	if p.cache.Dir != "" {
		return p.cache.Dir
	}
	return settings.DefaultBuildCacheDir()
}

// Stage predicates.

func always(*pipelineState) bool { return true }

func bundling(st *pipelineState) bool { return st.bundle }

func hasDeps(st *pipelineState) bool { return st.deps != nil }

func hasLock(st *pipelineState) bool { return st.lock != nil }

func hasLocalFiles(st *pipelineState) bool {
	return st.discovery != nil && len(st.discovery.LocalFiles) > 0
}

func (p *Packager) preflightEnabled(*pipelineState) bool { return p.preflight != nil && !p.dryRun }

func (p *Packager) composed(st *pipelineState) bool {
	return st.bundle && len(p.src.ComposeFiles()) > 0
}

func (p *Packager) vendoring(st *pipelineState) bool { return st.bundle && !p.vendor.Skip }

func (p *Packager) pluginsToVendor(st *pipelineState) bool {
	return p.vendoring(st) && len(p.src.Solution.Bundle.Plugins) > 0
}

func (p *Packager) depsToVendor(st *pipelineState) bool {
	return p.vendoring(st) && len(st.discovery.CatalogRefs) > 0
}

func (p *Packager) isDryRun(*pipelineState) bool { return p.dryRun }

func (p *Packager) composedOutEnabled(*pipelineState) bool { return p.composedOut != "" }

func (p *Packager) caching(st *pipelineState) bool { return st.bundle && !p.cache.Skip }

func (p *Packager) verifying(*pipelineState) bool { return !p.verify.Skip }

// cacheWritable reports whether a build-cache entry may be written: only for
// a verified artifact with a fingerprint, so a later run that hits the cache
// can rely on the artifact having been verified.
func (p *Packager) cacheWritable(st *pipelineState) bool {
	return p.caching(st) && p.verifying(st) && st.result.Build.BuildFingerprint != ""
}
