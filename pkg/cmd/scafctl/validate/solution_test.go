// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cleanSolution = `apiVersion: scafctl.io/v1
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

// unusedResolverSolution defines a resolver that is never referenced, which
// triggers the unused-resolver lint WARNING (fatal only under --strict).
const unusedResolverSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-solution
  version: 1.0.0
spec:
  resolvers:
    orphan:
      name: orphan
      description: never referenced anywhere
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  workflow:
    actions:
      show:
        name: show
        description: print a constant
        provider: message
        inputs:
          message: "hi"
`

func TestCommandValidateSolution_Metadata(t *testing.T) {
	t.Parallel()
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandValidateSolution(cliParams, streams, "mycli/validate")
	assert.Equal(t, "solution", cmd.Name())
	assert.Contains(t, cmd.Short, "mycli")
	assert.NotNil(t, cmd.Flags().Lookup("file"))
	assert.NotNil(t, cmd.Flags().Lookup("strict"))
}

func TestCommandValidateSolution_Clean(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cleanSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"-f", p, "-o", "quiet"})
	require.NoError(t, cmd.Execute())
}

func TestCommandValidateSolution_WarningPassesNonStrict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(unusedResolverSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"-f", p, "-o", "quiet"})
	require.NoError(t, cmd.Execute(), "a lint warning must not fail without --strict")
}

func TestCommandValidateSolution_WarningFailsStrict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(unusedResolverSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"-f", p, "-o", "quiet", "--strict"})
	err := cmd.Execute()
	require.Error(t, err, "a lint warning must fail under --strict")
	assert.Equal(t, exitcode.ValidationFailed, exitcode.GetCode(err))
}

func TestCommandValidateSolution_MissingFile(t *testing.T) {
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"-f", filepath.Join(t.TempDir(), "nope.yaml"), "-o", "quiet"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
}
