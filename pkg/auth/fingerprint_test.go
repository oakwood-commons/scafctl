// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFingerprintHash(t *testing.T) {
	t.Run("empty identity returns underscore", func(t *testing.T) {
		assert.Equal(t, "_", FingerprintHash(""))
	})

	t.Run("deterministic for same input", func(t *testing.T) {
		fp1 := FingerprintHash("client-a:tenant-1")
		fp2 := FingerprintHash("client-a:tenant-1")
		assert.Equal(t, fp1, fp2)
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		fp1 := FingerprintHash("client-a:tenant-1")
		fp2 := FingerprintHash("client-b:tenant-2")
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("result is 12 hex chars", func(t *testing.T) {
		fp := FingerprintHash("some-identity")
		require.Len(t, fp, 12)
		// Verify it's valid hex
		for _, c := range fp {
			assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
				"expected hex char, got %c", c)
		}
	})

	t.Run("similar inputs produce different hashes", func(t *testing.T) {
		fp1 := FingerprintHash("client-a:tenant-1")
		fp2 := FingerprintHash("client-a:tenant-2")
		assert.NotEqual(t, fp1, fp2)
	})
}
