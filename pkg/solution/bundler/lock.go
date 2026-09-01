// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"gopkg.in/yaml.v3"
)

// TODO: lint should catch duplicate plugins in bundle ?
const (
	// LockFileVersion is the current lock file format version.
	LockFileVersion = 1

	// DefaultLockFileName is the default lock file name.
	DefaultLockFileName = "solution.lock"
)

// LockFile represents the lock file that records vendored dependency state.
// It enables reproducible builds by replaying exact versions and digests.
type LockFile struct {
	// Version is the lock file format version.
	Version int `json:"version" yaml:"version" doc:"Lock file format version" example:"1"`

	// Dependencies lists vendored solution dependencies with their digests.
	Dependencies []LockDependency `json:"dependencies,omitempty" yaml:"dependencies,omitempty" doc:"Vendored solution dependencies" maxItems:"1000"`

	// Plugins lists vendored plugin dependencies with their digests.
	Plugins []LockPlugin `json:"plugins,omitempty" yaml:"plugins,omitempty" doc:"Vendored plugin dependencies" maxItems:"100"`
}

// LockDependency records metadata about a vendored catalog dependency.
type LockDependency struct {
	// Ref is the original catalog reference (e.g., "deploy-to-k8s@2.0.0" or "deploy-to-k8s@^1.5.0").
	Ref string `json:"ref" yaml:"ref" doc:"Original catalog reference" maxLength:"255" example:"deploy-to-k8s@2.0.0"`

	// ResolvedVersion is the exact semver version that was resolved and vendored.
	// For exact refs like "deploy-to-k8s@2.0.0" this equals the version in Ref.
	// For constraint refs like "deploy-to-k8s@^1.5.0" this is the resolved version (e.g., "1.5.2").
	ResolvedVersion string `json:"resolvedVersion,omitempty" yaml:"resolvedVersion,omitempty" doc:"Exact resolved version" maxLength:"50" example:"1.5.2"`

	// Constraint is the original version constraint, if any (e.g., "^1.5.0", ">=2.0.0").
	// Empty for exact version references.
	Constraint string `json:"constraint,omitempty" yaml:"constraint,omitempty" doc:"Original version constraint" maxLength:"100" example:"^1.5.0"`

	// Digest is the SHA-256 content digest of the vendored file.
	Digest string `json:"digest" yaml:"digest" doc:"SHA-256 content digest" maxLength:"128" example:"sha256:abc123..."`

	// ResolvedFrom is the catalog name from which the dependency was fetched.
	ResolvedFrom string `json:"resolvedFrom" yaml:"resolvedFrom" doc:"Source catalog name" maxLength:"255" example:"company-catalog"`

	// VendoredAt is the path relative to the bundle root where the file is stored.
	VendoredAt string `json:"vendoredAt" yaml:"vendoredAt" doc:"Relative path to vendored file" maxLength:"500" example:".scafctl/vendor/deploy-to-k8s@2.0.0.yaml"`
}

// LockPluginSource records the resolved remote source of a sourced plugin.
// Its presence (non-nil) marks the entry as sourced; unsourced entries leave
// it nil, mirroring solution.PluginDependency.IsSourced().
type LockPluginSource struct {
	// Registry is the canonical origin from the spec's source block
	// (known pre-resolution). Equals ResolvedCanonical for sourced entries.
	Registry string `json:"registry" yaml:"registry" doc:"Canonical source registry" maxLength:"255" example:"ghcr.io/org/plugins"`
	// Name is the RESOLVED remote leaf artifact name (never empty; the
	// ArtifactName() fallback is already applied at write time).
}

