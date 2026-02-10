// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"errors"
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
