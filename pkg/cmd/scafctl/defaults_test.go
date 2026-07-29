// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"testing"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func TestRegisterDefaults(t *testing.T) {
	t.Parallel()

	// RegisterDefaults should not panic and should be idempotent.
	RegisterDefaults()
	RegisterDefaults() // call twice to verify idempotency
}

// fakeTemplateFuncBinder is a minimal TemplateFuncBinder for exercising
// contextTemplateFuncs without pulling in the full authorfuncs library.
type fakeTemplateFuncBinder struct {
	funcs template.FuncMap
}

func (f fakeTemplateFuncBinder) Bind(context.Context) template.FuncMap { return f.funcs }
func (f fakeTemplateFuncBinder) Fingerprint() string                   { return "fake" }

func TestContextTemplateFuncs_NoBinder(t *testing.T) {
	t.Parallel()

	funcs := contextTemplateFuncs(context.Background())

	// Without an author-function binder on the context, only the context-bound
	// cel helper is present.
	if _, ok := funcs["cel"]; !ok {
		t.Fatalf("expected 'cel' function to be present, got keys %v", keysOf(funcs))
	}
	if _, ok := funcs["greet"]; ok {
		t.Fatalf("did not expect an author function without a binder")
	}
}

func TestContextTemplateFuncs_MergesAuthorFuncs(t *testing.T) {
	t.Parallel()

	binder := fakeTemplateFuncBinder{funcs: template.FuncMap{
		"greet": func() string { return "hi" },
	}}
	ctx := provider.WithTemplateFuncBinder(context.Background(), binder)

	funcs := contextTemplateFuncs(ctx)

	if _, ok := funcs["cel"]; !ok {
		t.Fatalf("expected 'cel' function to be present, got keys %v", keysOf(funcs))
	}
	if _, ok := funcs["greet"]; !ok {
		t.Fatalf("expected author function 'greet' to be merged, got keys %v", keysOf(funcs))
	}
}

func TestContextTemplateFuncs_CelWinsOnCollision(t *testing.T) {
	t.Parallel()

	// An author function named "cel" must not shadow the built-in context-bound
	// cel helper: cel is copied last and wins.
	binder := fakeTemplateFuncBinder{funcs: template.FuncMap{
		"cel": func() string { return "author-cel" },
	}}
	ctx := provider.WithTemplateFuncBinder(context.Background(), binder)

	funcs := contextTemplateFuncs(ctx)
	builtin := contextTemplateFuncs(context.Background())

	if len(funcs) != len(builtin) {
		t.Fatalf("expected collision to not add a key: got %d, builtin %d", len(funcs), len(builtin))
	}
}

func TestContextTemplateFuncs_EmptyBinder(t *testing.T) {
	t.Parallel()

	binder := fakeTemplateFuncBinder{funcs: nil}
	ctx := provider.WithTemplateFuncBinder(context.Background(), binder)

	funcs := contextTemplateFuncs(ctx)
	if _, ok := funcs["cel"]; !ok {
		t.Fatalf("expected 'cel' function to be present with an empty binder")
	}
}

func keysOf(m template.FuncMap) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
