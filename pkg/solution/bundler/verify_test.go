// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyResult_Passed_NoErrors(t *testing.T) {
	r := &VerifyResult{}
	assert.True(t, r.Passed())
}

func TestVerifyResult_Passed_WithErrors(t *testing.T) {
	r := &VerifyResult{
		Errors: []VerifyError{{Path: "file.yaml", Reason: "missing"}},
	}
	assert.False(t, r.Passed())
}

func TestVerifyResult_Passed_OnlyWarnings(t *testing.T) {
	r := &VerifyResult{Warnings: []string{"some warning"}}
	assert.True(t, r.Passed())
}

// TestVerifyBundle_NoBundle_WarnsIndependentOfCWD verifies that the no-bundle
// completeness path emits warnings for referenced local files and catalog deps
// based on the solution SPEC, regardless of the current working directory.
// Previously this used DiscoverFiles(sol, "."), which stats files against the
// CWD -- so running from an unrelated directory produced no warnings and let
// --strict wrongly pass on a bundle-less-but-incomplete artifact.
func TestVerifyBundle_NoBundle_WarnsIndependentOfCWD(t *testing.T) {
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    myFile:
      resolve:
        with:
          - provider: file
            inputs:
              path: "templates/main.tmpl"
              operation: read
    mySub:
      resolve:
        with:
          - provider: solution
            inputs:
              source: "deploy-to-k8s@2.0.0"
`)))

	// Run from a temp directory that does NOT contain templates/main.tmpl, to
	// prove the warnings do not depend on the CWD / filesystem.
	origWD, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	require.NoError(t, os.Chdir(t.TempDir()))

	// Empty bundleData -> the no-bundle path.
	result, err := VerifyBundle(context.Background(), sol, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, result)

	joined := ""
	for _, w := range result.Warnings {
		joined += w + "\n"
	}
	assert.Contains(t, joined, "local files but has no bundle",
		"must warn about referenced local files regardless of CWD")
	assert.Contains(t, joined, "catalog dependencies but has no vendored copies",
		"must warn about referenced catalog deps regardless of CWD")
}
