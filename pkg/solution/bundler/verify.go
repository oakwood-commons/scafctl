// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// VerifyError describes a single verification failure.
type VerifyError struct {
	// Path is the file or dependency path that failed verification.
	Path string `json:"path" yaml:"path" doc:"File or dependency path" maxLength:"500"`
	// Reason describes why the verification failed.
	Reason string `json:"reason" yaml:"reason" doc:"Failure reason" maxLength:"1000"`
}

// VerifyResult holds the outcome of bundle verification.
type VerifyResult struct {
	// Errors are hard failures — missing files or dependencies.
	Errors []VerifyError `json:"errors,omitempty" yaml:"errors,omitempty" doc:"Hard verification failures" maxItems:"10000"`
	// Warnings are non-fatal issues (e.g., empty glob patterns, missing bundle).
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal verification issues" maxItems:"10000"`
	// Successes are items that passed verification.
	Successes []string `json:"successes,omitempty" yaml:"successes,omitempty" doc:"Items that passed verification" maxItems:"10000"`
}

// Passed returns true when there are no errors.
func (r *VerifyResult) Passed() bool {
	return len(r.Errors) == 0
}

// VerifyBundle performs completeness verification of a solution bundle.
//
// It checks:
//   - Static file paths referenced by providers exist in the bundle
//   - Glob patterns in bundle.include match at least one bundled file
//   - Vendored catalog dependencies are present
//   - Plugin entries are recorded
//
// When bundleData is empty the function checks whether a bundle would be needed
// and returns warnings if local files or catalog dependencies are detected.
func VerifyBundle(ctx context.Context, sol *solution.Solution, bundleData []byte, lgr logr.Logger) (*VerifyResult, error) {
	result := &VerifyResult{}

	if len(bundleData) == 0 {
		return verifyNoBundleCase(sol, result, lgr)
	}

	return verifyWithBundle(ctx, sol, bundleData, result, lgr)
}

// verifyNoBundleCase handles verification when no bundle data is present.
func verifyNoBundleCase(sol *solution.Solution, result *VerifyResult, lgr logr.Logger) (*VerifyResult, error) {
	discovery, err := DiscoverFiles(sol, ".")
	if err != nil {
		lgr.V(1).Info("discovery failed during no-bundle verify", "error", err)
		return result, nil
	}
	if len(discovery.LocalFiles) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("solution references %d local files but has no bundle", len(discovery.LocalFiles)))
	}
	if len(discovery.CatalogRefs) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("solution references %d catalog dependencies but has no vendored copies", len(discovery.CatalogRefs)))
	}
	return result, nil
}

// verifyWithBundle extracts bundleData into a temp directory and runs all verification checks.
func verifyWithBundle(_ context.Context, sol *solution.Solution, bundleData []byte, result *VerifyResult, lgr logr.Logger) (*VerifyResult, error) {
	tmpDir, err := os.MkdirTemp("", "scafctl-verify-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, err := ExtractBundleTar(bundleData, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("extracting bundle: %w", err)
	}

	// Build a set of bundled file paths for quick lookup.
	bundledFiles := make(map[string]bool, len(manifest.Files))
	for _, f := range manifest.Files {
		bundledFiles[f.Path] = true
	}

	// Run discovery against the extracted tree.
	discovery, discErr := DiscoverFiles(sol, tmpDir)
	if discErr != nil {
		lgr.V(1).Info("discovery failed during verify", "error", discErr)
	} else {
		verifyStaticPaths(discovery, tmpDir, result)
		verifyGlobCoverage(sol, manifest, result)
		verifyVendoredDeps(discovery, bundledFiles, result)
	}

	verifyPlugins(manifest, result)

	return result, nil
}

// verifyStaticPaths checks that statically-referenced files exist on disk.
func verifyStaticPaths(discovery *DiscoveryResult, tmpDir string, result *VerifyResult) {
	for _, f := range discovery.LocalFiles {
		if f.Source != StaticAnalysis {
			continue
		}
		filePath := filepath.Join(tmpDir, f.RelPath)
		if _, statErr := os.Stat(filePath); statErr == nil {
			result.Successes = append(result.Successes, f.RelPath)
		} else {
			result.Errors = append(result.Errors, VerifyError{
				Path:   f.RelPath,
				Reason: "not found in bundle",
			})
		}
	}
}

// verifyGlobCoverage checks that each bundle.include pattern matches at least one bundled file.
func verifyGlobCoverage(sol *solution.Solution, manifest *BundleManifest, result *VerifyResult) {
	for _, pattern := range sol.Bundle.Include {
		matched := false
		for _, f := range manifest.Files {
			if MatchGlob(pattern, f.Path) {
				matched = true
				break
			}
		}
		if matched {
			result.Successes = append(result.Successes, fmt.Sprintf("glob:%s", pattern))
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pattern %q matches no bundled files", pattern))
		}
	}
}

// verifyVendoredDeps checks that all discovered catalog dependencies are present in the bundle.
func verifyVendoredDeps(discovery *DiscoveryResult, bundledFiles map[string]bool, result *VerifyResult) {
	for _, cr := range discovery.CatalogRefs {
		if bundledFiles[cr.VendorPath] {
			result.Successes = append(result.Successes, cr.VendorPath)
		} else {
			result.Errors = append(result.Errors, VerifyError{
				Path:   cr.VendorPath,
				Reason: "not found in bundle",
			})
		}
	}
}

// verifyPlugins records plugin entries from the manifest as successes.
func verifyPlugins(manifest *BundleManifest, result *VerifyResult) {
	for _, p := range manifest.Plugins {
		result.Successes = append(result.Successes, fmt.Sprintf("plugin:%s (%s) %s", p.Name, p.Kind, p.Version))
	}
}
