// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestExtractExplicitResolverRefs verifies that only EXPLICIT resolver
// accessors ({{ ._.name }} / {{ ._name }}) are returned -- bare data-context
// accessors must be excluded, because they may resolve against a step's data
// keys or a forEach alias rather than a resolver.
func TestExtractExplicitResolverRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		want     []string
	}{
		{
			name:     "explicit underscore accessor",
			template: "{{ ._.greeting }}",
			want:     []string{"greeting"},
		},
		{
			name:     "bare accessor is excluded",
			template: "{{ .someDataKey }}",
			want:     nil,
		},
		{
			name:     "mixed: only explicit is returned",
			template: "{{ ._.greeting }} {{ .dataKey }}",
			want:     []string{"greeting"},
		},
		{
			name:     "special variables excluded",
			template: "{{ .__item }} {{ .__index }}",
			want:     nil,
		},
		{
			name:     "nested field uses root segment only",
			template: "{{ ._.config.host }}",
			want:     []string{"config"},
		},
		{
			name:     "deduplicated",
			template: "{{ ._.a }} {{ ._.a }}",
			want:     []string{"a"},
		},
		{
			name:     "parse error yields nil",
			template: "{{ ._.unclosed",
			want:     nil,
		},
		{
			name:     "empty template",
			template: "",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractExplicitResolverRefs(tt.template, "", "")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUnscopedResolverRefs(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []UnscopedResolverRef
	}{
		{
			name: "explicit and bare and dollar",
			tmpl: `{{ ._.a }}{{ .b }}{{ $.c }}`,
			want: []UnscopedResolverRef{
				{Name: "a", Explicit: true},
				{Name: "b", Explicit: false},
				{Name: "c", Explicit: false},
			},
		},
		{
			name: "underscore form is explicit",
			tmpl: `{{ ._d }}`,
			want: []UnscopedResolverRef{{Name: "d", Explicit: true}},
		},
		{
			name: "special variables excluded",
			tmpl: `{{ .__self }}{{ .__item }}`,
			want: nil,
		},
		{
			name: "scoped references excluded",
			tmpl: `{{ range .items }}{{ .inner }}{{ end }}`,
			want: []UnscopedResolverRef{{Name: "items", Explicit: false}},
		},
		{
			name: "same name explicit and bare both reported",
			tmpl: `{{ ._.env }}{{ .env }}`,
			want: []UnscopedResolverRef{
				{Name: "env", Explicit: true},
				{Name: "env", Explicit: false},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnscopedResolverRefs(tt.tmpl, "", "", nil)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUnscopedResolverRefs_ParseError(t *testing.T) {
	_, err := UnscopedResolverRefs(`{{ .x `, "", "", nil)
	assert.Error(t, err)
}

func TestUnscopedActionRefs(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{
			name: "single action ref with trailing path",
			tmpl: `{{ .__actions.build.results }}`,
			want: []string{"build"},
		},
		{
			name: "multiple action refs de-duplicated in first-seen order",
			tmpl: `{{ .__actions.a }}{{ .__actions.b }}{{ .__actions.a.x }}`,
			want: []string{"a", "b"},
		},
		{
			name: "bare __actions root yields nothing",
			tmpl: `{{ .__actions }}`,
			want: nil,
		},
		{
			name: "resolver and plain refs are excluded",
			tmpl: `{{ ._.env }}{{ .field }}{{ .__actions.deploy }}`,
			want: []string{"deploy"},
		},
		{
			name: "scoped action refs excluded",
			tmpl: `{{ with .ctx }}{{ .__actions.inner }}{{ end }}{{ .__actions.outer }}`,
			want: []string{"outer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnscopedActionRefs(tt.tmpl, "", "", nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUnscopedActionRefs_ParseError(t *testing.T) {
	_, err := UnscopedActionRefs(`{{ .__actions.x `, "", "", nil)
	assert.Error(t, err)
}
