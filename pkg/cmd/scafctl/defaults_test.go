// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"testing"
)

func TestRegisterDefaults(t *testing.T) {
	t.Parallel()

	// RegisterDefaults should not panic and should be idempotent.
	RegisterDefaults()
	RegisterDefaults() // call twice to verify idempotency
}
