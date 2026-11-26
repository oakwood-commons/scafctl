// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
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

func TestCredentialRejection(t *testing.T) {
	first := AuthDegradedError{Registry: "reg.example.com", Handler: "github", CredentialSource: "github auth handler token"}

	t.Run("nil without a recorder", func(t *testing.T) {
		ctx := t.Context()
		RecordCredentialRejection(ctx, first)
		assert.Nil(t, CredentialRejection(ctx))
	})
	t.Run("nil when nothing was recorded", func(t *testing.T) {
		assert.Nil(t, CredentialRejection(WithCredentialRejections(t.Context())))
	})
	t.Run("keeps the first rejection", func(t *testing.T) {
		ctx := WithCredentialRejections(t.Context())
		RecordCredentialRejection(ctx, first)
		RecordCredentialRejection(ctx, AuthDegradedError{Registry: "other.example.com"})
		assert.Equal(t, &first, CredentialRejection(ctx))
	})
	t.Run("derived contexts share the recorder", func(t *testing.T) {
		ctx := WithCredentialRejections(t.Context())
		child, cancel := context.WithCancel(ctx)
		defer cancel()
		RecordCredentialRejection(child, first)
		assert.Equal(t, &first, CredentialRejection(ctx))
	})
	t.Run("returns a copy", func(t *testing.T) {
		ctx := WithCredentialRejections(t.Context())
		RecordCredentialRejection(ctx, first)
		CredentialRejection(ctx).Registry = "mutated"
		assert.Equal(t, "reg.example.com", CredentialRejection(ctx).Registry)
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

func TestBuilderError(t *testing.T) {
	t.Run("message renders detail then cause", func(t *testing.T) {
		cause := errors.New("boom")
		e := &BuilderError{
			Stage:   BuildStagePush,
			Builder: BuilderKindManifest,
			Detail:  "pushing config",
			Cause:   cause,
		}
		assert.Equal(t, "pushing config: boom", e.Error())
	})

	t.Run("message renders detail alone when no cause", func(t *testing.T) {
		e := &BuilderError{
			Stage:   BuildStageAssemble,
			Builder: BuilderKindManifest,
			Detail:  "building manifest: no layers",
		}
		assert.Equal(t, "building manifest: no layers", e.Error())
	})

	t.Run("unwrap returns cause", func(t *testing.T) {
		cause := errors.New("boom")
		e := &BuilderError{Stage: BuildStagePush, Builder: BuilderKindIndex, Detail: "pushing image index", Cause: cause}
		assert.ErrorIs(t, e, cause)
	})
}

func TestNoMatchingVersionError(t *testing.T) {
	t.Parallel()
	ref := Reference{Kind: ArtifactKindProvider, Name: "exec"}

	t.Run("newNoMatchingVersionError sorts descending, deduplicates, and strips internal pre-release suffix", func(t *testing.T) {
		t.Parallel()
		err := newNoMatchingVersionError(
			ref,
			">=1.0.0, >=0.0.0-0 || ^2.0.0, >=0.0.0-0",
			[]*semver.Version{
				semver.MustParse("0.4.0"),
				nil,
				semver.MustParse("0.5.0"),
				semver.MustParse("0.4.0"),
			},
			"official",
		)
		assert.Equal(t, ">=1.0.0 || ^2.0.0", err.Constraint)
		assert.Equal(t, []string{"0.5.0", "0.4.0"}, err.Available)
		assert.Equal(t, `artifact "exec" has no version matching ">=1.0.0 || ^2.0.0" in catalog "official" (available: 0.5.0, 0.4.0)`, err.Error())
		assert.ErrorIs(t, err, ErrNoMatchingVersion)
		assert.ErrorIs(t, err, ErrArtifactNotFound)
		assert.True(t, IsNotFound(err))

		got, ok := IsNoMatchingVersion(fmt.Errorf("resolving: %w", err))
		assert.True(t, ok)
		assert.Same(t, err, got)
	})

	t.Run("literal >=0.0.0-0 constraint is preserved", func(t *testing.T) {
		t.Parallel()
		err := newNoMatchingVersionError(ref, ">=0.0.0-0", []*semver.Version{semver.MustParse("0.1.0")}, "")
		assert.Equal(t, ">=0.0.0-0", err.Constraint)
		assert.Equal(t, `artifact "exec" has no version matching ">=0.0.0-0" (available: 0.1.0)`, err.Error())
	})

	t.Run("message variants without constraint or available", func(t *testing.T) {
		t.Parallel()
		e1 := &NoMatchingVersionError{Reference: ref, Catalog: "local"}
		assert.Equal(t, `artifact "exec" has no matching version in catalog "local"`, e1.Error())

		e2 := &NoMatchingVersionError{Reference: ref}
		assert.Equal(t, `artifact "exec" has no matching version`, e2.Error())

		got, ok := IsNoMatchingVersion(errors.New("unrelated"))
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("MockCatalog.Find returns NoMatchingVersionError when versions exist", func(t *testing.T) {
		t.Parallel()
		mc := NewMockCatalog("mock", WithListFunc(func(context.Context, ArtifactKind, string) ([]ArtifactInfo, error) {
			return []ArtifactInfo{
				{Reference: Reference{Kind: ArtifactKindProvider, Name: "exec", Version: semver.MustParse("0.4.0")}},
			}, nil
		}))
		_, err := mc.(*mockCatalog).Find(context.Background(), ArtifactQuery{Name: "exec", Kind: ArtifactKindProvider, Version: ">=1.0.0"})
		assert.ErrorIs(t, err, ErrNoMatchingVersion)
		assert.ErrorIs(t, err, ErrArtifactNotFound)
	})
}

func TestPolicyError(t *testing.T) {
	t.Parallel()

	err := &PolicyError{Message: `catalog "corp" is not in the allowed catalogs list`}
	assert.Equal(t, `catalog "corp" is not in the allowed catalogs list`, err.Error())
	assert.ErrorIs(t, err, ErrCatalogPolicyDenied)
	assert.ErrorIs(t, fmt.Errorf("plugin exec: %w", err), ErrCatalogPolicyDenied)
}
