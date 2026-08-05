// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refindex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNodeMap(t *testing.T) {
	raw := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nm
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`)

	nodes, err := NodeMap(raw)
	require.NoError(t, err)
	require.NotEmpty(t, nodes)

	// Dotted keys resolve to the value node at that path.
	name, ok := nodes["metadata.name"]
	require.True(t, ok, "expected metadata.name in the node map")
	assert.Equal(t, yaml.ScalarNode, name.Kind)
	assert.Equal(t, "nm", name.Value)

	// Sequence elements are addressed with [i].
	provider, ok := nodes["spec.resolvers.environment.resolve.with[0].provider"]
	require.True(t, ok, "expected the [0] sequence element path in the node map")
	assert.Equal(t, "parameter", provider.Value)
}

func TestNodeMap_ParseError(t *testing.T) {
	// Unclosed flow mapping is not valid YAML.
	_, err := NodeMap([]byte("metadata: {name: x"))
	assert.Error(t, err)
}

func TestNodeMap_Empty(t *testing.T) {
	nodes, err := NodeMap([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

// TestNodeMap_MatchesBuildValueNodeMap guards the internal alias against drift:
// the exported NodeMap and the internal helper must return identical maps.
func TestNodeMap_MatchesBuildValueNodeMap(t *testing.T) {
	raw := []byte("a:\n  b: c\n  d:\n    - e\n    - f\n")

	fromExport, err := NodeMap(raw)
	require.NoError(t, err)
	fromInternal, err := buildValueNodeMap(raw)
	require.NoError(t, err)

	require.Equal(t, len(fromExport), len(fromInternal))
	for path, node := range fromExport {
		other, ok := fromInternal[path]
		require.True(t, ok, "path %q missing from internal map", path)
		assert.Equal(t, node.Kind, other.Kind, "path %q kind mismatch", path)
		assert.Equal(t, node.Value, other.Value, "path %q value mismatch", path)
	}
}
