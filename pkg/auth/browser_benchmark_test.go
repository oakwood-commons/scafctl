// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

func BenchmarkResolveBrowserPreference(b *testing.B) {
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ResolveBrowserPreference(ctx, "")
	}
}

func BenchmarkValidateBrowserFlag(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ValidateBrowserFlag("msedge")
	}
}

func BenchmarkWarnIfBrowserUnavailable(b *testing.B) {
	ops := browserProbeOps{
		goos: "linux",
		lookPath: func(name string) (string, error) {
			return "/usr/bin/" + name, nil
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		warnIfBrowserUnavailableWithOps(ctx, "msedge", ops)
	}
}
