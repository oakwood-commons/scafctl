// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParamEqualities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		expr string
		want map[string][]any
		ok   bool
	}{
		{
			name: "simple equality string",
			expr: `_.action == "refresh"`,
			want: map[string][]any{"action": {"refresh"}},
			ok:   true,
		},
		{
			name: "reversed operand order",
			expr: `"refresh" == _.action`,
			want: map[string][]any{"action": {"refresh"}},
			ok:   true,
		},
		{
			name: "membership in list",
			expr: `_.action in ["show", "refresh", "json"]`,
			want: map[string][]any{"action": {"json", "refresh", "show"}},
			ok:   true,
		},
		{
			name: "OR of equalities same var",
			expr: `_.env == "prod" || _.env == "staging"`,
			want: map[string][]any{"env": {"prod", "staging"}},
			ok:   true,
		},
		{
			name: "AND across different vars",
			expr: `_.action == "deploy" && _.env == "prod"`,
			want: map[string][]any{"action": {"deploy"}, "env": {"prod"}},
			ok:   true,
		},
		{
			name: "int and bool literals",
			expr: `_.count == 3 || _.enabled == true`,
			want: map[string][]any{"count": {int64(3)}, "enabled": {true}},
			ok:   true,
		},
		{
			name: "dedupe repeated values",
			expr: `_.env == "prod" || _.env == "prod"`,
			want: map[string][]any{"env": {"prod"}},
			ok:   true,
		},
		{
			name: "nested field is not attributed to top-level param",
			expr: `_.config.mode == "fast"`,
			ok:   false,
		},
		// Unsupported / graceful-fallback shapes -> ok == false.
		{
			name: "inequality not captured",
			expr: `_.count > 3`,
			ok:   false,
		},
		{
			name: "negation not captured",
			expr: `_.action != "refresh"`,
			ok:   false,
		},
		{
			name: "non-literal comparison",
			expr: `_.a == _.b`,
			ok:   false,
		},
		{
			name: "membership with non-literal element",
			expr: `_.x in ["a", _.b]`,
			ok:   false,
		},
		{
			name: "bare boolean true",
			expr: `true`,
			ok:   false,
		},
		{
			name: "function call",
			expr: `_.name.startsWith("foo")`,
			ok:   false,
		},
		{
			name: "invalid expression",
			expr: `_.a ==`,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Expression(tt.expr).ParamEqualities(ctx)
			assert.Equal(t, tt.ok, ok, "ok mismatch for %q", tt.expr)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func BenchmarkParamEqualities(b *testing.B) {
	ctx := context.Background()
	expr := Expression(`_.action in ["show", "refresh", "json"] || _.env == "prod"`)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = expr.ParamEqualities(ctx)
	}
}
