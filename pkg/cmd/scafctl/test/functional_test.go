// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandFunctional(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	require.NotNil(t, cmd)
	assert.Equal(t, "functional [reference]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "func")
	assert.Contains(t, cmd.Aliases, "fn")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandFunctional_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	tests := []struct {
		name     string
		flagName string
	}{
		{"file", "file"},
		{"tests-path", "tests-path"},
		{"output", "output"},
		{"report-file", "report-file"},
		{"update-snapshots", "update-snapshots"},
		{"sequential", "sequential"},
		{"concurrency", "concurrency"},
		{"skip-builtins", "skip-builtins"},
		{"test-timeout", "test-timeout"},
		{"timeout", "timeout"},
		{"filter", "filter"},
		{"tag", "tag"},
		{"solution", "solution"},
		{"dry-run", "dry-run"},
		{"fail-fast", "fail-fast"},
		{"verbose", "verbose"},
		{"keep-sandbox", "keep-sandbox"},
		{"no-progress", "no-progress"},
		{"watch", "watch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			assert.NotNil(t, f, "flag %q should exist", tt.flagName)
		})
	}
}

func TestCommandFunctional_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandFunctional(cliParams, ioStreams, "scafctl/test")

	shorthands := map[string]string{
		"f": "file",
		"o": "output",
		"j": "concurrency",
		"v": "verbose",
		"w": "watch",
	}

	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func BenchmarkCommandFunctional(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandFunctional(cliParams, ioStreams, "scafctl/test")
	}
}
