// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

func BenchmarkMatchUpdateFilters(b *testing.B) {
	lock := &LockFile{
		Dependencies: []LockDependency{
			{Ref: "deploy-to-k8s@^1.5.0"},
			{Ref: "setup-env@2.0.0"},
		},
	}
	filters := []string{"deploy-to-k8s"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = matchUpdateFilters(nil, nil, lock, filters)
	}
}

func BenchmarkUpdateVendoredDependencies(b *testing.B) {
	content := validSolYAML("deploy-to-k8s", "1.5.2")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	fetcher := &mockCatalogFetcher{
		listings: map[string][]catalog.ArtifactInfo{
			"deploy-to-k8s": {
				{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: semver.MustParse("1.5.2")}, Catalog: "official"},
			},
		},
		solutions: map[string]mockFetchResult{
			"deploy-to-k8s@1.5.2": {
				content: content,
				info:    catalog.ArtifactInfo{Reference: catalog.Reference{Name: "deploy-to-k8s", Version: semver.MustParse("1.5.2")}, Catalog: "official"},
			},
		},
	}

	src := newFakeRemoteSource("myorg", "ghcr.io/myorg", "exec", "1.3.0")
	pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, src)
	if err != nil {
		b.Fatal(err)
	}
	aliasResolver := newTestAliasResolver("myorg", "oci://ghcr.io/myorg")

	sol := solutionWithSubRefs("deploy-to-k8s@^1.5.0")
	sol.Bundle.Plugins = []solution.PluginDependency{shortExecDep()}
	existingLock := &LockFile{
		Version: LockFileVersion,
		Dependencies: []LockDependency{
			{Ref: "deploy-to-k8s@^1.5.0", ResolvedVersion: "1.5.0", Constraint: "^1.5.0", Digest: digest, ResolvedFrom: "official"},
		},
		Plugins: []LockPlugin{
			{Name: "exec", Kind: "provider", Version: "1.2.3", Constraint: "^1.0.0", Digest: "sha256:aaa", ResolvedFrom: "myorg", ResolvedCanonical: "ghcr.io/myorg"},
		},
	}
	opts := VendorUpdateRunOptions{
		BundleRoot:      b.TempDir(),
		DryRun:          true,
		SolutionFetcher: fetcher,
		PluginOptions: &VendorAllPluginsOptions{
			CatalogResolver: aliasResolver,
			PluginFetcher:   pf,
			Platform:        "linux/amd64",
		},
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = UpdateVendoredDependencies(ctx, sol, existingLock, opts)
	}
}
