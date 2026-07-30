// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArtifactNotFoundError(t *testing.T) {
	ref := Reference{
		Kind: ArtifactKindSolution,
		Name: "my-solution",
	}

	t.Run("error message without catalog", func(t *testing.T) {
		err := &ArtifactNotFoundError{Reference: ref}
		assert.Contains(t, err.Error(), "my-solution")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("error message with catalog", func(t *testing.T) {
		err := &ArtifactNotFoundError{Reference: ref, Catalog: "local"}
		assert.Contains(t, err.Error(), "my-solution")
		assert.Contains(t, err.Error(), "local")
	})

	t.Run("unwrap returns base error", func(t *testing.T) {
		err := &ArtifactNotFoundError{Reference: ref}
		assert.True(t, errors.Is(err, ErrArtifactNotFound))
	})
}

func TestArtifactExistsError(t *testing.T) {
	ref := Reference{
		Kind: ArtifactKindSolution,
		Name: "my-solution",
	}

	err := &ArtifactExistsError{Reference: ref, Catalog: "local"}

	t.Run("error message", func(t *testing.T) {
		assert.Contains(t, err.Error(), "my-solution")
		assert.Contains(t, err.Error(), "already exists")
		assert.Contains(t, err.Error(), "--force")
	})

	t.Run("unwrap returns base error", func(t *testing.T) {
		assert.True(t, errors.Is(err, ErrArtifactExists))
	})
}

func TestInvalidReferenceError(t *testing.T) {
	err := &InvalidReferenceError{
		Input:   "bad-ref",
		Message: "invalid format",
	}

	t.Run("error message", func(t *testing.T) {
		assert.Contains(t, err.Error(), "bad-ref")
		assert.Contains(t, err.Error(), "invalid format")
	})

	t.Run("unwrap returns base error", func(t *testing.T) {
		assert.True(t, errors.Is(err, ErrInvalidReference))
	})
}

func TestIsNotFound(t *testing.T) {
	t.Run("returns true for ArtifactNotFoundError", func(t *testing.T) {
		err := &ArtifactNotFoundError{Reference: Reference{Name: "test"}}
		assert.True(t, IsNotFound(err))
	})

	t.Run("returns true for base error", func(t *testing.T) {
		assert.True(t, IsNotFound(ErrArtifactNotFound))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		assert.False(t, IsNotFound(errors.New("other error")))
	})
}

func TestIsExists(t *testing.T) {
	t.Run("returns true for ArtifactExistsError", func(t *testing.T) {
		err := &ArtifactExistsError{Reference: Reference{Name: "test"}}
		assert.True(t, IsExists(err))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		assert.False(t, IsExists(errors.New("other error")))
	})
}

func TestIsInvalidReference(t *testing.T) {
	t.Run("returns true for InvalidReferenceError", func(t *testing.T) {
		err := &InvalidReferenceError{Input: "bad", Message: "reason"}
		assert.True(t, IsInvalidReference(err))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		assert.False(t, IsInvalidReference(errors.New("other error")))
	})
}

func TestPlatformNotFoundError_Error(t *testing.T) {
	err := &PlatformNotFoundError{Platform: "linux/arm64", Available: []string{"linux/amd64", "darwin/amd64"}}
	msg := err.Error()
	assert.Contains(t, msg, "linux/arm64")
	assert.Contains(t, msg, "available")

	errNoAvail := &PlatformNotFoundError{Platform: "windows/amd64"}
	assert.Contains(t, errNoAvail.Error(), "windows/amd64")
	assert.NotContains(t, errNoAvail.Error(), "available")
}

func TestPlatformNotFoundError_Unwrap(t *testing.T) {
	err := &PlatformNotFoundError{Platform: "linux/arm64"}
	assert.ErrorIs(t, err, ErrPlatformNotFound)
}

func TestIsEnumerationNotSupported(t *testing.T) {
	t.Run("returns true for wrapped ErrEnumerationNotSupported", func(t *testing.T) {
		err := fmt.Errorf("enumerating repos: %w", ErrEnumerationNotSupported)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		assert.False(t, IsEnumerationNotSupported(errors.New("other error")))
	})
}

// fakeStaleReporter implements staleCredentialReporter for testing.
type fakeStaleReporter struct {
	stale   bool
	reg     string
	handler string
	source  string
}

func (f fakeStaleReporter) HasStaleCredentials() bool { return f.stale }
func (f fakeStaleReporter) Registry() string          { return f.reg }
func (f fakeStaleReporter) AuthHandlerUsed() string   { return f.handler }
func (f fakeStaleReporter) CredentialSource() string  { return f.source }

func TestAuthDegradedError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *AuthDegradedError
		want string
	}{
		{
			name: "with explicit credential source",
			err:  &AuthDegradedError{Registry: "reg.example.com", CredentialSource: "github auth handler token"},
			want: `authentication required for registry "reg.example.com": rejected github auth handler token; fell back to anonymous access`,
		},
		{
			name: "handler only (no source)",
			err:  &AuthDegradedError{Registry: "reg.example.com", Handler: "github"},
			want: `authentication required for registry "reg.example.com": rejected github auth handler credentials; fell back to anonymous access`,
		},
		{
			name: "no source or handler",
			err:  &AuthDegradedError{Registry: "reg.example.com"},
			want: `authentication required for registry "reg.example.com": rejected stored credentials; fell back to anonymous access`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestNewAuthDegradedError(t *testing.T) {
	t.Run("nil when not stale", func(t *testing.T) {
		assert.Nil(t, NewAuthDegradedError(fakeStaleReporter{stale: false, reg: "reg"}))
	})
	t.Run("nil reporter", func(t *testing.T) {
		assert.Nil(t, NewAuthDegradedError(nil))
	})
	t.Run("builds from stale reporter", func(t *testing.T) {
		e := NewAuthDegradedError(fakeStaleReporter{
			stale: true, reg: "reg.example.com", handler: "github", source: "github auth handler token",
		})
		if assert.NotNil(t, e) {
			assert.Equal(t, "reg.example.com", e.Registry)
			assert.Equal(t, "github", e.Handler)
			assert.Equal(t, "github auth handler token", e.CredentialSource)
		}
	})
}

func TestUnreachableError(t *testing.T) {
	cause := errors.New("dial tcp: lookup ghcr.io: no such host")

	t.Run("error message includes catalog and cause", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		assert.Contains(t, err.Error(), "official")
		assert.Contains(t, err.Error(), cause.Error())
	})

	t.Run("unwrap returns cause", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		assert.Same(t, cause, errors.Unwrap(err))
	})

	t.Run("errors.Is matches sentinel", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		assert.True(t, errors.Is(err, ErrCatalogUnreachable))
	})

	t.Run("errors.Is does not match unrelated sentinel", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		assert.False(t, errors.Is(err, ErrArtifactNotFound))
	})

	t.Run("errors.Is matches sentinel through wrapping", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		wrapped := fmt.Errorf("chain resolve failed: %w", err)
		assert.True(t, errors.Is(wrapped, ErrCatalogUnreachable))
	})

	t.Run("errors.As extracts typed value through wrapping", func(t *testing.T) {
		err := &UnreachableError{Catalog: "official", Cause: cause}
		wrapped := fmt.Errorf("chain resolve failed: %w", err)
		var target *UnreachableError
		assert.True(t, errors.As(wrapped, &target))
		assert.Same(t, err, target)
	})
}

