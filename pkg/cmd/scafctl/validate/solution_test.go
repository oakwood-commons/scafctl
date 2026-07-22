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

// TestCommandValidateSolution_QuietEmptyStdout verifies that `-o quiet` on a
// clean solution produces NO stdout (quiet's exit-code-only contract).
func TestCommandValidateSolution_QuietEmptyStdout(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cleanSolution), 0o600))

	streams, _, out := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"-f", p, "-o", "quiet"})
	require.NoError(t, cmd.Execute())
	assert.Empty(t, out.String(), "quiet must produce no stdout on a clean solution")
}

// TestCommandValidateSolution_PositionalCatalogRef verifies a positional
// catalog reference is accepted (routed to opts.File) rather than being
// rejected as a local path.
func TestCommandValidateSolution_PositionalCatalogRef(t *testing.T) {
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	// A bare catalog name (not a local path). It won't resolve to a real
	// solution here, but PreRunE must accept it (no "must use -f" error).
	cmd.SetArgs([]string{"nonexistent-catalog-ref", "-o", "quiet"})
	err := cmd.Execute()
	// Execution fails at load time (catalog miss), NOT at arg validation.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "must use -f/--file")
	assert.NotContains(t, err.Error(), "cannot use both")
}

// TestCommandValidateSolution_PositionalConflictsWithFile verifies that
// supplying both a positional arg and -f is rejected clearly.
func TestCommandValidateSolution_PositionalConflictsWithFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cleanSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"some-ref", "-f", p})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

// TestCommandValidateSolution_PositionalLocalPathRejected verifies that a
// positional local file path is rejected in favor of -f (mirroring lint).
func TestCommandValidateSolution_PositionalLocalPathRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cleanSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	cmd.SetArgs([]string{"./" + filepath.Base(p)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-f/--file")
}

// TestCommandValidateSolution_NoStdinClaim verifies the file flag help no
// longer advertises stdin support (which the loader does not implement).
func TestCommandValidateSolution_NoStdinClaim(t *testing.T) {
	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidateSolution(cliParams, streams, "scafctl/validate")
	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.NotContains(t, fileFlag.Usage, "stdin")
}
