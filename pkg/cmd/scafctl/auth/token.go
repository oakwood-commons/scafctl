// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"sort"
	"strings"
	"time"

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

// CommandToken creates the 'auth token' command.
func CommandToken(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	var (
		scopes       []string
		minValidFor  time.Duration
		forceRefresh bool
	)

	cmd := &cobra.Command{
		Use:   "token <handler>",
		Short: "Get an access token (for debugging)",
		Long: heredoc.Doc(`
			Get an access token from an auth handler.

			This command is primarily for debugging and testing. It retrieves
			a valid access token from the specified handler.

			For handlers that support per-request scopes (e.g., Entra), the --scope
			flag is required and specifies which resource scope to request.

			For handlers that do NOT support per-request scopes (e.g., GitHub),
			the --scope flag is not accepted. Scopes for these handlers are fixed
			at login time. Use 'scafctl auth login <handler> --scope <scope>' to
			change scopes.

			The token is cached to disk and will be reused if it has sufficient
			remaining validity for the specified --min-valid-for duration.

			WARNING: The token is sensitive and should not be shared or logged.

			Examples:
			  # Get a token for Microsoft Graph (Entra - supports per-request scopes)
			  scafctl auth token entra --scope "https://graph.microsoft.com/.default"

			  # Get a GitHub token (no --scope needed, scopes fixed at login)
			  scafctl auth token github

			  # Get a token that will be valid for at least 5 minutes
			  scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m

			  # Force a fresh token, bypassing the cache
			  scafctl auth token entra --scope "https://graph.microsoft.com/.default" --force-refresh

			  # Output as JSON (includes full token)
			  scafctl auth token entra --scope "https://management.azure.com/.default" -o json

			  # Get a GCP token for cloud-platform scope
			  scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform"

			  # Get a GCP token for BigQuery
			  scafctl auth token gcp --scope "https://www.googleapis.com/auth/bigquery"
		`),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			handlerName := args[0]

			// Validate handler name against registry
			if err := validateHandlerName(ctx, handlerName); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			handler, err := getHandler(ctx, handlerName)
			if err != nil {
				err = fmt.Errorf("failed to initialize auth handler: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			caps := handler.Capabilities()

			// Validate --scope against capabilities
			if len(scopes) > 0 && !auth.HasCapability(caps, auth.CapScopesOnTokenRequest) {
				err := fmt.Errorf(
					"the %q auth handler does not support per-request scopes; "+
						"scopes are fixed at login time. Use 'scafctl auth login %s --scope <scope>' to change scopes",
					handlerName, handlerName,
				)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Scope is required only for handlers that support per-request scopes
			if len(scopes) == 0 && auth.HasCapability(caps, auth.CapScopesOnTokenRequest) {
				err := fmt.Errorf("--scope is required for the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Sort scopes for deterministic cache key
			var scope string
			if len(scopes) > 0 {
				sorted := make([]string, len(scopes))
				copy(sorted, scopes)
				sort.Strings(sorted)
				scope = strings.Join(sorted, " ")
			}

			token, err := handler.GetToken(ctx, auth.TokenOptions{
				Scope:        scope,
				MinValidFor:  minValidFor,
				ForceRefresh: forceRefresh,
			})
			if err != nil {
				err = fmt.Errorf("failed to get token: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			result := map[string]any{
				"handler":     handlerName,
				"scope":       token.Scope,
				"tokenType":   token.TokenType,
				"expiresAt":   token.ExpiresAt,
				"expiresIn":   token.TimeUntilExpiry().String(),
				"accessToken": token.AccessToken,
			}

			// Parse output format
			format, _ := kvx.ParseOutputFormat(outputFlags.Output)

			// For table output, mask the token and display nicely
			if kvx.IsTableFormat(format) {
				w.Infof("Handler:    %s", handlerName)
				w.Infof("Scope:      %s", token.Scope)
				w.Infof("Type:       %s", token.TokenType)
				w.Infof("Expires:    %s", token.ExpiresAt.Format("2006-01-02 15:04:05"))
				w.Infof("Expires In: %s", token.TimeUntilExpiry().Round(time.Second))
				if len(token.AccessToken) > 20 {
					w.Infof("Token:      %s...%s", token.AccessToken[:10], token.AccessToken[len(token.AccessToken)-10:])
				} else {
					w.Infof("Token:      %s", token.AccessToken)
				}
				return nil
			}

			outputOpts := flags.NewKvxOutputOptionsFromFlags(
				outputFlags.Output,
				outputFlags.Interactive,
				outputFlags.Expression,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputAppName("scafctl auth token"),
			)
			outputOpts.IOStreams = ioStreams

			return outputOpts.Write(result)
		},
	}

	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "OAuth scope(s) for the token (required for handlers with scopes_on_token_request capability)")
	cmd.Flags().DurationVar(&minValidFor, "min-valid-for", auth.DefaultMinValidFor, "Minimum time the token should be valid for")
	cmd.Flags().BoolVarP(&forceRefresh, "force-refresh", "f", false, "Force acquiring a new token, ignoring any cached token")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}
