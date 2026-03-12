// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scaffold

import "testing"

func BenchmarkSolution_SimpleTemplate(b *testing.B) {
	opts := Options{
		Name:        "simple-solution",
		Description: "A simple benchmark solution",
	}

	for b.Loop() {
		_ = Solution(opts)
	}
}

func BenchmarkSolution_AllFeatures(b *testing.B) {
	opts := Options{
		Name:        "full-solution",
		Description: "Everything enabled",
		Version:     "2.0.0",
		Features: map[string]bool{
			"parameters":  true,
			"resolvers":   true,
			"actions":     true,
			"transforms":  true,
			"validation":  true,
			"tests":       true,
			"composition": true,
		},
	}

	for b.Loop() {
		_ = Solution(opts)
	}
}

func BenchmarkSolution_WithProviders(b *testing.B) {
	opts := Options{
		Name:        "provider-demo",
		Description: "Provider examples",
		Features: map[string]bool{
			"resolvers": true,
			"actions":   true,
		},
		Providers: []string{"http", "exec", "env", "git", "cel"},
	}

	for b.Loop() {
		_ = Solution(opts)
	}
}

func BenchmarkBuildYAML_Minimal(b *testing.B) {
	for b.Loop() {
		_ = BuildYAML("bench", "bench solution", "1.0.0", map[string]bool{}, nil)
	}
}

func BenchmarkBuildYAML_Full(b *testing.B) {
	features := map[string]bool{
		"parameters":  true,
		"resolvers":   true,
		"actions":     true,
		"transforms":  true,
		"validation":  true,
		"tests":       true,
		"composition": true,
	}
	providers := []string{"http", "exec", "env", "git", "cel"}

	for b.Loop() {
		_ = BuildYAML("bench-full", "full benchmark", "3.0.0", features, providers)
	}
}

func BenchmarkFeatureKeys(b *testing.B) {
	features := map[string]bool{
		"parameters":  true,
		"resolvers":   true,
		"actions":     true,
		"transforms":  true,
		"validation":  true,
		"tests":       true,
		"composition": true,
	}

	for b.Loop() {
		_ = FeatureKeys(features)
	}
}
