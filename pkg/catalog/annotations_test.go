// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnnotationBuilder_SetMap(t *testing.T) {
	t.Parallel()

	t.Run("merges user annotations", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			SetMap(map[string]string{"team": "platform", "env": "prod"}).
			Build()
		assert.Equal(t, "platform", ann["team"])
		assert.Equal(t, "prod", ann["env"])
	})

	t.Run("skips empty values", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			SetMap(map[string]string{"team": "platform", "empty": ""}).
			Build()
		assert.Equal(t, "platform", ann["team"])
		assert.NotContains(t, ann, "empty")
	})

	t.Run("nil map is safe", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			SetMap(nil).
			Build()
		assert.Empty(t, ann)
	})

	t.Run("engine keys override user annotations", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			SetMap(map[string]string{AnnotationDisplayName: "User Name"}).
			Set(AnnotationDisplayName, "Engine Name").
			Build()
		assert.Equal(t, "Engine Name", ann[AnnotationDisplayName])
	})
}

func TestAnnotationBuilder_Unset(t *testing.T) {
	t.Parallel()

	t.Run("removes an inherited key", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			SetMap(map[string]string{AnnotationSourceCanonical: "example.com", AnnotationDisplayName: "demo"}).
			Unset(AnnotationSourceCanonical).
			Build()
		assert.NotContains(t, ann, AnnotationSourceCanonical)
		assert.Equal(t, "demo", ann[AnnotationDisplayName])
	})

	t.Run("unsetting an absent key is a no-op", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			Unset(AnnotationSourceCanonical).
			Build()
		assert.Empty(t, ann)
	})

	t.Run("a later Set wins over Unset", func(t *testing.T) {
		t.Parallel()
		ann := NewAnnotationBuilder().
			Unset(AnnotationSourceCanonical).
			Set(AnnotationSourceCanonical, "example.com").
			Build()
		assert.Equal(t, "example.com", ann[AnnotationSourceCanonical])
	})
}
