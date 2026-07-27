// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// PackageReport is the machine-readable result of a `package/build solution`
// invocation. It is emitted on stdout via -o json|yaml and captures the
// build outcome without requiring the caller to inspect the catalog.
//
// The report is a stable contract for CI pipelines and tooling: it records the
// resolved artifact identity, storage digest, bundle manifest, completeness
// verification summary, and (optionally) the fully composed solution document.
type PackageReport struct {
	// Name is the resolved artifact name.
	Name string `json:"name" yaml:"name"`

	// Version is the resolved semantic version.
	Version string `json:"version" yaml:"version"`

	// Reference is the canonical artifact reference (e.g., "my-solution@1.2.3").
	Reference string `json:"reference" yaml:"reference"`

	// Digest is the content digest of the stored artifact (sha256:...).
	// Omitted from the report for dry-run builds, where nothing is stored.
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty"`

	// Catalog is the path or name of the catalog the artifact was stored in.
	// Omitted from the report for dry-run builds.
	Catalog string `json:"catalog,omitempty" yaml:"catalog,omitempty"`

	// Size is the stored artifact size in bytes.
	// Omitted from the report for dry-run builds.
	Size int64 `json:"size,omitempty" yaml:"size,omitempty"`

	// DryRun is true when the report describes what would be built without
	// storing anything.
	DryRun bool `json:"dryRun" yaml:"dryRun"`

	// CacheHit is true when the artifact was unchanged since the last build
	// and served from the build cache.
	CacheHit bool `json:"cacheHit" yaml:"cacheHit"`

	// Fingerprint is the build fingerprint used for cache lookups.
	Fingerprint string `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty"`

	// InputFileCount is the number of input files that contributed to the
	// build fingerprint.
	InputFileCount int `json:"inputFileCount,omitempty" yaml:"inputFileCount,omitempty"`

	// Bundle summarizes the files and dependencies packaged with the solution.
	// Nil when packaging with --no-bundle (only the solution YAML is stored).
	Bundle *BundleReport `json:"bundle,omitempty" yaml:"bundle,omitempty"`

	// Verification summarizes the built-bundle completeness check.
	// Nil when verification was skipped (--no-verify) or not applicable (dry-run).
	Verification *VerificationReport `json:"verification,omitempty" yaml:"verification,omitempty"`

	// Solution is the fully composed (flattened) solution document. It is
	// populated only when the caller requests it, so the report stays compact
	// by default.
	Solution *solution.Solution `json:"solution,omitempty" yaml:"solution,omitempty"`
}

