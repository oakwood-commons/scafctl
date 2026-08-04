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

const functionCmdFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: function-cmd-test # comment stays
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
    loud:
      params:
        - name: msg
      template: "{{ greet .args.msg }}!"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }}"
`

func TestRunRenameFunction_HappyPath(t *testing.T) {
	path := writeFixture(t, functionCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, CliParams: testCliParams()}, "function", pkgrefactor.RenameFunction, "greet", "welcome")
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, readErr)
	s := string(got)
	assert.Contains(t, s, "welcome:")
	assert.Contains(t, s, "{{ welcome .args.msg }}")
	assert.Contains(t, s, "{{ welcome ._.env }}")
	assert.NotContains(t, s, "greet")
	assert.Contains(t, s, "# comment stays")
	assert.Contains(t, bufs.out.String(), "Renamed function")
}

func TestRunRenameFunction_DryRun(t *testing.T) {
	path := writeFixture(t, functionCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, DryRun: true, CliParams: testCliParams()}, "function", pkgrefactor.RenameFunction, "greet", "welcome")
	require.NoError(t, err)

	got, _ := os.ReadFile(path) //nolint:gosec // test-controlled path
	assert.Equal(t, functionCmdFixture, string(got), "dry-run must not modify the file")
	assert.Contains(t, bufs.out.String(), "Would rename function")
	assert.Contains(t, bufs.out.String(), "greet -> welcome")
}
