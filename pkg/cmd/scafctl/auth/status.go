// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandStatus creates the 'auth status' command.
func CommandStatus(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags

	cmd := &cobra.Command{
		Use:   "status [handler]",
		Short: "Show authentication status",
		Long: heredoc.Doc(`
			Show the current authentication status for auth handlers.

			If no handler is specified, shows status for all known handlers.

			Supported handlers:
			- entra: Microsoft Entra ID
			- github: GitHub

			Examples:
			  # Show all auth status
			  scafctl auth status

			  # Show Entra auth status
			  scafctl auth status entra

			  # Show GitHub auth status
			  scafctl auth status github

			  # Output as JSON
			  scafctl auth status -o json
		`),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			handlers := SupportedHandlers()
			if len(args) > 0 {
				handlerName := args[0]
				if !IsSupportedHandler(handlerName) {
					err := fmt.Errorf("unknown auth handler: %s (supported: %v)", handlerName, SupportedHandlers())
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				handlers = []string{handlerName}
			}

			results := make([]map[string]any, 0, len(handlers))

			for _, handlerName := range handlers {
				handler, err := getHandler(ctx, handlerName)
				if err != nil {
					w.Warningf("Failed to initialize %s: %v", handlerName, err)
					continue
				}

				status, err := handler.Status(ctx)
				if err != nil {
					w.Warningf("Failed to check %s status: %v", handlerName, err)
					continue
				}

				result := map[string]any{
					"handler":       handlerName,
					"displayName":   handler.DisplayName(),
					"authenticated": status.Authenticated,
				}

				// Add identity type
				if status.IdentityType != "" {
					result["identityType"] = string(status.IdentityType)
				}

				// For service principal/workload identity, show client ID
				if status.ClientID != "" {
					result["clientId"] = status.ClientID
				}

				// For workload identity, show token file path
				if status.IdentityType == auth.IdentityTypeWorkloadIdentity && status.TokenFile != "" {
					result["tokenFile"] = status.TokenFile
				}

				if status.Authenticated && status.Claims != nil {
					if status.Claims.Email != "" {
						result["email"] = status.Claims.Email
					}
					if status.Claims.Name != "" {
						result["name"] = status.Claims.Name
					}
					if status.Claims.Username != "" {
						result["username"] = status.Claims.Username
					}
					if status.TenantID != "" {
						result["tenantId"] = status.TenantID
					}
					if !status.ExpiresAt.IsZero() {
						result["expiresAt"] = status.ExpiresAt
					}
					if !status.LastRefresh.IsZero() {
						result["lastRefresh"] = status.LastRefresh
					}
				}

				if len(status.Scopes) > 0 {
					result["scopes"] = status.Scopes
				}

				results = append(results, result)
			}

			if len(results) == 0 {
				err := fmt.Errorf("no auth handlers found")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			outputOpts := flags.NewKvxOutputOptionsFromFlags(
				outputFlags.Output,
				outputFlags.Interactive,
				outputFlags.Expression,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputAppName("scafctl auth status"),
			)
			outputOpts.IOStreams = ioStreams

			return outputOpts.Write(results)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	return cmd
}
