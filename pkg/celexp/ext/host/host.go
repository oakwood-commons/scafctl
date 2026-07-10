// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package host provides CEL extension functions that expose the host's resolved
// XDG configuration directory to in-process resolver expressions. The path
// honors paths.SetAppName, so a solution that references it stays
// branding-correct on any embedder instead of hardcoding ~/.config/scafctl.
package host

import (
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// ConfigDirFunc returns the host's resolved configuration directory. It is
// registered under two names: the namespaced, CEL-idiomatic host.configDir()
// and the portable hostConfigDir() that matches the Go template function name,
// so the same identifier works in resolver expressions and templates alike.
func ConfigDirFunc() celexp.ExtFunction {
	const dotted = "host.configDir"
	const portable = "hostConfigDir"
	binding := cel.FunctionBinding(func(_ ...ref.Val) ref.Val {
		return types.String(paths.ConfigDir())
	})
	return celexp.ExtFunction{
		Name:          dotted,
		Signature:     "host.configDir() -> string",
		Description:   "Returns the host's resolved configuration directory (branding-aware, honors the embedder's app name). Use host.configDir() (or the portable alias hostConfigDir(), which matches the Go template function) to build paths under the config dir without hardcoding a branded path",
		FunctionNames: []string{dotted, portable},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Build a config.d drop-in path",
				Expression:  `host.configDir() + "/config.d/clusters.yaml"`,
			},
			{
				Description: "Portable alias matching the Go template function name",
				Expression:  `hostConfigDir() + "/config.d/clusters.yaml"`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(dotted,
				cel.Overload(strings.ReplaceAll(dotted, ".", "_"),
					[]*cel.Type{},
					cel.StringType,
					binding,
				),
			),
			cel.Function(portable,
				cel.Overload(portable,
					[]*cel.Type{},
					cel.StringType,
					binding,
				),
			),
		},
	}
}