// LockPlugin records metadata about a vendored plugin dependency.
type LockPlugin struct {
	// Name is the plugin name.
	Name string `json:"name" yaml:"name" doc:"Plugin name" maxLength:"100" example:"azure-provider"`

	// Kind is the plugin kind (e.g., "provider", "auth-handler").
	Kind string `json:"kind" yaml:"kind" doc:"Plugin kind" maxLength:"50" example:"provider"`

	// Version is the resolved version string.
	Version string `json:"version" yaml:"version" doc:"Resolved version" maxLength:"50" example:"1.2.3"`

	// Constraint is the requested version as written in the solution: a semver
	// constraint (e.g. "^1.5.0", ">=2.0.0"), an exact version, or "latest".
	// Version records what was resolved from this constraint. It is refreshed
	// to the currently-requested value on every build, even when the pinned
	// Version is replayed from the lock.
	Constraint string `json:"constraint,omitempty" yaml:"constraint,omitempty" doc:"Requested version constraint" maxLength:"50" example:"^1.5.0"`

	// Digest is the SHA-256 content digest for the build platform.
	// When Digests is populated, this equals Digests[buildPlatform].
	Digest string `json:"digest" yaml:"digest" doc:"SHA-256 content digest" maxLength:"128" example:"sha256:abc123..."`

	// Digests maps each published platform (e.g. "linux/amd64") to its
	// content-layer SHA-256 digest. It is populated ONLY for genuine
	// multi-platform (OCI image index) plugins. For single-platform plugins it
	// is empty (nil): their sole digest lives in the primary Digest, which the
	// runtime verifies against on every os/arch. This "empty means
	// single-platform" invariant lets the fetcher distinguish a single-platform
	// binary from a platform genuinely absent from a multi-platform index.
	Digests map[string]string `json:"digests,omitempty" yaml:"digests,omitempty" doc:"Per-platform content digests (multi-platform plugins only)"`

	// ResolvedFrom is the source registry or catalog.
	ResolvedFrom string `json:"resolvedFrom" yaml:"resolvedFrom" doc:"Source registry or catalog" maxLength:"255" example:"plugins.example.com"`

	// ResolvedCanonical is the stable, machine-independent identity of the
	// source catalog (e.g. "ghcr.io/org/plugins"). Unlike ResolvedFrom, which
	// records the user-facing config alias, this survives catalog renames and
	// is portable across machines, so consumers can match the lock against a
	// catalog regardless of its local alias. Empty when the plugin was
	// resolved from a catalog with no portable identity (e.g. local).
	ResolvedCanonical string `json:"resolvedCanonical,omitempty" yaml:"resolvedCanonical,omitempty" doc:"Stable machine-independent source catalog identity" maxLength:"255" example:"ghcr.io/org/plugins"`

	// Signature holds Sigstore/cosign verification metadata captured at lock
	// time. Nil when the plugin was locked without signature verification.
	Signature *LockPluginSignature `json:"signature,omitempty" yaml:"signature,omitempty" doc:"Signature verification metadata"`

	Source *LockPluginSource `json:"source,omitempty" yaml:"source,omitempty" doc:"Resolved remote source (sourced plugins only)"`
}

// LockPluginSignature records cosign signature metadata captured during the
// lock (build) phase to allow verification auditing and drift detection.
type LockPluginSignature struct {
	// Issuer is the OIDC token issuer from the signing certificate
	// (e.g., "https://token.actions.githubusercontent.com").
	Issuer string `json:"issuer" yaml:"issuer" doc:"OIDC certificate issuer" example:"https://token.actions.githubusercontent.com" maxLength:"255"`

	// Identity is the certificate subject identity
	// (e.g., "https://github.com/oakwood-commons/scafctl-plugin-auth-entra/.github/workflows/release.yaml@refs/tags/v1.0.0").
	Identity string `json:"identity" yaml:"identity" doc:"Certificate subject identity" example:"https://github.com/oakwood-commons/scafctl-plugin-auth-entra/.github/workflows/release.yaml@refs/tags/v1.0.0" maxLength:"500"`

	// SignedAt is the signature timestamp in RFC 3339 format.
	SignedAt string `json:"signedAt" yaml:"signedAt" doc:"Signature timestamp (RFC 3339)" example:"2026-01-15T10:30:00Z" pattern:"^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(Z|[+-]\\d{2}:\\d{2})$" patternDescription:"RFC 3339 timestamp" maxLength:"30"`
}

