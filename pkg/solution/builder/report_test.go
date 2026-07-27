// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSolution() *solution.Solution {
	sol := &solution.Solution{}
	sol.Metadata.Name = "my-solution"
	sol.Metadata.Version = semver.MustParse("1.2.3")
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "aws-provider", Kind: solution.PluginKindProvider, Version: "^1.5.0"},
	}
	return sol
}

func TestNewPackageReport_StoredArtifact(t *testing.T) {
	sol := newTestSolution()
	build := &BuildResult{
		BuildFingerprint: "fp-123",
		InputFileCount:   4,
		Discovery: &bundler.DiscoveryResult{
			LocalFiles: []bundler.FileEntry{
				{RelPath: "templates/main.tf", Source: bundler.StaticAnalysis},
				{RelPath: "extra/notes.md", Source: bundler.ExplicitInclude},
			},
			CatalogRefs: []bundler.CatalogRefEntry{
				{Ref: "deploy-to-k8s@2.0.0", VendorPath: "vendor/deploy-to-k8s"},
			},
		},
	}
	info := &catalog.ArtifactInfo{
		Reference: catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.2.3")},
		Digest:    "sha256:abc123",
		Size:      2048,
	}
	vr := &bundler.VerifyResult{
		Warnings: []string{"dynamic path detected"},
	}

	report := NewPackageReport(PackageReportInput{
		Name:            "my-solution",
		Version:         "1.2.3",
		Solution:        sol,
		Build:           build,
		Info:            info,
		Verify:          vr,
		CatalogPath:     "/home/user/.local/catalog",
		IncludeSolution: true,
	})

	assert.Equal(t, "my-solution", report.Name)
	assert.Equal(t, "1.2.3", report.Version)
	assert.Equal(t, "my-solution@1.2.3", report.Reference)
	assert.Equal(t, "sha256:abc123", report.Digest)
	assert.Equal(t, int64(2048), report.Size)
	assert.Equal(t, "/home/user/.local/catalog", report.Catalog)
	assert.False(t, report.DryRun)
	assert.False(t, report.CacheHit)
	assert.Equal(t, "fp-123", report.Fingerprint)
	assert.Equal(t, 4, report.InputFileCount)

	require.NotNil(t, report.Bundle)
	assert.Equal(t, 2, report.Bundle.FileCount)
	require.Len(t, report.Bundle.Files, 2)
	assert.Equal(t, "templates/main.tf", report.Bundle.Files[0].Path)
	assert.Equal(t, "static-analysis", report.Bundle.Files[0].Source)
	assert.Equal(t, "explicit-include", report.Bundle.Files[1].Source)
	require.Len(t, report.Bundle.VendoredRefs, 1)
	assert.Equal(t, "deploy-to-k8s@2.0.0", report.Bundle.VendoredRefs[0].Ref)
	assert.Equal(t, "vendor/deploy-to-k8s", report.Bundle.VendoredRefs[0].VendorPath)
	require.Len(t, report.Bundle.Plugins, 1)
	assert.Equal(t, "aws-provider", report.Bundle.Plugins[0].Name)
	assert.Equal(t, "provider", report.Bundle.Plugins[0].Kind)
	assert.Equal(t, "^1.5.0", report.Bundle.Plugins[0].Version)

	require.NotNil(t, report.Verification)
	assert.True(t, report.Verification.Passed)
	assert.Empty(t, report.Verification.Errors)
	assert.Equal(t, []string{"dynamic path detected"}, report.Verification.Warnings)

	require.NotNil(t, report.Solution)
	assert.Equal(t, "my-solution", report.Solution.Metadata.Name)
}

func TestNewPackageReport_DryRunOmitsStoreFields(t *testing.T) {
	sol := newTestSolution()
	build := &BuildResult{
		Discovery: &bundler.DiscoveryResult{},
	}

	report := NewPackageReport(PackageReportInput{
		Name:     "my-solution",
		Version:  "1.2.3",
		Solution: sol,
		Build:    build,
		DryRun:   true,
	})

	assert.True(t, report.DryRun)
	assert.Empty(t, report.Digest)
	assert.Empty(t, report.Catalog)
	assert.Zero(t, report.Size)
	assert.Nil(t, report.Verification)
	assert.Nil(t, report.Solution, "solution not embedded unless requested")
}

func TestNewPackageReport_DryRunOmitsVerification(t *testing.T) {
	sol := newTestSolution()
	build := &BuildResult{
		Discovery: &bundler.DiscoveryResult{},
	}
	vr := &bundler.VerifyResult{Warnings: []string{"dynamic path detected"}}

	report := NewPackageReport(PackageReportInput{
		Name:     "my-solution",
		Version:  "1.2.3",
		Solution: sol,
		Build:    build,
		Verify:   vr,
		DryRun:   true,
	})

	assert.True(t, report.DryRun)
	assert.Nil(t, report.Verification, "verification is not applicable for dry-run builds")
}

func TestNewPackageReport_CacheHitUsesCacheEntry(t *testing.T) {
	sol := newTestSolution()
	build := &BuildResult{
		CacheHit: true,
		CacheEntry: &bundler.BuildCacheEntry{
			Fingerprint:     "fp-cache",
			ArtifactDigest:  "sha256:cached",
			InputFiles:      7,
			ArtifactName:    "my-solution",
			ArtifactVersion: "1.2.3",
		},
	}

	report := NewPackageReport(PackageReportInput{
		Name:            "my-solution",
		Version:         "1.2.3",
		Solution:        sol,
		Build:           build,
		CatalogPath:     "/cat",
		IncludeSolution: true,
	})

	assert.True(t, report.CacheHit)
	assert.Equal(t, "fp-cache", report.Fingerprint)
	assert.Equal(t, "sha256:cached", report.Digest)
	assert.Equal(t, 7, report.InputFileCount)
	assert.Equal(t, "/cat", report.Catalog)
}

func TestNewPackageReport_NoBundle(t *testing.T) {
	sol := newTestSolution()
	info := &catalog.ArtifactInfo{
		Reference: catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.2.3")},
		Digest:    "sha256:nobundle",
	}

	report := NewPackageReport(PackageReportInput{
		Name:     "my-solution",
		Version:  "1.2.3",
		Solution: sol,
		Info:     info,
	})

	assert.Nil(t, report.Bundle, "no bundle section when build result is nil")
	assert.Equal(t, "sha256:nobundle", report.Digest)
}

func TestNewPackageReport_VerificationErrors(t *testing.T) {
	sol := newTestSolution()
	vr := &bundler.VerifyResult{
		Errors: []bundler.VerifyError{
			{Path: "templates/missing.tf", Reason: "file not found in bundle"},
		},
	}

	report := NewPackageReport(PackageReportInput{
		Name:     "my-solution",
		Version:  "1.2.3",
		Solution: sol,
		Build:    &BuildResult{},
		Verify:   vr,
	})

	require.NotNil(t, report.Verification)
	assert.False(t, report.Verification.Passed)
	require.Len(t, report.Verification.Errors, 1)
	assert.Equal(t, "templates/missing.tf", report.Verification.Errors[0].Path)
	assert.Equal(t, "file not found in bundle", report.Verification.Errors[0].Reason)
}
