// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package dns

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSlugify verifies the dns package delegates to dnslabel.Slugify. The
// exhaustive transformation cases live in pkg/dnslabel; here we only guard the
// delegation so the template function stays wired to the shared implementation.
func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mixed case and special chars",
			input: "My-GitHub_Org.Name",
			want:  "my-github-org-name",
		},
		{
			name:  "max length truncation",
			input: strings.Repeat("a", 100),
			want:  strings.Repeat("a", 63),
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSlugifyFunc(t *testing.T) {
	f := SlugifyFunc()
	assert.Equal(t, "slugify", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.Contains(t, f.Func, "slugify")
}

func TestToDNSStringFunc(t *testing.T) {
	f := ToDNSStringFunc()
	assert.Equal(t, "toDnsString", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.Contains(t, f.Func, "toDnsString")
}
