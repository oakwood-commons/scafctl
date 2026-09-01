package reference

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Reference string

var (
	registryHostPattern = regexp.MustCompile(`^(localhost(:[0-9]+)?|[a-z0-9-]+(\.[a-z0-9-]+)+(:[0-9]+)?)(/[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	leafPattern         = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

var ErrInvalidReference = errors.New("artifact: invalid reference")

func (r Reference) Split() (registryPath, artifactName string, err error) {
	s := strings.TrimSpace(string(r))
	if s == "" {
		return "", "", ErrInvalidReference
	}

	i := strings.LastIndex(s, "/")
	switch {
	case i < 0:
		return "", s, nil // short name
	case i == 0, i == len(s)-1:
		return "", "", ErrInvalidReference // leading or trailing slash
	default:
		return s[:i], s[i+1:], nil
	}
}

// RegistryPath returns "ghcr.io/oakwood-commons", or "" for a short name.
func (r Reference) RegistryPath() string { p, _, _ := r.Split(); return p }

// ArtifactName returns "github" for both "github" and "ghcr.io/oakwood-commons/github".
func (r Reference) ArtifactName() string { _, n, _ := r.Split(); return n }

// IsShortName reports whether the reference has no registry component.
func (r Reference) IsShortName() bool { return r.RegistryPath() == "" && r.Valid() }

func (r Reference) Valid() bool { return r.Validate() == nil }

// Validate reports whether the reference is a structurally valid plugin
// reference of the form <name> or <registry>/<namespace...>/<name>. It rejects
// an inline version or digest (an '@' tail), a malformed registry host, an
// invalid artifact name, and empty or trailing path segments. The version is
// declared separately (e.g. in bundle.plugins), never inlined here. Validate
// performs no network I/O.
func (r Reference) Validate() error {
	s := strings.TrimSpace(string(r))
	if s == "" {
		return fmt.Errorf("%w: empty reference", ErrInvalidReference)
	}
	if strings.ContainsRune(s, '@') {
		return fmt.Errorf("%w: inline version or digest is not allowed in %q; a reference must be just <name> or <registry>/<name>, and the version is declared separately", ErrInvalidReference, s)
	}
	reg, leaf, err := r.Split()
	if err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidReference, s)
	}
	if !leafPattern.MatchString(leaf) {
		return fmt.Errorf("%w: invalid artifact name %q (must be lowercase alphanumeric with hyphens)", ErrInvalidReference, leaf)
	}
	if reg != "" && !registryHostPattern.MatchString(reg) {
		return fmt.Errorf("%w: invalid registry path %q (must be a host with an optional namespace, e.g. \"ghcr.io/myorg\")", ErrInvalidReference, reg)
	}
	return nil
}

func (r Reference) String() string { return string(r) }
