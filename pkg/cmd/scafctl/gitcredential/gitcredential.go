// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gitcredential implements the CLI wiring for the Git HTTPS credential
// helper protocol ("<binary> git-credential"). All protocol parsing, routing,
// token acquisition, and gitconfig mutation live in pkg/gitcredential.
package gitcredential

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gitcredential"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

type persistentFlags struct {
	profile string
	handler string
}

// CommandGitCredential creates the "git-credential" command group.
func CommandGitCredential(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	binName := resolveBinaryName(cliParams, path)
	pflags := &persistentFlags{}

	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "git-credential",
		Short: "Git HTTPS credential helper protocol",
		Long: heredoc.Docf(`
			Implements the Git credential helper protocol over HTTPS, serving tokens
			from %[1]s's authentication handlers (github, entra, gcp, and custom OAuth2)
			and AES-256-GCM encrypted secrets store directly to git clone, fetch, and push.

			Configure Git for built-in and configured hosts:
			  %[1]s git-credential install

			Inspect or remove managed Git credential helper entries:
			  %[1]s git-credential uninstall
		`, binName),
	})

	cmd.PersistentFlags().StringVar(&pflags.profile, "profile", "", "Override the auth profile used for credential resolution")
	cmd.PersistentFlags().StringVar(&pflags.handler, "handler", "", "Override the auth handler used for credential resolution")

	cmd.AddCommand(commandGet(cliParams, ioStreams, binName, pflags))
	cmd.AddCommand(commandStore(cliParams, ioStreams, binName, pflags))
	cmd.AddCommand(commandErase(cliParams, ioStreams, binName, pflags))
	cmd.AddCommand(commandCapability())
	cmd.AddCommand(commandInstall(cliParams, ioStreams, binName, pflags))
	cmd.AddCommand(commandUninstall(cliParams, ioStreams, binName))

	return cmd
}

func resolveBinaryName(cliParams *settings.Run, fallback string) string {
	if cliParams != nil && strings.TrimSpace(cliParams.BinaryName) != "" {
		return settings.SanitizeBinaryName(cliParams.BinaryName)
	}
	if strings.TrimSpace(fallback) != "" {
		return settings.SanitizeBinaryName(fallback)
	}
	return settings.CliBinaryName
}

func buildHelper(ctx context.Context, binName string, pflags *persistentFlags, requireStore bool) (*gitcredential.Helper, error) {
	if loadErr := config.LoadErrorFromContext(ctx); loadErr != nil {
		return nil, fmt.Errorf("invalid configuration: %w", loadErr)
	}

	store := secrets.FromContext(ctx)
	if store == nil && requireStore {
		var cfg *config.Config
		if c := config.FromContext(ctx); c != nil {
			cfg = c
		}
		var storeOpts []secrets.Option
		if cfg != nil {
			storeOpts = append(storeOpts, secrets.WithRequireSecureKeyring(cfg.Settings.RequireSecureKeyring))
		}
		s, err := secrets.New(storeOpts...)
		if err != nil {
			return nil, fmt.Errorf("initialize secrets store: %w", err)
		}
		store = s
	}

	helperOpts := []gitcredential.Option{
		gitcredential.WithBinaryName(binName),
	}

	reg := auth.RegistryFromContext(ctx)
	hasOverride := pflags != nil && (pflags.handler != "" || pflags.profile != "")
	if reg != nil || hasOverride {
		var resolverOpts []gitcredential.ResolverOption
		if cfg := config.FromContext(ctx); cfg != nil {
			resolverOpts = append(resolverOpts, gitcredential.WithConfig(cfg))
		}
		if pflags != nil {
			if pflags.handler != "" {
				resolverOpts = append(resolverOpts, gitcredential.WithHandlerOverride(pflags.handler))
			}
			if pflags.profile != "" {
				resolverOpts = append(resolverOpts, gitcredential.WithProfileOverride(pflags.profile))
			}
		}
		resolver := gitcredential.NewAuthTokenResolver(reg, resolverOpts...)
		helperOpts = append(helperOpts, gitcredential.WithTokenResolver(resolver))
	}

	return gitcredential.New(store, helperOpts...), nil
}

func streamsForProtocol(ioStreams *terminal.IOStreams) (in io.Reader, out, errOut io.Writer) {
	in = os.Stdin
	out = os.Stdout
	errOut = os.Stderr
	if ioStreams != nil {
		if ioStreams.In != nil {
			in = ioStreams.In
		}
		if ioStreams.Out != nil {
			out = ioStreams.Out
		}
		if ioStreams.ErrOut != nil {
			errOut = ioStreams.ErrOut
		}
	}
	return in, out, errOut
}

