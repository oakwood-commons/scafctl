// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandValidate creates the 'validate' command that validates solution
// artifacts and exits non-zero when validation fails.
func CommandValidate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "validate",
		Short: fmt.Sprintf("Validate that a %s definition is correct and ready (runs lint)", path),
		Long: `Validate is THE gate that determines whether a definition is correct and ready.

'validate solution' loads a solution and runs lint (which includes a JSON
Schema conformance check): lint errors and schema violations fail; lint
warnings are surfaced and, with --strict, are also fatal. 'validate resolver'
executes the resolver phases and, after they pass, additionally runs lint. In
this way 'validate' is the pass/fail gate for CI pipelines and pre-commit
checks, while 'lint' is the advisory subset that only reports authoring
warnings.

SUBCOMMANDS:
  solution  Validate a solution end-to-end (load + lint) -- the primary gate
  resolver  Validate resolvers (resolve, transform, validate) then run lint
  schema    Validate arbitrary data against a JSON Schema (JSON or YAML)`,
		SilenceUsage: true,
	})

	validatePath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(CommandValidateSolution(cliParams, ioStreams, validatePath))
	cCmd.AddCommand(run.CommandValidateResolver(cliParams, ioStreams, validatePath))
	cCmd.AddCommand(CommandValidateSchema(cliParams, ioStreams, validatePath))

	return cCmd
}
