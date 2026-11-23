// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
)

// DigestResolver resolves the digest currently published for a reference.
// Catalog implementations (e.g. RemoteCatalog) satisfy it.
type DigestResolver interface {
	Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error)
}

// VerifyCachedDigests checks rows that DeduplicateArtifacts merged from a cached
// local copy and a remote listing (Locations = local + remote). Remote list rows
// carry no digest, so the merge is unverified until the remote tag is resolved.
//
// resolvers maps a catalog's canonical identity (matched case-insensitively
// against ArtifactInfo.OriginKey) to the catalog that can resolve its tags.
// For each merged row with a known digest and version whose origin has a
// resolver, the published digest is resolved (at most concurrency requests in
// flight). When it differs from the cached digest the row is split into a
// stale local row and a remote row; when it matches the row is kept as is.
//
// Verification is best-effort: a row whose resolve fails stays merged and
// unverified, and the failures are returned joined in a non-nil error alongside
// the (still complete) result. Inputs are not mutated.
func VerifyCachedDigests(ctx context.Context, artifacts []ArtifactInfo, resolvers map[string]DigestResolver, concurrency int) ([]ArtifactInfo, error) {
	if len(resolvers) == 0 || len(artifacts) == 0 {
		return artifacts, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}

	byOrigin := make(map[string]DigestResolver, len(resolvers))
	for canonical, r := range resolvers {
		byOrigin[strings.ToLower(canonical)] = r
	}

	type outcome struct {
		digest string
		size   int64
		err    error
	}
	outcomes := make([]*outcome, len(artifacts))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, a := range artifacts {
		resolver, ok := byOrigin[a.OriginKey()]
		if !ok || !needsDigestVerification(a) {
			continue
		}
		ref := Reference{Kind: a.Reference.Kind, Name: a.Reference.Name, Version: a.Reference.Version}
		wg.Add(1)
		go func(i int, ref Reference) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				outcomes[i] = &outcome{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				outcomes[i] = &outcome{err: err}
				return
			}
			info, err := resolver.Resolve(ctx, ref)
			outcomes[i] = &outcome{digest: info.Digest, size: info.Size, err: err}
		}(i, ref)
	}
	wg.Wait()

	result := make([]ArtifactInfo, 0, len(artifacts))
	var errs []error
	for i, a := range artifacts {
		o := outcomes[i]
		switch {
		case o == nil:
			result = append(result, a)
		case o.err != nil:
			errs = append(errs, fmt.Errorf("verify %s@%s: %w", a.Reference.Name, a.Reference.Version, o.err))
			result = append(result, a)
		case o.digest == "" || o.digest == a.Digest:
			result = append(result, a)
		default:
			local, remote := splitStale(a, o.digest, o.size)
			result = append(result, local, remote)
		}
	}
	return result, errors.Join(errs...)
}

// needsDigestVerification reports whether a row is an unverified merge of a
// cached local copy and a remote listing that can be resolved by version.
func needsDigestVerification(a ArtifactInfo) bool {
	return a.Digest != "" &&
		a.Reference.Version != nil &&
		a.HasLocation(LocationLocal) &&
		a.HasLocation(LocationRemote)
}

// splitStale splits a merged row whose cached digest differs from the
// published one into a stale local row and a remote row.
func splitStale(merged ArtifactInfo, remoteDigest string, remoteSize int64) (ArtifactInfo, ArtifactInfo) {
	local := merged
	local.Locations = []ArtifactLocation{LocationLocal}
	local.Catalog = LocalCatalogName
	local.Stale = true
	local.Annotations = maps.Clone(merged.Annotations)

	remote := merged
	remote.Locations = []ArtifactLocation{LocationRemote}
	remote.Stale = false
	remote.Digest = remoteDigest
	remote.Size = remoteSize
	remote.CreatedAt = time.Time{}
	remote.ImageRef = ""
	remote.Annotations = maps.Clone(merged.Annotations)
	return local, remote
}
