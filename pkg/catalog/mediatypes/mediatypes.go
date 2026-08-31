package mediatypes

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package mediatypes holds the canonical OCI media-type strings for scafctl
// solution artifacts that are shared across packages which must not depend on
// one another (catalog, cache, solution/get).
//
// It deliberately imports nothing, so it can sit at the bottom of the
// dependency graph and be referenced from anywhere without an import cycle.
// This makes it the single source of truth for these wire strings: catalog
// re-exports them as catalog.MediaTypeSolution* and other packages reference
// them here, so the literal exists exactly once and cannot drift.

const (
	// SolutionBundle is the content-layer media type for solution bundle tar
	// archives.
	SolutionBundle = "application/vnd.scafctl.solution.bundle.v1+tar"

	// SolutionLock is the layer media type for a solution lock file
	// (JSON-encoded LockFile) stored as a dedicated layer on the solution
	// manifest.
	SolutionLock = "application/vnd.scafctl.solution.lock.v1+json"
)
