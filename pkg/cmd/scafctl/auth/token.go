// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/atotto/clipboard"
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
		force        bool
		rawToken     bool
		clip         bool
		decode       bool
		curl         bool
		curlURL      string
		exportToken  bool
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

			  # Print just the raw token value (useful for scripting)
			  export TOKEN=$(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --raw)

# Decode and display the JWT header and claims from the token (no signature validation)
		  scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

		  # Decode as JSON for full structured output (header + payload both present)
		  scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json

			  # Emit a ready-to-run curl command with the token injected
			  scafctl auth token entra --scope "https://management.azure.com/.default" --curl --curl-url "https://management.azure.com/subscriptions?api-version=2020-01-01"

			  # Export the token as a shell variable (eval-compatible)
			  eval $(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --export)
		`),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			handlerName := args[0]

			// --force is an alias for --force-refresh
			if force {
				forceRefresh = true
			}

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

			if rawToken {
				w.Plainln(token.AccessToken)
				return nil
			}

			if clip {
				if err := clipboard.WriteAll(token.AccessToken); err != nil {
					err = fmt.Errorf("failed to copy token to clipboard: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				w.Successf("Token copied to clipboard (expires in %s).", humanDuration(token.TimeUntilExpiry()))
				return nil
			}

			if exportToken {
				varName := tokenExportVarName(handlerName)
				w.Plainlnf("export %s=%s", varName, token.AccessToken)
				return nil
			}

			if curl {
				url := curlURL
				if url == "" {
					url = "<URL>"
				}
				w.Plainlnf("curl -H %q %q",
					fmt.Sprintf("Authorization: %s %s", token.TokenType, token.AccessToken), url)
				return nil
			}

			if decode {
				decoded, decErr := decodeJWT(token.AccessToken)
				if decErr != nil {
					decErr = fmt.Errorf("failed to decode JWT: %w", decErr)
					w.Errorf("%v", decErr)
					return exitcode.WithCode(decErr, exitcode.GeneralError)
				}

				// Resolve full group membership when the token contains a groups overage.
				// Entra emits _claim_names.groups instead of a groups claim when the user
				// belongs to more than 200 groups. We detect this and call the handler's
				// GroupsProvider to fetch the complete list via Microsoft Graph.
				if hasGroupsOverage(decoded) {
					payload, ok := decoded["payload"].(map[string]any)
					if !ok {
						payload = map[string]any{}
						decoded["payload"] = payload
					}
					payload["groups_overage"] = true
					if gp, ok := handler.(auth.GroupsProvider); ok {
						w.Warningf("Groups overage detected: fetching full group membership via Microsoft Graph...")
						groups, grpErr := gp.GetGroups(ctx)
						if grpErr != nil {
							payload["groups_resolved_error"] = grpErr.Error()
						} else {
							payload["groups_resolved"] = groups
							payload["groups_resolved_count"] = len(groups)
						}
					} else {
						payload["groups_resolved_error"] = "handler does not support group membership queries (GroupsProvider not implemented)"
					}
				}

				outputOpts := flags.NewKvxOutputOptionsFromFlags(
					outputFlags.Output,
					outputFlags.Interactive,
					outputFlags.Expression,
					kvx.WithOutputContext(ctx),
					kvx.WithOutputNoColor(cliParams.NoColor),
					kvx.WithOutputAppName("scafctl auth token --decode"),
				)
				outputOpts.IOStreams = ioStreams
				return outputOpts.Write(decoded)
			}

			result := map[string]any{
				"handler":     handlerName,
				"flow":        string(token.Flow),
				"scope":       token.Scope,
				"tokenType":   token.TokenType,
				"expiresAt":   token.ExpiresAt,
				"expiresIn":   token.TimeUntilExpiry().String(),
				"accessToken": token.AccessToken,
			}

			if token.SessionID != "" {
				result["sessionId"] = token.SessionID
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
	cmd.Flags().BoolVar(&force, "force", false, "Force acquiring a new token, ignoring any cached token (alias for --force-refresh)")
	cmd.Flags().BoolVar(&rawToken, "raw", false, "Print only the raw access token value (useful for scripting)")
	cmd.Flags().BoolVar(&clip, "clip", false, "Copy the token to the clipboard instead of printing it")
	cmd.Flags().BoolVar(&decode, "decode", false, "Decode and display JWT header and claims from the access token (no signature validation); use -o json for full structured output")
	cmd.Flags().BoolVar(&curl, "curl", false, "Emit a ready-to-run curl command with the token injected")
	cmd.Flags().StringVar(&curlURL, "curl-url", "", "URL to embed in the --curl output (default: '<URL>' placeholder)")
	cmd.Flags().BoolVar(&exportToken, "export", false, "Output a shell export statement: eval $(scafctl auth token ... --export)")
	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)

	return cmd
}

// hasGroupsOverage reports whether the decoded JWT payload contains a groups
// overage indicator. This occurs when the user belongs to more than 200 Entra
// groups — Entra omits the groups claim and instead emits _claim_names with a
// "groups" key pointing to a _claim_sources entry. Per Microsoft's guidance,
// callers must not use the _claim_sources endpoint value and should instead
// query Microsoft Graph directly.
func hasGroupsOverage(decoded map[string]any) bool {
	payload, ok := decoded["payload"].(map[string]any)
	if !ok {
		return false
	}
	claimNames, ok := payload["_claim_names"].(map[string]any)
	if !ok {
		return false
	}
	_, hasGroups := claimNames["groups"]
	return hasGroups
}

// decodeJWT parses a JWT and returns a map with "header" and "payload" keys,
// each containing the decoded claims for that section.
// The token signature is NOT validated — this is for display/debugging only.
func decodeJWT(tokenStr string) (map[string]any, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("not a valid JWT: expected at least 2 dot-separated segments, got %d", len(parts))
	}

	decodedHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode JWT header: %w", err)
	}
	decodedPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode JWT payload: %w", err)
	}

	var header map[string]any
	if err := json.Unmarshal(decodedHeader, &header); err != nil {
		return nil, fmt.Errorf("failed to parse JWT header JSON: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(decodedPayload, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse JWT payload JSON: %w", err)
	}

	// Augment well-known unix timestamp fields in the payload with human-readable counterparts.
	for _, field := range []string{"exp", "iat", "nbf", "auth_time"} {
		if v, ok := payload[field]; ok {
			if ts, ok := jwtFloat64(v); ok {
				payload[field+"_human"] = time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
			}
		}
	}

	return map[string]any{
		"header":  header,
		"payload": payload,
	}, nil
}

// jwtFloat64 converts a JSON-decoded numeric value to float64.
func jwtFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// tokenExportVarName returns the shell variable name used by --export.
func tokenExportVarName(handlerName string) string {
	return strings.ToUpper(handlerName) + "_TOKEN"
}
