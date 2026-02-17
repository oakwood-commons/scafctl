// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
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
		clientID       string
		hostname       string
		timeout        time.Duration
		flowStr        string
		federatedToken string
		scopes         []string
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

			For the 'github' handler, this supports:
			- device-code: Interactive authentication via browser (default)
			- pat: Personal access token from environment variables (GITHUB_TOKEN or GH_TOKEN)

			For Entra service principal flow, set these environment variables:
			- AZURE_CLIENT_ID: Application (client) ID
			- AZURE_TENANT_ID: Directory (tenant) ID
			- AZURE_CLIENT_SECRET: Client secret value

			For Entra workload identity flow, set these environment variables:
			- AZURE_CLIENT_ID: Application (client) ID
			- AZURE_TENANT_ID: Directory (tenant) ID
			- AZURE_FEDERATED_TOKEN_FILE: Path to projected token file
			  OR
			- AZURE_FEDERATED_TOKEN: Raw federated token (for testing)
			  OR
			- --federated-token flag: Pass token directly (for testing)

			By default, the Entra device code flow uses the Azure CLI's public client ID.
			The GitHub device code flow uses the scafctl OAuth App client ID.
			Use --client-id to specify a custom application registration.

			Supported handlers:
			- entra: Microsoft Entra ID
			- github: GitHub

			Examples:
			  # Login with Entra ID using device code flow (default)
			  scafctl auth login entra

			  # Login with GitHub using device code flow (default)
			  scafctl auth login github

			  # Login with GitHub Enterprise Server
			  scafctl auth login github --hostname github.example.com

			  # Login with GitHub PAT (requires GITHUB_TOKEN or GH_TOKEN env var)
			  scafctl auth login github --flow pat

			  # Login with a specific Entra tenant
			  scafctl auth login entra --tenant 08e70e8e-d05c-4449-a2c2-67bd0a9c4e79

			  # Login with a custom client ID
			  scafctl auth login entra --client-id 12345678-abcd-1234-abcd-123456789abc

			  # Login with Entra service principal (requires env vars)
			  scafctl auth login entra --flow service-principal

			  # Login with Entra workload identity (Kubernetes)
			  scafctl auth login entra --flow workload-identity

			  # Login with a custom timeout (device code only)
			  scafctl auth login entra --timeout 10m

			  # Login with specific scopes
			  scafctl auth login entra --scope https://graph.microsoft.com/User.Read
			  scafctl auth login github --scope repo --scope read:org
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
			flow, err := parseFlow(flowStr, handlerName)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Route to handler-specific login logic
			switch handlerName {
			case "github":
				return loginGitHub(ctx, w, flow, hostname, clientID, timeout, scopes)
			default:
				return loginEntra(ctx, w, flow, tenantID, clientID, timeout, federatedToken, flowStr, scopes)
			}
		},
	}

	cmd.Flags().StringVar(&tenantID, "tenant", "", "Azure tenant ID (overrides config, Entra only)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth application/client ID (overrides default)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "GitHub hostname for GHES (GitHub only, e.g. github.example.com)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authentication flow")
	cmd.Flags().StringVar(&flowStr, "flow", "", "Authentication flow (handler-specific)")
	cmd.Flags().StringVar(&federatedToken, "federated-token", "", "Federated token for Entra workload identity (sets AZURE_FEDERATED_TOKEN)")
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "OAuth scopes to request during login")

	return cmd
}

// loginGitHub handles the login flow for the GitHub auth handler.
func loginGitHub(ctx context.Context, w *writer.Writer, flow auth.Flow, hostname, clientID string, timeout time.Duration, scopes []string) error {
	// Auto-detect PAT if env vars are set and no explicit flow
	if flow == "" && ghauth.HasPATCredentials() {
		flow = auth.FlowPAT
		w.Info("Detected GitHub token in environment variables")
	}

	// Default to device code
	if flow == "" {
		flow = auth.FlowDeviceCode
	}

	// Get or create handler
	handler, err := getGitHubHandlerWithOverrides(ctx, hostname, clientID)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Check if already authenticated (skip for PAT)
	if flow != auth.FlowPAT {
		status, err := handler.Status(ctx)
		if err != nil {
			err = fmt.Errorf("failed to check auth status: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}

		if status.Authenticated {
			identity := status.Claims.DisplayIdentity()
			w.Infof("Already authenticated as %s", identity)
			w.Info("Use 'scafctl auth logout github' to sign out first, or continue to re-authenticate.")
			w.Info("")
		}
	}

	return executeLogin(ctx, w, handler, flow, "", timeout, scopes)
}

// loginEntra handles the login flow for the Entra auth handler.
func loginEntra(ctx context.Context, w *writer.Writer, flow auth.Flow, tenantID, clientID string, timeout time.Duration, federatedToken, flowStr string, scopes []string) error {
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

	// Default to device code if no flow detected
	if flow == "" {
		flow = auth.FlowDeviceCode
	}

	// Get or create handler
	handler, err := getEntraHandlerWithOverrides(ctx, tenantID, clientID)
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

	return executeLogin(ctx, w, handler, flow, tenantID, timeout, scopes)
}

// executeLogin runs the common login logic for any auth handler.
func executeLogin(ctx context.Context, w *writer.Writer, handler auth.Handler, flow auth.Flow, tenantID string, timeout time.Duration, scopes []string) error {
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
		Scopes:   scopes,
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
		w.Infof("  Name:     %s", result.Claims.Name)
	}
	if result.Claims.Username != "" && result.Claims.Username != result.Claims.Name {
		w.Infof("  Username: %s", result.Claims.Username)
	}
	if result.Claims.Email != "" {
		w.Infof("  Email:    %s", result.Claims.Email)
	}
	if result.Claims.TenantID != "" {
		w.Infof("  Tenant:   %s", result.Claims.TenantID)
	}
	if flow == auth.FlowServicePrincipal {
		w.Info("  Flow:     Service Principal")
	}
	if flow == auth.FlowWorkloadIdentity {
		w.Info("  Flow:     Workload Identity")
	}
	if flow == auth.FlowPAT {
		w.Info("  Flow:     Personal Access Token")
	}

	return nil
}

// parseFlow converts a flow string to an auth.Flow constant.
func parseFlow(flowStr, handlerName string) (auth.Flow, error) {
	if flowStr == "" {
		return "", nil // Will be auto-detected per handler
	}
	switch strings.ToLower(flowStr) {
	case "device-code", "devicecode":
		return auth.FlowDeviceCode, nil
	case "service-principal", "serviceprincipal", "sp":
		return auth.FlowServicePrincipal, nil
	case "workload-identity", "workloadidentity", "wi":
		return auth.FlowWorkloadIdentity, nil
	case "pat":
		return auth.FlowPAT, nil
	default:
		if handlerName == "github" {
			return "", fmt.Errorf("unknown flow: %s (valid for github: device-code, pat)", flowStr)
		}
		return "", fmt.Errorf("unknown flow: %s (valid for entra: device-code, service-principal, workload-identity)", flowStr)
	}
}
