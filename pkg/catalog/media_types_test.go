// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog/mediatypes"
	"github.com/stretchr/testify/assert"
)

// TestExportedMediaTypesTrackLeaf guards that catalog's exported solution
// media-type constants stay wired to the canonical mediatypes leaf package.
// If someone replaces an alias with a divergent literal, this fails.
func TestExportedMediaTypesTrackLeaf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, mediatypes.SolutionBundle, MediaTypeSolutionBundle)
	assert.Equal(t, mediatypes.SolutionLock, MediaTypeSolutionLock)
}