// BundleReport summarizes the contents of a solution bundle.
type BundleReport struct {
	// FileCount is the total number of local files in the bundle.
	FileCount int `json:"fileCount" yaml:"fileCount"`

	// Files lists the local files packaged in the bundle.
	Files []BundleFile `json:"files,omitempty" yaml:"files,omitempty"`

	// VendoredRefs lists the catalog dependencies vendored into the bundle.
	VendoredRefs []VendoredRef `json:"vendoredRefs,omitempty" yaml:"vendoredRefs,omitempty"`

	// Plugins lists the external plugin dependencies recorded for the bundle.
	Plugins []BundlePluginRef `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

// BundleFile describes a single local file packaged in a bundle.
type BundleFile struct {
	// Path is the file path relative to the bundle root.
	Path string `json:"path" yaml:"path"`

	// Source indicates how the file was discovered (static-analysis,
	// explicit-include, or test-include).
	Source string `json:"source" yaml:"source"`
}

// VendoredRef describes a catalog dependency vendored into a bundle.
type VendoredRef struct {
	// Ref is the original catalog reference (e.g., "deploy-to-k8s@2.0.0").
	Ref string `json:"ref" yaml:"ref"`

	// VendorPath is the path within the bundle where the artifact is stored.
	VendorPath string `json:"vendorPath" yaml:"vendorPath"`
}

// BundlePluginRef describes an external plugin dependency of a bundle.
type BundlePluginRef struct {
	// Name is the plugin's catalog reference.
	Name string `json:"name" yaml:"name"`

	// Kind is the plugin type (provider or auth-handler).
	Kind string `json:"kind" yaml:"kind"`

	// Version is the semver constraint or "latest".
	Version string `json:"version" yaml:"version"`
}

// VerificationReport summarizes a built-bundle completeness verification.
type VerificationReport struct {
	// Passed is true when the bundle has no completeness errors.
	Passed bool `json:"passed" yaml:"passed"`

	// Errors are hard completeness failures (missing files or dependencies).
	Errors []VerificationError `json:"errors,omitempty" yaml:"errors,omitempty"`

	// Warnings are non-fatal completeness issues.
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// VerificationError describes a single completeness failure.
type VerificationError struct {
	// Path is the file or dependency path that failed verification.
	Path string `json:"path" yaml:"path"`

	// Reason describes why the verification failed.
	Reason string `json:"reason" yaml:"reason"`
}

// PackageReportInput carries the data needed to assemble a PackageReport.
type PackageReportInput struct {
	// Name is the resolved artifact name.
	Name string

	// Version is the resolved semantic version.
	Version string

	// Solution is the composed (flattened) solution. Required.
	Solution *solution.Solution

	// Build is the bundle build result. Nil when packaging with --no-bundle.
	Build *BuildResult

	// Info is the stored artifact info. Nil for dry-run reports.
	Info *catalog.ArtifactInfo

	// Verify is the built-bundle verification result. Nil when verification
	// was skipped or is not applicable.
	Verify *bundler.VerifyResult

	// CatalogPath is the path or name of the catalog the artifact was stored in.
	CatalogPath string

	// DryRun marks the report as a preview (nothing stored).
	DryRun bool

	// IncludeSolution embeds the composed solution document in the report.
	IncludeSolution bool
}

// NewPackageReport assembles a PackageReport from the outcome of a package
// build. It is pure: it reads the provided inputs and never touches the
// filesystem or catalog.
func NewPackageReport(in PackageReportInput) *PackageReport {
	report := &PackageReport{
		Name:     in.Name,
		Version:  in.Version,
		DryRun:   in.DryRun,
		CacheHit: in.Build != nil && in.Build.CacheHit,
	}

	report.Reference = catalog.Reference{
		Kind: catalog.ArtifactKindSolution,
		Name: in.Name,
	}.String()
	if in.Version != "" {
		report.Reference = in.Name + "@" + in.Version
	}

	if in.Info != nil {
		report.Digest = in.Info.Digest
		report.Size = in.Info.Size
	}
	report.Catalog = in.CatalogPath

	if in.Build != nil {
		report.Fingerprint = in.Build.BuildFingerprint
		report.InputFileCount = in.Build.InputFileCount
		if in.Build.CacheHit && in.Build.CacheEntry != nil {
			if report.Fingerprint == "" {
				report.Fingerprint = in.Build.CacheEntry.Fingerprint
			}
			if report.InputFileCount == 0 {
				report.InputFileCount = in.Build.CacheEntry.InputFiles
			}
			if report.Digest == "" {
				report.Digest = in.Build.CacheEntry.ArtifactDigest
			}
		}
		report.Bundle = newBundleReport(in.Build.Discovery, in.Solution)
	}

	if in.Verify != nil && !in.DryRun {
		report.Verification = newVerificationReport(in.Verify)
	}

	if in.IncludeSolution {
		report.Solution = in.Solution
	}

	return report
}

// newBundleReport builds the bundle manifest section from discovery results and
// the composed solution's plugin dependencies.
func newBundleReport(discovery *bundler.DiscoveryResult, sol *solution.Solution) *BundleReport {
	br := &BundleReport{}

	if discovery != nil {
		br.FileCount = len(discovery.LocalFiles)
		for _, f := range discovery.LocalFiles {
			br.Files = append(br.Files, BundleFile{
				Path:   f.RelPath,
				Source: f.Source.String(),
			})
		}
		for _, ref := range discovery.CatalogRefs {
			br.VendoredRefs = append(br.VendoredRefs, VendoredRef{
				Ref:        ref.Ref,
				VendorPath: ref.VendorPath,
			})
		}
	}

	if sol != nil {
		for _, p := range sol.Bundle.Plugins {
			br.Plugins = append(br.Plugins, BundlePluginRef{
				Name:    p.Name,
				Kind:    string(p.Kind),
				Version: p.Version,
			})
		}
	}

	return br
}

// newVerificationReport translates a bundler.VerifyResult into the report shape.
func newVerificationReport(vr *bundler.VerifyResult) *VerificationReport {
	report := &VerificationReport{
		Passed:   vr.Passed(),
		Warnings: vr.Warnings,
	}
	for _, e := range vr.Errors {
		report.Errors = append(report.Errors, VerificationError{
			Path:   e.Path,
			Reason: e.Reason,
		})
	}
	return report
}
