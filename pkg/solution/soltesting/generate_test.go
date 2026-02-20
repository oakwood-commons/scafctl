// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DeriveTestName ---

func TestDeriveTestName_CommandOnly(t *testing.T) {
	got := DeriveTestName([]string{"render", "solution"}, nil)
	assert.Equal(t, "render-solution", got)
}

func TestDeriveTestName_WithResolverParam(t *testing.T) {
	got := DeriveTestName([]string{"render", "solution"}, []string{"-r", "env=prod"})
	assert.Equal(t, "render-solution-env-prod", got)
}

func TestDeriveTestName_MultipleParams(t *testing.T) {
	got := DeriveTestName([]string{"run", "resolver"}, []string{"-r", "env=prod", "-r", "region=us-east1"})
	assert.Equal(t, "run-resolver-env-prod-region-us-east1", got)
}

func TestDeriveTestName_PositionalArgs(t *testing.T) {
	got := DeriveTestName([]string{"run", "resolver"}, []string{"db", "config"})
	assert.Equal(t, "run-resolver-db-config", got)
}

func TestDeriveTestName_BooleanFlagSkipped(t *testing.T) {
	// Boolean flags (no value after them) should not contribute to the name.
	got := DeriveTestName([]string{"render", "solution"}, []string{"--compact", "-r", "env=dev"})
	assert.Equal(t, "render-solution-env-dev", got)
}

func TestDeriveTestName_SpecialCharsSlugified(t *testing.T) {
	got := DeriveTestName([]string{"run", "resolver"}, []string{"-r", "env=my_special+env"})
	// underscores and + become dashes; consecutive dashes collapse
	assert.Equal(t, "run-resolver-env-my-special-env", got)
}

func TestDeriveTestName_EmptyResult(t *testing.T) {
	got := DeriveTestName(nil, nil)
	assert.Equal(t, "generated-test", got)
}

// --- ensureOutputJSON ---

func TestEnsureOutputJSON_NonePresent(t *testing.T) {
	args := []string{"-r", "env=prod"}
	got := ensureOutputJSON(args)
	assert.Equal(t, []string{"-r", "env=prod", "-o", "json"}, got)
}

func TestEnsureOutputJSON_ShortFlagPresent(t *testing.T) {
	args := []string{"-r", "env=prod", "-o", "yaml"}
	got := ensureOutputJSON(args)
	assert.Equal(t, args, got)
}

func TestEnsureOutputJSON_LongFlagPresent(t *testing.T) {
	args := []string{"--output", "json"}
	got := ensureOutputJSON(args)
	assert.Equal(t, args, got)
}

func TestEnsureOutputJSON_InlineFlag(t *testing.T) {
	args := []string{"-o=yaml"}
	got := ensureOutputJSON(args)
	assert.Equal(t, args, got)
}

// --- deriveAssertions ---

func TestDeriveAssertions_NilData(t *testing.T) {
	out := deriveAssertions(nil, "__output", 0)
	require.Len(t, out, 1)
	assert.Equal(t, `__output == null`, string(out[0].Expression))
}

func TestDeriveAssertions_Bool(t *testing.T) {
	out := deriveAssertions(true, "__output", 0)
	require.Len(t, out, 1)
	assert.Equal(t, `__output == true`, string(out[0].Expression))
}

func TestDeriveAssertions_Number(t *testing.T) {
	out := deriveAssertions(float64(42), "__output", 0)
	require.Len(t, out, 1)
	assert.Equal(t, `__output == 42`, string(out[0].Expression))
}

func TestDeriveAssertions_Float(t *testing.T) {
	out := deriveAssertions(float64(3.14), "__output", 0)
	require.Len(t, out, 1)
	assert.Contains(t, string(out[0].Expression), "3.14")
}

func TestDeriveAssertions_String(t *testing.T) {
	out := deriveAssertions("hello world", "__output", 0)
	require.Len(t, out, 1)
	assert.Equal(t, `__output == "hello world"`, string(out[0].Expression))
}

func TestDeriveAssertions_StringWithQuotes(t *testing.T) {
	out := deriveAssertions(`say "hi"`, "__output", 0)
	require.Len(t, out, 1)
	// json.Marshal escapes the inner quotes properly.
	assert.Contains(t, string(out[0].Expression), `say \"hi\"`)
}

func TestDeriveAssertions_Slice(t *testing.T) {
	out := deriveAssertions([]any{"a", "b", "c"}, "__output", 0)
	require.Len(t, out, 1)
	assert.Equal(t, `size(__output) == 3`, string(out[0].Expression))
}

func TestDeriveAssertions_Map_Shallow(t *testing.T) {
	data := map[string]any{
		"status": "ok",
		"count":  float64(5),
	}
	out := deriveAssertions(data, "__output", 0)
	// Expect: size assertion + 2 leaf assertions
	require.Len(t, out, 3)
	exprs := make([]string, len(out))
	for i, a := range out {
		exprs[i] = string(a.Expression)
	}
	assert.Contains(t, exprs, `size(__output) == 2`)
	assert.Contains(t, exprs, `__output["count"] == 5`)
	assert.Contains(t, exprs, `__output["status"] == "ok"`)
}

