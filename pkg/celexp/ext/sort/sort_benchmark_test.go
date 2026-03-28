// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celsort

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
)

func BenchmarkObjectsFunc_CEL(b *testing.B) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("small_3items", func(b *testing.B) {
		ast, iss := env.Compile(`sort.objects([{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}, {"name": "Bob", "age": 28}], "name")`)
		if iss.Err() != nil {
			b.Fatal(iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _, _ = prg.Eval(cel.NoVars())
		}
	})

	b.Run("medium_10items", func(b *testing.B) {
		// Build a list of 10 objects as CEL literal
		var items string
		for i := 0; i < 10; i++ {
			if i > 0 {
				items += ", "
			}
			items += fmt.Sprintf(`{"name": "item%02d", "val": %d}`, 10-i, i)
		}
		expr := fmt.Sprintf("sort.objects([%s], \"name\")", items)

		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			b.Fatal(iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _, _ = prg.Eval(cel.NoVars())
		}
	})
}

func BenchmarkObjectsDescendingFunc_CEL(b *testing.B) {
	fn := ObjectsDescendingFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	if err != nil {
		b.Fatal(err)
	}

	ast, iss := env.Compile(`sort.objectsDescending([{"name": "Alice", "age": 25}, {"name": "Charlie", "age": 30}, {"name": "Bob", "age": 28}], "name")`)
	if iss.Err() != nil {
		b.Fatal(iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = prg.Eval(cel.NoVars())
	}
}
