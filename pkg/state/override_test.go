// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigForFile(t *testing.T) {
	t.Parallel()

	cfg, err := ConfigForFile("intent/sandbox.json", FormatIntent)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Enabled is a literal true.
	require.NotNil(t, cfg.Enabled)
	assert.Equal(t, true, cfg.Enabled.Literal)

	// Backend is the file provider pointed at the given path, in the given format.
	assert.Equal(t, FileBackendProvider, cfg.Backend.Provider)
	assert.Equal(t, FormatIntent, cfg.Backend.Format)
	require.Contains(t, cfg.Backend.Inputs, BackendInputPath)
	assert.Equal(t, "intent/sandbox.json", cfg.Backend.Inputs[BackendInputPath].Literal)

	// A run pointed at an explicit file is a complete substitution, not an
	// additional output -- no Emit targets.
	assert.Empty(t, cfg.Emit)
}

func TestConfigForFile_EmptyFormatMeansFull(t *testing.T) {
	t.Parallel()

	cfg, err := ConfigForFile("state.json", "")
	require.NoError(t, err)
	assert.Empty(t, cfg.Backend.Format, "an empty format is stored as-is and treated as full by projectState")
}

func TestConfigForFile_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := ConfigForFile("", FormatFull)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestDescribeOverride_NilConfigIsNotAnOverride(t *testing.T) {
	t.Parallel()

	info := DescribeOverride(nil)
	assert.False(t, info.Overridden)
	assert.Empty(t, info.PreviousProvider)
}

func TestDescribeOverride_ReportsPreviousProvider(t *testing.T) {
	t.Parallel()

	declared := &Config{
		Backend: Backend{Provider: "github"},
	}
	info := DescribeOverride(declared)
	assert.True(t, info.Overridden)
	assert.Equal(t, "github", info.PreviousProvider)
	assert.Zero(t, info.DroppedEmits, "a declared config with no Emit targets drops none")
}

func TestDescribeOverride_ReportsDroppedEmits(t *testing.T) {
	t.Parallel()

	declared := &Config{
		Backend: Backend{Provider: "file"},
		Emit: []EmitTarget{
			{Backend: Backend{Provider: "file", Format: FormatIntent}},
			{Backend: Backend{Provider: "http", Format: FormatIntent}},
		},
	}
	info := DescribeOverride(declared)
	assert.True(t, info.Overridden)
	assert.Equal(t, 2, info.DroppedEmits, "--state-file drops every configured Emit target")
}
