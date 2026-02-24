// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	entraauth "github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
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

// diagCheckStatus represents the result of a single diagnostic check.
type diagCheckStatus string

const (
	diagOK   diagCheckStatus = "ok"
	diagWarn diagCheckStatus = "warn"
	diagFail diagCheckStatus = "fail"
	diagInfo diagCheckStatus = "info"
)

// diagCheck represents one diagnostic check result.
type diagCheck struct {
	Category string          `json:"category"`
	Check    string          `json:"check"`
	Status   diagCheckStatus `json:"status"`
	Message  string          `json:"message"`
}

// CommandDiagnose creates the 'auth diagnose' command.
func CommandDiagnose(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var outputFlags flags.KvxOutputFlags
	var liveToken bool

	cmd := &cobra.Command{
		Use:     "diagnose",
		Aliases: []string{"doctor"},
		Short:   "Run auth diagnostics and report any issues",
		Long: heredoc.Doc(`
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
		`),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)

			// When -o/--interactive/--expression is given, suppress human text and
			// emit only the structured data at the end.
			structuredOutput := cmd.Flags().Changed("output") || outputFlags.Interactive || outputFlags.Expression != ""

			var checks []diagCheck
			addCheck := func(c diagCheck) {
				checks = append(checks, c)
				if structuredOutput {
					return
				}
				switch c.Status {
				case diagOK:
					w.Successf("[ok]   %s: %s", c.Check, c.Message)
				case diagWarn:
					w.Warningf("[warn] %s: %s", c.Check, c.Message)
				case diagFail:
					w.Errorf("[fail] %s: %s", c.Check, c.Message)
				case diagInfo:
					w.Infof("[info] %s: %s", c.Check, c.Message)
				}
			}

			// ── 1. Auth registry ──────────────────────────────────────────────
			handlerNames := listHandlers(ctx)
			if len(handlerNames) == 0 {
				addCheck(diagCheck{
					Category: "registry",
					Check:    "auth registry",
					Status:   diagFail,
					Message:  "no auth handlers registered — registry may not be initialised",
				})
			} else {
				addCheck(diagCheck{
					Category: "registry",
					Check:    "auth registry",
					Status:   diagOK,
					Message:  fmt.Sprintf("registered handlers: %v", handlerNames),
				})
			}

			// ── 2. Config file ────────────────────────────────────────────────
			cfgPath, cfgPathErr := paths.SearchConfigFile()
			if cfgPathErr != nil {
				addCheck(diagCheck{
					Category: "config",
					Check:    "config file",
					Status:   diagWarn,
					Message:  "config file not found — using built-in defaults",
				})
			} else {
				addCheck(diagCheck{
					Category: "config",
					Check:    "config file",
					Status:   diagOK,
					Message:  cfgPath,
				})
			}

			// Auth sections in config
			cfg := config.FromContext(ctx)
			if cfg != nil {
				if cfg.Auth.Entra != nil {
					addCheck(diagCheck{
						Category: "config",
						Check:    "config entra section",
						Status:   diagOK,
						Message:  fmt.Sprintf("clientId=%q tenantId=%q", cfg.Auth.Entra.ClientID, cfg.Auth.Entra.TenantID),
					})
				}
				if cfg.Auth.GitHub != nil {
					addCheck(diagCheck{
						Category: "config",
						Check:    "config github section",
						Status:   diagOK,
						Message:  fmt.Sprintf("clientId=%q hostname=%q", cfg.Auth.GitHub.ClientID, cfg.Auth.GitHub.Hostname),
					})
				}
				if cfg.Auth.GCP != nil {
					addCheck(diagCheck{
						Category: "config",
						Check:    "config gcp section",
						Status:   diagOK,
						Message:  fmt.Sprintf("clientId=%q impersonate=%q", cfg.Auth.GCP.ClientID, cfg.Auth.GCP.ImpersonateServiceAccount),
					})
				}
			}

			// ── 3. Environment variables ──────────────────────────────────────
			for _, c := range runEnvVarChecks() {
				addCheck(c)
			}

			// ── 3.5. Clock skew ───────────────────────────────────────────────
			addCheck(runClockSkewCheck())

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
					addCheck(diagCheck{
						Category: "handler",
						Check:    fmt.Sprintf("%s: init", name),
						Status:   diagFail,
						Message:  err.Error(),
					})
					failCount++
					continue
				}

				status, err := handler.Status(ctx)
				if err != nil {
					addCheck(diagCheck{
						Category: "handler",
						Check:    fmt.Sprintf("%s: status", name),
						Status:   diagFail,
						Message:  err.Error(),
					})
					failCount++
					continue
				}

				if !status.Authenticated {
					addCheck(diagCheck{
						Category: "handler",
						Check:    fmt.Sprintf("%s: authenticated", name),
						Status:   diagWarn,
						Message:  fmt.Sprintf("not authenticated — run 'scafctl auth login %s'", name),
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
				addCheck(diagCheck{
					Category: "handler",
					Check:    fmt.Sprintf("%s: authenticated", name),
					Status:   diagOK,
					Message:  msg,
				})

				// Cached token summary
				if lister, ok := handler.(authpkg.TokenLister); ok {
					tokens, err := lister.ListCachedTokens(ctx)
					if err != nil {
						addCheck(diagCheck{
							Category: "cache",
							Check:    fmt.Sprintf("%s: token cache", name),
							Status:   diagWarn,
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
						cacheStatus := diagOK
						if expired > 0 {
							cacheMsg += fmt.Sprintf(", %d expired", expired)
							cacheStatus = diagWarn
						}
						addCheck(diagCheck{
							Category: "cache",
							Check:    fmt.Sprintf("%s: token cache", name),
							Status:   cacheStatus,
							Message:  cacheMsg,
						})
					}
				}

				// Live token fetch
				if liveToken && authpkg.HasCapability(handler.Capabilities(), authpkg.CapScopesOnTokenRequest) {
					// Cannot do a generic live fetch without a scope — report info
					addCheck(diagCheck{
						Category: "live",
						Check:    fmt.Sprintf("%s: live token", name),
						Status:   diagInfo,
						Message:  "handler requires --scope for token fetch; use 'scafctl auth token " + name + " --scope <scope>' to test manually",
					})
				} else if liveToken {
					_, err := handler.GetToken(ctx, authpkg.TokenOptions{
						MinValidFor: authpkg.DefaultMinValidFor,
					})
					if err != nil {
						addCheck(diagCheck{
							Category: "live",
							Check:    fmt.Sprintf("%s: live token", name),
							Status:   diagFail,
							Message:  err.Error(),
						})
						failCount++
					} else {
						addCheck(diagCheck{
							Category: "live",
							Check:    fmt.Sprintf("%s: live token", name),
							Status:   diagOK,
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
				case diagFail:
					// counted above
				case diagWarn:
					warnCount++
				case diagOK:
					okCount++
				case diagInfo:
					// info checks do not affect counts
				}
			}

			if !structuredOutput {
				fmt.Fprintln(ioStreams.Out, "")
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
						"check":    c.Check,
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
					kvx.WithOutputAppName("scafctl auth diagnose"),
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

// runEnvVarChecks checks common environment variables for all known auth handlers.
func runEnvVarChecks() []diagCheck {
	var checks []diagCheck

	entraVars := []struct {
		name, desc string
	}{
		{"AZURE_CLIENT_ID", "Entra service principal client ID"},
		{"AZURE_TENANT_ID", "Entra tenant ID"},
		{"AZURE_CLIENT_SECRET", "Entra client secret (service principal)"},
		{"AZURE_FEDERATED_TOKEN_FILE", "Entra workload identity token file path"},
		{"AZURE_FEDERATED_TOKEN", "Entra workload identity token (raw)"},
	}
	for _, v := range entraVars {
		val := os.Getenv(v.name)
		if val != "" {
			checks = append(checks, diagCheck{
				Category: "env",
				Check:    fmt.Sprintf("env %s", v.name),
				Status:   diagOK,
				Message:  fmt.Sprintf("%s — set (%s)", v.desc, v.name),
			})
		}
	}
	if entraauth.HasServicePrincipalCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env entra: service-principal credentials",
			Status:   diagOK,
			Message:  "AZURE_CLIENT_ID + AZURE_TENANT_ID + AZURE_CLIENT_SECRET are all set",
		})
	}
	if entraauth.HasWorkloadIdentityCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env entra: workload-identity credentials",
			Status:   diagOK,
			Message:  "workload identity environment detected (AZURE_FEDERATED_TOKEN_FILE or AZURE_FEDERATED_TOKEN)",
		})
	}

	ghVars := []struct{ name, desc string }{
		{"GITHUB_TOKEN", "GitHub personal access token"},
		{"GH_TOKEN", "GitHub personal access token (alternate)"},
	}
	for _, v := range ghVars {
		if os.Getenv(v.name) != "" {
			checks = append(checks, diagCheck{
				Category: "env",
				Check:    fmt.Sprintf("env %s", v.name),
				Status:   diagOK,
				Message:  fmt.Sprintf("%s — set", v.desc),
			})
		}
	}
	if ghauth.HasPATCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env github: PAT credentials",
			Status:   diagOK,
			Message:  "GITHUB_TOKEN or GH_TOKEN is set",
		})
	}

	gcpVars := []struct{ name, desc string }{
		{"GOOGLE_APPLICATION_CREDENTIALS", "GCP service account key file path"},
		{"GOOGLE_EXTERNAL_ACCOUNT", "GCP workload identity external account config"},
		{"GOOGLE_CLOUD_PROJECT", "GCP project ID"},
	}
	for _, v := range gcpVars {
		if os.Getenv(v.name) != "" {
			checks = append(checks, diagCheck{
				Category: "env",
				Check:    fmt.Sprintf("env %s", v.name),
				Status:   diagOK,
				Message:  fmt.Sprintf("%s — set", v.desc),
			})
		}
	}
	if gcpauth.HasServiceAccountCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env gcp: service-account credentials",
			Status:   diagOK,
			Message:  "GOOGLE_APPLICATION_CREDENTIALS is set and points to a service account key",
		})
	}
	if gcpauth.HasWorkloadIdentityCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env gcp: workload-identity credentials",
			Status:   diagOK,
			Message:  "GCP workload identity environment detected",
		})
	}
	if gcpauth.HasGcloudADCCredentials() {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env gcp: gcloud ADC",
			Status:   diagOK,
			Message:  "gcloud Application Default Credentials file found",
		})
	}

	if len(checks) == 0 {
		checks = append(checks, diagCheck{
			Category: "env",
			Check:    "env: credential variables",
			Status:   diagInfo,
			Message:  "no auth-related environment variables detected (interactive login may still work)",
		})
	}

	return checks
}

