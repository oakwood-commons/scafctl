// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"testing"

	"github.com/google/cel-go/cel"
)

func BenchmarkConfigDirFunc_CEL(b *testing.B) {
	fn := ConfigDirFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("namespaced", func(b *testing.B) {
		ast, iss := env.Compile(`host.configDir()`)
		if iss.Err() != nil {
			b.Fatal(iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			_, _, _ = prg.Eval(cel.NoVars())
		}
	})

	b.Run("portable_alias", func(b *testing.B) {
		ast, iss := env.Compile(`hostConfigDir()`)
		if iss.Err() != nil {
			b.Fatal(iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			_, _, _ = prg.Eval(cel.NoVars())
		}
	})
}
