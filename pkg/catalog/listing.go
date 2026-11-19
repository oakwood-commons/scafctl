// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "context"

// ListingOptions configures BuildListing.
type ListingOptions struct {
	// AllVersions keeps every version instead of only the latest per
	// kind + name + origin.
	AllVersions bool
	// Resolvers maps a remote catalog's canonical identity to a resolver used
	// to verify cached copies against the published digest. Nil or empty skips
	// verification.
	Resolvers map[string]DigestResolver
	// Concurrency bounds in-flight digest resolutions (values < 1 mean 1).
	Concurrency int
	// Include, when set, keeps only rows for which it returns true. It runs
	// after verification so stale/remote splits are filtered individually.
	Include func(ArtifactInfo) bool
}

// BuildListing turns raw rows from the local store and remote catalogs into
// display rows: rows are merged across catalogs by origin
// (DeduplicateArtifacts), reduced to the latest version per origin unless
// AllVersions is set (LatestPerOrigin), cached copies are verified against
// their remote (VerifyCachedDigests), the Include filter is applied, and the
// result is sorted (SortArtifacts).
//
// A non-nil error reports best-effort verification failures only; the
// returned rows are complete and usable. The input slice is not mutated.
func BuildListing(ctx context.Context, artifacts []ArtifactInfo, opts ListingOptions) ([]ArtifactInfo, error) {
	rows := DeduplicateArtifacts(artifacts)
	if !opts.AllVersions {
		rows = LatestPerOrigin(rows)
	}

	rows, verifyErr := VerifyCachedDigests(ctx, rows, opts.Resolvers, opts.Concurrency)

	if opts.Include != nil {
		kept := make([]ArtifactInfo, 0, len(rows))
		for _, a := range rows {
			if opts.Include(a) {
				kept = append(kept, a)
			}
		}
		rows = kept
	}

	SortArtifacts(rows)
	return rows, verifyErr
}
