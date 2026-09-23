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
	err := &UnknownStateRefError{Location: "state.load.inputs.path", Refs: []string{"typo"}}
	msg := err.Error()
	assert.Contains(t, msg, "state.load.inputs.path")
	assert.Contains(t, msg, "typo")
	assert.Contains(t, msg, "no such resolver")
}

func TestNotFoundError_Error(t *testing.T) {
	withLocation := &NotFoundError{Location: "intent.json", Provider: "file"}
	assert.Equal(t, `state file "intent.json" does not exist`, withLocation.Error())

	withoutLocation := &NotFoundError{Provider: "github"}
	assert.Equal(t, "no state exists at the github load provider", withoutLocation.Error())
}

func TestMissingLocksError_Error(t *testing.T) {
	withLocation := &MissingLocksError{Location: "intent.json", Provider: "file", Resolvers: []string{"cluster_id", "project_id"}}
	msg := withLocation.Error()
	assert.Contains(t, msg, `state "intent.json" has parameters but no immutable locks`)
	assert.Contains(t, msg, "[cluster_id, project_id]")
	assert.Contains(t, msg, "would be re-derived instead of replayed from a saved lock")

	withoutLocation := &MissingLocksError{Provider: "github", Resolvers: []string{"cluster_id"}}
	assert.Contains(t, withoutLocation.Error(), "state from the github load provider has parameters")
}
