// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package cmdutil provides shared helpers for wiring scafctl cobra commands.
package cmdutil

import "github.com/spf13/cobra"

// MakeHelpOnlyGroup configures a parent command group so that a bare invocation
// (e.g. "scafctl catalog") prints help and exits 0, while an unknown subcommand
// (e.g. "scafctl catalog bogus") errors with "unknown command" and a non-zero
// exit code.
//
// This is required because cobra treats a *non-runnable* parent (one with no
// Run/RunE) specially: its execute() path returns flag.ErrHelp -- which is
// caught upstream and turned into "print help, return nil (exit 0)" -- *before*
// it ever validates positional args. As a result, setting Args: cobra.NoArgs
// alone has no effect on a non-runnable parent, and unknown subcommands are
// silently swallowed as a successful help invocation.
//
// Making the parent runnable with a help-only RunE flips Runnable() to true, so
// cobra validates args, and cobra.NoArgs then rejects the leftover positional
// (the unknown subcommand) with a non-zero exit. Bare invocation still shows
// help via the RunE, satisfying the CLI grammar rule that a group with no
// default action shows help rather than erroring.
//
// Do NOT use this on commands that legitimately accept positional arguments
// (e.g. "lint [name]" or "auth handlers [name]") -- it would reject their args.
//
// The command's Aliases, Short, Long, Example, and other fields are left
// untouched; only Args and RunE are set.
func MakeHelpOnlyGroup(cmd *cobra.Command) *cobra.Command {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		return c.Help()
	}
	return cmd
}