// FindDependency returns the lock entry for the given ref, or nil if not found.
// It first tries an exact Ref match, then falls back to matching by name
// (the part before @) to support constraint-based refs where the constraint
// string may differ between runs.
func (lf *LockFile) FindDependency(ref string) *LockDependency {
	if lf == nil {
		return nil
	}
	// Exact match first
	for i := range lf.Dependencies {
		if lf.Dependencies[i].Ref == ref {
			return &lf.Dependencies[i]
		}
	}
	// Fall back to name-based match for constraint refs
	name := refName(ref)
	for i := range lf.Dependencies {
		if refName(lf.Dependencies[i].Ref) == name {
			return &lf.Dependencies[i]
		}
	}
	return nil
}

// FindDependencyByName returns the lock entry matching the artifact name, or nil.
func (lf *LockFile) FindDependencyByName(name string) *LockDependency {
	if lf == nil {
		return nil
	}
	for i := range lf.Dependencies {
		if refName(lf.Dependencies[i].Ref) == name {
			return &lf.Dependencies[i]
		}
	}
	return nil
}

// refName extracts the artifact name from a catalog ref string.
// For "deploy-to-k8s@2.0.0" or "deploy-to-k8s@^1.5.0", returns "deploy-to-k8s".
// For bare names like "deploy-to-k8s", returns the name unchanged.
func refName(ref string) string {
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		return ref[:idx]
	}
	return ref
}

// FindPlugin returns the lock entry for a plugin by name and kind, or nil if not found.
func (lf *LockFile) FindPlugin(name, kind string) *LockPlugin {
	if lf == nil {
		return nil
	}
	for i := range lf.Plugins {
		if lf.Plugins[i].Name == name && lf.Plugins[i].Kind == kind {
			return &lf.Plugins[i]
		}
	}
	return nil
}

// FindPluginByIdentity returns the lock entry matching the full plugin identity
// (canonical origin, name, kind), or nil if none. Unlike FindPlugin, which
// matches on (name, kind) only and so collapses distinct cross-registry plugins
// that share a name, this scopes the match to a single canonical origin. The
// identity is unique in the lock -- a given (canonical, name, kind) is pinned to
// exactly one entry -- so a single match is returned rather than a slice.
//
// A canonical of "" matches an entry with an empty ResolvedCanonical, i.e. a
// short-name/local plugin with no portable origin. The returned pointer aliases
// the receiver's slice; do not retain it across mutations of lf.Plugins.
func (lf *LockFile) FindPluginByIdentity(canonical, name, kind string) *LockPlugin {
	if lf == nil {
		return nil
	}
	for i := range lf.Plugins {
		if lf.Plugins[i].Name == name &&
			lf.Plugins[i].Kind == kind &&
			lf.Plugins[i].ResolvedCanonical == canonical {
			return &lf.Plugins[i]
		}
	}
	return nil
}

// LoadLockFile reads and parses a lock file from the given path.
// Returns nil without error if the file does not exist.
func LoadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading lock file: %w", err)
	}

	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}

	if lf.Version != LockFileVersion {
		return nil, fmt.Errorf("unsupported lock file version %d (expected %d)", lf.Version, LockFileVersion)
	}

	return &lf, nil
}

// WriteLockFile serializes and writes the lock file to the given path.
func WriteLockFile(path string, lf *LockFile) error {
	if lf == nil {
		return fmt.Errorf("lock file is nil")
	}

	lf.Version = LockFileVersion

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("serializing lock file: %w", err)
	}

	// Prepend a header comment
	header := []byte("# This file is auto-generated by scafctl build. Do not edit.\n")
	header = append(header, data...)

	if err := os.WriteFile(path, header, 0o600); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	return nil
}

// MarshalLockJSON serializes a lock file to JSON for use as an OCI artifact
// layer. It stamps the current format version before marshaling. The on-disk
// solution.lock stays YAML; this is the wire encoding for the lock layer.
func MarshalLockJSON(lf *LockFile) ([]byte, error) {
	if lf == nil {
		return nil, fmt.Errorf("lock file is nil")
	}

	lf.Version = LockFileVersion

	data, err := json.Marshal(lf)
	if err != nil {
		return nil, fmt.Errorf("serializing lock file: %w", err)
	}

	return data, nil
}

