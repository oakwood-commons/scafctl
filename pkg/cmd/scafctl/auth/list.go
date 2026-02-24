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

// CommandList creates the 'auth list' command.
// It shows metadata for all cached tokens (refresh and minted access) for the
// specified handler, or for all handlers when no argument is given.
// Actual token values are never displayed; use 'auth token' for that.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	var (
		expiredOnly  bool
		validOnly    bool
		purgeExpired bool
	)

	var sortBy string

	cmd := &cobra.Command{
		Use:   "list [handler]",
		Short: "Show cached refresh and access token metadata",
		Long: heredoc.Doc(`
			Show metadata for all cached tokens (refresh and minted access tokens).

			By default all registered auth handlers are queried. When a handler name
			is provided only that handler's cached tokens are shown.

			The output includes token kind (refresh/access), scope, type, expiry,
			cache time, and whether the token is currently expired.  Actual token
			values are never displayed here — use 'scafctl auth token <handler>'
			to retrieve a token value.

			Examples:
			  # Show tokens for all handlers
			  scafctl auth list

			  # Show tokens for Entra only
			  scafctl auth list entra

			  # Show tokens for GCP only
			  scafctl auth list gcp

			  # Show only expired tokens
			  scafctl auth list --expired-only

			  # Show only valid (non-expired) tokens
			  scafctl auth list --valid-only

			  # Output as JSON
			  scafctl auth list -o json

			  # Output as YAML
			  scafctl auth list -o yaml

			  # Sort by expiry (soonest expiring first)
			  scafctl auth list --sort expires-at

			  # Sort by handler name
			  scafctl auth list --sort handler

			  # Remove expired access tokens from the cache (keeps valid tokens and the refresh token)
			  scafctl auth list --purge-expired
			  scafctl auth list entra --purge-expired
			`),
		Aliases:      []string{"ls"},
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			if expiredOnly && validOnly {
				err := fmt.Errorf("--expired-only and --valid-only are mutually exclusive")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if purgeExpired && (expiredOnly || validOnly) {
				err := fmt.Errorf("--purge-expired cannot be combined with --expired-only or --valid-only")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			handlerNames := listHandlers(ctx)
			if len(handlerNames) == 0 {
				err := fmt.Errorf("no auth handlers registered")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Filter to a single handler if specified
			if len(args) > 0 {
				handlerName := args[0]
				if err := validateHandlerName(ctx, handlerName); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				handlerNames = []string{handlerName}
			}

			// --purge-expired: remove expired access tokens and return a summary.
			if purgeExpired {
				total := 0
				for _, name := range handlerNames {
					handler, err := getHandler(ctx, name)
					if err != nil {
						w.Warningf("Failed to initialize %s: %v", name, err)
						continue
					}
					purger, ok := handler.(auth.TokenPurger)
					if !ok {
						w.Warningf("%s does not support token purging", name)
						continue
					}
					n, err := purger.PurgeExpiredTokens(ctx)
					if err != nil {
						w.Warningf("Failed to purge expired tokens for %s: %v", name, err)
						continue
					}
					if n > 0 {
						w.Successf("Purged %d expired access token(s) from %s.", n, name)
					} else {
						w.Infof("No expired access tokens found in %s.", name)
					}
					total += n
				}
				if len(handlerNames) > 1 {
					if total > 0 {
						w.Successf("Total: purged %d expired access token(s).", total)
					} else {
						w.Infof("No expired access tokens found in any handler.")
					}
				}
				return nil
			}

			results := make([]map[string]any, 0)

			for _, name := range handlerNames {
				handler, err := getHandler(ctx, name)
				if err != nil {
					w.Warningf("Failed to initialize %s: %v", name, err)
					continue
				}

				lister, ok := handler.(auth.TokenLister)
				if !ok {
					w.Warningf("%s does not support token listing", name)
					continue
				}

				tokens, err := lister.ListCachedTokens(ctx)
				if err != nil {
					w.Warningf("Failed to list tokens for %s: %v", name, err)
					continue
				}

				for _, t := range tokens {
					results = append(results, cachedTokenInfoToMap(t))
				}
			}

			if len(results) == 0 {
				w.Infof("No cached tokens found.")
				return nil
			}

			// Apply --expired-only / --valid-only filters.
			if expiredOnly || validOnly {
				filtered := make([]map[string]any, 0, len(results))
				for _, r := range results {
					isExpired, _ := r["isExpired"].(bool)
					if expiredOnly && isExpired {
						filtered = append(filtered, r)
					} else if validOnly && !isExpired {
						filtered = append(filtered, r)
					}
				}
				results = filtered
			}

			if len(results) == 0 {
				w.Infof("No cached tokens matched the filter.")
				return nil
			}

			// Apply --sort
			if sortBy != "" {
				if err := sortTokenResults(results, sortBy); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
			}

			outputOpts := flags.NewKvxOutputOptionsFromFlags(
				outputFlags.Output,
				outputFlags.Interactive,
				outputFlags.Expression,
				kvx.WithOutputContext(ctx),
				kvx.WithOutputNoColor(cliParams.NoColor),
				kvx.WithOutputAppName("scafctl auth list"),
			)
			outputOpts.IOStreams = ioStreams

			return outputOpts.Write(results)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	cmd.Flags().BoolVar(&expiredOnly, "expired-only", false, "Show only expired tokens")
	cmd.Flags().BoolVar(&validOnly, "valid-only", false, "Show only valid (non-expired) tokens")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort results by field: handler, kind, scope, expires-at, cached-at")
	cmd.Flags().BoolVar(&purgeExpired, "purge-expired", false, "Remove expired access tokens from the cache (the refresh token and valid tokens are preserved)")
	return cmd
}

// sortTokenResults sorts the list results in-place by the given field name.
func sortTokenResults(results []map[string]any, field string) error {
	var key string
	switch strings.ToLower(field) {
	case "handler":
		key = "handler"
	case "kind":
		key = "tokenKind"
	case "scope":
		key = "scope"
	case "expires-at", "expiresat":
		key = "expiresAt"
	case "cached-at", "cachedat":
		key = "cachedAt"
	default:
		return fmt.Errorf("unknown sort field %q: valid values are handler, kind, scope, expires-at, cached-at", field)
	}

	sort.Slice(results, func(i, j int) bool {
		vi, vj := results[i][key], results[j][key]
		// Time fields
		if ti, ok := vi.(time.Time); ok {
			if tj, ok := vj.(time.Time); ok {
				return ti.Before(tj)
			}
		}
		// String fields
		si, _ := vi.(string)
		sj, _ := vj.(string)
		return si < sj
	})
	return nil
}

// cachedTokenInfoToMap converts a CachedTokenInfo into a map suitable for kvx output.
func cachedTokenInfoToMap(t *auth.CachedTokenInfo) map[string]any {
	row := map[string]any{
		"handler":   t.Handler,
		"tokenKind": t.TokenKind,
		"isExpired": t.IsExpired,
	}

	if t.Scope != "" {
		row["scope"] = t.Scope
	}
	if t.TokenType != "" {
		row["tokenType"] = t.TokenType
	}
	if t.Flow != "" {
		row["flow"] = string(t.Flow)
	}
	if t.SessionID != "" {
		row["sessionId"] = t.SessionID
	}
	if !t.ExpiresAt.IsZero() {
		row["expiresAt"] = t.ExpiresAt
		if !t.IsExpired {
			row["expiresIn"] = humanDuration(t.TimeUntilExpiry())
		}
	}
	if !t.CachedAt.IsZero() {
		row["cachedAt"] = t.CachedAt
	}

	// Only access tokens can be retrieved via 'auth token'; refresh tokens are
	// used internally and have no direct retrieval command.
	if t.TokenKind == "access" {
		if t.Scope != "" {
			row["getTokenCommand"] = fmt.Sprintf("scafctl auth token %s --scope %q --raw", t.Handler, t.Scope)
		} else {
			row["getTokenCommand"] = fmt.Sprintf("scafctl auth token %s --raw", t.Handler)
		}
	}

	return row
}

// humanDuration formats a duration in a human-friendly way.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	switch {
	case hours > 24:
		days := hours / 24
		return fmt.Sprintf("%dd%dh", days, hours%24)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
