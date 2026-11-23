// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// resolveCommandProfile resolves an explicit per-command --profile flag or
// global --auth-profile context value, validates non-empty normalized names,
// and marks ctx as profile-resolved when an explicit selection was made
// (including "default" or "built-in", which normalize to "").
func resolveCommandProfile(ctx context.Context, cmd *cobra.Command, rawFlag string) (context.Context, string, bool, error) {
	flagExplicit := cmd.Flags().Changed("profile")
	globalRaw := auth.GlobalProfileFromContext(ctx)

	var effectiveProfile string
	switch {
	case flagExplicit:
		effectiveProfile = auth.NormalizeProfileName(rawFlag)
	case globalRaw != "":
		effectiveProfile = auth.NormalizeProfileName(globalRaw)
	}

	if effectiveProfile != "" {
		if err := auth.ValidateProfileName(effectiveProfile); err != nil {
			return ctx, "", false, err
		}
	}

	explicit := flagExplicit || globalRaw != ""
	if explicit {
		ctx = auth.WithProfile(ctx, effectiveProfile)
		ctx = auth.WithProfileResolved(ctx)
	}
	return ctx, effectiveProfile, explicit, nil
}

// withHandlerActiveProfile resolves handlerName's config activeProfile onto ctx
// when ctx has not already been marked profile-resolved, then marks ctx resolved.
func withHandlerActiveProfile(ctx context.Context, handlerName string) (context.Context, string) {
	if auth.IsProfileResolved(ctx) {
		return ctx, auth.ProfileFromContext(ctx)
	}
	profile := auth.ResolveActiveProfile(ctx, handlerName)
	ctx = auth.WithProfile(ctx, profile)
	ctx = auth.WithProfileResolved(ctx)
	return ctx, profile
}

// CommandProfile creates the 'auth profile' command group.
func CommandProfile(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "profile",
		Short: "Manage auth profiles",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage authentication profiles for scafctl.

			Profiles allow you to maintain multiple credentials for the same
			auth handler (e.g. work and personal GitHub accounts).

			Use 'scafctl auth profile delete <handler> <profile>' to remove a profile.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandProfileDelete(cliParams, ioStreams, cmdPath))

	return cmd
}
