// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authpage

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func BenchmarkDefaultRenderer(b *testing.B) {
	ctx := context.Background()
	data := Data{BinaryName: settings.CliBinaryName, State: StateSuccess}
	_, _ = DefaultRenderer(ctx, data) // warm template cache

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		doc, err := DefaultRenderer(ctx, data)
		if err != nil || len(doc) == 0 {
			b.Fatalf("DefaultRenderer failed: %v", err)
		}
	}
}

func BenchmarkRenderBundle(b *testing.B) {
	ctx := context.Background()
	_ = RenderBundle(ctx, nil, settings.CliBinaryName)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bundle := RenderBundle(ctx, nil, settings.CliBinaryName)
		if len(bundle.SuccessHTML) == 0 || len(bundle.FailureHTML) == 0 {
			b.Fatal("empty bundle")
		}
	}
}
