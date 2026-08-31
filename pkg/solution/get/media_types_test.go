// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog/mediatypes"
	"github.com/stretchr/testify/assert"
)

// TestMediaTypesTrackLeaf guards that this package's local media-type constants
// stay wired to the canonical mediatypes leaf package. If someone replaces an
// alias with a divergent literal, this fails.
func TestMediaTypesTrackLeaf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, mediatypes.SolutionBundle, mediaTypeSolutionBundle)
	assert.Equal(t, mediatypes.SolutionLock, mediaTypeSolutionLock)
}
