// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package dns

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple lowercase",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "mixed case",
			input: "Hello World",
			want:  "hello-world",
		},
		{
			name:  "underscores and special chars",
			input: "hello_world@2024!",
			want:  "hello-world-2024",
		},
		{
			name:  "consecutive special characters",
			input: "hello---world___test",
			want:  "hello-world-test",
		},
		{
			name:  "leading and trailing special chars",
			input: "---hello---",
			want:  "hello",
		},
		{
			name:  "unicode characters",
			input: "h\u00e9llo w\u00f6rld caf\u00e9",
			want:  "h-llo-w-rld-caf",
		},
		{
			name:  "all special characters",
			input: "@#$%^&*()",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "already valid DNS label",
			input: "my-valid-label",
			want:  "my-valid-label",
		},
		{
			name:  "digits only",
			input: "12345",
			want:  "12345",
		},
		{
			name:  "leading digits",
			input: "123-abc",
			want:  "123-abc",
		},
		{
			name:  "max length truncation",
			input: strings.Repeat("a", 100),
			want:  strings.Repeat("a", 63),
		},
		{
			name:  "truncation at hyphen boundary",
			input: strings.Repeat("a", 63) + "-extra",
			want:  strings.Repeat("a", 63),
		},
		{
			name:  "truncation creates trailing hyphen",
			input: strings.Repeat("a", 62) + "--extra",
			want:  strings.Repeat("a", 62),
		},
		{
			name:  "dots replaced",
			input: "my.service.name",
			want:  "my-service-name",
		},
		{
			name:  "slashes replaced",
			input: "org/repo/name",
			want:  "org-repo-name",
		},
		{
			name:  "CamelCase",
			input: "MyApplicationName",
			want:  "myapplicationname",
		},
		{
			name:  "kubernetes namespace style",
			input: "My Kube_Namespace",
			want:  "my-kube-namespace",
		},
		{
			name:  "github org style",
			input: "My-GitHub_Org.Name",
			want:  "my-github-org-name",
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
