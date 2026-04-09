// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
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

// authStatusSchema controls table column display: handler, authenticated, email/username,
// and expiresIn are visible; other fields are hidden in table view but included in json/yaml.
var authStatusSchema = []byte(`{
	"type": "array",
	"items": {
		"type": "object",
		"required": ["handler", "displayName", "authenticated"],
		"properties": {
			"handler":        { "type": "string", "title": "Handler", "maxLength": 20 },
			"displayName":    { "type": "string", "title": "Name" },
			"authenticated":  { "type": "boolean", "title": "Auth" },
			"email":          { "type": "string", "title": "Email" },
			"username":       { "type": "string", "title": "Username" },
			"expiresIn":      { "type": "string", "title": "Expires In" },
			"identityType":   { "type": "string", "deprecated": true },
			"clientId":       { "type": "string", "deprecated": true },
			"tokenFile":      { "type": "string", "deprecated": true },
			"name":           { "type": "string", "deprecated": true },
			"tenantId":       { "type": "string", "deprecated": true },
			"expiresAt":      { "type": "string", "deprecated": true },
			"lastRefresh":    { "type": "string", "deprecated": true },
			"scopes":         { "type": "array", "deprecated": true },
			"cachedTokens":   { "type": "integer", "deprecated": true },
			"hint":           { "type": "string", "deprecated": true }
		}
	}
}`)

// CommandStatus creates the 'auth status' command.
func CommandStatus(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	var exitCodeFlag bool
	var warnWithin time.Duration

	cmd := &cobra.Command{
		Use:   "status [handler]",
		Short: "Show authentication status",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Show the current authentication status for auth handlers.

			If no handler is specified, shows status for all registered handlers.

			Use --exit-code to make the command exit non-zero when any handler
			is not authenticated — useful for scripting and health checks.

			Examples:
			  # Show all auth status
			  scafctl auth status

			  # Show Entra auth status
			  scafctl auth status entra

			  # Show GitHub auth status
			  scafctl auth status github

			  # Show GCP auth status
			  scafctl auth status gcp

			  # Output as JSON
			  scafctl auth status -o json

			  # Exit non-zero if not authenticated (for scripts)
			  scafctl auth status entra --exit-code

			  # Exit non-zero if any token expires within 10 minutes (pre-flight check)
			  scafctl auth status --warn-within 10m
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			handlers := listHandlers(ctx)
			if len(args) > 0 {
				handlerName := args[0]
				if err := validateHandlerName(ctx, handlerName); err != nil {
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

				// Always include all columns so kvx detects a homogeneous
				// array and renders a proper columnar table. The schema
				// controls which columns are visible in table vs json/yaml.
				result := map[string]any{
					"handler":       handlerName,
					"displayName":   handler.DisplayName(),
					"authenticated": status.Authenticated,
					"email":         "",
					"username":      "",
					"expiresIn":     "",
					"hint":          "",
					"identityType":  "",
					"clientId":      "",
					"tokenFile":     "",
					"name":          "",
					"tenantId":      "",
					"expiresAt":     "",
					"lastRefresh":   "",
					"scopes":        []string{},
					"cachedTokens":  0,
				}

				if !status.Authenticated {
					result["hint"] = fmt.Sprintf("run '%s auth login %s' to authenticate", cliParams.BinaryName, handlerName)
				}

				if status.IdentityType != "" {
					result["identityType"] = string(status.IdentityType)
				}

				if status.ClientID != "" {
					result["clientId"] = status.ClientID
				}

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
						if timeUntil := time.Until(status.ExpiresAt); timeUntil > 0 {
							result["expiresIn"] = humanDuration(timeUntil)
						}
					}
					if !status.LastRefresh.IsZero() {
						result["lastRefresh"] = status.LastRefresh
					}
				}

				if len(status.Scopes) > 0 {
					result["scopes"] = status.Scopes
				}

				// Cached token count (when available).
				if lister, ok := handler.(auth.TokenLister); ok {
					if tokens, err := lister.ListCachedTokens(ctx); err == nil {
						result["cachedTokens"] = len(tokens)
					}
				}

				results = append(results, result)
			}

			if len(results) == 0 {
				err := fmt.Errorf("no auth handlers found")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			outputOpts := flags.ToKvxOutputOptions(&outputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnOrder([]string{"handler", "displayName", "authenticated", "email", "username", "expiresIn"}),
				kvx.WithOutputSchemaJSON(authStatusSchema),
			)

			if err := outputOpts.Write(results); err != nil {
				return err
			}

			// If --exit-code is set, return non-zero when any handler is not authenticated.
			if exitCodeFlag {
				for _, r := range results {
					if authenticated, ok := r["authenticated"].(bool); ok && !authenticated {
						err := fmt.Errorf("one or more auth handlers are not authenticated")
						return exitcode.WithCode(err, exitcode.GeneralError)
					}
				}
			}

			// If --warn-within is set, return non-zero when any authenticated handler's
			// token expires within the given window.
			if warnWithin > 0 {
				for _, r := range results {
					authenticated, _ := r["authenticated"].(bool)
					if !authenticated {
						continue
					}
					if expiresAt, ok := r["expiresAt"].(time.Time); ok {
						if time.Until(expiresAt) < warnWithin {
							err := fmt.Errorf("one or more tokens expire within %s", warnWithin)
							w.Warningf("%v", err)
							return exitcode.WithCode(err, exitcode.GeneralError)
						}
					}
				}
			}
			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	cmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false, "Exit non-zero if any handler is not authenticated (useful for scripting)")
	cmd.Flags().DurationVar(&warnWithin, "warn-within", 0, "Exit non-zero if any authenticated handler's token expires within this duration (e.g. 10m, 1h)")
	return cmd
}
