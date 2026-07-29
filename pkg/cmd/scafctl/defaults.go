// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"maps"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl/ext/celeval"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// RegisterDefaults registers the default CEL environment, cache, and Go
// template factories required before calling Root(). Embedders that need
// the standard scafctl behaviour should call this once at startup.
//
// The Go template and CEL extension factories are set once (they establish the
// authoritative built-in function set). Embedders extend the Go template
// function set additively via RootOptions.GoTemplateFuncs or
// gotmpl.RegisterFuncs / gotmpl.RegisterFuncsOverride rather than by replacing
// the factory. The CEL cache and env factories can still be overridden by
// calling the corresponding Set*Factory functions before RegisterDefaults
// consumes them.
func RegisterDefaults() {
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)
	gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)
	gotmpl.SetContextFuncBinderFactory(contextTemplateFuncs)
}

// contextTemplateFuncs produces the context-aware Go template functions applied
// to each template clone right before execution: the context-bound `cel`
// function plus any solution-author-defined helpers (spec.functions) carried on
// the provider context. Rebinding these per execution (rather than relying on
// the closures captured at parse time, which are frozen into the shared
// template cache) ensures every render uses the current request's context --
// its cancellation, timeouts, and CEL cost limits -- instead of the context
// from whichever execution first populated the cache entry.
func contextTemplateFuncs(ctx context.Context) template.FuncMap {
	celFuncs := celeval.CelFuncWithContext(ctx)

	binder, ok := provider.TemplateFuncBinderFromContext(ctx)
	if !ok || binder == nil {
		return celFuncs
	}
	authorFuncs := binder.Bind(ctx)
	if len(authorFuncs) == 0 {
		return celFuncs
	}

	merged := make(template.FuncMap, len(celFuncs)+len(authorFuncs))
	maps.Copy(merged, authorFuncs)
	maps.Copy(merged, celFuncs)
	return merged
}
