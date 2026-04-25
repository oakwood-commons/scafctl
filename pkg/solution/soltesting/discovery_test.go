// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const discoverySolutionWithTests = `apiVersion: scafctl.io/v1
metadata:
  name: my-solution
spec:
  testing:
    cases:
      smoke-test:
        command: [render, solution]
        tags: [smoke, fast]
        assertions:
          - contains: "success"
      integration-test:
        command: [run, resolver]
        tags: [integration]
        assertions:
          - contains: "resolved"
      _base-template:
        command: [lint]
        tags: [base]
`

const discoverySolutionWithTestConfig = `apiVersion: scafctl.io/v1
metadata:
  name: configured-solution
spec:
  testing:
    config:
      skipBuiltins: true
      env:
        FOO: bar
    cases:
      basic:
        command: [render, solution]
        assertions:
          - contains: "ok"
`

const discoverySolutionWithoutTests = `apiVersion: scafctl.io/v1
metadata:
  name: no-tests
spec:
  resolvers: []
`

func TestDiscoverSolutions_Directory(t *testing.T) {
	dir := t.TempDir()

	writeSandboxFile(t, dir, "sol1/solution.yaml", discoverySolutionWithTests)
	writeSandboxFile(t, dir, "sol2/solution.yaml", discoverySolutionWithTestConfig)
	writeSandboxFile(t, dir, "sol3/solution.yaml", discoverySolutionWithoutTests)
	writeSandboxFile(t, dir, "notasolution.txt", "just text")

	results, err := soltesting.DiscoverSolutions(dir)
	require.NoError(t, err)

	require.Len(t, results, 2)
	assert.Equal(t, "configured-solution", results[0].SolutionName)
	assert.Equal(t, "my-solution", results[1].SolutionName)
}

func TestDiscoverSolutions_SingleFile(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithTests), 0o644))

	results, err := soltesting.DiscoverSolutions(solFile)
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "my-solution", results[0].SolutionName)
	assert.Len(t, results[0].Cases, 3)
}

func TestDiscoverFromFile(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithTests), 0o644))

	st, err := soltesting.DiscoverFromFile(solFile)
	require.NoError(t, err)
	require.NotNil(t, st)

	assert.Equal(t, "my-solution", st.SolutionName)
	assert.Contains(t, st.Cases, "smoke-test")
	assert.Contains(t, st.Cases, "integration-test")
	assert.Contains(t, st.Cases, "_base-template")
	assert.Equal(t, "smoke-test", st.Cases["smoke-test"].Name)
}

func TestDiscoverFromFile_WithTestConfig(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithTestConfig), 0o644))

	st, err := soltesting.DiscoverFromFile(solFile)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.NotNil(t, st.Config)
	assert.True(t, st.Config.SkipBuiltins.All)
}

func TestDiscoverFromFile_NoTests(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithoutTests), 0o644))

	st, err := soltesting.DiscoverFromFile(solFile)
	require.NoError(t, err)
	assert.Nil(t, st)
}

func TestFilterTests_NamePattern(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"smoke-test":       {Name: "smoke-test", Command: []string{"render"}},
				"integration-test": {Name: "integration-test", Command: []string{"run"}},
				"_template":        {Name: "_template", Command: []string{"x"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		NamePatterns: []string{"smoke*"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Cases, "smoke-test")
	assert.NotContains(t, result[0].Cases, "integration-test")
	assert.NotContains(t, result[0].Cases, "_template")
}

func TestFilterTests_SolutionTestPattern(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"smoke-test": {Name: "smoke-test", Command: []string{"render"}},
				"other-test": {Name: "other-test", Command: []string{"run"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		NamePatterns: []string{"my-solution/smoke*"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Cases, "smoke-test")
	assert.NotContains(t, result[0].Cases, "other-test")
}

func TestFilterTests_TagFilter(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"smoke-test":       {Name: "smoke-test", Tags: []string{"smoke", "fast"}, Command: []string{"x"}},
				"integration-test": {Name: "integration-test", Tags: []string{"integration"}, Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		Tags: []string{"smoke"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Cases, "smoke-test")
	assert.NotContains(t, result[0].Cases, "integration-test")
}

func TestFilterTests_SolutionFilter(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "alpha",
			Cases: map[string]*soltesting.TestCase{
				"test1": {Name: "test1", Command: []string{"x"}},
			},
		},
		{
			SolutionName: "beta",
			Cases: map[string]*soltesting.TestCase{
				"test2": {Name: "test2", Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		SolutionPatterns: []string{"alpha"},
	})

	require.Len(t, result, 1)
	assert.Equal(t, "alpha", result[0].SolutionName)
}

func TestFilterTests_CombinedAND(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"smoke-fast": {Name: "smoke-fast", Tags: []string{"smoke"}, Command: []string{"x"}},
				"smoke-slow": {Name: "smoke-slow", Tags: []string{"integration"}, Command: []string{"y"}},
				"other":      {Name: "other", Tags: []string{"smoke"}, Command: []string{"z"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		NamePatterns: []string{"smoke*"},
		Tags:         []string{"smoke"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Cases, "smoke-fast")
	assert.NotContains(t, result[0].Cases, "smoke-slow")
	assert.NotContains(t, result[0].Cases, "other")
}

func TestFilterTests_TemplateExclusion(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"test1":     {Name: "test1", Command: []string{"x"}},
				"_template": {Name: "_template", Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Cases, "test1")
	assert.NotContains(t, result[0].Cases, "_template")
}

func TestFilterTests_NoFilters(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Cases: map[string]*soltesting.TestCase{
				"test1": {Name: "test1", Command: []string{"x"}},
				"test2": {Name: "test2", Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{})

	require.Len(t, result, 1)
	assert.Len(t, result[0].Cases, 2)
}

func TestFilterTests_PreservesMetadataFields(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName:   "my-solution",
			FilePath:       "/path/to/solution.yaml",
			BundleIncludes: []string{"templates/**", "config.yaml"},
			DetectedFiles:  []string{"data/seed.json"},
			Cases: map[string]*soltesting.TestCase{
				"test1": {Name: "test1", Command: []string{"x"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{})

	require.Len(t, result, 1)
	assert.Equal(t, []string{"templates/**", "config.yaml"}, result[0].BundleIncludes)
	assert.Equal(t, []string{"data/seed.json"}, result[0].DetectedFiles)
	assert.Equal(t, "/path/to/solution.yaml", result[0].FilePath)
}

func TestSortedTestNames(t *testing.T) {
	st := soltesting.SolutionTests{
		Cases: map[string]*soltesting.TestCase{
			"z-test":        {Name: "z-test"},
			"a-test":        {Name: "a-test"},
			"builtin:lint":  {Name: "builtin:lint"},
			"builtin:parse": {Name: "builtin:parse"},
			"m-test":        {Name: "m-test"},
		},
	}

	names := soltesting.SortedTestNames(st)
	require.Len(t, names, 5)

	assert.Equal(t, "builtin:lint", names[0])
	assert.Equal(t, "builtin:parse", names[1])
	assert.Equal(t, "a-test", names[2])
	assert.Equal(t, "m-test", names[3])
	assert.Equal(t, "z-test", names[4])
}

func TestDiscoverFromFile_DetectsDirectoryProviderFiles(t *testing.T) {
	solutionYAML := `apiVersion: scafctl/v1
