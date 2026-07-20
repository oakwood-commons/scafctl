// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// aliasListSchema drives table rendering for 'auth alias list'. The root is an
// array because the command writes a bare []map[string]any (one entry per
// alias) to kvx.
var aliasListSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"properties": {
			"selector": { "type": "string", "title": "Selector", "maxLength": 40 },
			"url":      { "type": "string", "title": "Endpoint URL", "maxLength": 80 }
		}
	}
}`)

// CommandAlias creates the 'auth alias' command group for managing static
// hostname aliases (auth.handlers.<name>.hostname.aliases).
func CommandAlias(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "alias",
		Short: "Manage static hostname aliases for an auth handler",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage static hostname aliases for a host-aware auth handler.

			An alias maps a short selector (e.g. "prod") to a concrete endpoint URL
			(e.g. "https://api.prod.example.com:6443"). At login time, passing
			'--hostname <selector>' resolves the alias to its URL before
			authenticating. Static aliases take precedence over any dynamic
			hostname resolver configured for the handler.

			Aliases are stored in config under auth.handlers.<handler>.hostname.aliases.
			Only handlers that declare the hostname capability accept aliases.

			Examples:
			  # Add or update an alias
			  scafctl auth alias set openshift prod https://api.prod.example.com:6443

			  # List a handler's aliases
			  scafctl auth alias list openshift

			  # Remove an alias
			  scafctl auth alias remove openshift prod
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(commandAliasSet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandAliasList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandAliasRemove(cliParams, ioStreams, cmdPath))
	return cmd
}

// requireHostnameCapability returns an error when the named handler does not
// support hostname aliasing.
func requireHostnameCapability(cmd *cobra.Command, handlerName string) error {
	handler, err := getHandler(cmd.Context(), handlerName)
	if err != nil {
		return exitcode.WithCode(fmt.Errorf("failed to initialize auth handler: %w", err), exitcode.GeneralError)
	}
	if !auth.HasCapability(handler.Capabilities(), auth.CapHostname) {
		return exitcode.WithCode(
			fmt.Errorf("the %q auth handler does not support hostname aliases", handlerName),
			exitcode.InvalidInput,
		)
	}
	return nil
}

// configPathFromCmd returns the value of the root --config flag, if set.
func configPathFromCmd(cmd *cobra.Command) string {
	if f := cmd.Root().Flag("config"); f != nil {
		return f.Value.String()
	}
	return ""
}

func commandAliasSet(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <handler> <selector> <url>",
		Short: "Add or update a hostname alias",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Add or update a static hostname alias for an auth handler.

			Example:
			  scafctl auth alias set openshift prod https://api.prod.example.com:6443
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			handlerName, selector, url := args[0], args[1], args[2]

			if err := requireHostnameCapability(cmd, handlerName); err != nil {
				return err
			}

			mgr := config.NewManager(configPathFromCmd(cmd))
			cfg, err := mgr.Load()
			if err != nil {
				return exitcode.WithCode(fmt.Errorf("loading config: %w", err), exitcode.GeneralError)
			}

			hn := ensureHostnameConfig(cfg, handlerName)
			if hn.Aliases == nil {
				hn.Aliases = map[string]string{}
			}
			hn.Aliases[selector] = url

			if err := mgr.Save(); err != nil {
				return exitcode.WithCode(fmt.Errorf("saving config: %w", err), exitcode.GeneralError)
			}
			if w != nil {
				w.Successf("Set alias %q -> %q for handler %q", selector, url, handlerName)
			}
			return nil
		},
	}
}

func commandAliasList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	cmd := &cobra.Command{
		Use:   "list <handler>",
		Short: "List a handler's hostname aliases",
		Long: strings.ReplaceAll(heredoc.Doc(`
			List the static hostname aliases configured for an auth handler.

			Example:
			  scafctl auth alias list openshift
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFlags.AppName = cliParams.BinaryName
			handlerName := args[0]

			mgr := config.NewManager(configPathFromCmd(cmd))
			cfg, err := mgr.Load()
			if err != nil {
				return exitcode.WithCode(fmt.Errorf("loading config: %w", err), exitcode.GeneralError)
			}

			aliases := handlerAliases(cfg, handlerName)
			selectors := make([]string, 0, len(aliases))
			for s := range aliases {
				selectors = append(selectors, s)
			}
			sort.Strings(selectors)

			items := make([]map[string]any, 0, len(selectors))
			for _, s := range selectors {
				items = append(items, map[string]any{"selector": s, "url": aliases[s]})
			}

			outputOpts := flags.ToKvxOutputOptions(&outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"selector", "url"}),
				kvx.WithOutputSchemaJSON(aliasListSchema),
			)
			return outputOpts.Write(items)
		},
	}
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}

func commandAliasRemove(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <handler> <selector>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a hostname alias",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Remove a static hostname alias from an auth handler.

			Example:
			  scafctl auth alias remove openshift prod
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			handlerName, selector := args[0], args[1]

			if err := requireHostnameCapability(cmd, handlerName); err != nil {
				return err
			}

			mgr := config.NewManager(configPathFromCmd(cmd))
			cfg, err := mgr.Load()
			if err != nil {
				return exitcode.WithCode(fmt.Errorf("loading config: %w", err), exitcode.GeneralError)
			}

			aliases := handlerAliases(cfg, handlerName)
			if _, ok := aliases[selector]; !ok {
				return exitcode.WithCode(
					fmt.Errorf("no alias %q found for handler %q", selector, handlerName),
					exitcode.InvalidInput,
				)
			}
			delete(aliases, selector)
			pruneEmptyHostname(cfg, handlerName)

			if err := mgr.Save(); err != nil {
				return exitcode.WithCode(fmt.Errorf("saving config: %w", err), exitcode.GeneralError)
			}
			if w != nil {
				w.Successf("Removed alias %q from handler %q", selector, handlerName)
			}
			return nil
		},
	}
}

// ensureHostnameConfig returns the handler's HostnameConfig, allocating the
// Handlers map, HandlerConfig entry, and HostnameConfig as needed.
func ensureHostnameConfig(cfg *config.Config, handlerName string) *config.HostnameConfig {
	if cfg.Auth.Handlers == nil {
		cfg.Auth.Handlers = map[string]config.HandlerConfig{}
	}
	hc := cfg.Auth.Handlers[handlerName]
	if hc.Hostname == nil {
		hc.Hostname = &config.HostnameConfig{}
	}
	cfg.Auth.Handlers[handlerName] = hc
	return hc.Hostname
}

// handlerAliases returns the alias map for a handler, or nil when absent.
func handlerAliases(cfg *config.Config, handlerName string) map[string]string {
	hc, ok := cfg.Auth.Handlers[handlerName]
	if !ok || hc.Hostname == nil {
		return nil
	}
	return hc.Hostname.Aliases
}

// pruneEmptyHostname removes empty Hostname/HandlerConfig entries so config
// stays tidy after the last alias is removed.
func pruneEmptyHostname(cfg *config.Config, handlerName string) {
	hc, ok := cfg.Auth.Handlers[handlerName]
	if !ok || hc.Hostname == nil {
		return
	}
	if len(hc.Hostname.Aliases) == 0 && hc.Hostname.Resolver == nil {
		hc.Hostname = nil
		cfg.Auth.Handlers[handlerName] = hc
	}
	if hc.Hostname == nil && len(hc.Settings) == 0 {
		delete(cfg.Auth.Handlers, handlerName)
	}
}
