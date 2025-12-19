package gotmpl

import (
	"context"
	"strings"
	"testing"
	"text/template"

	"github.com/kcloutie/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	tests := []struct {
		name         string
		defaultFuncs template.FuncMap
		wantFuncs    bool
	}{
		{
			name:         "nil funcs creates empty map",
			defaultFuncs: nil,
			wantFuncs:    false,
		},
		{
			name: "with default funcs",
			defaultFuncs: template.FuncMap{
				"test": func() string { return "test" },
			},
			wantFuncs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(tt.defaultFuncs)
			require.NotNil(t, svc)
			require.NotNil(t, svc.defaultFuncs)
			if tt.wantFuncs {
				assert.Len(t, svc.defaultFuncs, 1)
			}
		})
	}
}

func TestService_Execute_Basic(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name    string
		opts    TemplateOptions
		want    string
		wantErr bool
	}{
		{
			name: "simple template",
			opts: TemplateOptions{
				Name:    "test",
				Content: "Hello, {{.Name}}!",
				Data:    map[string]any{"Name": "World"},
			},
			want:    "Hello, World!",
			wantErr: false,
		},
		{
			name: "empty content",
			opts: TemplateOptions{
				Name:    "test",
				Content: "",
				Data:    nil,
			},
			wantErr: true,
		},
		{
			name: "no name provided",
			opts: TemplateOptions{
				Content: "Hello!",
				Data:    nil,
			},
			want:    "Hello!",
			wantErr: false,
		},
		{
			name: "template with loop",
			opts: TemplateOptions{
				Name:    "loop",
				Content: "{{range .Items}}{{.}},{{end}}",
				Data:    map[string]any{"Items": []string{"a", "b", "c"}},
			},
			want:    "a,b,c,",
			wantErr: false,
		},
		{
			name: "template with conditionals",
			opts: TemplateOptions{
				Name:    "conditional",
				Content: "{{if .Show}}visible{{else}}hidden{{end}}",
				Data:    map[string]any{"Show": true},
			},
			want:    "visible",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil)
			result, err := svc.Execute(ctx, tt.opts)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Output)
		})
	}
}

func TestService_Execute_CustomDelimiters(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name       string
		leftDelim  string
		rightDelim string
		content    string
		data       any
		want       string
	}{
		{
			name:       "default delimiters",
			leftDelim:  "",
			rightDelim: "",
			content:    "{{.Name}}",
			data:       map[string]any{"Name": "Test"},
			want:       "Test",
		},
		{
			name:       "custom delimiters",
			leftDelim:  "[[",
			rightDelim: "]]",
			content:    "[[.Name]]",
			data:       map[string]any{"Name": "Custom"},
			want:       "Custom",
		},
		{
			name:       "curly brace delimiters",
			leftDelim:  "{%",
			rightDelim: "%}",
			content:    "{%.Name%}",
			data:       map[string]any{"Name": "Jinja"},
			want:       "Jinja",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil)
			result, err := svc.Execute(ctx, TemplateOptions{
				Name:       "delim-test",
				Content:    tt.content,
				Data:       tt.data,
				LeftDelim:  tt.leftDelim,
				RightDelim: tt.rightDelim,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Output)
		})
	}
}

func TestService_Execute_CustomFunctions(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name         string
		defaultFuncs template.FuncMap
		optsFuncs    template.FuncMap
		content      string
		want         string
	}{
		{
			name: "custom function",
			optsFuncs: template.FuncMap{
				"upper": strings.ToUpper,
			},
			content: "{{upper .Name}}",
			want:    "WORLD",
		},
		{
			name: "default function",
			defaultFuncs: template.FuncMap{
				"upper": strings.ToUpper,
			},
			content: "{{upper .Name}}",
			want:    "WORLD",
		},
		{
			name: "override default with custom",
			defaultFuncs: template.FuncMap{
				"transform": strings.ToUpper,
			},
			optsFuncs: template.FuncMap{
				"transform": strings.ToLower,
			},
			content: "{{transform .Name}}",
			want:    "world",
		},
		{
			name: "multiple functions",
			optsFuncs: template.FuncMap{
				"upper": strings.ToUpper,
				"lower": strings.ToLower,
			},
			content: "{{upper .First}} {{lower .Second}}",
			want:    "HELLO world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(tt.defaultFuncs)
			result, err := svc.Execute(ctx, TemplateOptions{
				Name:    "func-test",
				Content: tt.content,
				Data: map[string]any{
					"Name":   "World",
					"First":  "hello",
					"Second": "WORLD",
				},
				Funcs: tt.optsFuncs,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Output)
		})
	}
}

