// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"testing"
)

func BenchmarkMetadataProvider_Execute(b *testing.B) {
	p := New()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, nil)
	}
}
