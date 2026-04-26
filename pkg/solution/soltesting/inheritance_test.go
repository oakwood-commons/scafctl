// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveExtends_SimpleInheritance(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Command: []string{"render", "solution"},
			Args:    []string{"--verbose"},
			Assertions: []soltesting.Assertion{
				{Contains: "success"},
			},
			Tags: []string{"smoke"},
		},
		"child": {
			Name:        "child",
			Extends:     []string{"_base"},
			Args:        []string{"--output", "json"},
			Description: "child test",
			Assertions: []soltesting.Assertion{
				{Contains: "result"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Empty(t, child.Extends)
	assert.Equal(t, []string{"render", "solution"}, child.Command)
	assert.Equal(t, []string{"--verbose", "--output", "json"}, child.Args)
	assert.Len(t, child.Assertions, 2)
	assert.Equal(t, "success", child.Assertions[0].Contains)
	assert.Equal(t, "result", child.Assertions[1].Contains)
	assert.Equal(t, []string{"smoke"}, child.Tags)
	assert.Equal(t, "child test", child.Description)
}

func TestResolveExtends_MultiExtends(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base1": {
			Name:    "_base1",
			Command: []string{"render", "solution"},
			Tags:    []string{"smoke"},
			Env:     map[string]string{"KEY1": "val1", "SHARED": "base1"},
		},
		"_base2": {
			Name: "_base2",
			Args: []string{"--flag"},
			Tags: []string{"integration", "smoke"},
			Env:  map[string]string{"KEY2": "val2", "SHARED": "base2"},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base1", "_base2"},
			Assertions: []soltesting.Assertion{
				{Contains: "output"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, []string{"render", "solution"}, child.Command)
	assert.Equal(t, []string{"--flag"}, child.Args)
	assert.Equal(t, []string{"smoke", "integration"}, child.Tags)
	assert.Equal(t, "base2", child.Env["SHARED"])
	assert.Equal(t, "val1", child.Env["KEY1"])
	assert.Equal(t, "val2", child.Env["KEY2"])
}

func TestResolveExtends_DeepChain(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_grandparent": {
			Name:    "_grandparent",
			Command: []string{"lint"},
			Tags:    []string{"base"},
		},
		"_parent": {
			Name:    "_parent",
			Extends: []string{"_grandparent"},
			Args:    []string{"--strict"},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_parent"},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, []string{"lint"}, child.Command)
	assert.Equal(t, []string{"--strict"}, child.Args)
	assert.Equal(t, []string{"base"}, child.Tags)
	assert.Len(t, child.Assertions, 1)
}

func TestResolveExtends_CircularDetection(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"a": {Name: "a", Extends: []string{"b"}, Command: []string{"x"}},
		"b": {Name: "b", Extends: []string{"a"}, Command: []string{"y"}},
	}

	err := soltesting.ResolveExtends(tests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestResolveExtends_NonExistentReference(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"child": {Name: "child", Extends: []string{"_doesnt_exist"}, Command: []string{"x"}},
	}

	err := soltesting.ResolveExtends(tests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestResolveExtends_DepthLimit(t *testing.T) {
	tests := make(map[string]*soltesting.TestCase)
	// Build a chain deeper than MaxExtendsDepth + 2 to ensure depth check triggers
	depth := soltesting.MaxExtendsDepth + 3
	for i := 0; i <= depth; i++ {
		name := fmt.Sprintf("_level%d", i)
		tc := &soltesting.TestCase{Name: name, Command: []string{"x"}}
		if i > 0 {
			tc.Extends = []string{fmt.Sprintf("_level%d", i-1)}
		}
		tests[name] = tc
	}

	err := soltesting.ResolveExtends(tests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depth exceeds")
}

func TestResolveExtends_InitMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name: "_base",
			Init: []soltesting.InitStep{{Command: "base-setup"}},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Command: []string{"test"},
			Init:    []soltesting.InitStep{{Command: "child-setup"}},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	require.Len(t, child.Init, 2)
	assert.Equal(t, "base-setup", child.Init[0].Command)
	assert.Equal(t, "child-setup", child.Init[1].Command)
}

func TestResolveExtends_CleanupMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Cleanup: []soltesting.InitStep{{Command: "base-cleanup"}},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Command: []string{"test"},
			Cleanup: []soltesting.InitStep{{Command: "child-cleanup"}},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	require.Len(t, child.Cleanup, 2)
	assert.Equal(t, "base-cleanup", child.Cleanup[0].Command)
	assert.Equal(t, "child-cleanup", child.Cleanup[1].Command)
}

func TestResolveExtends_ScalarChildWins(t *testing.T) {
	baseTimeout := duration.New(30 * time.Second)
	childTimeout := duration.New(60 * time.Second)
	exitCode := 42

	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:          "_base",
			Description:   "base desc",
			Timeout:       &baseTimeout,
			ExpectFailure: true,
			Skip:          soltesting.SkipValue{Expression: celexp.Expression("true")},
			Retries:       2,
		},
		"child": {
			Name:        "child",
			Extends:     []string{"_base"},
			Command:     []string{"test"},
			Description: "child desc",
			Timeout:     &childTimeout,
			ExitCode:    &exitCode,
			Retries:     5,
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, "child desc", child.Description)
	assert.Equal(t, 60*time.Second, child.Timeout.Duration)
	assert.True(t, child.ExpectFailure)
	assert.Equal(t, 42, *child.ExitCode)
	assert.Equal(t, 5, child.Retries)
	assert.Equal(t, celexp.Expression("true"), child.Skip.Expression)
}

func TestResolveExtends_FilesMergeDedup(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:  "_base",
			Files: []string{"shared.yaml", "common.yaml"},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Command: []string{"test"},
			Files:   []string{"test.yaml", "shared.yaml"},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, []string{"shared.yaml", "common.yaml", "test.yaml"}, child.Files)
}

func TestResolveExtends_EnvMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name: "_base",
			Env:  map[string]string{"BASE": "val", "SHARED": "base-val"},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Command: []string{"test"},
			Env:     map[string]string{"CHILD": "val", "SHARED": "child-val"},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, "val", child.Env["BASE"])
	assert.Equal(t, "val", child.Env["CHILD"])
	assert.Equal(t, "child-val", child.Env["SHARED"])
}

