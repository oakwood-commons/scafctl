// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package runmode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromContext_DefaultsCLI(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, CLI, FromContext(ctx))
}

func TestWithMode_RoundTrips(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
	}{
		{name: "CLI", mode: CLI},
		{name: "API", mode: API},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithMode(context.Background(), tt.mode)
			assert.Equal(t, tt.mode, FromContext(ctx))
		})
	}
}

func TestMode_String(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{mode: CLI, want: "cli"},
		{mode: API, want: "api"},
		{mode: Mode(0), want: "unknown"},
		{mode: Mode(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.mode.String())
		})
	}
}

func TestWithMode_OverridesPrevious(t *testing.T) {
	ctx := WithMode(context.Background(), CLI)
	ctx = WithMode(ctx, API)
	assert.Equal(t, API, FromContext(ctx))
}

func BenchmarkFromContext(b *testing.B) {
	ctx := WithMode(context.Background(), API)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = FromContext(ctx)
	}
}
