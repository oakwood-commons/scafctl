// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// firstAutoVersion is the version auto-increment assigns when no built
// version of the solution exists yet.
const firstAutoVersion = "0.1.0"

// latestAlias is the virtual version that catalog references accept but
// packaging rejects: an artifact needs a concrete version.
const latestAlias = "latest"

// ErrInvalidIdentity marks an identity error caused by user input (flags or
// solution metadata), as opposed to a catalog failure.
var ErrInvalidIdentity = errors.New("invalid artifact identity")

// ErrDevVersion is returned (wrapped in ErrInvalidIdentity) when the resolved
// version is the development placeholder solution.DefaultVersion and
// IdentityOptions.AllowDevVersion is not set.
var ErrDevVersion = errors.New("development placeholder version is not allowed")

// ArtifactFinder resolves a single catalog artifact by annotation identity.
// *catalog.LocalCatalog satisfies it.
type ArtifactFinder interface {
	Find(ctx context.Context, query catalog.ArtifactQuery) (catalog.ArtifactInfo, error)
}

// IdentityOptions are the flags that choose the artifact name and version.
type IdentityOptions struct {
	// Name is --name.
	Name string
	// Version is --version.
	Version string
	// Tag is --tag: "name[@version]" or a remote reference such as
	// "ghcr.io/org/solutions/name@1.2.3". Only the name and version are
	// used; the registry is ignored because a packaged artifact is built
	// locally. Mutually exclusive with Name and Version.
	Tag string
	// Bump is --bump: patch, minor, or major. Mutually exclusive with an
	// explicit version (Version or a versioned Tag).
	Bump string
	// AllowDevVersion is --allow-dev-version: permit packaging under the
	// placeholder solution.DefaultVersion. Only an explicit version (Version
	// or a versioned Tag) can resolve to it; a solution without a version is
	// auto-incremented instead.
	AllowDevVersion bool
}

// Validate checks flag combinations and formats without any I/O, so callers
// can reject bad input before loading the solution. Errors wrap
// ErrInvalidIdentity.
func (o IdentityOptions) Validate() error {
	if _, err := parseIdentityFlags(o); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}
	return nil
}

// VersionSource records where the resolved version came from, so the CLI can
// explain it.
type VersionSource string

const (
	// VersionFromFlag is --version.
	VersionFromFlag VersionSource = "flag"
	// VersionFromTag is the version part of --tag.
	VersionFromTag VersionSource = "tag"
	// VersionFromMetadata is metadata.version.
	VersionFromMetadata VersionSource = "metadata"
	// VersionFromBump is --bump over the highest built version.
	VersionFromBump VersionSource = "bump"
	// VersionFromAutoIncrement is the next patch over the highest built
	// version, or firstAutoVersion when none exists.
	VersionFromAutoIncrement VersionSource = "auto-increment"
)

// Identity is the catalog identity a solution is packaged under.
type Identity struct {
	// Ref is the built solution reference (Kind, Name, Version, Built).
	Ref catalog.Reference
	// VersionSource is where Ref.Version came from.
	VersionSource VersionSource
	// OverriddenVersion is the metadata.version replaced by a different
	// explicit version (--version or --tag); nil when nothing was replaced.
	OverriddenVersion *semver.Version
}

// ResolveIdentity picks the name and version src is packaged under.
//
// Name: --name or --tag > metadata.name > solution file name.
//
// Version: --bump > --version or --tag > metadata.version > auto-increment.
// Bump and auto-increment start from the latest version built locally
// (pulled copies are ignored). A metadata.version equal to the placeholder
// solution.DefaultVersion counts as unset, so a solution without a version
// is auto-incremented. finder is only queried for bump and auto-increment.
// Resolving to the placeholder itself requires opts.AllowDevVersion.
//
// Input errors wrap ErrInvalidIdentity; catalog errors do not.
func ResolveIdentity(ctx context.Context, finder ArtifactFinder, src *SolutionSource, opts IdentityOptions) (*Identity, error) {
	flags, err := parseIdentityFlags(opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}

	meta := src.Solution.Metadata
	name, err := solution.ResolveArtifactName(flags.name, meta.Name, src.Path())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}

	id := &Identity{Ref: catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: name, Built: true}}
	metaVersion := meta.Version
	if metaVersion != nil && metaVersion.Equal(solution.DefaultVersion()) {
		metaVersion = nil
	}

	switch {
	case flags.bump != "":
		latest, err := latestBuiltVersion(ctx, finder, name)
		if err != nil {
			return nil, err
		}
		if latest == nil {
			return nil, fmt.Errorf("%w: --bump: %w", ErrInvalidIdentity, &solution.ErrNoVersions{Name: name})
		}
		id.Ref.Version = nextVersion(latest, flags.bump)
		id.VersionSource = VersionFromBump
	case flags.version != nil:
		id.Ref.Version = flags.version
		id.VersionSource = flags.versionSource
		if metaVersion != nil && !metaVersion.Equal(flags.version) {
			id.OverriddenVersion = metaVersion
		}
	case metaVersion != nil:
		id.Ref.Version = metaVersion
		id.VersionSource = VersionFromMetadata
	default:
		latest, err := latestBuiltVersion(ctx, finder, name)
		if err != nil {
			return nil, err
		}
		id.Ref.Version = semver.MustParse(firstAutoVersion)
		if latest != nil {
			id.Ref.Version = nextVersion(latest, solution.BumpPatch)
		}
		id.VersionSource = VersionFromAutoIncrement
	}

	if id.Ref.Version.Equal(solution.DefaultVersion()) && !opts.AllowDevVersion {
		return nil, fmt.Errorf("%w: %w: %s", ErrInvalidIdentity, ErrDevVersion, id.Ref.Version)
	}
	return id, nil
}

