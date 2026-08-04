// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"os"
	"testing"

	pkgrefactor "github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const callCmdFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: call-cmd-test # comment stays
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
  workflow:
    actions:
      a1:
        call: fetch
`

func TestRunRenameCall_HappyPath(t *testing.T) {
	path := writeFixture(t, callCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, CliParams: testCliParams()}, "call", pkgrefactor.RenameCall, "fetch", "download")
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, readErr)
	s := string(got)
	assert.Contains(t, s, "download:")
	assert.Contains(t, s, "- call: download")
	assert.NotContains(t, s, "call: fetch")
	assert.Contains(t, s, "# comment stays")
	assert.Contains(t, bufs.out.String(), "Renamed call")
}

func TestRunRenameCall_DryRun(t *testing.T) {
	path := writeFixture(t, callCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, DryRun: true, CliParams: testCliParams()}, "call", pkgrefactor.RenameCall, "fetch", "download")
	require.NoError(t, err)

	got, _ := os.ReadFile(path) //nolint:gosec // test-controlled path
	assert.Equal(t, callCmdFixture, string(got), "dry-run must not modify the file")
	assert.Contains(t, bufs.out.String(), "Would rename call")
	assert.Contains(t, bufs.out.String(), "fetch -> download")
}
