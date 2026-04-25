// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"testing"
	"text/template/parse"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGoTemplateReferences(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		leftDelim  string
		rightDelim string
		want       []string // Just the paths for easier comparison
		wantErr    bool
	}{
		{
			name:     "simple field reference",
			template: "{{.Name}}",
			want:     []string{".Name"},
		},
		{
			name:     "multiple field references",
			template: "{{.Name}} {{.Age}} {{.Email}}",
			want:     []string{".Name", ".Age", ".Email"},
		},
		{
			name:     "nested field reference",
			template: "{{.User.Name}}",
			want:     []string{".User.Name"},
		},
		{
			name:     "deeply nested field reference",
			template: "{{.User.Profile.Address.City}}",
			want:     []string{".User.Profile.Address.City"},
		},
		{
			name:     "range over slice",
			template: "{{range .Items}}{{.}}{{end}}",
			want:     []string{".Items"},
		},
		{
			name:     "range with nested field",
			template: "{{range .Users}}{{.Name}}{{end}}",
			want:     []string{".Users", ".Name"},
		},
		{
			name:     "if condition with field",
			template: "{{if .IsActive}}active{{end}}",
			want:     []string{".IsActive"},
		},
		{
			name:     "if-else with fields",
			template: "{{if .Show}}{{.Content}}{{else}}{{.Default}}{{end}}",
			want:     []string{".Show", ".Content", ".Default"},
		},
		{
			name:     "with statement",
			template: "{{with .User}}{{.Name}}{{end}}",
			want:     []string{".User", ".Name"},
		},
		{
			name:       "custom delimiters",
			template:   "[[.Name]] [[.Age]]",
			leftDelim:  "[[",
			rightDelim: "]]",
			want:       []string{".Name", ".Age"},
		},
		{
			name:     "duplicate references removed",
			template: "{{.Name}} {{.Name}} {{.Name}}",
			want:     []string{".Name"},
		},
		{
			name: "complex template",
			template: `{{if .User.IsAdmin}}
				Admin: {{.User.Name}}
				{{range .User.Permissions}}
					- {{.}}
				{{end}}
			{{else}}
				User: {{.User.Name}}
			{{end}}`,
			want: []string{".User.IsAdmin", ".User.Name", ".User.Permissions"},
		},
		{
			name:     "empty template fails",
			template: "",
			wantErr:  true,
		},
		{
			name:     "template with only text",
			template: "Hello, World!",
			want:     []string{},
		},
		{
			name:     "chained method calls",
			template: "{{.User.Name.ToUpper}}",
			want:     []string{".User.Name.ToUpper"},
		},
		{
			name:     "dot in range",
			template: "{{range .Items}}{{.Name}}{{end}}",
			want:     []string{".Items", ".Name"},
		},
		{
			name:     "invalid template syntax",
			template: "{{.Name",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGoTemplateReferences(tt.template, tt.leftDelim, tt.rightDelim)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Extract just the paths for comparison
			gotPaths := make([]string, len(got))
			for i, ref := range got {
				gotPaths[i] = ref.Path
			}

			assert.ElementsMatch(t, tt.want, gotPaths, "Expected paths to match (order-independent)")
		})
	}
}

func TestGetGoTemplateReferences_DefaultDelimiters(t *testing.T) {
	// Test that empty delimiters use defaults
	template := "{{.Name}}"
	refs, err := GetGoTemplateReferences(template, "", "")

	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, ".Name", refs[0].Path)
}

func TestGetGoTemplateReferences_PositionInfo(t *testing.T) {
	template := "{{.Name}}"
	refs, err := GetGoTemplateReferences(template, "", "")

	require.NoError(t, err)
	require.Len(t, refs, 1)

	// Position should be present
	assert.NotEmpty(t, refs[0].Position)
	assert.Contains(t, refs[0].Position, "pos:")
}

func TestGetGoTemplateReferences_ComplexNesting(t *testing.T) {
	template := `
{{if .Config.Debug}}
	Debug Mode: {{.Config.Level}}
	{{range .Config.Loggers}}
		Logger: {{.Name}} - Level: {{.Level}}
		{{if .Enabled}}
			Output: {{.Output}}
		{{end}}
	{{end}}
{{end}}
`
	refs, err := GetGoTemplateReferences(template, "", "")

	require.NoError(t, err)

	gotPaths := make([]string, len(refs))
	for i, ref := range refs {
		gotPaths[i] = ref.Path
	}

	expected := []string{
		".Config.Debug",
		".Config.Level",
		".Config.Loggers",
		".Name",
		".Level",
		".Enabled",
		".Output",
	}

	assert.ElementsMatch(t, expected, gotPaths)
}

