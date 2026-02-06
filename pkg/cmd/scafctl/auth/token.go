package auth

import (
	"fmt"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
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
		scope       string
		minValidFor time.Duration
	)

	cmd := &cobra.Command{
		Use:   "token <handler>",
		Short: "Get an access token (for debugging)",
		Long: heredoc.Doc(`
			Get an access token from an auth handler.

			This command is primarily for debugging and testing. It retrieves
			a valid access token for the specified scope.

			The token is cached to disk and will be reused if it has sufficient
			remaining validity for the specified --min-valid-for duration.

			WARNING: The token is sensitive and should not be shared or logged.

			Supported handlers:
			- entra: Microsoft Entra ID

			Examples:
			  # Get a token for Microsoft Graph
			  scafctl auth token entra --scope "https://graph.microsoft.com/.default"

			  # Get a token that will be valid for at least 5 minutes
			  scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m

			  # Output as JSON (includes full token)
			  scafctl auth token entra --scope "https://management.azure.com/.default" -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			handlerName := args[0]

			// Validate handler name
			if !IsSupportedHandler(handlerName) {
				return fmt.Errorf("unknown auth handler: %s (supported: %v)", handlerName, SupportedHandlers())
			}

			if scope == "" {
				return fmt.Errorf("--scope is required")
			}

			handler, err := getEntraHandler(ctx)
			if err != nil {
				return fmt.Errorf("failed to initialize auth handler: %w", err)
			}

			token, err := handler.GetToken(ctx, auth.TokenOptions{
				Scope:       scope,
				MinValidFor: minValidFor,
			})
			if err != nil {
				return fmt.Errorf("failed to get token: %w", err)
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

	cmd.Flags().StringVar(&scope, "scope", "", "OAuth scope for the token (required)")
	cmd.Flags().DurationVar(&minValidFor, "min-valid-for", auth.DefaultMinValidFor, "Minimum time the token should be valid for")
	_ = cmd.MarkFlagRequired("scope")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}
