// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gitcredential"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

func explicitConfigPathFromCmd(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	if f := cmd.Flag("config"); f != nil {
		if val := strings.TrimSpace(f.Value.String()); val != "" {
			return val
		}
	}
	if root := cmd.Root(); root != nil {
		if f := root.PersistentFlags().Lookup("config"); f != nil {
			if val := strings.TrimSpace(f.Value.String()); val != "" {
				return val
			}
		}
		if f := root.Flag("config"); f != nil {
			if val := strings.TrimSpace(f.Value.String()); val != "" {
				return val
			}
		}
	}
	return ""
}

func shouldUseKvxRenderer(outputOpts *kvx.OutputOptions) bool {
	if outputOpts == nil {
		return false
	}
	if kvx.IsStructuredFormat(outputOpts.Format) ||
		kvx.IsQuietFormat(outputOpts.Format) ||
		outputOpts.Interactive ||
		outputOpts.Expression != "" ||
		outputOpts.Where != "" {
		return true
	}
	return outputOpts.FormatExplicit &&
		outputOpts.Format != kvx.OutputFormatAuto &&
		outputOpts.Format != kvx.OutputFormatText
}

func commandInstall(cliParams *settings.Run, ioStreams *terminal.IOStreams, binName string, pflags *persistentFlags) *cobra.Command {
	var (
		hosts        []string
		local        bool
		file         string
		globalHelper bool
		force        bool
		shim         bool
		binDir       string
		kvxFlags     flags.KvxOutputFlags
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: fmt.Sprintf("Configure Git to use %s as an HTTPS credential helper", binName),
		Long: heredoc.Docf(`
			Configures Git to invoke '%[1]s git-credential' for HTTPS remotes.

			By default, per-host credential.<url>.helper and useHttpPath = true entries
			are written to your global gitconfig for github.com, gist.github.com,
			dev.azure.com, source.developers.google.com, and any Git hosts configured
			under auth.github, auth.customOAuth2[].gitHost, or auth.git.rules.

			Existing foreign credential helpers are never overwritten unless --force
			is passed.

			Examples:
			  # Configure default and config-defined Git hosts in ~/.gitconfig
			  %[1]s git-credential install

			  # Configure specific hosts or repository sub-paths
			  %[1]s git-credential install --host github.com --host gitlab.example.com

			  # Configure only the current Git repository (.git/config)
			  %[1]s git-credential install --local

			  # Also install a git-credential-%[1]s symlink or shim on PATH
			  %[1]s git-credential install --shim
		`, binName),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			if loadErr := config.LoadErrorFromContext(ctx); loadErr != nil {
				wrapped := fmt.Errorf("invalid configuration: %w", loadErr)
				if w != nil {
					w.Errorf("%v", wrapped)
				}
				return exitcode.WithCode(wrapped, exitcode.ConfigError)
			}

			var workingDir string
			if wd, ok := provider.WorkingDirectoryFromContext(ctx); ok {
				workingDir = wd
			}

			var profile, handler string
			if pflags != nil {
				profile = pflags.profile
				handler = pflags.handler
			}

			res, err := gitcredential.Install(ctx, gitcredential.InstallOptions{
				BinaryName:   binName,
				ConfigPath:   explicitConfigPathFromCmd(cmd),
				Hosts:        hosts,
				Local:        local,
				File:         file,
				GlobalHelper: globalHelper,
				Force:        force,
				Shim:         shim,
				BinDir:       binDir,
				WorkingDir:   workingDir,
				Config:       config.FromContext(ctx),
				AuthRegistry: auth.RegistryFromContext(ctx),
				Profile:      profile,
				Handler:      handler,
			})
			if err != nil {
				if w != nil {
					w.Errorf("%v", err)
				}
				code := exitcode.GeneralError
				if errors.Is(err, gitcredential.ErrForeignHelper) ||
					errors.Is(err, gitcredential.ErrInsecureTransport) ||
					errors.Is(err, gitcredential.ErrInvalidProtocolInput) {
					code = exitcode.InvalidInput
				}
				return exitcode.WithCode(err, code)
			}

			kvxFlags.AppName = binName + " git-credential install"
			noColor := cliParams != nil && cliParams.NoColor
			outputOpts := flags.ToKvxOutputOptionsFromCmd(cmd, &kvxFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(noColor),
				kvx.WithOutputAppName(binName+" git-credential install"),
			)

			if w != nil && !kvx.IsQuietFormat(outputOpts.Format) {
				for _, warn := range res.Warnings {
					w.WarnStderrf("%s", warn)
				}
			}

			if shouldUseKvxRenderer(outputOpts) {
				return outputOpts.Write(res)
			}

			if w != nil {
				if res.GlobalHelper {
					w.Successf("Configured global Git credential helper in %s", res.ConfigFile)
				} else {
					for _, u := range res.ConfiguredURLs {
						w.Successf("Configured Git credential helper for %s in %s", u, res.ConfigFile)
					}
				}
				if res.ShimPath != "" {
					w.Successf("Installed %s at %s", res.ShimMethod, res.ShimPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&hosts, "host", nil, "Target Git host or https://<host>[/<path>] URL (repeatable; defaults to built-in and configured Git hosts)")
	cmd.Flags().BoolVar(&local, "local", false, "Write to the current repository's .git/config instead of global gitconfig")
	cmd.Flags().StringVar(&file, "file", "", "Write to an explicit gitconfig file path")
	cmd.Flags().BoolVar(&globalHelper, "global-helper", false, "Configure global credential.helper instead of per-host entries")
	cmd.Flags().BoolVar(&force, "force", false, "Replace existing foreign credential.helper entries or unmanaged shims")
	cmd.Flags().BoolVar(&shim, "shim", false, fmt.Sprintf("Also install a git-credential-%s symlink or forwarding shim in --bin-dir", binName))
	cmd.Flags().StringVar(&binDir, "bin-dir", gitcredential.DefaultBinDir, "Directory for the optional git-credential symlink or shim")
	flags.AddKvxOutputFlagsToStruct(cmd, &kvxFlags)

	return cmd
}

func commandUninstall(cliParams *settings.Run, ioStreams *terminal.IOStreams, binName string) *cobra.Command {
	var (
		hosts        []string
		local        bool
		file         string
		globalHelper bool
		shim         bool
		binDir       string
		kvxFlags     flags.KvxOutputFlags
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: fmt.Sprintf("Remove %s-managed Git credential helper configuration", binName),
		Long: heredoc.Docf(`
			Removes %[1]s-managed Git credential helper entries from gitconfig while
			preserving any foreign credential helpers.

			When --shim is specified, also removes the git-credential-%[1]s symlink or
			forwarding shim in --bin-dir after verifying its '# %[1]s-managed forwarding shim'
			header.

			Examples:
			  # Remove all %[1]s-managed entries from ~/.gitconfig
			  %[1]s git-credential uninstall

			  # Remove only a specific host
			  %[1]s git-credential uninstall --host gitlab.example.com

			  # Remove from .git/config and delete the managed shim
			  %[1]s git-credential uninstall --local --shim
		`, binName),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			var workingDir string
			if wd, ok := provider.WorkingDirectoryFromContext(ctx); ok {
				workingDir = wd
			}

			shimRequested := shim || cmd.Flags().Changed("bin-dir")

			res, err := gitcredential.Uninstall(ctx, gitcredential.UninstallOptions{
				BinaryName:   binName,
				Hosts:        hosts,
				Local:        local,
				File:         file,
				GlobalHelper: globalHelper,
				Shim:         shimRequested,
				BinDir:       binDir,
				WorkingDir:   workingDir,
			})
			if err != nil {
				if w != nil {
					w.Errorf("%v", err)
				}
				code := exitcode.GeneralError
				if errors.Is(err, gitcredential.ErrForeignHelper) {
					code = exitcode.InvalidInput
				}
				return exitcode.WithCode(err, code)
			}

			kvxFlags.AppName = binName + " git-credential uninstall"
			noColor := cliParams != nil && cliParams.NoColor
			outputOpts := flags.ToKvxOutputOptionsFromCmd(cmd, &kvxFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(noColor),
				kvx.WithOutputAppName(binName+" git-credential uninstall"),
			)

			if shouldUseKvxRenderer(outputOpts) {
				return outputOpts.Write(res)
			}

			if w != nil {
				removedAny := res.RemovedGlobalHelper || len(res.RemovedURLs) > 0 || res.ShimRemoved
				if !removedAny {
					w.Successf("No %s-managed Git credential helper entries found in %s", binName, res.ConfigFile)
					return nil
				}
				if res.RemovedGlobalHelper {
					w.Successf("Removed global Git credential helper from %s", res.ConfigFile)
				}
				for _, u := range res.RemovedURLs {
					w.Successf("Removed Git credential helper for %s from %s", u, res.ConfigFile)
				}
				if res.ShimRemoved {
					w.Successf("Removed %s", res.ShimPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&hosts, "host", nil, "Remove only specific host or https://<host>[/<path>] entries")
	cmd.Flags().BoolVar(&local, "local", false, "Remove from the current repository's .git/config")
	cmd.Flags().StringVar(&file, "file", "", "Remove from an explicit gitconfig file path")
	cmd.Flags().BoolVar(&globalHelper, "global-helper", false, "Remove only global credential.helper")
	cmd.Flags().BoolVar(&shim, "shim", false, fmt.Sprintf("Also remove the managed git-credential-%s symlink or shim", binName))
	cmd.Flags().StringVar(&binDir, "bin-dir", gitcredential.DefaultBinDir, "Directory where the symlink or shim was installed")
	flags.AddKvxOutputFlagsToStruct(cmd, &kvxFlags)

	return cmd
}
