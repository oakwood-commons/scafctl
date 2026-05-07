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
