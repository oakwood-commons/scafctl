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
              name: "env"
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
