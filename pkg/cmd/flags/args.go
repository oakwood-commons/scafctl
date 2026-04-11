// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"fmt"

	"github.com/spf13/cobra"
)

// RequireArg returns a cobra.PositionalArgs validator that requires exactly one
// positional argument. If missing, it returns an error naming the expected
// argument and showing an example usage. This replaces cobra.ExactArgs(1)
// with a more descriptive error message.
func RequireArg(argName, example string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("missing required argument: <%s>\n\n  Example: %s\n  Run '%s --help' for more information",
				argName, example, cmd.CommandPath())
		}
		if len(args) > 1 {
			return fmt.Errorf("expected 1 argument <%s>, got %d\n\n  Example: %s\n  Run '%s --help' for more information",
				argName, len(args), example, cmd.CommandPath())
		}
		return nil
	}
}

// RequireArgs returns a cobra.PositionalArgs validator that requires exactly n
// positional arguments with a descriptive error message naming the expected
// arguments and showing an example.
func RequireArgs(n int, argDesc, example string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("expected %d argument(s) %s, got %d\n\n  Example: %s\n  Run '%s --help' for more information",
				n, argDesc, len(args), example, cmd.CommandPath())
		}
		return nil
	}
}