func TestGetGoTemplateReferences_WithVariables(t *testing.T) {
	// Template variables ($var) should not be included
	template := `{{$user := .User}}{{$user.Name}}`
	refs, err := GetGoTemplateReferences(template, "", "")

	require.NoError(t, err)

	gotPaths := make([]string, len(refs))
	for i, ref := range refs {
		gotPaths[i] = ref.Path
	}

	// Should only contain .User, not $user
	assert.Contains(t, gotPaths, ".User")
	assert.NotContains(t, gotPaths, "$user")
	assert.NotContains(t, gotPaths, "$user.Name")
}

func TestTemplateReference(t *testing.T) {
	ref := TemplateReference{
		Path:     ".User.Name",
		Position: "pos:123",
	}

	assert.Equal(t, ".User.Name", ref.Path)
	assert.Equal(t, "pos:123", ref.Position)
}

func TestService_GetReferences(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	svc := NewService(nil)

	tests := []struct {
		name    string
		opts    TemplateOptions
		want    []string
		wantErr bool
	}{
		{
			name: "simple reference with custom delimiters",
			opts: TemplateOptions{
				Name:       "test.tmpl",
				Content:    "[[.User.Name]]",
				LeftDelim:  "[[",
				RightDelim: "]]",
			},
			want: []string{".User.Name"},
		},
		{
			name: "multiple references with defaults",
			opts: TemplateOptions{
				Name:    "test.tmpl",
				Content: "{{.Name}} {{.Age}}",
			},
			want: []string{".Name", ".Age"},
		},
		{
			name: "empty content fails",
			opts: TemplateOptions{
				Name:    "test.tmpl",
				Content: "",
			},
			wantErr: true,
		},
		{
			name: "no name uses default",
			opts: TemplateOptions{
				Content: "{{.Test}}",
			},
			want: []string{".Test"},
		},
		{
			name: "complex template",
			opts: TemplateOptions{
				Name: "complex.tmpl",
				Content: `{{range .Items}}
					{{.Name}} - {{.Price}}
				{{end}}`,
			},
			want: []string{".Items", ".Name", ".Price"},
		},
		{
			name: "builtin index function with __actions",
			opts: TemplateOptions{
				Name: "actions.tmpl",
				Content: `{{ range (index .__actions "write-files" "results" "filesStatus") }}  - {{ .path }}
{{ end }}`,
			},
			want: []string{".__actions", ".path"},
		},
		{
			name: "builtin len function",
			opts: TemplateOptions{
				Name:    "len.tmpl",
				Content: `{{ len .Items }} items: {{ .Title }}`,
			},
			want: []string{".Items", ".Title"},
		},
		{
			name: "builtin printf function",
			opts: TemplateOptions{
				Name:    "printf.tmpl",
				Content: `{{ printf "%s-%s" .Env .Region }}`,
			},
			want: []string{".Env", ".Region"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := svc.GetReferences(ctx, tt.opts)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			gotPaths := make([]string, len(refs))
			for i, ref := range refs {
				gotPaths[i] = ref.Path
			}

			assert.ElementsMatch(t, tt.want, gotPaths)
		})
	}
}

func TestExtractBasePath(t *testing.T) {
	t.Run("FieldNode returns dot-prefixed path", func(t *testing.T) {
		node := &parse.FieldNode{Ident: []string{"Items"}}
		result := extractBasePath(node)
		assert.Equal(t, ".Items", result)
	})

	t.Run("VariableNode returns empty string", func(t *testing.T) {
		node := &parse.VariableNode{Ident: []string{"$var"}}
		result := extractBasePath(node)
		assert.Equal(t, "", result)
	})

	t.Run("DotNode returns dot", func(t *testing.T) {
		node := &parse.DotNode{}
		result := extractBasePath(node)
		assert.Equal(t, ".", result)
	})

	t.Run("Other node type returns empty string", func(t *testing.T) {
		node := &parse.NilNode{}
		result := extractBasePath(node)
		assert.Equal(t, "", result)
	})
}

func TestBuildFieldPath_Empty(t *testing.T) {
	result := buildFieldPath(nil)
	assert.Equal(t, "", result)
}

func TestBuildFieldPath_Single(t *testing.T) {
	result := buildFieldPath([]string{"Name"})
	assert.Equal(t, ".Name", result)
}

func TestWalkArg_ChainNode(t *testing.T) {
	// {{(.User).Name}} produces a ChainNode in the template AST
	refs, err := GetGoTemplateReferences("{{(.User).Name}}", "", "")
	require.NoError(t, err)
	paths := make([]string, len(refs))
	for i, r := range refs {
		paths[i] = r.Path
	}
	// ChainNode branch should extract the chained field path
	assert.NotEmpty(t, refs)
}

func TestWalkArg_PipeNodeArg(t *testing.T) {
	// {{(.Name)}} produces a PipeNode as a command argument (parenthesized sub-pipeline)
	refs, err := GetGoTemplateReferences("{{(.Name)}}", "", "")
	require.NoError(t, err)
	paths := make([]string, len(refs))
	for i, r := range refs {
		paths[i] = r.Path
	}
	// The nested PipeNode branch should find .Name
	assert.Contains(t, paths, ".Name")
}
