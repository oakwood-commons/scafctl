// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// AmbiguousKindError reports that a kindless artifact lookup matched local
// catalog entries under more than one artifact kind.
type AmbiguousKindError struct {
	Reference Reference
	Kinds     []ArtifactKind
}

// Error implements the error interface.
func (e *AmbiguousKindError) Error() string {
	names := make([]string, len(e.Kinds))
	for i, k := range e.Kinds {
		names[i] = k.String()
	}
	return fmt.Sprintf("artifact %q matches multiple kinds (%s)", e.Reference.String(), strings.Join(names, ", "))
}

// Unwrap returns ErrAmbiguousArtifact for errors.Is support.
func (e *AmbiguousKindError) Unwrap() error {
	return ErrAmbiguousArtifact
}

// ResolveAnyKind resolves ref in the local catalog across solution, provider,
// and auth-handler kinds when ref.Kind is empty.
//
// When ref.Kind is already set, ResolveAnyKind behaves identically to Resolve.
// When ref.Kind is empty:
//   - 0 matching kinds returns *ArtifactNotFoundError;
//   - 1 matching kind returns its ArtifactInfo, or its single-kind
//     *AmbiguousArtifactError when multiple origins hold that artifact;
//   - 2+ matching kinds returns *AmbiguousKindError listing the matched kinds.
func (c *LocalCatalog) ResolveAnyKind(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ref.Kind != "" {
		return c.resolveLocked(ctx, ref)
	}

	var (
		matchedInfos []ArtifactInfo
		matchedKinds []ArtifactKind
		singleAmbErr *AmbiguousArtifactError
	)

	for _, kind := range allArtifactKinds {
		candidate := ref
		candidate.Kind = kind

		info, err := c.resolveLocked(ctx, candidate)
		if err == nil {
			matchedInfos = append(matchedInfos, info)
			matchedKinds = append(matchedKinds, kind)
			continue
		}
		if IsNotFound(err) {
			continue
		}
		var ambErr *AmbiguousArtifactError
		if errors.As(err, &ambErr) {
			matchedKinds = append(matchedKinds, kind)
			singleAmbErr = ambErr
			continue
		}
		return ArtifactInfo{}, err
	}

	switch len(matchedKinds) {
	case 0:
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	case 1:
		if singleAmbErr != nil {
			return ArtifactInfo{}, singleAmbErr
		}
		return matchedInfos[0], nil
	default:
		return ArtifactInfo{}, &AmbiguousKindError{
			Reference: ref,
			Kinds:     matchedKinds,
		}
	}
}
