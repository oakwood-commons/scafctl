// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authorfuncs

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// benchFunctions returns a small library exercising both body kinds and
// composition (a template body calling a sibling CEL function).
func benchFunctions() map[string]*spec.Function {
	return map[string]*spec.Function{
		"greet": {
			Params: []*spec.ParamDef{{Name: "name", Type: spec.TypeString, Required: true}},
			Cel:    `"HELLO " + _.args.name + "!"`,
		},
		"shout": {
			Params:   []*spec.ParamDef{{Name: "who", Type: spec.TypeString, Required: true}},
			Template: `{{ greet .args.who }} ({{ .args.who | upper }})`,
		},
	}
}

// BenchmarkCompile measures the cost of compiling a function library
// (parameter validation, body compilation, cycle detection, fingerprinting).
func BenchmarkCompile(b *testing.B) {
	fns := benchFunctions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(fns); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvokeCel measures repeated invocation of a CEL-bodied author
// function (the per-render hot path: bindArgs + coercion + CEL eval).
func BenchmarkInvokeCel(b *testing.B) {
	lib, err := Compile(benchFunctions())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	funcs := lib.Bind(ctx)
	greet, ok := funcs["greet"].(func(...any) (any, error))
	if !ok {
		b.Fatalf("greet has unexpected type %T", funcs["greet"])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := greet("world"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInvokeTemplate measures repeated invocation of a template-bodied
// author function that composes a sibling function (nested template render).
func BenchmarkInvokeTemplate(b *testing.B) {
	lib, err := Compile(benchFunctions())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	funcs := lib.Bind(ctx)
	shout, ok := funcs["shout"].(func(...any) (any, error))
	if !ok {
		b.Fatalf("shout has unexpected type %T", funcs["shout"])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := shout("world"); err != nil {
			b.Fatal(err)
		}
	}
}
