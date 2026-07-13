// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package host provides Go template extension functions that expose the host's
// resolved XDG configuration directory. The path honors paths.SetAppName, so a
// solution that writes under it stays branding-correct on any embedder instead
// of hardcoding ~/.config/scafctl.
package host

import (
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// ConfigDirFunc returns an ExtFunction that yields the host's resolved
// configuration directory.
//
// Example usage in a Go template:
//
//	{{ hostConfigDir }}/config.d/clusters.yaml
func ConfigDirFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "hostConfigDir",
		Description: "Returns the host's resolved configuration directory (branding-aware, honors the embedder's app name). " +
			"Use it to write config.d drop-ins without hardcoding a branded path. " +
			"The same name is callable in CEL resolver expressions as hostConfigDir() (aliased to host.configDir()).",
		Custom: true,
		Examples: []gotmpl.Example{
			{
				Description: "Build a config.d drop-in path",
				Template:    `{{ hostConfigDir }}/config.d/clusters.yaml`,
			},
		},
		Func: template.FuncMap{
			"hostConfigDir": paths.ConfigDir,
		},
	}
}