func TestService_Execute_Replacements(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name         string
		content      string
		replacements []Replacement
		data         any
		want         string
		wantErr      bool
	}{
		{
			name:    "single replacement",
			content: "Hello {{.Name}}! Your age is {{AGE_PLACEHOLDER}}",
			replacements: []Replacement{
				{Find: "{{AGE_PLACEHOLDER}}", Replace: "TEMP_UUID"},
			},
			data: map[string]any{"Name": "John"},
			want: "Hello John! Your age is {{AGE_PLACEHOLDER}}",
		},
		{
			name:    "multiple replacements",
			content: "{{.Greeting}} LITERAL_1 and LITERAL_2",
			replacements: []Replacement{
				{Find: "LITERAL_1"},
				{Find: "LITERAL_2"},
			},
			data: map[string]any{"Greeting": "Hello"},
			want: "Hello LITERAL_1 and LITERAL_2",
		},
		{
			name:    "replacement not found",
			content: "{{.Name}}",
			replacements: []Replacement{
				{Find: "NOT_PRESENT"},
			},
			data: map[string]any{"Name": "Test"},
			want: "Test",
		},
		{
			name:    "empty find string",
			content: "{{.Name}}",
			replacements: []Replacement{
				{Find: ""},
			},
			data: map[string]any{"Name": "Test"},
			want: "Test",
		},
		{
			name:    "complex template with replacement",
			content: "{{range .Items}}{{.}} {{end}}KEEP_THIS",
			replacements: []Replacement{
				{Find: "KEEP_THIS", Replace: "SAFE_UUID"},
			},
			data: map[string]any{"Items": []string{"a", "b"}},
			want: "a b KEEP_THIS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil)
			result, err := svc.Execute(ctx, TemplateOptions{
				Name:         "replacement-test",
				Content:      tt.content,
				Data:         tt.data,
				Replacements: tt.replacements,
			})

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Output)
		})
	}
}

func TestService_Execute_MissingKey(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name       string
		missingKey MissingKeyOption
		content    string
		data       any
		wantErr    bool
		want       string
	}{
		{
			name:       "default behavior - no error",
			missingKey: MissingKeyDefault,
			content:    "{{.Missing}}",
			data:       map[string]any{},
			wantErr:    false,
			want:       "<no value>",
		},
		{
			name:       "zero - returns zero value",
			missingKey: MissingKeyZero,
			content:    "{{.Missing}}",
			data:       map[string]any{},
			wantErr:    false,
			want:       "<no value>", // Still shows <no value> for non-existent keys
		},
		{
			name:       "error - stops execution",
			missingKey: MissingKeyError,
			content:    "{{.Missing}}",
			data:       map[string]any{},
			wantErr:    true,
		},
		{
			name:       "empty uses default",
			missingKey: "",
			content:    "{{.Missing}}",
			data:       map[string]any{},
			wantErr:    false,
			want:       "<no value>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil)
			result, err := svc.Execute(ctx, TemplateOptions{
				Name:       "missing-key-test",
				Content:    tt.content,
				Data:       tt.data,
				MissingKey: tt.missingKey,
			})

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Output)
		})
	}
}