kind: Solution
metadata:
  name: detect-files-test
spec:
  resolvers:
    my-template:
      type: object
      resolve:
        with:
          - provider: directory
            inputs:
              path: templates/app
              operation: list
    my-data:
      type: object
      resolve:
        with:
          - provider: directory
            inputs:
              path: data/configs
              operation: list
    no-directory:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    cases:
      basic:
        command: [run, resolver]
        assertions:
          - expression: '__exitCode == 0'
`
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o644))

	// Create the referenced directories so the solution is valid
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates", "app"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "data", "configs"), 0o755))

	st, err := soltesting.DiscoverFromFile(solPath)
	require.NoError(t, err)
	require.NotNil(t, st)

	assert.Equal(t, []string{"data/configs/**", "templates/app/**"}, st.DetectedFiles)
}

func TestDiscoverFromFile_NoDetectedFilesWithoutDirectoryProvider(t *testing.T) {
	solutionYAML := `apiVersion: scafctl/v1
kind: Solution
metadata:
  name: no-files-test
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    cases:
      basic:
        command: [run, resolver]
        assertions:
          - expression: '__exitCode == 0'
`
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(solutionYAML), 0o644))

	st, err := soltesting.DiscoverFromFile(solPath)
	require.NoError(t, err)
	require.NotNil(t, st)

	assert.Empty(t, st.DetectedFiles)
}

func TestDiscoverFromFile_DetectsFileDepsFromComposeResolvers(t *testing.T) {
	solutionYAML := `apiVersion: scafctl/v1
kind: Solution
metadata:
  name: compose-file-deps-test
compose:
  - resolvers.yaml
spec:
  testing:
    cases:
      basic:
        command: [run, resolver]
        assertions:
          - expression: '__exitCode == 0'
`
	resolversYAML := `apiVersion: scafctl/v1
kind: Solution
spec:
  resolvers:
    my-files:
      type: object
      resolve:
        with:
          - provider: directory
            inputs:
              path: templates/app
              operation: list
    my-data:
      type: object
      resolve:
        with:
          - provider: directory
            inputs:
              path: data/configs
              operation: list
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(solutionYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "resolvers.yaml"), []byte(resolversYAML), 0o644))

	st, err := soltesting.DiscoverFromFile(filepath.Join(dir, "solution.yaml"))
	require.NoError(t, err)
	require.NotNil(t, st)

	assert.Equal(t, []string{"data/configs/**", "templates/app/**"}, st.DetectedFiles)
}
