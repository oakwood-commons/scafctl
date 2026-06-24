// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRewriteAliasArgs(t *testing.T) {
	tests := []struct {
		name          string
		argv          []string
		wantRewritten []string
		wantAlias     bool
	}{
		{
			name:          "empty argv",
			argv:          nil,
			wantRewritten: nil,
			wantAlias:     false,
		},
		{
			name:          "non-alias binary",
			argv:          []string{"/usr/local/bin/scafctl", "credential-helper", "get"},
			wantRewritten: []string{"/usr/local/bin/scafctl", "credential-helper", "get"},
			wantAlias:     false,
		},
		{
			name:          "alias get",
			argv:          []string{"/home/u/.local/bin/docker-credential-scafctl", "get"},
			wantRewritten: []string{"/home/u/.local/bin/docker-credential-scafctl", "credential-helper", "get"},
			wantAlias:     true,
		},
		{
			name:          "alias store",
			argv:          []string{"docker-credential-scafctl", "store"},
			wantRewritten: []string{"docker-credential-scafctl", "credential-helper", "store"},
			wantAlias:     true,
		},
		{
			name:          "alias erase",
			argv:          []string{"docker-credential-scafctl", "erase"},
			wantRewritten: []string{"docker-credential-scafctl", "credential-helper", "erase"},
			wantAlias:     true,
		},
		{
			name:          "alias list",
			argv:          []string{"docker-credential-scafctl", "list"},
			wantRewritten: []string{"docker-credential-scafctl", "credential-helper", "list"},
			wantAlias:     true,
		},
		{
			name:          "alias with no verb",
			argv:          []string{"docker-credential-scafctl"},
			wantRewritten: []string{"docker-credential-scafctl", "credential-helper"},
			wantAlias:     true,
		},
		{
			name:          "embedder binary name",
			argv:          []string{"/opt/mybin/docker-credential-mybin", "get"},
			wantRewritten: []string{"/opt/mybin/docker-credential-mybin", "credential-helper", "get"},
			wantAlias:     true,
		},
		{
			name:          "windows exe suffix",
			argv:          []string{"docker-credential-mybin.exe", "get"},
			wantRewritten: []string{"docker-credential-mybin.exe", "credential-helper", "get"},
			wantAlias:     true,
		},
		{
			name:          "unknown verb still routed",
			argv:          []string{"docker-credential-scafctl", "bogus"},
			wantRewritten: []string{"docker-credential-scafctl", "credential-helper", "bogus"},
			wantAlias:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRewritten, gotAlias := RewriteAliasArgs(tt.argv)
			assert.Equal(t, tt.wantAlias, gotAlias)
			assert.Equal(t, tt.wantRewritten, gotRewritten)
		})
	}
}

// TestRewriteAliasArgs_DoesNotMutateInput ensures the input slice is not
// modified in place when an alias is detected.
func TestRewriteAliasArgs_DoesNotMutateInput(t *testing.T) {
	argv := []string{"docker-credential-scafctl", "get"}
	original := make([]string, len(argv))
	copy(original, argv)

	_, isAlias := RewriteAliasArgs(argv)

	assert.True(t, isAlias)
	assert.Equal(t, original, argv, "input argv must not be mutated")
}