func TestService_Execute_DisableBuiltinFuncs(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	defaultFuncs := template.FuncMap{
		"myFunc": func(s string) string { return "modified: " + s },
	}

	t.Run("with builtin funcs enabled", func(t *testing.T) {
		svc := NewService(defaultFuncs)
		result, err := svc.Execute(ctx, TemplateOptions{
			Name:                "test",
			Content:             "{{myFunc .Value}}",
			Data:                map[string]any{"Value": "test"},
			DisableBuiltinFuncs: false,
		})

		require.NoError(t, err)
		assert.Equal(t, "modified: test", result.Output)
	})

	t.Run("with builtin funcs disabled", func(t *testing.T) {
		svc := NewService(defaultFuncs)
		_, err := svc.Execute(ctx, TemplateOptions{
			Name:                "test",
			Content:             "{{myFunc .Value}}",
			Data:                map[string]any{"Value": "test"},
			DisableBuiltinFuncs: true,
		})

		// Should fail because myFunc is not available
		assert.Error(t, err)
	})
}

func TestService_Execute_ComplexScenario(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Complex scenario combining multiple features
	svc := NewService(template.FuncMap{
		"default": func(s string) string {
			if s == "" {
				return "default-value"
			}
			return s
		},
	})

	result, err := svc.Execute(ctx, TemplateOptions{
		Name:       "complex.tmpl",
		Content:    "Name: [[.Name]], Age: [[.Age]], Literal: KEEP_ME, Default: [[default .Empty]]",
		LeftDelim:  "[[",
		RightDelim: "]]",
		Data: map[string]any{
			"Name":  "Alice",
			"Age":   30,
			"Empty": "",
		},
		Replacements: []Replacement{
			{Find: "KEEP_ME", Replace: "TEMP_LITERAL"},
		},
		Funcs: template.FuncMap{
			"custom": strings.ToUpper,
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Name: Alice, Age: 30, Literal: KEEP_ME, Default: default-value", result.Output)
	assert.Equal(t, "complex.tmpl", result.TemplateName)
}

func TestExecute_ConvenienceFunction(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	result, err := Execute(ctx, TemplateOptions{
		Name:    "convenience-test",
		Content: "Hello, {{.Name}}!",
		Data:    map[string]any{"Name": "World"},
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result.Output)
	assert.Equal(t, "convenience-test", result.TemplateName)
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "shorter than max",
			input:  "short",
			maxLen: 10,
			want:   "short",
		},
		{
			name:   "equal to max",
			input:  "exactly10c",
			maxLen: 10,
			want:   "exactly10c",
		},
		{
			name:   "longer than max",
			input:  "this is a very long string",
			maxLen: 10,
			want:   "this is a ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_Execute_ErrorCases(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tests := []struct {
		name    string
		opts    TemplateOptions
		wantErr string
	}{
		{
			name: "invalid template syntax",
			opts: TemplateOptions{
				Name:    "invalid",
				Content: "{{.Name",
				Data:    map[string]any{},
			},
			wantErr: "parse error",
		},
		{
			name: "undefined function",
			opts: TemplateOptions{
				Name:    "undefined-func",
				Content: "{{undefinedFunc .Name}}",
				Data:    map[string]any{"Name": "test"},
			},
			wantErr: "parse error",
		},
		{
			name: "execution error - wrong data type",
			opts: TemplateOptions{
				Name:    "type-error",
				Content: "{{range .Items}}{{.}}{{end}}",
				Data:    map[string]any{"Items": "not-a-slice"},
			},
			wantErr: "execution error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil)
			_, err := svc.Execute(ctx, tt.opts)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestService_Execute_ReplacementEdgeCases(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	t.Run("replacement removed by template logic", func(t *testing.T) {
		svc := NewService(nil)
		result, err := svc.Execute(ctx, TemplateOptions{
			Name:    "removed-replacement",
			Content: "{{if .Show}}LITERAL{{end}}",
			Data:    map[string]any{"Show": false},
			Replacements: []Replacement{
				{Find: "LITERAL", Replace: "UUID_TEST"},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, "", result.Output)
		// Replacement was made but placeholder was removed by template logic
		assert.Equal(t, 1, result.ReplacementsMade)
	})

	t.Run("multiple occurrences of same replacement", func(t *testing.T) {
		svc := NewService(nil)
		result, err := svc.Execute(ctx, TemplateOptions{
			Name:    "multi-occurrence",
			Content: "LITERAL and LITERAL again",
			Data:    nil,
			Replacements: []Replacement{
				{Find: "LITERAL", Replace: "TEMP"},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, "LITERAL and LITERAL again", result.Output)
	})
}