func commandGet(_ *settings.Run, ioStreams *terminal.IOStreams, binName string, pflags *persistentFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "get",
		Short:         "Resolve Git HTTPS credentials from stdin attributes",
		Long:          "Reads Git credential attributes from stdin and writes resolved username/password lines to stdout.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			in, out, errOut := streamsForProtocol(ioStreams)

			req, err := gitcredential.ParseRequest(in)
			if err != nil {
				writeProtocolStderr(errOut, err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			helper, storeInitErr := buildHelper(ctx, binName, pflags, secrets.FromContext(ctx) == nil)
			if storeInitErr != nil {
				if config.LoadErrorFromContext(ctx) != nil {
					writeProtocolStderr(errOut, storeInitErr.Error())
					return exitcode.WithCode(storeInitErr, exitcode.ConfigError)
				}
				// Dynamic auth handlers do not require the fallback secrets store;
				// only bypass store initialization when req routes to an auth handler.
				noStoreHelper, noStoreErr := buildHelper(ctx, binName, pflags, false)
				if noStoreErr == nil && noStoreHelper != nil && noStoreHelper.MatchesAuthHandler(ctx, req) {
					helper = noStoreHelper
				} else {
					writeProtocolStderr(errOut, storeInitErr.Error())
					return exitcode.WithCode(storeInitErr, exitcode.GeneralError)
				}
			}

			resp, err := helper.Get(ctx, req)
			if err != nil {
				if errors.Is(err, gitcredential.ErrCredentialsNotFound) {
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				msg := formatGetError(ctx, binName, err)
				writeProtocolStderr(errOut, msg)
				return exitcode.WithCode(fmt.Errorf("%s", msg), exitcode.GeneralError)
			}

			if err := gitcredential.WriteResponse(out, resp); err != nil {
				writeProtocolStderr(errOut, err.Error())
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			return nil
		},
	}
}

func commandStore(_ *settings.Run, ioStreams *terminal.IOStreams, binName string, pflags *persistentFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "store",
		Short:         "Store static Git HTTPS credentials for non-handler hosts",
		Long:          "Reads Git credential attributes from stdin and stores static credentials for non-handler hosts (no-op for auth-handler-managed remotes).",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProtocolMutation(cmd.Context(), ioStreams, binName, pflags, true, (*gitcredential.Helper).Store)
		},
	}
}

func commandErase(_ *settings.Run, ioStreams *terminal.IOStreams, binName string, pflags *persistentFlags) *cobra.Command {
	return &cobra.Command{
		Use:           "erase",
		Short:         "Erase stored static Git HTTPS credentials for non-handler hosts",
		Long:          "Reads Git credential attributes from stdin and removes stored static credentials for non-handler hosts (no-op for auth-handler-managed remotes).",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProtocolMutation(cmd.Context(), ioStreams, binName, pflags, false, (*gitcredential.Helper).Erase)
		},
	}
}

func runProtocolMutation(
	ctx context.Context,
	ioStreams *terminal.IOStreams,
	binName string,
	pflags *persistentFlags,
	isStore bool,
	op func(*gitcredential.Helper, context.Context, *gitcredential.Request) error,
) error {
	in, _, errOut := streamsForProtocol(ioStreams)

	req, err := gitcredential.ParseRequest(in)
	if err != nil {
		writeProtocolStderr(errOut, err.Error())
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	helper, err := buildHelper(ctx, binName, pflags, secrets.FromContext(ctx) == nil)
	if err != nil {
		if config.LoadErrorFromContext(ctx) != nil {
			writeProtocolStderr(errOut, err.Error())
			return exitcode.WithCode(err, exitcode.ConfigError)
		}
		// Auth-handler-backed hosts and empty requests are strict no-ops even if
		// initializing a disk store fails; non-handler mutations fail closed.
		noStoreHelper, _ := buildHelper(ctx, binName, pflags, false)
		if noStoreHelper != nil && noStoreHelper.IsMutationNoOp(ctx, req, isStore) {
			if opErr := op(noStoreHelper, ctx, req); opErr != nil {
				writeProtocolStderr(errOut, opErr.Error())
				return exitcode.WithCode(opErr, exitcode.GeneralError)
			}
			return nil
		}
		writeProtocolStderr(errOut, err.Error())
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if err := op(helper, ctx, req); err != nil {
		writeProtocolStderr(errOut, err.Error())
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	return nil
}

func commandCapability() *cobra.Command {
	return &cobra.Command{
		Use:           "capability",
		Short:         "Advertise Git credential helper protocol capabilities (Git 2.46+)",
		Hidden:        true,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}

func formatGetError(ctx context.Context, fallbackBin string, err error) string {
	var reauth *gitcredential.ReauthRequiredError
	if errors.As(err, &reauth) {
		bin := settings.BinaryNameFromContext(ctx)
		if strings.TrimSpace(bin) == "" {
			bin = fallbackBin
		}
		profileArg := reauth.Profile
		if profileArg == "" {
			profileArg = auth.BuiltinProfileName
		}
		loginCmd := fmt.Sprintf("%s auth login %s --profile %s", bin, reauth.Handler, profileArg)
		return fmt.Sprintf(
			"no valid credentials for %s: run '%s' to re-authenticate (interactive login cannot run inside a credential-helper subprocess)",
			reauth.ServerURL, loginCmd,
		)
	}
	return err.Error()
}

func writeProtocolStderr(errOut io.Writer, msg string) {
	if errOut == nil || strings.TrimSpace(msg) == "" {
		return
	}
	_, _ = io.WriteString(errOut, msg+"\n")
}
