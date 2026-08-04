// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestDiagnostics_ParseError(t *testing.T) {
	diags := Diagnostics([]byte(":\n  :\n    :"), "bad.yaml", "scafctl", nil)
	require.Len(t, diags, 1)
	assert.Equal(t, protocol.DiagnosticSeverityError, *diags[0].Severity)
	assert.Contains(t, diags[0].Message, "failed to parse solution")
	assert.Equal(t, "scafctl", *diags[0].Source)
}

func TestDiagnostics_CleanReturnsNonNil(t *testing.T) {
	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)
	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: clean
spec:
  resolvers:
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: hello
`)
	diags := Diagnostics(content, "clean.yaml", "scafctl", reg)
	assert.NotNil(t, diags, "must be non-nil so publishing clears stale diagnostics")
}

func TestDiagnostics_UndefinedResolverProducesPositioned(t *testing.T) {
	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)
	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad
spec:
  resolvers:
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.doesNotExist
`)
	diags := Diagnostics(content, "bad.yaml", "scafctl", reg)
	require.NotEmpty(t, diags, "an unknown resolver reference should produce a finding")
	// Every diagnostic has a source, a rule code, and a valid range.
	for _, d := range diags {
		require.NotNil(t, d.Severity)
		assert.Equal(t, "scafctl", *d.Source)
		require.NotNil(t, d.Code)
		assert.GreaterOrEqual(t, d.Range.End.Character, d.Range.Start.Character)
	}
}
