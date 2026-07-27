// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"fmt"
	"testing"
	"text/template"
)

// registerBenchFuncs populates the registry with n additive functions and a
// handful of override functions so the merge paths exercise a realistic,
// non-empty registry. It resets the registry on cleanup.
func registerBenchFuncs(b *testing.B, n int) {
	b.Helper()
	ResetRegistryForTesting()
	b.Cleanup(ResetRegistryForTesting)

	additive := make(template.FuncMap, n)
	for i := 0; i < n; i++ {
		additive[fmt.Sprintf("embFn%d", i)] = func() string { return "v" }
	}
	if err := RegisterFuncs(additive); err != nil {
		b.Fatalf("RegisterFuncs: %v", err)
	}
	if err := RegisterFuncsOverride(template.FuncMap{
		"embOverride": func() string { return "o" },
	}); err != nil {
		b.Fatalf("RegisterFuncsOverride: %v", err)
	}
}

// BenchmarkGetExtensionFuncMap measures the per-service merge (factory base +
// env strip + additive + override), which runs on every service construction.
func BenchmarkGetExtensionFuncMap(b *testing.B) {
	registerBenchFuncs(b, 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getExtensionFuncMap()
	}
}

// BenchmarkRegisteredFuncs measures building the discoverability list consumed
// by the CLI and MCP tooling.
func BenchmarkRegisteredFuncs(b *testing.B) {
	registerBenchFuncs(b, 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegisteredFuncs()
	}
}
