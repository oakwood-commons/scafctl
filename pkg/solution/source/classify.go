// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package source classifies solution path inputs to determine how a solution
// should be fetched (stdin, URL, remote registry, catalog, or local file).
package source

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/oakwood-commons/scafctl/pkg/filepath"
)

// ErrInvalidRef is returned when a solution path is syntactically recognizable
// as a catalog or registry reference but is malformed.
var ErrInvalidRef = errors.New("invalid solution reference")

// Kind identifies how a solution path should be fetched.
type Kind int

const (
	// KindUnknown is the zero value; returned alongside an error.
	KindUnknown Kind = iota
	// KindAutoDiscover means no path was given; the caller should
	// auto-discover a solution file.
	KindAutoDiscover
	// KindStdin means the solution is read from standard input ("-").
	KindStdin
	// KindURL is an http(s) URL.
	KindURL
	// KindRemoteRef is an OCI registry reference
	// (e.g. "ghcr.io/org/name@1.0.0", "oci://ghcr.io/org/name").
	KindRemoteRef
	// KindCatalogQualified is a bare name scoped to an explicitly given
	// catalog (e.g. "name@1.0.0" with catalog "official").
	KindCatalogQualified
	// KindBareName is a catalog name with optional version and no explicit
	// catalog (e.g. "name", "name@1.0.0").
	KindBareName
	// KindLocalFile is a local filesystem path.
	KindLocalFile
)

// String returns a human-readable name for the kind.
func (k Kind) String() string {
	switch k {
	case KindAutoDiscover:
		return "auto-discover"
	case KindStdin:
		return "stdin"
	case KindURL:
		return "url"
	case KindRemoteRef:
		return "remote-ref"
	case KindCatalogQualified:
		return "catalog-qualified"
	case KindBareName:
		return "bare-name"
	case KindLocalFile:
		return "local-file"
	case KindUnknown:
	}
	return "unknown"
}

// Classification is the result of classifying a solution path.
type Classification struct {
	Kind    Kind   `json:"kind" yaml:"kind" doc:"How the solution should be fetched"`
	Ref     string `json:"ref" yaml:"ref" doc:"The original, unmodified input path" maxLength:"4096" example:"ghcr.io/org/my-solution@1.0.0"`
	Catalog string `json:"catalog,omitempty" yaml:"catalog,omitempty" doc:"Catalog the name is scoped to; set only for catalog-qualified sources" maxLength:"255" example:"official"`
	Version string `json:"version,omitempty" yaml:"version,omitempty" doc:"Semver constraint selecting the version; set only for unpinned catalog or registry references" maxLength:"255" example:"^1.2.0"`
}

// Option scopes a classification.
type Option func(*options)

type options struct {
	catalog string
	version string
}

// WithCatalog scopes a bare name to the named catalog. An empty name is a
// no-op, so a flag value can be passed through unchecked.
func WithCatalog(name string) Option {
	return func(o *options) { o.catalog = name }
}

// WithVersion selects the version by a semver constraint (e.g. "^1.2.0",
// ">=1.0 <3.0"). An empty constraint is a no-op, so a flag value can be
// passed through unchecked.
func WithVersion(constraint string) Option {
	return func(o *options) { o.version = constraint }
}

const (
	stdinPath              = "-"
	ociScheme              = "oci://"
	schemeSeparator        = "://"
	refPathSeparator       = "/"
	versionSeparator       = "@"
	tagSeparator           = ":"
	localhostHost          = "localhost"
	registryHostIndicators = ".:"
	windowsPathSeparator   = "\\"
	relativePathPrefix     = "."
	homeDirPrefix          = "~"
	currentDirSegment      = "."
	parentDirSegment       = ".."
)

// solutionFileExtensions are suffixes that mark a path as a local file.
var solutionFileExtensions = []string{".yaml", ".yml", ".json"}

// Classify determines, purely syntactically, how a solution path should be
// fetched. It performs no I/O and no catalog/alias lookup.
//
// WithCatalog scopes a bare name to one catalog; it is an error for every
// other kind. WithVersion selects the version by a semver constraint; it is
// valid only for a bare name, catalog-qualified name, or registry reference
// that does not already pin a version, tag, or digest.
//
// Rules, first match wins:
//
//	""                              -> KindAutoDiscover
//	"-"                             -> KindStdin
//	http(s)://...                   -> KindURL
//	oci://...                       -> KindRemoteRef (other schemes error)
//	/x, ./x, ~/x, C:\x, x\y, *.yaml -> KindLocalFile
//	host.tld/.../name[@v|:tag]      -> KindRemoteRef
//	a/b (non-host first segment)    -> KindLocalFile
//	name[@v] with catalogName       -> KindCatalogQualified
//	name[@v]                        -> KindBareName
func Classify(path string, opts ...Option) (Classification, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	c, err := classifyPath(path)
	if err != nil {
		return Classification{}, err
	}
	if c, err = applyCatalog(c, o.catalog); err != nil {
		return Classification{}, err
	}
	if c, err = applyVersion(c, o.version); err != nil {
		return Classification{}, err
	}
	return c, nil
}

// applyCatalog scopes a bare name to catalog.
func applyCatalog(c Classification, catalog string) (Classification, error) {
	if catalog == "" {
		return c, nil
	}
	if c.Kind != KindBareName {
		return Classification{}, invalidRef(c.Ref,
			fmt.Sprintf("catalog %q applies only to a name[@version], not a %s", catalog, c.Kind))
	}
	c.Kind = KindCatalogQualified
	c.Catalog = catalog
	return c, nil
}

