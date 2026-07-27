// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl/ext/celeval"
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
	gotmpl.SetContextFuncBinderFactory(celeval.CelFuncWithContext)
}
