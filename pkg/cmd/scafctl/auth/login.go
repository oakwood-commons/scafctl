// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogin creates the 'auth login' command.
func CommandLogin(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		tenantID                  string
		clientID                  string
		hostname                  string
		timeout                   time.Duration
		flowStr                   string
		federatedToken            string
		scopes                    []string
		impersonateServiceAccount string
		callbackPort              int
		force                     bool
		skipIfAuthenticated       bool
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

			For the 'gcp' handler, this supports:
			- interactive: Browser-based OAuth (default for workstations)
			- service-principal: Service account key (GOOGLE_APPLICATION_CREDENTIALS)
			- workload-identity: Workload Identity Federation (GOOGLE_EXTERNAL_ACCOUNT)
			- metadata: GCE metadata server (auto-detected on GCE/GKE/Cloud Run)
			- gcloud-adc: Use existing gcloud Application Default Credentials file

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
			- gcp: Google Cloud Platform

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

			  # Login with GCP using browser OAuth (default)
			  scafctl auth login gcp

			  # Login with GCP service account key
			  scafctl auth login gcp --flow service-principal

			  # Login with GCP workload identity federation
			  scafctl auth login gcp --flow workload-identity

			  # Login with GCE metadata server
			  scafctl auth login gcp --flow metadata

			  # Login using existing gcloud ADC credentials
			  scafctl auth login gcp --flow gcloud-adc

			  # Login with GCP service account impersonation
			  scafctl auth login gcp --impersonate-service-account my-sa@project.iam.gserviceaccount.com

			  # Login with GCP and specific scopes
			  scafctl auth login gcp --scope https://www.googleapis.com/auth/bigquery
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

			// Get handler to check capabilities
			handler, err := getHandler(ctx, handlerName)
			if err != nil {
				err = fmt.Errorf("failed to initialize auth handler: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			caps := handler.Capabilities()

			// Validate capability-gated flags
			if tenantID != "" && !auth.HasCapability(caps, auth.CapTenantID) {
				err := fmt.Errorf("--tenant is not supported by the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if hostname != "" && !auth.HasCapability(caps, auth.CapHostname) {
				err := fmt.Errorf("--hostname is not supported by the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if federatedToken != "" && !auth.HasCapability(caps, auth.CapFederatedToken) {
				err := fmt.Errorf("--federated-token is not supported by the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if len(scopes) > 0 && !auth.HasCapability(caps, auth.CapScopesOnLogin) {
				err := fmt.Errorf("--scope is not supported at login time by the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if callbackPort != 0 && !auth.HasCapability(caps, auth.CapCallbackPort) {
				err := fmt.Errorf("--callback-port is not supported by the %q auth handler", handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Parse flow
			flow, err := parseFlow(flowStr, handlerName)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Validate impersonation flag
			if impersonateServiceAccount != "" && handlerName != "gcp" {
				err := fmt.Errorf("--impersonate-service-account is only supported by the 'gcp' auth handler")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Route to handler-specific login logic
			switch handlerName {
			case "github":
				return loginGitHub(ctx, w, flow, hostname, clientID, timeout, scopes, force, skipIfAuthenticated)
			case "gcp":
				return loginGCP(ctx, w, flow, clientID, impersonateServiceAccount, callbackPort, timeout, scopes, force, skipIfAuthenticated)
			default:
				return loginEntra(ctx, w, flow, tenantID, clientID, callbackPort, timeout, federatedToken, flowStr, scopes, force, skipIfAuthenticated)
			}
		},
	}

	cmd.Flags().StringVar(&tenantID, "tenant", "", "Azure tenant ID (overrides config, requires tenant_id capability)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth application/client ID (overrides default)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Hostname for enterprise/self-hosted instances (requires hostname capability)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authentication flow")
	cmd.Flags().StringVar(&flowStr, "flow", "", "Authentication flow (handler-specific)")
	cmd.Flags().StringVar(&federatedToken, "federated-token", "", "Federated token for workload identity (requires federated_token capability)")
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "OAuth scopes to request during login (requires scopes_on_login capability)")
	cmd.Flags().StringVar(&impersonateServiceAccount, "impersonate-service-account", "", "GCP service account email to impersonate (gcp handler only)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Re-authenticate even if already logged in (logs out first)")
	cmd.Flags().BoolVar(&skipIfAuthenticated, "skip-if-authenticated", false, "Exit successfully without re-authenticating if already logged in (idempotent for scripts)")
	cmd.Flags().IntVar(&callbackPort, "callback-port", 0, "Fixed port for the OAuth callback server (e.g. 8400); the redirect URI becomes http://localhost:<port>. Register this URI in your app registration. 0 = ephemeral (default).")

	return cmd
}

// loginGitHub handles the login flow for the GitHub auth handler.
func loginGitHub(ctx context.Context, w *writer.Writer, flow auth.Flow, hostname, clientID string, timeout time.Duration, scopes []string, force, skipIfAuthenticated bool) error {
	// Auto-detect PAT if env vars are set and no explicit flow.
	// Skip auto-detection when user provides --scope flags, since scopes
	// only apply to the device code flow (PAT scopes are fixed at creation).
	if flow == "" && ghauth.HasPATCredentials() && len(scopes) == 0 {
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

	// Force re-auth: log out first if already authenticated.
	if force {
		_ = handler.Logout(ctx) // best-effort; ignore error
	} else if flow != auth.FlowPAT {
		// Check if already authenticated (skip for PAT)
		status, err := handler.Status(ctx)
		if err != nil {
			err = fmt.Errorf("failed to check auth status: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}

		if status.Authenticated {
			identity := status.Claims.DisplayIdentity()
			if skipIfAuthenticated {
				w.Infof("Already authenticated as %s — skipping login.", identity)
				return nil
			}
			w.Warningf("Already authenticated as %s.", identity)
			w.Warning("Use 'scafctl auth logout github' to sign out first, or use --force to re-authenticate.")
			w.Info("")
		}
	}

	return executeLogin(ctx, w, handler, flow, "", 0, timeout, scopes)
}

// loginGCP handles the login flow for the GCP auth handler.
func loginGCP(ctx context.Context, w *writer.Writer, flow auth.Flow, clientID, impersonateServiceAccount string, callbackPort int, timeout time.Duration, scopes []string, force, skipIfAuthenticated bool) error {
	// Auto-detect flow based on available credentials (highest priority first)
	if flow == "" && gcpauth.HasWorkloadIdentityCredentials() {
		flow = auth.FlowWorkloadIdentity
		w.Info("Detected workload identity credentials in environment")
	} else if flow == "" && gcpauth.HasServiceAccountCredentials() {
		flow = auth.FlowServicePrincipal
		w.Info("Detected service account key in environment")
	}

	// Default to interactive (browser OAuth)
	if flow == "" {
		flow = auth.FlowInteractive
	}

	// Get or create handler with overrides
	handler, err := getGCPHandlerWithOverrides(ctx, clientID, impersonateServiceAccount)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Force re-auth: log out first if already authenticated.
	if force {
		_ = handler.Logout(ctx) // best-effort; ignore error
	} else if flow == auth.FlowInteractive || flow == auth.FlowGcloudADC {
		// Check if already authenticated (skip for non-interactive flows)
		status, err := handler.Status(ctx)
		if err != nil {
			err = fmt.Errorf("failed to check auth status: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}

		if status.Authenticated {
			identity := status.Claims.DisplayIdentity()
			if skipIfAuthenticated {
				w.Infof("Already authenticated as %s — skipping login.", identity)
				return nil
			}
			w.Warningf("Already authenticated as %s.", identity)
			w.Warning("Use 'scafctl auth logout gcp' to sign out first, or use --force to re-authenticate.")
			w.Info("")
		}
	}

	return executeLogin(ctx, w, handler, flow, "", callbackPort, timeout, scopes)
}

// loginEntra handles the login flow for the Entra auth handler.
func loginEntra(ctx context.Context, w *writer.Writer, flow auth.Flow, tenantID, clientID string, callbackPort int, timeout time.Duration, federatedToken, flowStr string, scopes []string, force, skipIfAuthenticated bool) error {
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

	// Default to interactive (browser OAuth with authorization code + PKCE)
	if flow == "" {
		flow = auth.FlowInteractive
	}

	// Get or create handler
	handler, err := getEntraHandlerWithOverrides(ctx, tenantID, clientID)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Force re-auth: log out first if already authenticated.
	if force {
		_ = handler.Logout(ctx) // best-effort; ignore error
	} else if flow != auth.FlowServicePrincipal {
		// Check if already authenticated (skip for service principal)
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
			if skipIfAuthenticated {
				w.Infof("Already authenticated as %s — skipping login.", identity)
				return nil
			}
			w.Warningf("Already authenticated as %s.", identity)
			w.Warning("Use 'scafctl auth logout entra' to sign out first, or use --force to re-authenticate.")
			w.Info("")
		}
	}

	return executeLogin(ctx, w, handler, flow, tenantID, callbackPort, timeout, scopes)
}

// executeLogin runs the common login logic for any auth handler.
// For device-code flows on a terminal, it uses the kvx status screen TUI.
// All other flows (and non-terminal output) use plain text output.
func executeLogin(ctx context.Context, w *writer.Writer, handler auth.Handler, flow auth.Flow, tenantID string, callbackPort int, timeout time.Duration, scopes []string) error {
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

	ioStreams := w.IOStreams()

	// Use kvx status TUI for device-code flows when running in a terminal.
	if flow == auth.FlowDeviceCode && skvx.IsTerminal(ioStreams.Out) {
		return executeLoginWithStatusTUI(ctx, w, handler, flow, tenantID, callbackPort, timeout, scopes, ioStreams)
	}

	// Plain-text login path (non-terminal, or non-device-code flows).
	loginOpts := auth.LoginOptions{
		TenantID:     tenantID,
		Scopes:       scopes,
		Flow:         flow,
		Timeout:      timeout,
		CallbackPort: callbackPort,
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
	return displayLoginResult(w, result, flow)
}

// executeLoginWithStatusTUI runs the device-code login flow using the kvx status
// screen TUI. It:
//  1. Starts handler.Login in a goroutine with a DeviceCodeCallback that captures
//     the verification URL and user code.
//  2. Waits for the device code before launching the TUI (avoids empty-data start).
//  3. Launches tui.Run with a DisplaySchema status view and a Done channel that
//     receives the login outcome when the goroutine completes.
func executeLoginWithStatusTUI(
	ctx context.Context,
	w *writer.Writer,
	handler auth.Handler,
	flow auth.Flow,
	tenantID string,
	callbackPort int,
	timeout time.Duration,
	scopes []string,
	ioStreams *terminal.IOStreams,
) error {
	type deviceCodeData struct {
		userCode        string
		verificationURI string
	}
	type loginOutcome struct {
		result *auth.Result
		err    error
	}

	deviceCodeChan := make(chan deviceCodeData, 1)
	outcomeChan := make(chan loginOutcome, 1)
	done := make(chan tui.StatusResult, 1)

	loginOpts := auth.LoginOptions{
		TenantID:     tenantID,
		Scopes:       scopes,
		Flow:         flow,
		Timeout:      timeout,
		CallbackPort: callbackPort,
		DeviceCodeCallback: func(userCode, verificationURI, _ string) {
			select {
			case deviceCodeChan <- deviceCodeData{userCode: userCode, verificationURI: verificationURI}:
			default:
			}
		},
	}

	go func() {
		result, err := handler.Login(ctx, loginOpts)
		outcomeChan <- loginOutcome{result: result, err: err}
	}()

	// Wait for the device code, an early completion, or cancellation.
	w.Infof("Initiating authentication with %s...", handler.DisplayName())
	var dci deviceCodeData
	select {
	case dci = <-deviceCodeChan:
		// Device code ready - proceed to TUI.
	case outcome := <-outcomeChan:
		// Login completed before device code was shown (unusual).
		if outcome.err != nil {
			if ctx.Err() != nil {
				return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
			}
			err := fmt.Errorf("authentication failed: %w", outcome.err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		w.Info("")
		return displayLoginResult(w, outcome.result, flow)
	case <-ctx.Done():
		return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	// Forward the login outcome to the TUI done channel.
	// capturedOutcome is safe to read after outcomeReady is closed because
	// close(outcomeReady) happens-before the receive on that channel.
	var capturedOutcome loginOutcome
	outcomeReady := make(chan struct{})
	go func() {
		outcome := <-outcomeChan
		capturedOutcome = outcome
		if outcome.err != nil {
			done <- tui.StatusResult{Err: outcome.err}
		} else {
			identity := outcome.result.Claims.DisplayIdentity()
			if identity == "" {
				identity = "unknown user"
			}
			done <- tui.StatusResult{Message: "Authenticated as " + identity}
		}
		close(outcomeReady)
	}()

	data := map[string]any{
		"title": fmt.Sprintf("Sign in to %s", handler.DisplayName()),
		"url":   dci.verificationURI,
		"code":  dci.userCode,
	}

	schema := &tui.DisplaySchema{
		Version: "v1",
		Status: &tui.StatusDisplayConfig{
			TitleField:     "title",
			WaitMessage:    "Waiting for authentication...",
			SuccessMessage: "Authenticated successfully!",
			DoneBehavior:   tui.DoneBehaviorExitAfterDelay,
			DoneDelay:      "2s",
			DisplayFields: []tui.StatusFieldDisplay{
				{Label: "URL", Field: "url"},
				{Label: "Code", Field: "code"},
			},
			Actions: []tui.StatusActionConfig{
				{
					Label: "Copy code",
					Type:  "copy-value",
					Field: "code",
					Keys:  tui.StatusKeyBindings{Vim: "c", Emacs: "alt+c", Function: "f2"},
				},
				{
					Label: "Open URL",
					Type:  "open-url",
					Field: "url",
					Keys:  tui.StatusKeyBindings{Vim: "o", Emacs: "alt+o", Function: "f3"},
				},
			},
		},
	}

	cfg := tui.DefaultConfig()
	cfg.AppName = "scafctl"
	cfg.DisplaySchema = schema
	cfg.Done = done

	teaOpts := tui.WithIO(ioStreams.In, ioStreams.Out)
	if runErr := tui.Run(data, cfg, teaOpts...); runErr != nil {
		if ctx.Err() != nil {
			return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		return fmt.Errorf("authentication display failed: %w", runErr)
	}

	// TUI exited normally (done channel received, DoneDelay elapsed).
	// The outcomeReady channel should already be closed at this point.
	select {
	case <-outcomeReady:
		// Outcome is captured - continue to post-display.
	default:
		// TUI exited before login completed (user quit early) - treat as cancelled.
		return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
	}

	if capturedOutcome.err != nil {
		if ctx.Err() != nil {
			return exitcode.WithCode(auth.ErrUserCancelled, exitcode.GeneralError)
		}
		err := fmt.Errorf("authentication failed: %w", capturedOutcome.err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	return displayLoginResult(w, capturedOutcome.result, flow)
}

// displayLoginResult prints the authentication success header and claim details.
func displayLoginResult(w *writer.Writer, result *auth.Result, flow auth.Flow) error {
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
	if flow == auth.FlowMetadata {
		w.Info("  Flow:     Metadata Server")
	}
	if flow == auth.FlowInteractive {
		w.Info("  Flow:     Interactive (Browser OAuth)")
	}
	return nil
}

// parseFlow converts a flow string to an auth.Flow constant.
// Delegates to auth.ParseFlow in the shared auth package.
func parseFlow(flowStr, handlerName string) (auth.Flow, error) {
	return auth.ParseFlow(flowStr, handlerName)
}
