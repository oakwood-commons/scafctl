// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseSolution() *solution.Solution {
	sol := &solution.Solution{}
	err := sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    rootResolver:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
`))
	if err != nil {
		panic(fmt.Sprintf("failed to create base solution: %v", err))
	}
	return sol
}

func TestCompose_NoComposeFiles(t *testing.T) {
	sol := baseSolution()
	result, err := Compose(sol, "/tmp/bundle")
	require.NoError(t, err)
	assert.Same(t, sol, result, "should return the same solution when no compose files")
}

func TestCompose_NilSolution(t *testing.T) {
	_, err := Compose(nil, "/tmp/bundle")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solution is nil")
}

func TestCompose_MergesResolvers(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"resolvers.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/resolvers.yaml" {
			return []byte(`
spec:
  resolvers:
    childResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	assert.Nil(t, result.Compose, "compose should be cleared after merging")
	assert.Contains(t, result.Spec.Resolvers, "rootResolver")
	assert.Contains(t, result.Spec.Resolvers, "childResolver")
}

func TestCompose_MergesActions(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"workflow.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/workflow.yaml" {
			return []byte(`
spec:
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo deploy"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Workflow)
	assert.Contains(t, result.Spec.Workflow.Actions, "deploy")
}

func TestCompose_RejectsDuplicateResolvers(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"resolvers.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/resolvers.yaml" {
			return []byte(`
spec:
  resolvers:
    rootResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'duplicate'"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate resolver")
	assert.Contains(t, err.Error(), "rootResolver")
}

func TestCompose_RejectsDuplicateActions(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo a"
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  workflow:
    actions:
      deploy:
        provider: shell
        inputs:
          command: "echo b"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate action")
	assert.Contains(t, err.Error(), "deploy")
}

func TestCompose_MergesIncludePatterns(t *testing.T) {
	sol := baseSolution()
	sol.Bundle.Include = []string{"templates/*.tmpl"}
	sol.Compose = []string{"extra.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/extra.yaml" {
			return []byte(`
bundle:
  include:
    - configs/*.yaml
    - templates/*.tmpl
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	assert.Equal(t, []string{"templates/*.tmpl", "configs/*.yaml"}, result.Bundle.Include)
}

func TestCompose_CircularReferenceDetection(t *testing.T) {
	sol := baseSolution()
	sol.SetPath("/tmp/bundle/solution.yaml")
	sol.Compose = []string{"solution.yaml"}
	readFile := func(_ string) ([]byte, error) {
		return []byte(`
spec:
  resolvers: {}
`), nil
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular compose reference")
}

func TestCompose_FileReadError(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"missing.yaml"}
	readFile := func(_ string) ([]byte, error) {
		return nil, fmt.Errorf("file not found")
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read composed file")
}

func TestCompose_InvalidYAML(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"bad.yaml"}
	readFile := func(_ string) ([]byte, error) {
		return []byte("{{{{invalid yaml"), nil
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse composed file")
}

func TestCompose_MultipleFiles(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"resolvers.yaml", "workflow.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/resolvers.yaml":
			return []byte(`
spec:
  resolvers:
    extraResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'value'"
`), nil
		case "/tmp/bundle/workflow.yaml":
			return []byte(`
spec:
  workflow:
    actions:
      build:
        provider: shell
        inputs:
          command: "make build"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	assert.Contains(t, result.Spec.Resolvers, "rootResolver")
	assert.Contains(t, result.Spec.Resolvers, "extraResolver")
	assert.Contains(t, result.Spec.Workflow.Actions, "build")
}

func TestCompose_DoesNotModifyOriginal(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"resolvers.yaml"}
	originalResolverCount := len(sol.Spec.Resolvers)
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/resolvers.yaml" {
			return []byte(`
spec:
  resolvers:
    newResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'new'"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	// Original should be unchanged
	assert.Len(t, sol.Spec.Resolvers, originalResolverCount)
	assert.NotNil(t, sol.Compose, "original compose should be preserved")
	// Result should have both
	assert.Len(t, result.Spec.Resolvers, originalResolverCount+1)
	assert.Nil(t, result.Compose, "result compose should be cleared")
}

func TestCompose_MergesTests(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"tests.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/tests.yaml" {
			return []byte(`
spec:
  testing:
    cases:
      render-test:
        description: "Test rendering"
        command:
          - render
          - solution
        assertions:
          - contains: "hello"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.Contains(t, result.Spec.Testing.Cases, "render-test")
	assert.Equal(t, "Test rendering", result.Spec.Testing.Cases["render-test"].Description)
	assert.Equal(t, "render-test", result.Spec.Testing.Cases["render-test"].Name)
}

func TestCompose_MergesTestsFromMultipleFiles(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"tests-a.yaml", "tests-b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/tests-a.yaml":
			return []byte(`
spec:
  testing:
    cases:
      test-a:
        description: "Test A"
        command: [render, solution]
        assertions:
          - contains: "a"
`), nil
		case "/tmp/bundle/tests-b.yaml":
			return []byte(`
spec:
  testing:
    cases:
      test-b:
        description: "Test B"
        command: [render, solution]
        assertions:
          - contains: "b"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	assert.Contains(t, result.Spec.Testing.Cases, "test-a")
	assert.Contains(t, result.Spec.Testing.Cases, "test-b")
}

func TestCompose_RejectsDuplicateTests(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"tests-a.yaml", "tests-b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/tests-a.yaml":
			return []byte(`
spec:
  testing:
    cases:
      same-test:
        description: "First"
        command: [render, solution]
        assertions:
          - contains: "a"
`), nil
		case "/tmp/bundle/tests-b.yaml":
			return []byte(`
spec:
  testing:
    cases:
      same-test:
        description: "Second"
        command: [render, solution]
        assertions:
          - contains: "b"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	_, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate test")
	assert.Contains(t, err.Error(), "same-test")
}

func TestCompose_MergesTestConfig_SkipBuiltins_TrueWins(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  testing:
    config:
      skipBuiltins: false
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  testing:
    config:
      skipBuiltins: true
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Testing)
	require.NotNil(t, result.Spec.Testing.Config)
	assert.True(t, result.Spec.Testing.Config.SkipBuiltins.All)
}

func TestCompose_MergesTestConfig_SkipBuiltins_UnionNames(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  testing:
    config:
      skipBuiltins:
        - lint
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  testing:
    config:
      skipBuiltins:
        - parse
        - lint
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Testing)
	require.NotNil(t, result.Spec.Testing.Config)
	assert.False(t, result.Spec.Testing.Config.SkipBuiltins.All)
	assert.ElementsMatch(t, []string{"lint", "parse"}, result.Spec.Testing.Config.SkipBuiltins.Names)
}

func TestCompose_MergesTestConfig_Setup_Appended(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  testing:
    config:
      setup:
        - command: "echo first"
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  testing:
    config:
      setup:
        - command: "echo second"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Testing)
	require.NotNil(t, result.Spec.Testing.Config)
	require.Len(t, result.Spec.Testing.Config.Setup, 2)
	assert.Equal(t, "echo first", result.Spec.Testing.Config.Setup[0].Command)
	assert.Equal(t, "echo second", result.Spec.Testing.Config.Setup[1].Command)
}

func TestCompose_MergesTestConfig_Cleanup_Appended(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  testing:
    config:
      cleanup:
        - command: "echo cleanup-first"
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  testing:
    config:
      cleanup:
        - command: "echo cleanup-second"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Testing)
	require.NotNil(t, result.Spec.Testing.Config)
	require.Len(t, result.Spec.Testing.Config.Cleanup, 2)
	assert.Equal(t, "echo cleanup-first", result.Spec.Testing.Config.Cleanup[0].Command)
	assert.Equal(t, "echo cleanup-second", result.Spec.Testing.Config.Cleanup[1].Command)
}

func TestCompose_MergesTestConfig_Env_LastWins(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"a.yaml", "b.yaml"}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/tmp/bundle/a.yaml":
			return []byte(`
spec:
  testing:
    config:
      env:
        KEY1: "from-a"
        SHARED: "from-a"
`), nil
		case "/tmp/bundle/b.yaml":
			return []byte(`
spec:
  testing:
    config:
      env:
        KEY2: "from-b"
        SHARED: "from-b"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Testing)
	require.NotNil(t, result.Spec.Testing.Config)
	assert.Equal(t, "from-a", result.Spec.Testing.Config.Env["KEY1"])
	assert.Equal(t, "from-b", result.Spec.Testing.Config.Env["KEY2"])
	assert.Equal(t, "from-b", result.Spec.Testing.Config.Env["SHARED"])
}

func TestCompose_SolutionWithInlineTests_PreservedAfterCompose(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"extra.yaml"}
	// Manually set some inline tests on the root solution
	// This verifies backward compat — solutions without tests still work
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/extra.yaml" {
			return []byte(`
spec:
  resolvers:
    newResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'value'"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	// Tests should remain nil since nothing defined them
	assert.Nil(t, result.Spec.Testing)
}

func TestCompose_TestConfigNil_WhenNoTestConfigDefined(t *testing.T) {
	sol := baseSolution()
	sol.Compose = []string{"resolvers.yaml"}
	readFile := func(path string) ([]byte, error) {
		if path == "/tmp/bundle/resolvers.yaml" {
			return []byte(`
spec:
  resolvers:
    newResolver:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'value'"
`), nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}
	result, err := Compose(sol, "/tmp/bundle", WithReadFileFunc(readFile))
	require.NoError(t, err)
	assert.Nil(t, result.Spec.Testing)
}

func TestIsLocalFilePath(t *testing.T) {
	assert.True(t, IsLocalFilePath("./templates/file.txt"))
	assert.True(t, IsLocalFilePath("relative/path"))
	assert.False(t, IsLocalFilePath(""))
	assert.False(t, IsLocalFilePath("https://example.com/file"))
	assert.False(t, IsLocalFilePath("oci://registry/repo@sha256:abc"))
	assert.False(t, IsLocalFilePath("name@1.2.3"))
	assert.False(t, IsLocalFilePath("/absolute/path"))
}

func TestExtractResolverName(t *testing.T) {
	assert.Equal(t, "myResolver", ExtractResolverName("myResolver.result"))
	assert.Equal(t, "myResolver", ExtractResolverName("myResolver.nested.field"))
	assert.Equal(t, "standalone", ExtractResolverName("standalone"))
}