// identityFlags are IdentityOptions after parsing and conflict checks.
type identityFlags struct {
	name          string
	version       *semver.Version
	versionSource VersionSource
	bump          solution.BumpLevel
}

// parseIdentityFlags validates flag combinations and parses --tag,
// --version, and --bump. It does no I/O.
func parseIdentityFlags(opts IdentityOptions) (identityFlags, error) {
	var flags identityFlags

	if opts.Bump != "" {
		level, err := solution.ParseBumpLevel(opts.Bump)
		if err != nil {
			return identityFlags{}, err
		}
		flags.bump = level
	}

	name, version, source := opts.Name, opts.Version, VersionFromFlag
	if opts.Tag != "" {
		if opts.Name != "" || opts.Version != "" {
			return identityFlags{}, errors.New("--tag cannot be used together with --name or --version")
		}
		var err error
		if name, version, err = parseTag(opts.Tag); err != nil {
			return identityFlags{}, err
		}
		source = VersionFromTag
	}
	flags.name = name

	if version != "" {
		if flags.bump != "" {
			return identityFlags{}, errors.New("--bump cannot be used together with --version or a versioned --tag")
		}
		v, err := semver.NewVersion(version)
		if err != nil {
			return identityFlags{}, fmt.Errorf("invalid version %q: %w", version, err)
		}
		flags.version = v
		flags.versionSource = source
	}
	return flags, nil
}

// parseTag splits a --tag value into a name and an optional version string.
// It accepts "name", "name@version", and remote references (registry
// ignored). It rejects other artifact kinds, digests, and "latest".
func parseTag(tag string) (name, version string, err error) {
	if catalog.LooksLikeRemoteReference(tag) {
		remote, err := catalog.ParseRemoteReference(tag)
		if err != nil {
			return "", "", fmt.Errorf("invalid --tag %q: %w", tag, err)
		}
		if remote.Kind != "" && remote.Kind != catalog.ArtifactKindSolution {
			return "", "", fmt.Errorf("--tag %q references kind %q but this command packages solutions", tag, remote.Kind)
		}
		name, version = remote.Name, remote.Tag
	} else {
		name, version, _ = strings.Cut(strings.TrimSpace(tag), "@")
		if strings.Contains(tag, "@") && version == "" {
			return "", "", fmt.Errorf("invalid --tag %q: version cannot be empty after @", tag)
		}
	}

	switch {
	case name == "":
		return "", "", fmt.Errorf("invalid --tag %q: name cannot be empty", tag)
	case strings.EqualFold(version, latestAlias):
		return "", "", fmt.Errorf("--tag %q: %q is not a valid version; specify a concrete semver", tag, latestAlias)
	case strings.HasPrefix(version, digest.SHA256.String()+":"):
		return "", "", fmt.Errorf("--tag %q: a digest is not a version; specify a concrete semver", tag)
	}
	return name, version, nil
}

// latestBuiltVersion returns the latest version of the named solution built
// locally (the catalog's "latest": newest stable, unless the context opts
// into pre-releases), or nil when there is none.
func latestBuiltVersion(ctx context.Context, finder ArtifactFinder, name string) (*semver.Version, error) {
	if finder == nil {
		return nil, ErrNilCatalog
	}
	info, err := finder.Find(ctx, catalog.ArtifactQuery{
		Kind:   catalog.ArtifactKindSolution,
		Name:   name,
		Source: catalog.SourceSelector{Kind: catalog.SourceBuilt},
	})
	switch {
	case catalog.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("finding latest built version of %q: %w", name, err)
	}
	return info.Reference.Version, nil
}

// nextVersion increments v by level, dropping any prerelease or metadata.
func nextVersion(v *semver.Version, level solution.BumpLevel) *semver.Version {
	switch level {
	case solution.BumpMajor:
		return semver.New(v.Major()+1, 0, 0, "", "")
	case solution.BumpMinor:
		return semver.New(v.Major(), v.Minor()+1, 0, "", "")
	case solution.BumpPatch:
	}
	// Patch is the default bump, including for an unset level.
	return semver.New(v.Major(), v.Minor(), v.Patch()+1, "", "")
}
