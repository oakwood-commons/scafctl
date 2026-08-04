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

const actionCmdFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: action-cmd-test # comment stays
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        provider: shell
        inputs:
          command: make build
      deploy:
        dependsOn:
          - build
        provider: shell
        when:
          expr: __actions.build.results.exitCode == 0
        inputs:
          command:
            tmpl: 'deploy {{ .__actions.build.results.stdout }}'
`

func TestRunRenameAction_HappyPath(t *testing.T) {
	path := writeFixture(t, actionCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, CliParams: testCliParams()}, "action", pkgrefactor.RenameAction, "build", "compile")
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, readErr)
	s := string(got)
	// Every reference form is rewritten: definition, dependsOn, CEL, template.
	assert.Contains(t, s, "compile:")
	assert.Contains(t, s, "- compile")
	assert.Contains(t, s, "__actions.compile.results.exitCode")
	assert.Contains(t, s, ".__actions.compile.results.stdout")
	// The old action name must not survive in any reference form. The literal
	// 'command: make build' is unrelated text and is correctly left untouched.
	assert.NotContains(t, s, "build:")
	assert.NotContains(t, s, "- build")
	assert.NotContains(t, s, "__actions.build")
	assert.Contains(t, s, "# comment stays")
	assert.Contains(t, bufs.out.String(), "Renamed action")
}

func TestRunRenameAction_DryRunLeavesFileUnchanged(t *testing.T) {
	path := writeFixture(t, actionCmdFixture)
	ctx, bufs := testContext(t)

	err := runRename(ctx, &renameOptions{File: path, DryRun: true, CliParams: testCliParams()}, "action", pkgrefactor.RenameAction, "build", "compile")
	require.NoError(t, err)

	got, _ := os.ReadFile(path) //nolint:gosec // test-controlled path
	assert.Equal(t, actionCmdFixture, string(got), "dry-run must not modify the file")
	assert.Contains(t, bufs.out.String(), "Would rename action")
	assert.Contains(t, bufs.out.String(), "build -> compile")
}
