// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

func TestConfigDirFunc_Metadata(t *testing.T) {
	fn := ConfigDirFunc()
	assert.Equal(t, "hostConfigDir", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Examples)
	assert.Contains(t, fn.Func, "hostConfigDir")
}

func TestHostFuncs_RenderInTemplate(t *testing.T) {
	funcs := template.FuncMap{}
	for k, v := range ConfigDirFunc().Func {
		funcs[k] = v
	}

	tmpl, err := template.New("t").Funcs(funcs).Parse(`{{ hostConfigDir }}/config.d/x.yaml`)
	require.NoError(t, err)
	var sb strings.Builder
	require.NoError(t, tmpl.Execute(&sb, nil))
	assert.Equal(t, paths.ConfigDir()+"/config.d/x.yaml", sb.String())
}
