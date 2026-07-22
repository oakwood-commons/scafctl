// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolutionResult_Passed(t *testing.T) {
	tests := []struct {
		name       string
		result     *SolutionResult
		strict     bool
		wantPassed bool
	}{
		{
			name:       "nil result fails",
			result:     nil,
			wantPassed: false,
		},
		{
			name:       "nil lint fails",
			result:     &SolutionResult{},
			wantPassed: false,
		},
		{
			name:       "clean passes",
			result:     &SolutionResult{Lint: &lint.Result{}},
			wantPassed: true,
		},
		{
			name:       "errors fail",
			result:     &SolutionResult{Lint: &lint.Result{ErrorCount: 1}},
			wantPassed: false,
		},
		{
			name:       "warnings pass in non-strict",
			result:     &SolutionResult{Lint: &lint.Result{WarnCount: 2}},
			strict:     false,
			wantPassed: true,
		},
		{
			name:       "warnings fail in strict",
			result:     &SolutionResult{Lint: &lint.Result{WarnCount: 2}},
			strict:     true,
			wantPassed: false,
		},
		{
			name:       "errors fail in strict even with no warnings",
			result:     &SolutionResult{Lint: &lint.Result{ErrorCount: 1}},
			strict:     true,
			wantPassed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantPassed, tc.result.Passed(tc.strict))
		})
	}
}

func TestValidateSolution_LoadsAndLints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: valid-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      name: greeting
      description: a friendly greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  workflow:
    actions:
      show:
        name: show
        description: print the greeting
        provider: message
        inputs:
          message: "{{ ._.greeting }}"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)

	res, err := Solution(context.Background(), path, reg)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, path, res.File)
	require.NotNil(t, res.Lint)
	assert.Equal(t, 0, res.Lint.ErrorCount, "clean solution should have no lint errors: %+v", res.Lint.Findings)
	assert.True(t, res.Passed(false))
}

func TestValidateSolution_MissingFile(t *testing.T) {
	res, err := Solution(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"), nil)
	require.Error(t, err)
	assert.Nil(t, res)
}