func TestDeriveAssertions_DepthLimit(t *testing.T) {
	// At depth 2 (maxGenerateDepth), string leaves are still included.
	// At depth 3, recursion stops.
	nested := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": "should-not-appear",
			},
		},
	}
	out := deriveAssertions(nested, "__output", 0)
	exprs := make([]string, len(out))
	for i, a := range out {
		exprs[i] = string(a.Expression)
	}
	// level3 value should NOT appear (depth would be 3 > maxGenerateDepth)
	for _, e := range exprs {
		assert.NotContains(t, e, "should-not-appear")
	}
	// But level2 size assertion should appear (it's at depth 2)
	found := false
	for _, e := range exprs {
		if strings.Contains(e, `["level2"]`) {
			found = true
		}
	}
	assert.True(t, found, "expected a depth-2 assertion for level2")
}

func TestDeriveAssertions_MaxCap(t *testing.T) {
	// Build a map with more keys than MaxGeneratedAssertions to verify the cap.
	large := make(map[string]any, 30)
	for i := 0; i < 30; i++ {
		large[fmt.Sprintf("key%02d", i)] = "value"
	}
	out := deriveAssertions(large, "__output", 0)
	// The raw assertions will exceed MaxGeneratedAssertions; Generate() caps them.
	// Here we just verify deriveAssertions itself can produce many entries.
	assert.True(t, len(out) > MaxGeneratedAssertions)
}

// --- Generate ---

func TestGenerate_Basic(t *testing.T) {
	tmp := t.TempDir()
	rawJSON := []byte(`{"status":"ok","count":2}`)
	var data any
	require.NoError(t, json.Unmarshal(rawJSON, &data))

	result, err := Generate(&GenerateInput{
		Command:     []string{"render", "solution"},
		Args:        []string{"-r", "env=prod"},
		SnapshotDir: tmp,
		Data:        data,
		RawJSON:     rawJSON,
	})
	require.NoError(t, err)
	assert.Equal(t, "render-solution-env-prod", result.TestName)
	assert.NotNil(t, result.TestCase)
	assert.True(t, result.SnapshotWritten)
	assert.FileExists(t, filepath.Join(tmp, "render-solution-env-prod.json"))

	// Test args should contain -o json
	assert.Contains(t, result.TestCase.Args, "-o")
	assert.Contains(t, result.TestCase.Args, "json")

	// Should carry the "generated" tag
	assert.Contains(t, result.TestCase.Tags, "generated")
}

func TestGenerate_ExplicitTestName(t *testing.T) {
	tmp := t.TempDir()
	result, err := Generate(&GenerateInput{
		Command:     []string{"run", "resolver"},
		TestName:    "my-custom-test",
		SnapshotDir: tmp,
		Data:        map[string]any{"env": "prod"},
		RawJSON:     []byte(`{"env":"prod"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, "my-custom-test", result.TestName)
	assert.FileExists(t, filepath.Join(tmp, "my-custom-test.json"))
}

func TestGenerate_NoRawJSON_NoSnapshot(t *testing.T) {
	result, err := Generate(&GenerateInput{
		Command: []string{"render", "solution"},
		Data:    map[string]any{"actions": map[string]any{}},
	})
	require.NoError(t, err)
	assert.False(t, result.SnapshotWritten)
	assert.Empty(t, result.TestCase.Snapshot)
}

func TestGenerate_NoData_NoAssertions(t *testing.T) {
	result, err := Generate(&GenerateInput{
		Command: []string{"render", "solution"},
	})
	require.NoError(t, err)
	assert.Empty(t, result.TestCase.Assertions)
}

func TestGenerate_AssertionsCapped(t *testing.T) {
	large := make(map[string]any, 30)
	for i := 0; i < 30; i++ {
		large[fmt.Sprintf("key%02d", i)] = "value"
	}
	result, err := Generate(&GenerateInput{
		Command: []string{"render", "solution"},
		Data:    large,
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.TestCase.Assertions), MaxGeneratedAssertions)
}

func TestGenerate_MissingCommand_Error(t *testing.T) {
	_, err := Generate(&GenerateInput{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestGenerate_SnapshotNormalized(t *testing.T) {
	tmp := t.TempDir()
	rawJSON := []byte(`{"createdAt":"2024-01-01T00:00:00Z","id":"abc123"}`)
	result, err := Generate(&GenerateInput{
		Command:     []string{"render", "solution"},
		SnapshotDir: tmp,
		RawJSON:     rawJSON,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(result.SnapshotPath)
	require.NoError(t, err)
	// Timestamps replaced by <TIMESTAMP>
	assert.Contains(t, string(content), TimestampPlaceholder)
}

// --- GenerateToYAML ---

func TestGenerateToYAML(t *testing.T) {
	result := &GenerateResult{
		TestName: "my-test",
		TestCase: &TestCase{
			Description: "test desc",
			Command:     []string{"render", "solution"},
			Args:        []string{"-o", "json"},
			Tags:        []string{"generated"},
		},
	}
	data, err := GenerateToYAML(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), "my-test:")
	assert.Contains(t, string(data), "test desc")
}
