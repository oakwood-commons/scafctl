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
		Short: fmt.Sprintf("Validate %s solution artifacts and fail on validation errors", path),
		Long: `Validate executes a solution's artifacts and exits non-zero when validation fails.

Unlike the 'run' commands, which surface validation failures as non-fatal
diagnostics, 'validate' treats them as errors. Use it as a validation gate in
CI pipelines or pre-commit checks.

SUBCOMMANDS:
  resolver  Validate resolvers (resolve, transform, and validate phases)`,
		SilenceUsage: true,
	})

	validatePath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(run.CommandValidateResolver(cliParams, ioStreams, validatePath))

	return cCmd
}
