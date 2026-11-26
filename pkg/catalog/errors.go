// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
)

// ErrArtifactNotFound is returned when an artifact cannot be found.
var ErrArtifactNotFound = errors.New("artifact not found")

// ErrArtifactExists is returned when storing an artifact that already exists.
var ErrArtifactExists = errors.New("artifact already exists")

// ErrInvalidReference is returned when a reference is malformed.
var ErrInvalidReference = errors.New("invalid reference")

// ErrInvalidQuery is returned when an artifact query is under-specified or
// internally inconsistent.
var ErrInvalidQuery = errors.New("invalid artifact query")

// ErrAmbiguousArtifact is returned when a query that must identify a single
// artifact matches more than one.
var ErrAmbiguousArtifact = errors.New("ambiguous artifact")

// InvalidQueryError provides details about a malformed artifact query.
type InvalidQueryError struct {
	Query   ArtifactQuery
	Message string
}

// Error implements the error interface.
func (e *InvalidQueryError) Error() string {
	return fmt.Sprintf("invalid artifact query %s: %s", e.Query.String(), e.Message)
}

// Unwrap returns the base error for errors.Is support.
func (e *InvalidQueryError) Unwrap() error {
	return ErrInvalidQuery
}

// AmbiguousArtifactError reports that a query matched several distinct
// artifacts. Matches holds every candidate so callers can present them, and
// the message names the selectors that would narrow the query.
type AmbiguousArtifactError struct {
	Query   ArtifactQuery
	Matches []ArtifactInfo
}

// Error implements the error interface.
func (e *AmbiguousArtifactError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "artifact query %s matched %d artifacts", e.Query.String(), len(e.Matches))

	if hints := e.Discriminators(); len(hints) > 0 {
		fmt.Fprintf(&sb, "; narrow by %s", strings.Join(hints, " or "))
	}

	for _, m := range e.Matches {
		fmt.Fprintf(&sb, "\n  - %s (%s) %s", m.Reference.VersionOrDigest(), describeSource(m.Annotations), m.Digest)
	}
	return sb.String()
}

// Discriminators names the query field that would narrow the match set to a
// single artifact.
//
// Source takes precedence over version: when the matches span several origins
// the ambiguity is an origin ambiguity, and comparing versions across
// different origins is meaningless. Only once every match shares one origin
// does a version difference become the thing to narrow by.
func (e *AmbiguousArtifactError) Discriminators() []string {
	versions := make(map[string]struct{}, len(e.Matches))
	sources := make(map[string]struct{}, len(e.Matches))
	for _, m := range e.Matches {
		versions[m.Annotations[AnnotationVersion]] = struct{}{}
		sources[describeSource(m.Annotations)] = struct{}{}
	}

	switch {
	case len(sources) > 1:
		return []string{"source"}
	case len(versions) > 1:
		return []string{"version"}
	default:
		return nil
	}
}

// Unwrap returns the base error for errors.Is support.
func (e *AmbiguousArtifactError) Unwrap() error {
	return ErrAmbiguousArtifact
}

// AmbiguousCandidate describes one artifact an ambiguous query matched, in a
// form callers can use to build follow-up commands.
type AmbiguousCandidate struct {
	// Reference is the matched artifact's identity.
	Reference Reference

	// Origin is the value that selects this artifact as a source: SourceBuiltKeyword
	// for locally built artifacts, or the canonical remote catalog identity.
	// It round-trips through ParseSourceSelector.
	Origin string

	// Digest is the matched artifact's content digest.
	Digest string
}

// Candidates returns every matched artifact with the origin that selects it,
// in the order the matches were found.
func (e *AmbiguousArtifactError) Candidates() []AmbiguousCandidate {
	candidates := make([]AmbiguousCandidate, 0, len(e.Matches))
	for _, m := range e.Matches {
		origin := m.Annotations[AnnotationSourceCanonical]
		if origin == "" {
			origin = SourceBuiltKeyword
		}
		candidates = append(candidates, AmbiguousCandidate{
			Reference: m.Reference,
			Origin:    origin,
			Digest:    m.Digest,
		})
	}
	return candidates
}