func TestIsCatalogUnreachable(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		base := &UnreachableError{Catalog: "official", Cause: errors.New("boom")}
		got, ok := IsCatalogUnreachable(base)
		assert.True(t, ok)
		assert.Same(t, base, got)
	})

	t.Run("wrapped", func(t *testing.T) {
		base := &UnreachableError{Catalog: "official", Cause: errors.New("boom")}
		wrapped := fmt.Errorf("resolving: %w", base)
		got, ok := IsCatalogUnreachable(wrapped)
		assert.True(t, ok)
		assert.Same(t, base, got)
	})

	t.Run("unrelated error", func(t *testing.T) {
		got, ok := IsCatalogUnreachable(errors.New("nope"))
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("nil", func(t *testing.T) {
		got, ok := IsCatalogUnreachable(nil)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("does not match ArtifactNotFoundError", func(t *testing.T) {
		got, ok := IsCatalogUnreachable(&ArtifactNotFoundError{Reference: Reference{Name: "test"}})
		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func TestIsAuthDegraded(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		base := &AuthDegradedError{Registry: "r"}
		got, ok := IsAuthDegraded(base)
		assert.True(t, ok)
		assert.Same(t, base, got)
	})
	t.Run("wrapped", func(t *testing.T) {
		base := &AuthDegradedError{Registry: "r"}
		wrapped := fmt.Errorf("listing failed: %w", base)
		got, ok := IsAuthDegraded(wrapped)
		assert.True(t, ok)
		assert.Same(t, base, got)
	})
	t.Run("unrelated error", func(t *testing.T) {
		got, ok := IsAuthDegraded(errors.New("nope"))
		assert.False(t, ok)
		assert.Nil(t, got)
	})
	t.Run("nil", func(t *testing.T) {
		got, ok := IsAuthDegraded(nil)
		assert.False(t, ok)
		assert.Nil(t, got)
	})
}
