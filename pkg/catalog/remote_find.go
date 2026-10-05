// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/Masterminds/semver/v3"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// Find resolves the single artifact in this remote catalog that query
// selects, mirroring LocalCatalog.Find. Name and Kind are required.
//
// A registry exposes no scafctl annotations on its tag list, so the lookup
// is tag-driven: the repository's semver tags are listed, narrowed by the
// query's version selector ("" / "latest", an exact version, or a semver
// constraint), and the highest survivor is resolved to its manifest. An open
// version prefers stable releases unless the context opts into pre-releases
// (see WithIncludePreRelease), matching LocalCatalog.Find and Resolve.
//
// A Digest narrows further: candidates are resolved newest first and the
// first whose manifest digest matches wins.
//
// Every artifact here originates from this catalog, so a SourceBuilt
// selector, or a SourceRemote selector naming another catalog, never matches.
// A miss -- including an unknown repository -- is an ArtifactNotFoundError;
// network and registry failures are returned as-is so callers can tell
// "absent" from "could not ask" (and only fall through on the former).
func (c *RemoteCatalog) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	if err := query.Validate(); err != nil {
		return ArtifactInfo{}, err
	}

	ref := Reference{Kind: query.Kind, Name: query.Name}
	notFound := &ArtifactNotFoundError{Reference: ref, Catalog: c.name}

	if !c.servesSource(query.Source) {
		return ArtifactInfo{}, notFound
	}

	versions, err := c.listVersions(ctx, ref)
	if err != nil {
		if isRegistryNotFound(err) {
			return ArtifactInfo{}, notFound
		}
		return ArtifactInfo{}, err
	}

	for _, v := range versionCandidates(ctx, query, versions) {
		candidate := ref
		candidate.Version = v
		info, err := c.resolveWithKind(ctx, candidate)
		if err != nil {
			return ArtifactInfo{}, err
		}
		if query.Digest == "" || info.Digest == query.Digest {
			return info, nil
		}
	}

	return ArtifactInfo{}, notFound
}

// servesSource reports whether artifacts from this catalog can satisfy s.
func (c *RemoteCatalog) servesSource(s SourceSelector) bool {
	switch s.Kind {
	case SourceAny:
		return true
	case SourceRemote:
		return s.Remote == c.canonicalID()
	case SourceBuilt:
		return false
	}
	return false
}

// versionCandidates returns the versions query's version selector accepts,
// newest first. An open selector applies the same stable-first preference as
// latestCandidates.
func versionCandidates(ctx context.Context, query ArtifactQuery, versions []*semver.Version) []*semver.Version {
	var matcher VersionOrConstraint
	matcher.Set(query.Version)

	matched := make([]*semver.Version, 0, len(versions))
	for _, v := range versions {
		if matcher.satisfiesVersion(v) {
			matched = append(matched, v)
		}
	}

	if wantsLatest(query) {
		matched = latestVersionCandidates(ctx, matched)
	}

	sort.Sort(sort.Reverse(semver.Collection(matched)))
	return matched
}

// latestVersionCandidates is latestCandidates for bare versions: it drops
// pre-releases unless the context opts in, falling back to every version when
// no stable release exists.
func latestVersionCandidates(ctx context.Context, versions []*semver.Version) []*semver.Version {
	if IncludePreReleaseFromContext(ctx) {
		return versions
	}

	stable := make([]*semver.Version, 0, len(versions))
	for _, v := range versions {
		if !IsPreRelease(v) {
			stable = append(stable, v)
		}
	}
	if len(stable) == 0 {
		return versions
	}
	return stable
}

// isRegistryNotFound reports whether err is a registry 404, e.g. NAME_UNKNOWN
// when listing tags of a repository that does not exist.
func isRegistryNotFound(err error) bool {
	var resp *errcode.ErrorResponse
	return errors.As(err, &resp) && resp.StatusCode == http.StatusNotFound
}
