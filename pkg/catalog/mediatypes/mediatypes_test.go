package mediatypes

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWireContract pins the exact media-type strings. These values are part of
// the on-the-wire OCI artifact contract: changing them breaks compatibility
// with already-published artifacts, so this test forces any change to be
// deliberate.
func TestWireContract(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "application/vnd.scafctl.solution.bundle.v1+tar", SolutionBundle)
	assert.Equal(t, "application/vnd.scafctl.solution.lock.v1+json", SolutionLock)
}
