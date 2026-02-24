// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sourcepos

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSourceMap_BasicMapping(t *testing.T) {
	yamlData := []byte("apiVersion: v1\nkind: Solution\nmetadata:\n  name: test-solution\n  description: A test\nspec:\n  resolvers:\n    appName:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: hello\n")

	sm, err := BuildSourceMap(yamlData, "test.yaml")
	require.NoError(t, err)
	assert.Greater(t, sm.Len(), 0)

	pos, ok := sm.Get("apiVersion")
	assert.True(t, ok)
	assert.Equal(t, 1, pos.Line)
	assert.Equal(t, "test.yaml", pos.File)

	pos, ok = sm.Get("kind")
	assert.True(t, ok)
	assert.Equal(t, 2, pos.Line)

	pos, ok = sm.Get("metadata.name")
	assert.True(t, ok)
	assert.Equal(t, 4, pos.Line)

	pos, ok = sm.Get("spec.resolvers.appName")
	assert.True(t, ok)
	assert.Equal(t, 8, pos.Line)

	pos, ok = sm.Get("spec.resolvers.appName.resolve.with[0]")
	assert.True(t, ok)
	assert.Equal(t, 11, pos.Line)

	pos, ok = sm.Get("spec.resolvers.appName.resolve.with[0].inputs.value")
	assert.True(t, ok)
	assert.Equal(t, 13, pos.Line)
}

func TestBuildSourceMap_UnknownPath(t *testing.T) {
	sm, err := BuildSourceMap([]byte("foo: bar"), "")
	require.NoError(t, err)
	_, ok := sm.Get("nonexistent")
	assert.False(t, ok)
}

func TestBuildSourceMap_InvalidYAML(t *testing.T) {
	_, err := BuildSourceMap([]byte(":\n  :\n    :"), "bad.yaml")
	assert.Error(t, err)
}

func TestBuildSourceMap_EmptyFile(t *testing.T) {
	sm, err := BuildSourceMap([]byte(""), "empty.yaml")
	require.NoError(t, err)
	assert.Equal(t, 0, sm.Len())
}

func TestPosition_String(t *testing.T) {
	p1 := Position{Line: 10, Column: 5, File: "solution.yaml"}
	assert.Equal(t, "solution.yaml:10:5", p1.String())

	p2 := Position{Line: 3, Column: 1}
	assert.Equal(t, "3:1", p2.String())
}

func TestPosition_IsZero(t *testing.T) {
	assert.True(t, Position{}.IsZero())
	assert.False(t, Position{Line: 1}.IsZero())
	assert.False(t, Position{Column: 1}.IsZero())
	assert.True(t, Position{File: "foo.yaml"}.IsZero())
}

func TestSourceMap_NilSafety(t *testing.T) {
	var sm *SourceMap
	_, ok := sm.Get("anything")
	assert.False(t, ok)
	assert.Equal(t, 0, sm.Len())
	assert.Nil(t, sm.Paths())
	sm.Set("foo", Position{Line: 1})
	sm.Merge(NewSourceMap())
}

func TestSourceMap_Merge(t *testing.T) {
	sm1 := NewSourceMap()
	sm1.Set("a", Position{Line: 1, Column: 1, File: "file1.yaml"})
	sm1.Set("b", Position{Line: 2, Column: 1, File: "file1.yaml"})

	sm2 := NewSourceMap()
	sm2.Set("b", Position{Line: 10, Column: 1, File: "file2.yaml"})
	sm2.Set("c", Position{Line: 20, Column: 1, File: "file2.yaml"})

	sm1.Merge(sm2)
	assert.Equal(t, 3, sm1.Len())

	pos, ok := sm1.Get("a")
	assert.True(t, ok)
	assert.Equal(t, "file1.yaml", pos.File)

	pos, ok = sm1.Get("b")
	assert.True(t, ok)
	assert.Equal(t, "file2.yaml", pos.File)
	assert.Equal(t, 10, pos.Line)

	pos, ok = sm1.Get("c")
	assert.True(t, ok)
	assert.Equal(t, "file2.yaml", pos.File)
}

func TestSourceMap_Paths(t *testing.T) {
	sm := NewSourceMap()
	sm.Set("z", Position{Line: 1})
	sm.Set("a", Position{Line: 2})
	sm.Set("m", Position{Line: 3})
	paths := sm.Paths()
	sort.Strings(paths)
	assert.Equal(t, []string{"a", "m", "z"}, paths)
}

func TestBuildSourceMap_SequenceWithMappings(t *testing.T) {
	yamlData := []byte("items:\n  - name: first\n    value: 1\n  - name: second\n    value: 2\n")
	sm, err := BuildSourceMap(yamlData, "seq.yaml")
	require.NoError(t, err)

	pos, ok := sm.Get("items[0].name")
	assert.True(t, ok)
	assert.Equal(t, 2, pos.Line)

	pos, ok = sm.Get("items[1].name")
	assert.True(t, ok)
	assert.Equal(t, 4, pos.Line)
}

func TestBuildSourceMap_DeeplyNested(t *testing.T) {
	sm, err := BuildSourceMap([]byte("a:\n  b:\n    c:\n      d: value\n"), "")
	require.NoError(t, err)
	pos, ok := sm.Get("a.b.c.d")
	assert.True(t, ok)
	assert.Equal(t, 4, pos.Line)
}
