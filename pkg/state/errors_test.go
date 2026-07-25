// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCycleError_Error(t *testing.T) {
	err := &CycleError{Location: "state.enabled", Refs: []string{"saved", "region"}}
	msg := err.Error()
	assert.Contains(t, msg, "state.enabled")
	assert.Contains(t, msg, "saved, region")
	assert.Contains(t, msg, "circular dependency")
}

func TestUnknownStateRefError_Error(t *testing.T) {
	err := &UnknownStateRefError{Location: "state.backend.inputs.path", Refs: []string{"typo"}}
	msg := err.Error()
	assert.Contains(t, msg, "state.backend.inputs.path")
	assert.Contains(t, msg, "typo")
	assert.Contains(t, msg, "no such resolver")
}