// ArtifactNotFoundError provides details about a missing artifact.
type ArtifactNotFoundError struct {
	Reference Reference
	Catalog   string // Optional: which catalog was checked
}

// Error implements the error interface.
func (e *ArtifactNotFoundError) Error() string {
	if e.Catalog != "" {
		return fmt.Sprintf("artifact %q not found in catalog %q", e.Reference.String(), e.Catalog)
	}
	return fmt.Sprintf("artifact %q not found", e.Reference.String())
}

// Unwrap returns the base error for errors.Is support.
func (e *ArtifactNotFoundError) Unwrap() error {
	return ErrArtifactNotFound
}

// ArtifactExistsError provides details about a duplicate artifact.
type ArtifactExistsError struct {
	Reference Reference
	Catalog   string
}

// Error implements the error interface.
func (e *ArtifactExistsError) Error() string {
	return fmt.Sprintf("artifact %q already exists in catalog %q (use --force to overwrite)", e.Reference.String(), e.Catalog)
}

// Unwrap returns the base error for errors.Is support.
func (e *ArtifactExistsError) Unwrap() error {
	return ErrArtifactExists
}

// InvalidReferenceError provides details about an invalid reference.
type InvalidReferenceError struct {
	Input   string
	Message string
}

// Error implements the error interface.
func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid reference %q: %s", e.Input, e.Message)
}

// Unwrap returns the base error for errors.Is support.
func (e *InvalidReferenceError) Unwrap() error {
	return ErrInvalidReference
}

// IsNotFound returns true if the error indicates an artifact was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrArtifactNotFound)
}

// IsArtifactNotFoundError is an alias for IsNotFound.
func IsArtifactNotFoundError(err error) bool {
	return IsNotFound(err)
}

// ErrPlatformNotFound is returned when no matching platform is found in an image index.
var ErrPlatformNotFound = errors.New("platform not found")

// PlatformNotFoundError provides details about a missing platform in an image index.
type PlatformNotFoundError struct {
	Platform  string   // The requested platform (e.g. "linux/amd64")
	Available []string // Available platforms in the index
}

// Error implements the error interface.
func (e *PlatformNotFoundError) Error() string {
	if len(e.Available) > 0 {
		return fmt.Sprintf("platform %q not found in image index (available: %v)", e.Platform, e.Available)
	}
	return fmt.Sprintf("platform %q not found in image index", e.Platform)
}

// Unwrap returns the base error for errors.Is support.
func (e *PlatformNotFoundError) Unwrap() error {
	return ErrPlatformNotFound
}

// IsPlatformNotFound returns true if the error indicates a missing platform.
func IsPlatformNotFound(err error) bool {
	return errors.Is(err, ErrPlatformNotFound)
}

// IsExists returns true if the error indicates an artifact already exists.
func IsExists(err error) bool {
	return errors.Is(err, ErrArtifactExists)
}

// IsInvalidReference returns true if the error indicates an invalid reference.
func IsInvalidReference(err error) bool {
	return errors.Is(err, ErrInvalidReference)
}

// ErrCatalogUnreachable is returned when a catalog could not be contacted
// (network, DNS, timeout, or other transport-level failure), as distinct
// from the catalog being reachable and reporting that the artifact does not
// exist. Distinguishing the two lets callers (e.g. ChainCatalog) tell "this
// artifact genuinely does not exist anywhere" apart from "one catalog could
// not be reached, so we cannot say either way."
var ErrCatalogUnreachable = errors.New("catalog unreachable")

// UnreachableError provides details about a catalog that could not be
// contacted due to a network/transport-level failure.
type UnreachableError struct {
	// Catalog is the name of the catalog that could not be reached.
	Catalog string
	// Cause is the underlying transport error (DNS failure, connection
	// refused, timeout, etc.).
	Cause error
}