// ParseLockJSON parses a JSON-encoded lock file, as read from an OCI artifact
// layer, and validates its format version.
func ParseLockJSON(data []byte) (*LockFile, error) {
	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}

	if lf.Version != LockFileVersion {
		return nil, fmt.Errorf("unsupported lock file version %d (expected %d)", lf.Version, LockFileVersion)
	}

	return &lf, nil
}

type hasRegistry interface {
	HasRegistry() bool
	Registry() string
}
type hasArtifactName interface {
	ArtifactName() string
}
type pluginKind interface {
	PluginKind() solution.PluginKind
}

type pluginArtifact interface {
	hasRegistry
	hasArtifactName
	pluginKind
}

type versionPluginArtifact interface {
	VersionConstraint() string
	pluginArtifact
}

// FindPluginByDep resolves the lock entry for a solution dependency. It is the
// concrete boundary over the generic findPluginByDep core.
func (lf *LockFile) FindPluginByDep(dep solution.PluginDependency) *LockPlugin {
	return findPluginByDep(lf, dep)
}

// FindLockPluginByDep resolves the lock entry for any dependency-like value.
// It is the exported generic boundary over the findPluginByDep core, letting
// callers in other packages resolve a lock entry without narrowing to a
// concrete solution.PluginDependency.
func FindLockPluginByDep[T pluginArtifact](lf *LockFile, dep T) *LockPlugin {
	return findPluginByDep(lf, dep)
}

// findPluginByDep is the generic core, constrained to the minimal behavior it
// needs so any dependency-like value can be resolved against the lock.
func findPluginByDep[T pluginArtifact](lf *LockFile, dep T) *LockPlugin {
	if lf == nil {
		return nil
	}
	kind := string(dep.PluginKind())
	if dep.HasRegistry() {
		return lf.findByRegistry(dep.Registry(), dep.ArtifactName(), kind)
	}
	return lf.find(dep.ArtifactName(), kind)
}

// findByRegistry: presence of Source is the guard; leaf+registry give drift safety.
func (lf *LockFile) findByRegistry(registry, name, kind string) *LockPlugin {
	for i := range lf.Plugins {
		e := &lf.Plugins[i]
		if e.Source != nil && e.Kind == kind &&
			e.Source.Registry == registry && e.Name == name {
			return e
		}
	}
	return nil
}

func (lf *LockFile) find(name, kind string) *LockPlugin {
	for i := range lf.Plugins {
		e := &lf.Plugins[i]
		if e.Source == nil && e.Name == name && e.Kind == kind {
			return e
		}
	}
	return nil
}

// FindPluginByVersionConstraint resolves the lock entry for a solution
// dependency, additionally matching on its version constraint. It is the
// concrete boundary over the generic findPluginByVersionConstraint core.
func (lf *LockFile) FindPluginByVersionConstraint(dep solution.PluginDependency) *LockPlugin {
	return findPluginByVersionConstraint(lf, dep)
}

// findPluginByVersionConstraint is the generic core, constrained to the minimal
// behavior it needs so any constraint-bearing dependency value can be resolved.
func findPluginByVersionConstraint[T versionPluginArtifact](lf *LockFile, dep T) *LockPlugin {
	if lf == nil {
		return nil
	}
	if dep.HasRegistry() {
		return lf.findByRegistryAndConstraint(dep.Registry(), dep.ArtifactName(), string(dep.PluginKind()), dep.VersionConstraint())
	}
	return lf.findByConstraint(dep.ArtifactName(), string(dep.PluginKind()), dep.VersionConstraint())
}

func (lf *LockFile) findByRegistryAndConstraint(registry, name, kind, constraint string) *LockPlugin {
	for i := range lf.Plugins {
		e := &lf.Plugins[i]
		if e.Source != nil && e.Kind == kind &&
			e.Source.Registry == registry && e.Name == name &&
			e.Constraint == constraint {
			return e
		}
	}
	return nil
}

func (lf *LockFile) findByConstraint(name, kind, constraint string) *LockPlugin {
	for i := range lf.Plugins {
		e := &lf.Plugins[i]
		if e.Source == nil && e.Name == name && e.Kind == kind && e.Constraint == constraint {
			return e
		}
	}
	return nil
}
