package auth

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogin creates the 'auth login' command.
func CommandLogin(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		tenantID       string
		timeout        time.Duration
		flowStr        string
		federatedToken string
	)

	cmd := &cobra.Command{
		Use:   "login <handler>",
		Short: "Authenticate with an auth handler",
		Long: heredoc.Doc(`
			Authenticate with an authentication handler.

			For the 'entra' handler, this supports multiple authentication flows:
			- device-code: Interactive authentication via browser (default)
			- service-principal: Non-interactive authentication for CI/CD
			- workload-identity: Kubernetes workload identity (AKS)

			For service principal flow, set these environment variables:
			- AZURE_CLIENT_ID: Application (client) ID
			- AZURE_TENANT_ID: Directory (tenant) ID
			- AZURE_CLIENT_SECRET: Client secret value

			For workload identity flow, set these environment variables:
			- AZURE_CLIENT_ID: Application (client) ID
			- AZURE_TENANT_ID: Directory (tenant) ID
			- AZURE_FEDERATED_TOKEN_FILE: Path to projected token file
			  OR
			- AZURE_FEDERATED_TOKEN: Raw federated token (for testing)
			  OR
			- --federated-token flag: Pass token directly (for testing)

			Supported handlers:
			- entra: Microsoft Entra ID

			Examples:
			  # Login with Entra ID using device code flow (default)
			  scafctl auth login entra

			  # Login with a specific tenant
			  scafctl auth login entra --tenant 08e70e8e-d05c-4449-a2c2-67bd0a9c4e79

			  # Login with service principal (requires env vars)
			  scafctl auth login entra --flow service-principal

			  # Login with workload identity (Kubernetes)
			  scafctl auth login entra --flow workload-identity

			  # Test workload identity with a direct token
			  scafctl auth login entra --flow workload-identity --federated-token "eyJ..."

			  # Login with a custom timeout (device code only)
			  scafctl auth login entra --timeout 10m
		`),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			handlerName := args[0]

			// Validate handler name
			if !IsSupportedHandler(handlerName) {
				err := fmt.Errorf("unknown auth handler: %s (supported: %v)", handlerName, SupportedHandlers())
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Parse flow
			flow, err := parseFlow(flowStr)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// If --federated-token is provided, set the env var for workload identity
			if federatedToken != "" {
				if err := os.Setenv(entra.EnvAzureFederatedToken, federatedToken); err != nil {
					err = fmt.Errorf("failed to set federated token: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				// Auto-select workload identity flow if not explicitly set
				if flowStr == "" {
					flow = auth.FlowWorkloadIdentity
				}
			}

			// Auto-detect workload identity if env vars are set and no explicit flow (highest priority)
			if flowStr == "" && entra.HasWorkloadIdentityCredentials() {
				flow = auth.FlowWorkloadIdentity
				w.Info("Detected workload identity credentials in environment")
			} else if flowStr == "" && entra.HasServicePrincipalCredentials() {
				// Auto-detect service principal if env vars are set and no explicit flow
				flow = auth.FlowServicePrincipal
				w.Info("Detected service principal credentials in environment variables")
			}

			// Get or create handler
			handler, err := getEntraHandlerWithTenant(ctx, tenantID)
			if err != nil {
				err = fmt.Errorf("failed to initialize auth handler: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Check if already authenticated (skip for service principal)
			if flow != auth.FlowServicePrincipal {
				status, err := handler.Status(ctx)
				if err != nil {
					err = fmt.Errorf("failed to check auth status: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}

				if status.Authenticated {
					identity := status.Claims.Email
					if identity == "" {
						identity = status.Claims.Name
					}
					if identity == "" {
						identity = status.Claims.Subject
					}
					w.Infof("Already authenticated as %s", identity)
					w.Info("Use 'scafctl auth logout entra' to sign out first, or continue to re-authenticate.")
					w.Info("")
				}
			}

			// Set up cancellation handling
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt)
			go func() {
				<-sigChan
				w.Info("")
				w.Warning("Authentication cancelled by user.")
				cancel()
			}()
			defer signal.Stop(sigChan)

			// Prepare login options
			loginOpts := auth.LoginOptions{
				TenantID: tenantID,
				Flow:     flow,
				Timeout:  timeout,
				DeviceCodeCallback: func(userCode, verificationURI, _ string) {
					w.Info("")
					w.Info("To sign in, use a web browser to open the page:")
					w.Infof("  %s", verificationURI)
					w.Info("")
					w.Infof("Enter the code: %s", userCode)
					w.Info("")
					w.Info("Waiting for authentication...")
				},
			}

			result, err := handler.Login(ctx, loginOpts)
			if err != nil {
				if ctx.Err() != nil {
					return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
				}
				err = fmt.Errorf("authentication failed: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Info("")
			w.Success("Authentication successful!")
			if result.Claims.Name != "" {
				w.Infof("  Name:   %s", result.Claims.Name)
			}
			if result.Claims.Email != "" {
				w.Infof("  Email:  %s", result.Claims.Email)
			}
			if result.Claims.TenantID != "" {
				w.Infof("  Tenant: %s", result.Claims.TenantID)
			}
			if flow == auth.FlowServicePrincipal {
				w.Info("  Flow:   Service Principal")
			}
			if flow == auth.FlowWorkloadIdentity {
				w.Info("  Flow:   Workload Identity")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&tenantID, "tenant", "", "Azure tenant ID (overrides config)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authentication flow")
	cmd.Flags().StringVar(&flowStr, "flow", "", "Authentication flow: 'device-code' (default), 'service-principal', or 'workload-identity'")
	cmd.Flags().StringVar(&federatedToken, "federated-token", "", "Federated token for workload identity (for testing; sets AZURE_FEDERATED_TOKEN)")

	return cmd
}

// parseFlow converts a flow string to an auth.Flow constant.
func parseFlow(flowStr string) (auth.Flow, error) {
	if flowStr == "" {
		return auth.FlowDeviceCode, nil
	}
	switch strings.ToLower(flowStr) {
	case "device-code", "devicecode":
		return auth.FlowDeviceCode, nil
	case "service-principal", "serviceprincipal", "sp":
		return auth.FlowServicePrincipal, nil
	case "workload-identity", "workloadidentity", "wi":
		return auth.FlowWorkloadIdentity, nil
	default:
		return "", fmt.Errorf("unknown flow: %s (valid: device-code, service-principal, workload-identity)", flowStr)
	}
}
