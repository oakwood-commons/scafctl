// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
)

func TestCommandProvider(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandProvider(cliParams, streams, "")

	assert.Equal(t, "provider <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Contains(t, cmd.Aliases, "prov")
	assert.Contains(t, cmd.Aliases, "p")
}

func TestExtractProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"empty args", nil, ""},
		{"single provider name", []string{"http"}, "http"},
		{"skips flags", []string{"--dry-run", "http"}, "http"},
		{"skips help", []string{"help", "http"}, "http"},
		{"first non-flag wins", []string{"http", "extra"}, "http"},
		{"flags only", []string{"--help", "--dry-run"}, ""},
		{"mixed flags and name", []string{"-i", "static", "--dry-run"}, "static"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractProviderName(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractProviderNameFromOSArgs(t *testing.T) {
	tests := []struct {
		name     string
		osArgs   []string
		expected string
	}{
		{
			"standard invocation",
			[]string{"scafctl", "run", "provider", "http", "--help"},
			"http",
		},
		{
			"prov alias",
			[]string{"scafctl", "run", "prov", "static", "--help"},
			"static",
		},
		{
			"p alias",
			[]string{"scafctl", "run", "p", "env", "--help"},
			"env",
		},
		{
			"no provider name",
			[]string{"scafctl", "run", "provider", "--help"},
			"",
		},
		{
			"provider not in args",
			[]string{"scafctl", "run", "resolver", "--help"},
			"",
		},
		{
			"flag before name",
			[]string{"scafctl", "run", "provider", "--dry-run", "http"},
			"http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore os.Args
			originalArgs := os.Args
			t.Cleanup(func() { os.Args = originalArgs })
			os.Args = tt.osArgs

			result := extractProviderNameFromOSArgs()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkExtractProviderName(b *testing.B) {
	args := []string{"--dry-run", "-i", "http", "--capability", "from"}
	for b.Loop() {
		extractProviderName(args)
	}
}

func BenchmarkExtractProviderNameFromOSArgs(b *testing.B) {
	originalArgs := os.Args
	b.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"scafctl", "run", "provider", "http", "--help"}

	b.ResetTimer()
	for b.Loop() {
		extractProviderNameFromOSArgs()
	}
}
