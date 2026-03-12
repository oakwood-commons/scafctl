// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDataContext(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		file    string
		want    any
		wantErr string
	}{
		{
			name: "both empty returns nil",
			want: nil,
		},
		{
			name:    "both set returns error",
			data:    `{"a":1}`,
			file:    "some.json",
			wantErr: "cannot use both --data and --file",
		},
		{
			name: "inline JSON object",
			data: `{"items":[1,2,3]}`,
			want: map[string]any{"items": []any{float64(1), float64(2), float64(3)}},
		},
		{
			name: "inline YAML",
			data: "name: hello\ncount: 42",
			want: map[string]any{"name": "hello", "count": 42},
		},
		{
			name:    "invalid data",
			data:    "{not valid",
			wantErr: "data is not valid JSON or YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildDataContext(tt.data, tt.file)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildDataContext_File(t *testing.T) {
	dir := t.TempDir()

	jsonFile := filepath.Join(dir, "data.json")
	require.NoError(t, os.WriteFile(jsonFile, []byte(`{"key":"val"}`), 0o644))

	got, err := BuildDataContext("", jsonFile)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"key": "val"}, got)
}

func TestLoadDataFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("json file", func(t *testing.T) {
		p := filepath.Join(dir, "test.json")
		require.NoError(t, os.WriteFile(p, []byte(`[1,2,3]`), 0o644))

		got, err := LoadDataFile(p)
		require.NoError(t, err)
		assert.Equal(t, []any{float64(1), float64(2), float64(3)}, got)
	})

	t.Run("yaml file", func(t *testing.T) {
		p := filepath.Join(dir, "test.yaml")
		require.NoError(t, os.WriteFile(p, []byte("items:\n  - a\n  - b\n"), 0o644))

		got, err := LoadDataFile(p)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"items": []any{"a", "b"}}, got)
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadDataFile(filepath.Join(dir, "nope.json"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading file")
	})

	t.Run("invalid content", func(t *testing.T) {
		p := filepath.Join(dir, "bad.txt")
		require.NoError(t, os.WriteFile(p, []byte("{bad json"), 0o644))

		_, err := LoadDataFile(p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data is not valid JSON or YAML")
	})
}

func TestParseVars(t *testing.T) {
	tests := []struct {
		name    string
		vars    []string
		want    map[string]any
		wantErr string
	}{
		{
			name: "nil returns nil",
			vars: nil,
			want: nil,
		},
		{
			name: "empty returns nil",
			vars: []string{},
			want: nil,
		},
		{
			name: "simple string",
			vars: []string{"name=hello"},
			want: map[string]any{"name": "hello"},
		},
		{
			name: "multiple vars",
			vars: []string{"a=1", "b=two"},
			want: map[string]any{"a": float64(1), "b": "two"},
		},
		{
			name: "JSON value",
			vars: []string{`items=[1,2,3]`},
			want: map[string]any{"items": []any{float64(1), float64(2), float64(3)}},
		},
		{
			name: "value with equals sign",
			vars: []string{"expr=a==b"},
			want: map[string]any{"expr": "a==b"},
		},
		{
			name:    "missing equals",
			vars:    []string{"novalue"},
			wantErr: "invalid variable format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVars(tt.vars)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkBuildDataContext(b *testing.B) {
	data := `{"items":[1,2,3],"nested":{"key":"value"}}`
	for b.Loop() {
		_, _ = BuildDataContext(data, "")
	}
}

func BenchmarkParseVars(b *testing.B) {
	vars := []string{"name=hello", "count=42", `items=[1,2,3]`}
	for b.Loop() {
		_, _ = ParseVars(vars)
	}
}

func BenchmarkLoadDataFile(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "bench.json")
	_ = os.WriteFile(p, []byte(`{"items":[1,2,3],"nested":{"key":"value"}}`), 0o644)

	b.ResetTimer()
	for b.Loop() {
		_, _ = LoadDataFile(p)
	}
}
