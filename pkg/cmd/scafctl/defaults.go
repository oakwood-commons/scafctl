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
// Individual factories can still be overridden afterwards by calling the
// corresponding Set*Factory functions directly.
func RegisterDefaults() {
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)
	gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)
	gotmpl.SetContextFuncBinderFactory(celeval.CelFuncWithContext)
}
