// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

func BenchmarkBuildFunctionDetail(b *testing.B) {
	fn := &celexp.ExtFunction{
		Name:          "test.func",
		Description:   "A test function for benchmarking",
		FunctionNames: []string{"test.func", "test_func"},
		Custom:        true,
		Links:         []string{"https://example.com"},
		Examples: []celexp.Example{
			{Description: "Example 1", Expression: `test.func("hello")`},
			{Description: "Example 2", Expression: `test.func("world")`},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildFunctionDetail(fn)
	}
}

func BenchmarkBuildFunctionList(b *testing.B) {
	funcs := celexp.ExtFunctionList{
		{Name: "fn1", Description: "First function", Custom: true, Examples: []celexp.Example{{Description: "ex1", Expression: "fn1()"}}},
		{Name: "fn2", Description: "Second function", Custom: true, Examples: []celexp.Example{{Description: "ex2", Expression: "fn2()"}}},
		{Name: "fn3", Description: "Third function", Custom: false, Links: []string{"https://example.com"}},
		{Name: "fn4", Description: "Fourth function", Custom: true, FunctionNames: []string{"fn4", "fn4_alias"}},
		{Name: "fn5", Description: "Fifth function", Custom: true},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildFunctionList(funcs)
	}
}
