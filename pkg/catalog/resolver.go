// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
)

// SolutionResolver wraps a Catalog to provide solution fetching by name[@version].
// It implements the CatalogResolver interface from pkg/solution/get.
type SolutionResolver struct {
	catalog Catalog
	logger  logr.Logger
}

// NewSolutionResolver creates a resolver that fetches solutions from the given catalog.
func NewSolutionResolver(catalog Catalog, logger logr.Logger) *SolutionResolver {
	return &SolutionResolver{
		catalog: catalog,
		logger:  logger.WithName("solution-resolver"),
	}
}

// FetchSolution retrieves a solution from the catalog by name[@version].
// The input format is "name" or "name@version" (e.g., "my-solution" or "my-solution@1.2.3").
// Returns the solution content as bytes.
func (r *SolutionResolver) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, error) {
	// Parse the name[@version] format
	name, version := parseNameVersion(nameWithVersion)

	// Build the reference string for parsing
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(ArtifactKindSolution, refStr)
	if err != nil {
		return nil, fmt.Errorf("invalid solution reference %q: %w", nameWithVersion, err)
	}

	r.logger.V(1).Info("fetching solution from catalog",
		"name", name,
		"version", version,
		"catalog", r.catalog.Name())

	content, info, err := r.catalog.Fetch(ctx, ref)
	if err != nil {
		return nil, err
	}

	r.logger.V(1).Info("fetched solution from catalog",
		"name", info.Reference.Name,
		"version", info.Reference.Version,
		"digest", info.Digest,
		"catalog", r.catalog.Name())

	return content, nil
}

// parseNameVersion splits "name@version" into (name, version).
// If no @ is present, returns (input, "").
func parseNameVersion(input string) (string, string) {
	// Handle digest references (sha256:...)
	if strings.Contains(input, "@sha256:") {
		parts := strings.SplitN(input, "@sha256:", 2)
		return parts[0], "sha256:" + parts[1]
	}

	// Handle version references
	if idx := strings.LastIndex(input, "@"); idx != -1 {
		return input[:idx], input[idx+1:]
	}

	return input, ""
}