func TestExtendsChainString(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_grandparent": {Name: "_grandparent"},
		"_parent":      {Name: "_parent", Extends: []string{"_grandparent"}},
		"child":        {Name: "child", Extends: []string{"_parent"}},
	}

	chain := soltesting.ExtendsChainString(tests, "child")
	assert.Equal(t, "child -> _parent -> _grandparent", chain)
}

func TestResolveExtends_InputsMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Command: []string{"run", "resolver"},
			Inputs: map[string]string{
				"env":    "dev",
				"region": "us-west-2",
			},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Inputs: map[string]string{
				"env": "prod",  // override
				"app": "myapp", // new key
			},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, "prod", child.Inputs["env"])         // child overrides parent
	assert.Equal(t, "us-west-2", child.Inputs["region"]) // inherited from parent
	assert.Equal(t, "myapp", child.Inputs["app"])        // child-only key
}

func TestResolveExtends_ServicesMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Command: []string{"run", "resolver"},
			Services: []soltesting.ServiceConfig{
				{Name: "api", Type: "http"},
			},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Services: []soltesting.ServiceConfig{
				{Name: "db", Type: "http"},
			},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Len(t, child.Services, 2)
	assert.Equal(t, "api", child.Services[0].Name)
	assert.Equal(t, "db", child.Services[1].Name)
}

func TestResolveExtends_MocksMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Command: []string{"run", "resolver"},
			Mocks: map[string]any{
				"token": "base-token",
				"cfg":   "base-cfg",
			},
		},
		"child": {
			Name:    "child",
			Extends: []string{"_base"},
			Mocks: map[string]any{
				"token": "child-token", // override
				"extra": "child-extra", // new key
			},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	child := tests["child"]
	assert.Equal(t, "child-token", child.Mocks["token"]) // child overrides parent
	assert.Equal(t, "base-cfg", child.Mocks["cfg"])      // inherited from parent
	assert.Equal(t, "child-extra", child.Mocks["extra"]) // child-only key
}

func TestResolveExtends_BaseDirMerge(t *testing.T) {
	tests := map[string]*soltesting.TestCase{
		"_base": {
			Name:    "_base",
			Command: []string{"run", "resolver"},
			BaseDir: "myapp",
		},
		"child-inherits": {
			Name:    "child-inherits",
			Extends: []string{"_base"},
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
		"child-overrides": {
			Name:    "child-overrides",
			Extends: []string{"_base"},
			BaseDir: "otherapp",
			Assertions: []soltesting.Assertion{
				{Contains: "ok"},
			},
		},
	}

	require.NoError(t, soltesting.ResolveExtends(tests))

	assert.Equal(t, "myapp", tests["child-inherits"].BaseDir, "child should inherit BaseDir from parent")
	assert.Equal(t, "otherapp", tests["child-overrides"].BaseDir, "child should override BaseDir")
}
