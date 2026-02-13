// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- SkipBuiltinsValue tests ---

func TestSkipBuiltinsValue_YAML_Bool_True(t *testing.T) {
	var s soltesting.SkipBuiltinsValue
	err := yaml.Unmarshal([]byte(`true`), &s)
	require.NoError(t, err)
	assert.True(t, s.All)
	assert.Empty(t, s.Names)
}

func TestSkipBuiltinsValue_YAML_Bool_False(t *testing.T) {
	var s soltesting.SkipBuiltinsValue
	err := yaml.Unmarshal([]byte(`false`), &s)
	require.NoError(t, err)
	assert.False(t, s.All)
	assert.Empty(t, s.Names)
}

func TestSkipBuiltinsValue_YAML_StringList(t *testing.T) {
	var s soltesting.SkipBuiltinsValue
	err := yaml.Unmarshal([]byte(`["lint", "parse"]`), &s)
	require.NoError(t, err)
	assert.False(t, s.All)
	assert.Equal(t, []string{"lint", "parse"}, s.Names)
}

func TestSkipBuiltinsValue_YAML_RoundTrip_Bool(t *testing.T) {
	original := soltesting.SkipBuiltinsValue{All: true}
	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var roundTripped soltesting.SkipBuiltinsValue
	err = yaml.Unmarshal(data, &roundTripped)
	require.NoError(t, err)
	assert.True(t, roundTripped.All)
	assert.Empty(t, roundTripped.Names)
}

func TestSkipBuiltinsValue_YAML_RoundTrip_Names(t *testing.T) {
	original := soltesting.SkipBuiltinsValue{Names: []string{"lint", "parse"}}
	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var roundTripped soltesting.SkipBuiltinsValue
	err = yaml.Unmarshal(data, &roundTripped)
	require.NoError(t, err)
	assert.False(t, roundTripped.All)
	assert.Equal(t, []string{"lint", "parse"}, roundTripped.Names)
}

func TestSkipBuiltinsValue_YAML_RoundTrip_False(t *testing.T) {
	original := soltesting.SkipBuiltinsValue{}
	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var roundTripped soltesting.SkipBuiltinsValue
	err = yaml.Unmarshal(data, &roundTripped)
	require.NoError(t, err)
	assert.False(t, roundTripped.All)
	assert.Empty(t, roundTripped.Names)
}

