// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

func TestConfigDirFunc_Metadata(t *testing.T) {
	fn := ConfigDirFunc()
	assert.Equal(t, "host.configDir", fn.Name)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	// Registered under both the namespaced and portable (template-matching) name.
	assert.Contains(t, fn.FunctionNames, "host.configDir")
	assert.Contains(t, fn.FunctionNames, "hostConfigDir")
}

func TestConfigDirFunc_CELIntegration(t *testing.T) {
	fn := ConfigDirFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	// Both the namespaced and the portable alias must evaluate identically.
	for _, expr := range []string{`host.configDir()`, `hostConfigDir()`} {
		ast, iss := env.Compile(expr)
		require.NoError(t, iss.Err(), expr)
		prg, err := env.Program(ast)
		require.NoError(t, err, expr)
		out, _, err := prg.Eval(map[string]any{})
		require.NoError(t, err, expr)
		assert.Equal(t, paths.ConfigDir(), out.Value(), expr)
	}
}
