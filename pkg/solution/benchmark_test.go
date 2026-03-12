// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
)

var benchSolutionYAML = []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bench-solution
  displayName: Benchmark Solution
  description: A solution used for benchmarking
  category: application
  version: 2.1.0
  tags:
    - benchmark
    - performance
  maintainers:
    - name: Test User
      email: test@example.com
  links:
    - name: Docs
      url: https://example.com/docs
catalog:
  visibility: public
  beta: false
  disabled: false
spec:
  resolvers:
    environment:
      provider: parameter
      with:
        - key: environment
    region:
      provider: static
      with:
        - value: us-east-1
    appName:
      provider: cel
      with:
        - expression: "'app-' + _.environment"
      dependsOn:
        - environment
    config:
      provider: static
      dependsOn:
        - region
        - appName
      with:
        - value: configured
`)

var benchSolutionLargeYAML = buildBenchSolutionLargeYAML()

func buildBenchSolutionLargeYAML() []byte {
	// Build a large YAML with many resolvers
	b := []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: large-bench
  displayName: Large Benchmark
  description: Large solution for benchmarking
  category: application
  version: 1.0.0
  tags:
    - benchmark
catalog:
  visibility: private
spec:
  resolvers:
`)
	for i := 0; i < 50; i++ {
		b = append(b, []byte("    resolver"+string(rune('A'+i%26))+string(rune('0'+i/26))+":\n")...)
		b = append(b, []byte("      provider: static\n")...)
		b = append(b, []byte("      with:\n")...)
		b = append(b, []byte("        - value: data\n")...)
	}
	return b
}

func BenchmarkSolution_FromYAML(b *testing.B) {
	for b.Loop() {
		s := &Solution{}
		_ = s.FromYAML(benchSolutionYAML)
	}
}

func BenchmarkSolution_FromYAML_Large(b *testing.B) {
	for b.Loop() {
		s := &Solution{}
		_ = s.FromYAML(benchSolutionLargeYAML)
	}
}

func BenchmarkSolution_UnmarshalFromBytes(b *testing.B) {
	for b.Loop() {
		s := &Solution{}
		_ = s.UnmarshalFromBytes(benchSolutionYAML)
	}
}

func BenchmarkSolution_ToJSON(b *testing.B) {
	s := &Solution{
		APIVersion: DefaultAPIVersion,
		Kind:       SolutionKind,
		Metadata: Metadata{
			Name:        "bench-solution",
			DisplayName: "Benchmark Solution",
			Description: "For benchmarking",
			Version:     semver.MustParse("1.0.0"),
			Tags:        []string{"bench", "perf"},
		},
		Catalog: Catalog{Visibility: "public"},
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = s.ToJSON()
	}
}

func BenchmarkSolution_ToYAML(b *testing.B) {
	s := &Solution{
		APIVersion: DefaultAPIVersion,
		Kind:       SolutionKind,
		Metadata: Metadata{
			Name:        "bench-solution",
			DisplayName: "Benchmark Solution",
			Description: "For benchmarking",
			Version:     semver.MustParse("1.0.0"),
			Tags:        []string{"bench", "perf"},
		},
		Catalog: Catalog{Visibility: "public"},
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = s.ToYAML()
	}
}

func BenchmarkSolution_Validate(b *testing.B) {
	s := &Solution{
		APIVersion: DefaultAPIVersion,
		Kind:       SolutionKind,
		Metadata: Metadata{
			Name:    "bench-valid",
			Version: semver.MustParse("1.0.0"),
		},
		Catalog: Catalog{Visibility: "public"},
	}

	b.ResetTimer()
	for b.Loop() {
		_ = s.Validate()
	}
}

func BenchmarkSolution_LoadFromBytes(b *testing.B) {
	for b.Loop() {
		s := &Solution{}
		_ = s.LoadFromBytes(benchSolutionYAML)
	}
}

func BenchmarkSolution_RoundTrip(b *testing.B) {
	for b.Loop() {
		s := &Solution{}
		if err := s.FromYAML(benchSolutionYAML); err != nil {
			b.Fatal(err)
		}
		_, _ = s.ToJSON()
		_, _ = s.ToYAML()
	}
}