// applyVersion attaches a version constraint to an unpinned catalog or
// registry reference.
func applyVersion(c Classification, constraint string) (Classification, error) {
	if constraint == "" {
		return c, nil
	}
	if c.Kind != KindBareName && c.Kind != KindCatalogQualified && c.Kind != KindRemoteRef {
		return Classification{}, invalidRef(c.Ref,
			fmt.Sprintf("version constraint %q applies only to a catalog name or registry reference, not a %s", constraint, c.Kind))
	}
	if hasPinnedVersion(c) {
		return Classification{}, invalidRef(c.Ref,
			fmt.Sprintf("version constraint %q cannot be combined with a version, tag, or digest in the reference; use one or the other", constraint))
	}
	if _, err := semver.NewConstraint(constraint); err != nil {
		return Classification{}, fmt.Errorf("%w %q: invalid version constraint %q: %w", ErrInvalidRef, c.Ref, constraint, err)
	}
	c.Version = constraint
	return c, nil
}

// hasPinnedVersion reports whether a catalog or registry reference already
// names a version, tag, or digest.
func hasPinnedVersion(c Classification) bool {
	if c.Kind != KindRemoteRef {
		return strings.Contains(c.Ref, versionSeparator)
	}
	ref := strings.TrimPrefix(c.Ref, ociScheme)
	if strings.Contains(ref, versionSeparator) {
		return true
	}
	// A ':' after the last '/' is a tag; one before it is a registry port.
	return strings.LastIndex(ref, tagSeparator) > strings.LastIndex(ref, refPathSeparator)
}

// classifyPath classifies path on its own, without a catalog scope.
func classifyPath(path string) (Classification, error) {
	switch {
	case path == "":
		return Classification{Kind: KindAutoDiscover, Ref: path}, nil
	case path == stdinPath:
		return Classification{Kind: KindStdin, Ref: path}, nil
	case filepath.IsURL(path):
		return Classification{Kind: KindURL, Ref: path}, nil
	}

	if strings.Contains(path, schemeSeparator) {
		rest, ok := strings.CutPrefix(path, ociScheme)
		if !ok {
			return Classification{}, invalidRef(path, "unsupported scheme or malformed URL")
		}
		if err := validateRemoteRef(path, rest); err != nil {
			return Classification{}, err
		}
		return Classification{Kind: KindRemoteRef, Ref: path}, nil
	}

	if looksLikeLocalPath(path) {
		return Classification{Kind: KindLocalFile, Ref: path}, nil
	}

	if first, _, found := strings.Cut(path, refPathSeparator); found {
		if isRegistryHost(first) {
			if err := validateRemoteRef(path, path); err != nil {
				return Classification{}, err
			}
			return Classification{Kind: KindRemoteRef, Ref: path}, nil
		}
		return Classification{Kind: KindLocalFile, Ref: path}, nil
	}

	if err := validateNameVersion(path); err != nil {
		return Classification{}, err
	}
	return Classification{Kind: KindBareName, Ref: path}, nil
}

// looksLikeLocalPath reports unambiguous local filesystem signals.
func looksLikeLocalPath(path string) bool {
	if strings.HasPrefix(path, refPathSeparator) ||
		strings.HasPrefix(path, relativePathPrefix) ||
		strings.HasPrefix(path, homeDirPrefix) {
		return true
	}
	if filepath.HasWindowsDrivePrefix(path) || strings.Contains(path, windowsPathSeparator) {
		return true
	}
	lower := strings.ToLower(path)
	for _, ext := range solutionFileExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// isRegistryHost reports whether a first path segment looks like a registry
// hostname ("ghcr.io", "localhost", "localhost:5000").
func isRegistryHost(segment string) bool {
	return segment == localhostHost || strings.ContainsAny(segment, registryHostIndicators)
}

// validateNameVersion checks a "name[@version]" path.
func validateNameVersion(path string) error {
	name, version, hasVersion := strings.Cut(path, versionSeparator)
	switch {
	case strings.Contains(version, versionSeparator):
		return invalidRef(path, "multiple '@' separators")
	case name == "":
		return invalidRef(path, "empty name")
	case hasVersion && version == "":
		return invalidRef(path, "empty version after '@'")
	}
	return nil
}

// validateRemoteRef checks "<host>/<repo...>/<name>[@version|:tag]" with any
// "oci://" prefix already stripped from ref. Digests ("@sha256:...") are allowed.
func validateRemoteRef(path, ref string) error {
	repo, version, hasVersion := strings.Cut(ref, versionSeparator)
	switch {
	case strings.Contains(version, versionSeparator):
		return invalidRef(path, "multiple '@' separators")
	case hasVersion && version == "":
		return invalidRef(path, "empty version after '@'")
	}

	if !hasVersion {
		if i := strings.LastIndex(repo, tagSeparator); i > strings.LastIndex(repo, refPathSeparator) {
			if repo[i+1:] == "" {
				return invalidRef(path, "empty tag after ':'")
			}
			repo = repo[:i]
		}
	}

	if host, _, _ := strings.Cut(repo, refPathSeparator); host == "" {
		return invalidRef(path, "missing registry host")
	}
	for _, segment := range strings.Split(repo, refPathSeparator) {
		if segment == currentDirSegment || segment == parentDirSegment {
			return invalidRef(path, "relative path segment in repository")
		}
	}
	slash := strings.LastIndex(repo, refPathSeparator)
	if slash == -1 || repo[slash+1:] == "" {
		return invalidRef(path, "missing artifact name")
	}
	return nil
}

func invalidRef(path, reason string) error {
	return fmt.Errorf("%w %q: %s", ErrInvalidRef, path, reason)
}