// runClockSkewCheck compares the local system clock against the Date header
// returned by a well-known HTTPS endpoint (cloudflare.com).
// A skew > 5 minutes can cause token validation failures.
func runClockSkewCheck() diagCheck {
	const endpoint = "https://cloudflare.com"
	const maxSkew = 5 * time.Minute
	const timeout = 4 * time.Second

	client := &http.Client{Timeout: timeout}
	before := time.Now()
	resp, err := client.Head(endpoint) //nolint:noctx // no context needed for a simple diagnostic probe
	if err != nil {
		return diagCheck{
			Category: "clock",
			Check:    "clock skew",
			Status:   diagWarn,
			Message:  fmt.Sprintf("could not reach %s to check clock skew: %v", endpoint, err),
		}
	}
	defer resp.Body.Close()
	after := time.Now()
	localMid := before.Add(after.Sub(before) / 2) // midpoint of the round-trip

	dateHeader := resp.Header.Get("Date")
	if dateHeader == "" {
		return diagCheck{
			Category: "clock",
			Check:    "clock skew",
			Status:   diagInfo,
			Message:  fmt.Sprintf("no Date header returned by %s; cannot check clock skew", endpoint),
		}
	}

	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		return diagCheck{
			Category: "clock",
			Check:    "clock skew",
			Status:   diagWarn,
			Message:  fmt.Sprintf("could not parse Date header %q: %v", dateHeader, err),
		}
	}

	skew := localMid.Sub(serverTime)
	if skew < 0 {
		skew = -skew
	}

	if skew > maxSkew {
		return diagCheck{
			Category: "clock",
			Check:    "clock skew",
			Status:   diagFail,
			Message:  fmt.Sprintf("clock skew is %s (local: %s, server: %s) — token validation may fail (JWT nbf/exp checks require skew < 5m)", skew.Round(time.Second), localMid.UTC().Format(time.RFC3339), serverTime.UTC().Format(time.RFC3339)),
		}
	}

	return diagCheck{
		Category: "clock",
		Check:    "clock skew",
		Status:   diagOK,
		Message:  fmt.Sprintf("clock skew is %s (within acceptable range)", skew.Round(time.Millisecond)),
	}
}
