// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandSwitch creates the 'auth switch' command.
func CommandSwitch(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <handler> [profile]",
		Short: "Switch the active auth profile",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Switch the active authentication profile for a handler.

			This sets the activeProfile in the config file so that subsequent commands
			use the specified profile's credentials by default without needing
			--profile on every invocation.

			Use an empty string "" or "built-in" to switch back to the built-in (unnamed) profile.

			Examples:
			  # Switch GitHub to the "work" profile
			  scafctl auth switch github work

			  # Switch Entra to the "personal" profile
			  scafctl auth switch entra personal

			  # Switch back to the built-in profile
			  scafctl auth switch github built-in

			  # List configured profiles for a handler (no profile arg)
			  scafctl auth switch github
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			ctx := cmd.Context()
			switch len(args) {
			case 0:
				return listKnownHandlers(ctx), cobra.ShellCompDirectiveNoFileComp
			case 1:
				// Complete profile names for this handler
				profiles := auth.ListConfiguredProfiles(ctx, args[0])
				profiles = append(profiles, auth.BuiltinProfileName)
				return profiles, cobra.ShellCompDirectiveNoFileComp
			default:
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			handlerName := args[0]

			// Validate handler name
			if err := validateHandlerName(ctx, handlerName); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// If no profile specified, list available profiles
			if len(args) < 2 {
				profiles := auth.ListConfiguredProfiles(ctx, handlerName)
				activeProfile := auth.ResolveActiveProfile(ctx, handlerName)

				if len(profiles) == 0 && activeProfile == "" {
					w.Infof("No profiles configured for %s.", handlerName)
					w.Infof("Use '%s auth login %s --profile <name>' to create a profile.", cliParams.BinaryName, handlerName)
					return nil
				}

				w.Infof("Profiles for %s:", handlerName)
				allProfiles := append([]string{auth.BuiltinProfileName}, profiles...)
				sort.Strings(allProfiles[1:])
				for _, p := range allProfiles {
					marker := "  "
					if (p == auth.BuiltinProfileName && activeProfile == "") || p == activeProfile {
						marker = "* "
					}
					w.Infof("%s%s", marker, p)
				}
				return nil
			}

			profileName := args[1]

			// Normalize "default" and "built-in" to the unnamed built-in profile
			profileName = auth.NormalizeProfileName(profileName)
			if profileName != "" {
				if err := auth.ValidateProfileName(profileName); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
			}

			// Load config manager and set activeProfile
			var configPathOpt string
			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				configPathOpt = configFlag.Value.String()
			}
			mgr := appconfig.NewManager(configPathOpt)
			cfg, err := mgr.Load()
			if err != nil {
				err = fmt.Errorf("failed to load config: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Set active profile on the config struct directly
			switch handlerName {
			case "entra":
				if cfg.Auth.Entra == nil {
					cfg.Auth.Entra = &appconfig.EntraAuthConfig{}
				}
				cfg.Auth.Entra.ActiveProfile = profileName
			case "github":
				if cfg.Auth.GitHub == nil {
					cfg.Auth.GitHub = &appconfig.GitHubAuthConfig{}
				}
				cfg.Auth.GitHub.ActiveProfile = profileName
			case "gcp":
				if cfg.Auth.GCP == nil {
					cfg.Auth.GCP = &appconfig.GCPAuthConfig{}
				}
				cfg.Auth.GCP.ActiveProfile = profileName
			default:
				err := fmt.Errorf("handler %q does not support profile switching (only entra, github, gcp)", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			if err := mgr.Save(); err != nil {
				err = fmt.Errorf("failed to save config: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if profileName == "" {
				w.Successf("Switched %s to built-in profile.", handlerName)
			} else {
				w.Successf("Switched %s to profile %q.", handlerName, profileName)
			}
			return nil
		},
	}

	return cmd
}
