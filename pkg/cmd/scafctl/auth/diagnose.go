// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/diagnose"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// clockSkewCheckFunc is the function used to perform the clock skew check.
// Tests can replace this to avoid real network calls.
var clockSkewCheckFunc = diagnose.RunClockSkewCheck

// CommandDiagnose creates the 'auth diagnose' command.
func CommandDiagnose(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	var liveToken bool

	cmd := &cobra.Command{
		Use:     "diagnose",
		Aliases: []string{"doctor"},
		Short:   "Run auth diagnostics and report any issues",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Run a series of diagnostic checks on the authentication configuration
			and report any issues found.

			Checks include:
			  - Auth registry health (handlers registered)
			  - Config file presence and auth sections
			  - Environment variables for each handler
			  - Clock skew (compares local time against an HTTPS server)
			  - Current authentication status (are you logged in?)
			  - Cached token health (expired tokens, missing tokens)

			When a handler name is provided, the handler status and cache checks
			are scoped to that handler only.

			Use --live-token to also attempt a live token fetch for each authenticated
			handler, confirming end-to-end token acquisition works.

			Examples:
			  # Run auth diagnostics (all handlers)
			  scafctl auth diagnose

			  # Run diagnostics for Entra only
			  scafctl auth diagnose entra

			  # Run diagnostics and attempt a live token fetch
			  scafctl auth diagnose --live-token

			  # Output diagnostics as JSON
			  scafctl auth diagnose -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
			outputFlags.AppName = cliParams.BinaryName

			// When -o/--interactive/--expression is given, suppress human text and
			// emit only the structured data at the end.
			structuredOutput := cmd.Flags().Changed("output") || outputFlags.Interactive || outputFlags.Expression != ""

			var checks []diagnose.Check
			addCheck := func(c diagnose.Check) {
				checks = append(checks, c)
				if structuredOutput {
					return
				}
				switch c.Status {
				case diagnose.StatusOK:
					w.Successf("[ok]   %s: %s", c.Name, c.Message)
				case diagnose.StatusWarn:
					w.Warningf("[warn] %s: %s", c.Name, c.Message)
				case diagnose.StatusFail:
					w.Errorf("[fail] %s: %s", c.Name, c.Message)
				case diagnose.StatusInfo:
					w.Infof("[info] %s: %s", c.Name, c.Message)
				}
			}

			// ── 1. Auth registry ──────────────────────────────────────────────
			handlerNames := listHandlers(ctx)
			if len(handlerNames) == 0 {
				addCheck(diagnose.Check{
					Category: "registry",
					Name:     "auth registry",
					Status:   diagnose.StatusFail,
					Message:  "no auth handlers registered — registry may not be initialised",
				})
			} else {
				addCheck(diagnose.Check{
					Category: "registry",
					Name:     "auth registry",
					Status:   diagnose.StatusOK,
					Message:  fmt.Sprintf("registered handlers: %v", handlerNames),
				})
			}

			// ── 2. Config file ────────────────────────────────────────────────
			cfgPath, cfgPathErr := paths.SearchConfigFile()
			if cfgPathErr != nil {
				addCheck(diagnose.Check{
					Category: "config",
					Name:     "config file",
					Status:   diagnose.StatusWarn,
					Message:  "config file not found — using built-in defaults",
				})
			} else {
				addCheck(diagnose.Check{
					Category: "config",
					Name:     "config file",
					Status:   diagnose.StatusOK,
					Message:  cfgPath,
				})
			}

			// Auth sections in config
			cfg := config.FromContext(ctx)
			if cfg != nil {
				if cfg.Auth.Entra != nil {
					addCheck(diagnose.Check{
						Category: "config",
						Name:     "config entra section",
						Status:   diagnose.StatusOK,
						Message:  fmt.Sprintf("clientId=%q tenantId=%q", cfg.Auth.Entra.ClientID, cfg.Auth.Entra.TenantID),
					})
				}
				if cfg.Auth.GitHub != nil {
					addCheck(diagnose.Check{
						Category: "config",
						Name:     "config github section",
						Status:   diagnose.StatusOK,
						Message:  fmt.Sprintf("clientId=%q hostname=%q", cfg.Auth.GitHub.ClientID, cfg.Auth.GitHub.Hostname),
					})
				}
				if cfg.Auth.GCP != nil {
					addCheck(diagnose.Check{
						Category: "config",
						Name:     "config gcp section",
						Status:   diagnose.StatusOK,
						Message:  fmt.Sprintf("clientId=%q impersonate=%q", cfg.Auth.GCP.ClientID, cfg.Auth.GCP.ImpersonateServiceAccount),
					})
				}
			}

			// ── 3. Environment variables ──────────────────────────────────────
			for _, c := range diagnose.RunEnvVarChecks() {
				addCheck(c)
			}

			// ── 3.5. Clock skew ───────────────────────────────────────────────
			addCheck(clockSkewCheckFunc())

			// ── 4. Handler authentication status & token health ───────────────
			// When a handler name is provided, scope checks to that handler only.
			if len(args) > 0 {
				handlerName := args[0]
				if err := validateHandlerName(ctx, handlerName); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				handlerNames = []string{handlerName}
			}

			failCount := 0
			for _, name := range handlerNames {
				handler, err := getHandler(ctx, name)
				if err != nil {
					addCheck(diagnose.Check{
						Category: "handler",
						Name:     fmt.Sprintf("%s: init", name),
						Status:   diagnose.StatusFail,
						Message:  err.Error(),
					})
					failCount++
					continue
				}

				status, err := handler.Status(ctx)
				if err != nil {
					addCheck(diagnose.Check{
						Category: "handler",
						Name:     fmt.Sprintf("%s: status", name),
						Status:   diagnose.StatusFail,
						Message:  err.Error(),
					})
					failCount++
					continue
				}

				if !status.Authenticated {
					addCheck(diagnose.Check{
						Category: "handler",
						Name:     fmt.Sprintf("%s: authenticated", name),
						Status:   diagnose.StatusWarn,
						Message:  fmt.Sprintf("not authenticated — run '%s auth login %s'", cliParams.BinaryName, name),
					})
					continue
				}

				identity := ""
				if status.Claims != nil {
					identity = status.Claims.DisplayIdentity()
				}
				msg := fmt.Sprintf("authenticated as %q", identity)
				if !status.ExpiresAt.IsZero() {
					msg += fmt.Sprintf(", expires in %s", humanDuration(time.Until(status.ExpiresAt)))
				}
				addCheck(diagnose.Check{
					Category: "handler",
					Name:     fmt.Sprintf("%s: authenticated", name),
					Status:   diagnose.StatusOK,
					Message:  msg,
				})

				// Cached token summary
				if lister, ok := handler.(authpkg.TokenLister); ok {
					tokens, err := lister.ListCachedTokens(ctx)
					if err != nil {
						addCheck(diagnose.Check{
							Category: "cache",
							Name:     fmt.Sprintf("%s: token cache", name),
							Status:   diagnose.StatusWarn,
							Message:  fmt.Sprintf("could not read cached tokens: %v", err),
						})
					} else {
						expired := 0
						for _, t := range tokens {
							if t.IsExpired {
								expired++
							}
						}
						cacheMsg := fmt.Sprintf("%d cached token(s)", len(tokens))
						cacheStatus := diagnose.StatusOK
						if expired > 0 {
							cacheMsg += fmt.Sprintf(", %d expired", expired)
							cacheStatus = diagnose.StatusWarn
						}
						addCheck(diagnose.Check{
							Category: "cache",
							Name:     fmt.Sprintf("%s: token cache", name),
							Status:   cacheStatus,
							Message:  cacheMsg,
						})
					}
				}

				// Live token fetch
				if liveToken && authpkg.HasCapability(handler.Capabilities(), authpkg.CapScopesOnTokenRequest) {
					// Cannot do a generic live fetch without a scope — report info
					addCheck(diagnose.Check{
						Category: "live",
						Name:     fmt.Sprintf("%s: live token", name),
						Status:   diagnose.StatusInfo,
						Message:  "handler requires --scope for token fetch; use '" + cliParams.BinaryName + " auth token " + name + " --scope <scope>' to test manually",
					})
				} else if liveToken {
					_, err := handler.GetToken(ctx, authpkg.TokenOptions{
						MinValidFor: authpkg.DefaultMinValidFor,
					})
					if err != nil {
						addCheck(diagnose.Check{
							Category: "live",
							Name:     fmt.Sprintf("%s: live token", name),
							Status:   diagnose.StatusFail,
							Message:  err.Error(),
						})
						failCount++
					} else {
						addCheck(diagnose.Check{
							Category: "live",
							Name:     fmt.Sprintf("%s: live token", name),
							Status:   diagnose.StatusOK,
							Message:  "token acquired successfully",
						})
					}
				}
			}

			// ── 5. Summary ────────────────────────────────────────────────────
			warnCount := 0
			okCount := 0
			for _, c := range checks {
				switch c.Status {
				case diagnose.StatusFail:
					// counted above
				case diagnose.StatusWarn:
					warnCount++
				case diagnose.StatusOK:
					okCount++
				case diagnose.StatusInfo:
					// info checks do not affect counts
				}
			}

			if !structuredOutput {
				w.Plainln("")
				switch {
				case failCount > 0:
					w.Errorf("Diagnostics complete: %d failure(s), %d warning(s), %d ok", failCount, warnCount, okCount)
				case warnCount > 0:
					w.Warningf("Diagnostics complete: %d warning(s), %d ok (no failures)", warnCount, okCount)
				default:
					w.Successf("Diagnostics complete: all %d checks passed.", okCount)
				}
			}

			// Structured output — only when explicitly requested.
			if structuredOutput {
				results := make([]map[string]any, 0, len(checks))
				for _, c := range checks {
					results = append(results, map[string]any{
						"category": c.Category,
						"check":    c.Name,
						"status":   string(c.Status),
						"message":  c.Message,
					})
				}
				outputOpts := flags.NewKvxOutputOptionsFromFlags(
					outputFlags.Output,
					outputFlags.Interactive,
					outputFlags.Expression,
					kvx.WithOutputContext(ctx),
					kvx.WithOutputNoColor(cliParams.NoColor),
					kvx.WithOutputAppName(cliParams.BinaryName+" auth diagnose"),
				)
				outputOpts.IOStreams = ioStreams
				return outputOpts.Write(results)
			}

			// Only return non-zero when explicitly requested (structured) or
			// when running in human mode.
			if failCount > 0 {
				return exitcode.WithCode(fmt.Errorf("one or more diagnostic checks failed"), exitcode.GeneralError)
			}
			return nil
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &outputFlags)
	cmd.Flags().BoolVar(&liveToken, "live-token", false, "Attempt a live token fetch for each authenticated handler to confirm end-to-end health")

	return cmd
}