// Error implements the error interface.
func (e *UnreachableError) Error() string {
	return fmt.Sprintf("catalog %q unreachable: %v", e.Catalog, e.Cause)
}

// Unwrap returns the underlying transport error so callers can inspect the
// root cause (e.g. errors.As for *net.DNSError).
func (e *UnreachableError) Unwrap() error {
	return e.Cause
}

// Is reports whether target is ErrCatalogUnreachable, allowing
// errors.Is(err, ErrCatalogUnreachable) to match without needing to unwrap
// to Cause (which may be an arbitrary transport error, not the sentinel).
func (e *UnreachableError) Is(target error) bool {
	return target == ErrCatalogUnreachable //nolint:errorlint // intentional sentinel identity check
}

// IsCatalogUnreachable reports whether err (or any error it wraps) is a
// UnreachableError, returning the typed value when it is.
func IsCatalogUnreachable(err error) (*UnreachableError, bool) {
	var e *UnreachableError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// ErrEnumerationNotSupported is returned when a registry does not support
// the _catalog endpoint for repository enumeration.
var ErrEnumerationNotSupported = errors.New("registry does not support repository enumeration")

// IsEnumerationNotSupported returns true if the error indicates the registry
// does not support listing all repositories.
func IsEnumerationNotSupported(err error) bool {
	return errors.Is(err, ErrEnumerationNotSupported)
}

// BuilderKind identifies which builder produced a BuilderError.
type BuilderKind string

const (
	// BuilderKindManifest identifies errors raised by a ManifestBuilder.
	BuilderKindManifest BuilderKind = "manifest"
	// BuilderKindIndex identifies errors raised by an IndexBuilder.
	BuilderKindIndex BuilderKind = "index"
)

// BuildStage is the phase of artifact assembly that failed.
type BuildStage string

const (
	// BuildStageAssemble covers validating the builder chain and deriving a
	// descriptor from in-memory state (e.g. missing layers, config, platforms).
	BuildStageAssemble BuildStage = "assemble"
	// BuildStageEncode covers serializing the manifest or index to JSON.
	BuildStageEncode BuildStage = "encode"
	// BuildStagePush covers writing a layer, config, manifest, or index blob.
	BuildStagePush BuildStage = "push"
	// BuildStagePlatform covers resolving or validating a platform string.
	BuildStagePlatform BuildStage = "platform"
)

// BuilderError reports a failure while assembling or pushing an OCI artifact
// through a ManifestBuilder or an IndexBuilder. Stage and Builder identify the
// failing phase and producer; Platform is set for platform-scoped failures;
// Detail is a human-readable description of the specific thing that failed
// (e.g. "no layers", "pushing config"); Cause holds the underlying error.
type BuilderError struct {
	Stage    BuildStage
	Builder  BuilderKind
	Platform string
	Detail   string
	Cause    error
}

// Error implements the error interface. Detail carries the full non-cause
// message prefix, so callers that relied on the previous fmt.Errorf strings
// still see the same text.
func (e *BuilderError) Error() string {
	if e.Cause == nil {
		return e.Detail
	}
	return e.Detail + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error so callers can inspect the root cause.
func (e *BuilderError) Unwrap() error {
	return e.Cause
}

// TagError reports a failure while tagging an artifact descriptor. Tag names
// the tag being written (empty for pre-write validation errors); Reason
// describes a validation failure when Cause is nil; Cause holds the underlying
// tagger error when the write itself failed.
type TagError struct {
	Tag    string
	Reason string
	Cause  error
}

// Error implements the error interface, preserving the message shapes that
// preceded this typed error.
func (e *TagError) Error() string {
	switch {
	case e.Cause != nil:
		return fmt.Sprintf("tagging artifact %q: %v", e.Tag, e.Cause)
	case e.Reason != "":
		return "building tag: " + e.Reason
	default:
		return "tagging artifact"
	}
}

// Unwrap returns the underlying tagger error so callers can inspect the cause.
func (e *TagError) Unwrap() error {
	return e.Cause
}

// AuthDegradedError indicates that stored credentials for a registry were
// rejected and a request fell back to anonymous access. Its result is
// therefore degraded/incomplete rather than authoritative. The message is
// data-only (registry and credential source); callers (e.g. the CLI) are
// responsible for adding a binary-name-specific fix hint, since the binary
// name is context-driven.
type AuthDegradedError struct {
	// Registry is the registry host whose credentials were rejected.
	Registry string
	// Handler is the auth handler that provided the rejected credentials
	// (may be empty when credentials came from another source).
	Handler string
	// CredentialSource is a human-readable description of the rejected
	// credential source (may be empty).
	CredentialSource string
}

// Error implements the error interface with a data-only message.
func (e *AuthDegradedError) Error() string {
	source := e.CredentialSource
	if source == "" {
		if e.Handler != "" {
			source = fmt.Sprintf("%s auth handler credentials", e.Handler)
		} else {
			source = "stored credentials"
		}
	}
	return fmt.Sprintf("authentication required for registry %q: rejected %s; fell back to anonymous access", e.Registry, source)
}

// credentialRejectionsKey is the context key for *credentialRejections.
type credentialRejectionsKey struct{}

// credentialRejections holds the first credential rejection recorded during
// the requests made with one context.
type credentialRejections struct {
	mu    sync.Mutex
	first *AuthDegradedError
}

// WithCredentialRejections returns a context in which a RemoteCatalog records
// a registry rejecting its credentials and the request falling back to
// anonymous access. The record covers only requests made with the returned
// context (or one derived from it), so scope it to one command or API
// request; read it with CredentialRejection.
func WithCredentialRejections(ctx context.Context) context.Context {
	return context.WithValue(ctx, credentialRejectionsKey{}, &credentialRejections{})
}

// CredentialRejection returns the first credential rejection recorded in ctx,
// or nil when none was recorded or ctx was not prepared with
// WithCredentialRejections. A non-nil result means at least one result was
// served anonymously and may omit private content.
func CredentialRejection(ctx context.Context) *AuthDegradedError {
	r, _ := ctx.Value(credentialRejectionsKey{}).(*credentialRejections)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.first == nil {
		return nil
	}
	cp := *r.first
	return &cp
}

// RecordCredentialRejection stores rejection in ctx's recorder unless one was
// already recorded. It is a no-op without WithCredentialRejections.
// RemoteCatalog calls it on an anonymous fallback; other Catalog
// implementations can call it to report the same condition.
func RecordCredentialRejection(ctx context.Context, rejection AuthDegradedError) {
	r, _ := ctx.Value(credentialRejectionsKey{}).(*credentialRejections)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.first == nil {
		r.first = &rejection
	}
}

// IsAuthDegraded reports whether err (or any error it wraps) is an
// AuthDegradedError, returning the typed value when it is.
func IsAuthDegraded(err error) (*AuthDegradedError, bool) {
	var e *AuthDegradedError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// ErrCredentialsRejected marks a lookup that failed after the registry
// rejected this catalog's credentials. Anonymous access cannot tell "absent"
// from "hidden" (registries answer anonymous callers for a private repository
// with 401, 403 or 404), so such a failure is never reported as not-found:
// a strict source chain must halt rather than serve the same name from a
// lower-priority catalog.
var ErrCredentialsRejected = errors.New("registry rejected the catalog's credentials and anonymous access cannot confirm the artifact is absent; re-authenticate and retry")

// ErrCatalogPolicyDenied is returned when a catalog allowlist or per-catalog
// plugin allowlist policy forbids serving an artifact from a catalog.
var ErrCatalogPolicyDenied = errors.New("disallowed by catalog policy")

// PolicyError wraps a policy-denial message so errors.Is matches
// ErrCatalogPolicyDenied without changing the user-facing error text.
type PolicyError struct {
	Message string
}

// Error implements the error interface.
func (e *PolicyError) Error() string {
	return e.Message
}

// Is reports whether target is ErrCatalogPolicyDenied.
func (e *PolicyError) Is(target error) bool {
	return target == ErrCatalogPolicyDenied //nolint:errorlint // intentional sentinel identity check
}

// ErrNoMatchingVersion is returned when an artifact exists in a catalog (one or
// more versions are published), but none of its versions satisfy the requested
// version or semver constraint.
var ErrNoMatchingVersion = errors.New("no matching artifact version")

// NoMatchingVersionError reports that an artifact exists in a catalog, but none
// of its published versions satisfy the requested version or semver constraint.
//
// Unwrap returns ErrArtifactNotFound so multi-catalog chains continue falling
// through to lower-priority catalogs that may publish a matching version, while
// Is matches ErrNoMatchingVersion so callers and aggregateChainError can
// distinguish a constraint mismatch from a completely absent artifact.
type NoMatchingVersionError struct {
	Reference  Reference
	Constraint string
	Available  []string
	Catalog    string
}

// Error implements the error interface.
func (e *NoMatchingVersionError) Error() string {
	constraint := stripInternalPreReleaseSuffix(e.Constraint)
	var avail string
	if len(e.Available) > 0 {
		avail = fmt.Sprintf(" (available: %s)", strings.Join(e.Available, ", "))
	}
	if e.Catalog != "" {
		if constraint != "" {
			return fmt.Sprintf("artifact %q has no version matching %q in catalog %q%s", e.Reference.String(), constraint, e.Catalog, avail)
		}
		return fmt.Sprintf("artifact %q has no matching version in catalog %q%s", e.Reference.String(), e.Catalog, avail)
	}
	if constraint != "" {
		return fmt.Sprintf("artifact %q has no version matching %q%s", e.Reference.String(), constraint, avail)
	}
	return fmt.Sprintf("artifact %q has no matching version%s", e.Reference.String(), avail)
}

// Unwrap returns ErrArtifactNotFound so chain lookups treat a version miss in
// one catalog as eligible to fall through to subsequent catalogs.
func (e *NoMatchingVersionError) Unwrap() error {
	return ErrArtifactNotFound
}

// Is reports whether target is ErrNoMatchingVersion.
func (e *NoMatchingVersionError) Is(target error) bool {
	return target == ErrNoMatchingVersion //nolint:errorlint // intentional sentinel identity check
}

// IsNoMatchingVersion reports whether err (or any error it wraps) is a
// NoMatchingVersionError, returning the typed value when it is.
func IsNoMatchingVersion(err error) (*NoMatchingVersionError, bool) {
	var e *NoMatchingVersionError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func newNoMatchingVersionError(ref Reference, constraint string, versions []*semver.Version, catalogName string) *NoMatchingVersionError {
	sorted := make([]*semver.Version, 0, len(versions))
	for _, v := range versions {
		if v != nil {
			sorted = append(sorted, v)
		}
	}
	sort.Sort(sort.Reverse(semver.Collection(sorted)))

	available := make([]string, 0, len(sorted))
	seen := make(map[string]bool, len(sorted))
	for _, v := range sorted {
		s := v.String()
		if !seen[s] {
			seen[s] = true
			available = append(available, s)
		}
	}

	return &NoMatchingVersionError{
		Reference:  ref,
		Constraint: stripInternalPreReleaseSuffix(constraint),
		Available:  available,
		Catalog:    catalogName,
	}
}

func stripInternalPreReleaseSuffix(constraint string) string {
	return strings.TrimSpace(strings.ReplaceAll(constraint, ", >=0.0.0-0", ""))
}
