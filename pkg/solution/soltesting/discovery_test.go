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
  tests:
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
  testConfig:
    skipBuiltins: true
    env:
      FOO: bar
  tests:
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
	assert.Len(t, results[0].Tests, 3)
}

func TestDiscoverFromFile(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithTests), 0o644))

	st, err := soltesting.DiscoverFromFile(solFile)
	require.NoError(t, err)
	require.NotNil(t, st)

	assert.Equal(t, "my-solution", st.SolutionName)
	assert.Contains(t, st.Tests, "smoke-test")
	assert.Contains(t, st.Tests, "integration-test")
	assert.Contains(t, st.Tests, "_base-template")
	assert.Equal(t, "smoke-test", st.Tests["smoke-test"].Name)
}

func TestDiscoverFromFile_WithTestConfig(t *testing.T) {
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(discoverySolutionWithTestConfig), 0o644))

	st, err := soltesting.DiscoverFromFile(solFile)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.NotNil(t, st.TestConfig)
	assert.True(t, st.TestConfig.SkipBuiltins.All)
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
			Tests: map[string]*soltesting.TestCase{
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
	assert.Contains(t, result[0].Tests, "smoke-test")
	assert.NotContains(t, result[0].Tests, "integration-test")
	assert.NotContains(t, result[0].Tests, "_template")
}

func TestFilterTests_SolutionTestPattern(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Tests: map[string]*soltesting.TestCase{
				"smoke-test": {Name: "smoke-test", Command: []string{"render"}},
				"other-test": {Name: "other-test", Command: []string{"run"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		NamePatterns: []string{"my-solution/smoke*"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Tests, "smoke-test")
	assert.NotContains(t, result[0].Tests, "other-test")
}

func TestFilterTests_TagFilter(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Tests: map[string]*soltesting.TestCase{
				"smoke-test":       {Name: "smoke-test", Tags: []string{"smoke", "fast"}, Command: []string{"x"}},
				"integration-test": {Name: "integration-test", Tags: []string{"integration"}, Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{
		Tags: []string{"smoke"},
	})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Tests, "smoke-test")
	assert.NotContains(t, result[0].Tests, "integration-test")
}

func TestFilterTests_SolutionFilter(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "alpha",
			Tests: map[string]*soltesting.TestCase{
				"test1": {Name: "test1", Command: []string{"x"}},
			},
		},
		{
			SolutionName: "beta",
			Tests: map[string]*soltesting.TestCase{
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
			Tests: map[string]*soltesting.TestCase{
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
	assert.Contains(t, result[0].Tests, "smoke-fast")
	assert.NotContains(t, result[0].Tests, "smoke-slow")
	assert.NotContains(t, result[0].Tests, "other")
}

func TestFilterTests_TemplateExclusion(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Tests: map[string]*soltesting.TestCase{
				"test1":     {Name: "test1", Command: []string{"x"}},
				"_template": {Name: "_template", Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{})

	require.Len(t, result, 1)
	assert.Contains(t, result[0].Tests, "test1")
	assert.NotContains(t, result[0].Tests, "_template")
}

func TestFilterTests_NoFilters(t *testing.T) {
	solutions := []soltesting.SolutionTests{
		{
			SolutionName: "my-solution",
			Tests: map[string]*soltesting.TestCase{
				"test1": {Name: "test1", Command: []string{"x"}},
				"test2": {Name: "test2", Command: []string{"y"}},
			},
		},
	}

	result := soltesting.FilterTests(solutions, soltesting.FilterOptions{})

	require.Len(t, result, 1)
	assert.Len(t, result[0].Tests, 2)
}

func TestSortedTestNames(t *testing.T) {
	st := soltesting.SolutionTests{
		Tests: map[string]*soltesting.TestCase{
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
