// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

func BenchmarkBuildFunctionDetail(b *testing.B) {
	fn := &gotmpl.ExtFunction{
		Name:        "testFunc",
		Description: "A test function for benchmarking",
		Custom:      true,
		Links:       []string{"https://example.com"},
		Examples: []gotmpl.Example{
			{Description: "Example 1", Template: `{{ testFunc "hello" }}`},
			{Description: "Example 2", Template: `{{ testFunc "world" }}`},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildFunctionDetail(fn)
	}
}

func BenchmarkBuildFunctionList(b *testing.B) {
	funcs := gotmpl.ExtFunctionList{
		{Name: "fn1", Description: "First function", Custom: true, Examples: []gotmpl.Example{{Description: "ex1", Template: "{{ fn1 }}"}}},
		{Name: "fn2", Description: "Second function", Custom: true, Examples: []gotmpl.Example{{Description: "ex2", Template: "{{ fn2 }}"}}},
		{Name: "fn3", Description: "Third function", Custom: false, Links: []string{"https://example.com"}},
		{Name: "fn4", Description: "Fourth function", Custom: true},
		{Name: "fn5", Description: "Fifth function", Custom: true},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildFunctionList(funcs)
	}
}