func TestSkipBuiltinsValue_JSON_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		value    soltesting.SkipBuiltinsValue
		expected string
	}{
		{"all", soltesting.SkipBuiltinsValue{All: true}, "true"},
		{"none", soltesting.SkipBuiltinsValue{}, "false"},
		{"names", soltesting.SkipBuiltinsValue{Names: []string{"lint"}}, `["lint"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			var roundTripped soltesting.SkipBuiltinsValue
			err = json.Unmarshal(data, &roundTripped)
			require.NoError(t, err)
			assert.Equal(t, tt.value.All, roundTripped.All)
			if tt.value.Names != nil {
				assert.Equal(t, tt.value.Names, roundTripped.Names)
			}
		})
	}
}

func TestSkipBuiltinsValue_IsSkipped(t *testing.T) {
	assert.True(t, soltesting.SkipBuiltinsValue{All: true}.IsSkipped())
	assert.True(t, soltesting.SkipBuiltinsValue{Names: []string{"lint"}}.IsSkipped())
	assert.False(t, soltesting.SkipBuiltinsValue{}.IsSkipped())
}

// --- TestCase.IsTemplate tests ---

func TestTestCase_IsTemplate(t *testing.T) {
	assert.True(t, (&soltesting.TestCase{Name: "_base"}).IsTemplate())
	assert.True(t, (&soltesting.TestCase{Name: "_base-render"}).IsTemplate())
	assert.False(t, (&soltesting.TestCase{Name: "render-test"}).IsTemplate())
	assert.False(t, (&soltesting.TestCase{Name: "a"}).IsTemplate())
	assert.False(t, (&soltesting.TestCase{Name: ""}).IsTemplate())
}

// --- TestCase.GetInjectFile tests ---

func TestTestCase_GetInjectFile(t *testing.T) {
	// nil defaults to true
	tc := &soltesting.TestCase{}
	assert.True(t, tc.GetInjectFile())

	// explicit true
	bTrue := true
	tc = &soltesting.TestCase{InjectFile: &bTrue}
	assert.True(t, tc.GetInjectFile())

	// explicit false
	bFalse := false
	tc = &soltesting.TestCase{InjectFile: &bFalse}
	assert.False(t, tc.GetInjectFile())
}

// --- TestCase.Validate tests ---

func TestTestCase_Validate_Valid(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:        "render-test",
		Description: "Test rendering",
		Command:     []string{"render", "solution"},
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	assert.NoError(t, tc.Validate())
}

func TestTestCase_Validate_ValidTemplate(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:        "_base-render",
		Description: "Base render template",
		Command:     []string{"render", "solution"},
	}
	// Templates don't require assertions or snapshot
	assert.NoError(t, tc.Validate())
}

func TestTestCase_Validate_ValidWithExtends(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "render-test",
		Extends: []string{"_base"},
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	// Tests with extends don't require command
	assert.NoError(t, tc.Validate())
}

func TestTestCase_Validate_ValidWithSnapshot(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:     "render-test",
		Command:  []string{"render", "solution"},
		Snapshot: "testdata/expected.json",
	}
	// Snapshot can be used instead of assertions
	assert.NoError(t, tc.Validate())
}

func TestTestCase_Validate_InvalidName(t *testing.T) {
	tests := []struct {
		name      string
		testName  string
		expectErr bool
	}{
		{"starts with hyphen", "-invalid", true},
		{"contains space", "invalid name", true},
		{"contains dot", "invalid.name", true},
		{"empty", "", true},
		{"valid", "valid-name", false},
		{"valid with underscore", "valid_name", false},
		{"valid starting digit", "1test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &soltesting.TestCase{
				Name:    tt.testName,
				Command: []string{"render", "solution"},
				Assertions: []soltesting.Assertion{
					{Contains: "hello"},
				},
			}
			err := tc.Validate()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTestCase_Validate_InvalidTemplateName(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "_",
		Command: []string{"render", "solution"},
	}
	assert.Error(t, tc.Validate())
}

func TestTestCase_Validate_MutualExclusion_ExitCodeAndExpectFailure(t *testing.T) {
	exitCode := 1
	tc := &soltesting.TestCase{
		Name:          "test",
		Command:       []string{"render", "solution"},
		ExitCode:      &exitCode,
		ExpectFailure: true,
		Assertions: []soltesting.Assertion{
			{Contains: "error"},
		},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestTestCase_Validate_MissingCommandNonTemplate(t *testing.T) {
	tc := &soltesting.TestCase{
		Name: "test",
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestTestCase_Validate_MissingAssertionsAndSnapshot(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "test",
		Command: []string{"render", "solution"},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "assertions or snapshot")
}

func TestTestCase_Validate_ArgContainsFileFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"short flag", []string{"-r", "env=prod", "-f", "solution.yaml"}},
		{"long flag", []string{"--file", "solution.yaml"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &soltesting.TestCase{
				Name:    "test",
				Command: []string{"render", "solution"},
				Args:    tt.args,
				Assertions: []soltesting.Assertion{
					{Contains: "hello"},
				},
			}
			err := tc.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "must not contain -f or --file")
		})
	}
}

func TestTestCase_Validate_RetriesOutOfRange(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "test",
		Command: []string{"render", "solution"},
		Retries: 11,
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retries")
}

func TestTestCase_Validate_NegativeRetries(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "test",
		Command: []string{"render", "solution"},
		Retries: -1,
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retries")
}

func TestTestCase_Validate_FieldLimits(t *testing.T) {
	t.Run("too many assertions", func(t *testing.T) {
		assertions := make([]soltesting.Assertion, soltesting.MaxAssertionsPerTest+1)
		for i := range assertions {
			assertions[i] = soltesting.Assertion{Contains: "test"}
		}
		tc := &soltesting.TestCase{
			Name:       "test",
			Command:    []string{"render", "solution"},
			Assertions: assertions,
		}
		err := tc.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "assertions count")
	})

	t.Run("too many files", func(t *testing.T) {
		files := make([]string, soltesting.MaxFilesPerTest+1)
		for i := range files {
			files[i] = "file.txt"
		}
		tc := &soltesting.TestCase{
			Name:    "test",
			Command: []string{"render", "solution"},
			Files:   files,
			Assertions: []soltesting.Assertion{
				{Contains: "hello"},
			},
		}
		err := tc.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "files count")
	})

	t.Run("too many tags", func(t *testing.T) {
		tags := make([]string, soltesting.MaxTagsPerTest+1)
		for i := range tags {
			tags[i] = "tag"
		}
		tc := &soltesting.TestCase{
			Name:    "test",
			Command: []string{"render", "solution"},
			Tags:    tags,
			Assertions: []soltesting.Assertion{
				{Contains: "hello"},
			},
		}
		err := tc.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "tags count")
	})
}

func TestTestCase_Validate_NegativeTimeout(t *testing.T) {
	tc := &soltesting.TestCase{
		Name:    "test",
		Command: []string{"render", "solution"},
		Timeout: &duration.Duration{Duration: -1 * time.Second},
		Assertions: []soltesting.Assertion{
			{Contains: "hello"},
		},
	}
	err := tc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be positive")
}

// --- Assertion.Validate tests ---

func TestAssertion_Validate_ExactlyOneRequired(t *testing.T) {
	// No assertion type set
	a := &soltesting.Assertion{}
	err := a.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestAssertion_Validate_TooManyTypes(t *testing.T) {
	a := &soltesting.Assertion{
		Contains: "hello",
		Regex:    "world",
	}
	err := a.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestAssertion_Validate_ValidTypes(t *testing.T) {
	tests := []struct {
		name      string
		assertion soltesting.Assertion
	}{
		{"expression", soltesting.Assertion{Expression: "true"}},
		{"regex", soltesting.Assertion{Regex: "^hello.*"}},
		{"contains", soltesting.Assertion{Contains: "hello"}},
		{"notRegex", soltesting.Assertion{NotRegex: "error"}},
		{"notContains", soltesting.Assertion{NotContains: "panic"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tt.assertion.Validate())
		})
	}
}

func TestAssertion_Validate_InvalidTarget(t *testing.T) {
	a := &soltesting.Assertion{
		Contains: "hello",
		Target:   "invalid",
	}
	err := a.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target must be one of")
}

func TestAssertion_Validate_ValidTargets(t *testing.T) {
	targets := []string{"", "stdout", "stderr", "combined"}
	for _, target := range targets {
		t.Run("target_"+target, func(t *testing.T) {
			a := &soltesting.Assertion{
				Contains: "hello",
				Target:   target,
			}
			assert.NoError(t, a.Validate())
		})
	}
}

func TestAssertion_Validate_InvalidRegex(t *testing.T) {
	a := &soltesting.Assertion{
		Regex: "[invalid",
	}
	err := a.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestAssertion_Validate_InvalidNotRegex(t *testing.T) {
	a := &soltesting.Assertion{
		NotRegex: "[invalid",
	}
	err := a.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid notRegex")
}

// --- YAML full struct round-trip tests ---

func TestTestCase_YAML_FullRoundTrip(t *testing.T) {
	input := `
name: render-test
description: Test rendering
command:
  - render
  - solution
args:
  - "-r"
  - "env=prod"
tags:
  - smoke
  - render
assertions:
  - contains: "hello"
    target: stdout
  - regex: "^output.*"
timeout: "30s"
retries: 2
`
	var tc soltesting.TestCase
	err := yaml.Unmarshal([]byte(input), &tc)
	require.NoError(t, err)

	assert.Equal(t, "render-test", tc.Name)
	assert.Equal(t, "Test rendering", tc.Description)
	assert.Equal(t, []string{"render", "solution"}, tc.Command)
	assert.Equal(t, []string{"-r", "env=prod"}, tc.Args)
	assert.Equal(t, []string{"smoke", "render"}, tc.Tags)
	assert.Len(t, tc.Assertions, 2)
	assert.Equal(t, "hello", tc.Assertions[0].Contains)
	assert.Equal(t, "stdout", tc.Assertions[0].Target)
	assert.Equal(t, "^output.*", tc.Assertions[1].Regex)
	require.NotNil(t, tc.Timeout)
	assert.Equal(t, 30*time.Second, tc.Timeout.Duration)
	assert.Equal(t, 2, tc.Retries)

	// Round-trip
	data, err := yaml.Marshal(tc)
	require.NoError(t, err)

	var tc2 soltesting.TestCase
	err = yaml.Unmarshal(data, &tc2)
	require.NoError(t, err)
	assert.Equal(t, tc.Name, tc2.Name)
	assert.Equal(t, tc.Command, tc2.Command)
	assert.Equal(t, tc.Timeout.Duration, tc2.Timeout.Duration)
}

func TestTestConfig_YAML_RoundTrip(t *testing.T) {
	input := `
skipBuiltins: true
env:
  KEY: value
setup:
  - command: "echo setup"
    timeout: 10
cleanup:
  - command: "echo cleanup"
`
	var tc soltesting.TestConfig
	err := yaml.Unmarshal([]byte(input), &tc)
	require.NoError(t, err)

	assert.True(t, tc.SkipBuiltins.All)
	assert.Equal(t, "value", tc.Env["KEY"])
	require.Len(t, tc.Setup, 1)
	assert.Equal(t, "echo setup", tc.Setup[0].Command)
	assert.Equal(t, 10, tc.Setup[0].Timeout)
	require.Len(t, tc.Cleanup, 1)
	assert.Equal(t, "echo cleanup", tc.Cleanup[0].Command)

	// Round-trip
	data, err := yaml.Marshal(tc)
	require.NoError(t, err)

	var tc2 soltesting.TestConfig
	err = yaml.Unmarshal(data, &tc2)
	require.NoError(t, err)
	assert.True(t, tc2.SkipBuiltins.All)
	assert.Equal(t, "value", tc2.Env["KEY"])
}

func TestTestConfig_YAML_SkipBuiltinsNames_RoundTrip(t *testing.T) {
	input := `
skipBuiltins:
  - lint
  - parse
`
	var tc soltesting.TestConfig
	err := yaml.Unmarshal([]byte(input), &tc)
	require.NoError(t, err)

	assert.False(t, tc.SkipBuiltins.All)
	assert.Equal(t, []string{"lint", "parse"}, tc.SkipBuiltins.Names)

	// Round-trip
	data, err := yaml.Marshal(tc)
	require.NoError(t, err)

	var tc2 soltesting.TestConfig
	err = yaml.Unmarshal(data, &tc2)
	require.NoError(t, err)
	assert.False(t, tc2.SkipBuiltins.All)
	assert.Equal(t, []string{"lint", "parse"}, tc2.SkipBuiltins.Names)
}
