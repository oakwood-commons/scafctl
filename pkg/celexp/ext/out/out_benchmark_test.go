// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package out

import (
	"testing"

	"github.com/google/cel-go/cel"
)

func BenchmarkNilFunc_CEL(b *testing.B) {
	fn := NilFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("string_input", func(b *testing.B) {
		ast, iss := env.Compile(`out.nil("some value")`)
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

	b.Run("int_input", func(b *testing.B) {
		ast, iss := env.Compile(`out.nil(42)`)
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
