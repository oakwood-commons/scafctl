// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTemplatePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
		wantKind pathKind
	}{
		{"explicit resolver dotted", "._.config.host", "config", pathExplicitResolver},
		{"explicit resolver root", "._.name", "name", pathExplicitResolver},
		{"underscore alias form", "._name", "name", pathExplicitResolver},
		{"special self", ".__self", "", pathSpecial},
		{"special item", ".__item", "", pathSpecial},
		{"special actions", ".__actions.write", "", pathSpecial},
		{"bare field root", ".appName", "appName", pathField},
		{"bare field nested", ".config.host", "config", pathField},
		{"no leading dot", "appName", "", pathSpecial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, kind := classifyTemplatePath(tt.path)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantKind, kind)
		})
	}
}

func TestFirstSegment(t *testing.T) {
	assert.Equal(t, "config", firstSegment("config.host.port"))
	assert.Equal(t, "name", firstSegment("name"))
	assert.Equal(t, "", firstSegment(""))
}

func TestExtractResolverDeps(t *testing.T) {
	tests := []struct {
		name string
		in   ResolverDepsInput
		want []string
	}{
		{
			name: "no data input scans bare fields",
			in: ResolverDepsInput{
				Template: `{{ .foo }} and {{ .bar.baz }}`,
			},
			want: []string{"foo", "bar"},
		},
		{
			name: "explicit resolver ref always a dep",
			in: ResolverDepsInput{
				Template: `{{ ._.token }}`,
			},
			want: []string{"token"},
		},
		{
			name: "explicit resolver ref a dep even with data input",
			in: ResolverDepsInput{
				Template:         `{{ ._.token }} {{ .local }}`,
				HasDataInput:     true,
				DataKeys:         map[string]bool{"local": true},
				DataKeysComplete: true,
			},
			want: []string{"token"},
		},
		{
			name: "data input with known keys excludes those keys",
			in: ResolverDepsInput{
				Template:         `{{ .appName }} {{ .other }}`,
				HasDataInput:     true,
				DataKeys:         map[string]bool{"appName": true},
				DataKeysComplete: true,
			},
			// appName is a data key (excluded); other is not, so it still
			// resolves against resolver data.
			want: []string{"other"},
		},
		{
			name: "data input with dynamic keys skips bare fields",
			in: ResolverDepsInput{
				Template:         `{{ .appName }} {{ ._.token }}`,
				HasDataInput:     true,
				DataKeysComplete: false,
			},
			want: []string{"token"},
		},
		{
			name: "forEach alias excluded",
			in: ResolverDepsInput{
				Template: `{{ .proj.name }} {{ .env }}`,
				Aliases:  map[string]bool{"proj": true},
			},
			want: []string{"env"},
		},
		{
			name: "special variables skipped",
			in: ResolverDepsInput{
				Template: `{{ .__self }} {{ .__item }} {{ .real }}`,
			},
			want: []string{"real"},
		},
		{
			name: "de-dup preserves first-seen order",
			in: ResolverDepsInput{
				Template: `{{ .a }} {{ .b }} {{ .a }}`,
			},
			want: []string{"a", "b"},
		},
		{
			name: "custom delimiters",
			in: ResolverDepsInput{
				Template:   `<< .foo >>`,
				LeftDelim:  "<<",
				RightDelim: ">>",
			},
			want: []string{"foo"},
		},
		{
			name: "parse error yields nil",
			in: ResolverDepsInput{
				Template: `{{ .foo `,
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractResolverDeps(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}
