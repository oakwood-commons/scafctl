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
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	skvx "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogin creates the 'auth login' command.
func CommandLogin(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
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
		registry                  string
		registryScope             string
		writeRegistryAuth         bool
	)

	cmd := &cobra.Command{
		Use:   "login <handler>",
		Short: "Authenticate with an auth handler",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Authenticate with an authentication handler.

			For the 'entra' handler, this supports multiple authentication flows:
			- device-code: Interactive authentication via browser (default)
			- service-principal: Non-interactive authentication for CI/CD
			- workload-identity: Kubernetes workload identity (AKS)

			For the 'github' handler, this supports:
			- interactive: Opens browser for authentication (default).
			            Without 'clientSecret' configured: uses device code flow with automatic
			            browser open (identical to 'gh auth login'). With 'clientSecret'
			            configured in scafctl config: uses OAuth Authorization Code + PKCE.
			- device-code: Device code flow without browser auto-open; prints a code and URL
			               for manual entry (headless/SSH/CI use).
			- pat: Personal access token from environment variables (GITHUB_TOKEN or GH_TOKEN)
			- github-app: GitHub App installation token (for CI/automation)

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
			The default GitHub interactive flow uses the built-in OAuth App client ID with
			device code + automatic browser open (no client secret required).
			To use browser redirect (Authorization Code + PKCE), configure 'auth.github.clientSecret'
			in the scafctl config file and use --client-id with your OAuth App's client ID.
			Use --client-id to specify a custom application registration.

			Supported handlers:
			- entra: Microsoft Entra ID
			- github: GitHub
			- gcp: Google Cloud Platform

			Examples:
			  # Login with Entra ID using device code flow (default)
			  scafctl auth login entra

			  # Login with GitHub (default: device code + browser auto-open, like 'gh auth login')
			  scafctl auth login github

			  # Login with GitHub using browser redirect (requires clientSecret in config)
			  scafctl auth login github --client-id <your-oauth-app-client-id>

			  # Login with GitHub headless device code (SSH/CI: prints code+URL, no browser)
			  scafctl auth login github --flow device-code

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
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         flags.RequireArg("handler", cliParams.BinaryName+" auth login gcp"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}
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
			var loginErr error
			switch handlerName {
			case "github":
				loginErr = loginGitHub(ctx, w, cliParams.BinaryName, flow, hostname, clientID, callbackPort, timeout, scopes, force, skipIfAuthenticated)
			case "gcp":
				loginErr = loginGCP(ctx, w, cliParams.BinaryName, flow, clientID, impersonateServiceAccount, callbackPort, timeout, scopes, force, skipIfAuthenticated)
			case "entra":
				loginErr = loginEntra(ctx, w, cliParams.BinaryName, flow, tenantID, clientID, callbackPort, timeout, federatedToken, flowStr, scopes, force, skipIfAuthenticated)
			default:
				// Generic custom OAuth2 handler (e.g. quay, custom IdP).
				// Use the handler already resolved from the registry; no built-in
				// flow-detection or provider-specific overrides apply.
				loginErr = loginGeneric(ctx, w, cliParams.BinaryName, handler, handlerName, flow, callbackPort, timeout, scopes, force, skipIfAuthenticated)
			}

			if loginErr != nil {
				return loginErr
			}

			// Post-login registry bridge.
			// Re-create the handler with the same overrides used during login so that
			// token retrieval uses the correct clientID fingerprint and refresh token.
			if registry != "" {
				var bridgeHandler auth.Handler
				var bridgeErr error
				switch handlerName {
				case "github":
					bridgeHandler, bridgeErr = getGitHubHandlerWithOverrides(ctx, hostname, clientID)
				case "gcp":
					bridgeHandler, bridgeErr = getGCPHandlerWithOverrides(ctx, clientID, impersonateServiceAccount)
				case "entra":
					bridgeHandler, bridgeErr = getEntraHandlerWithOverrides(ctx, tenantID, clientID)
				default:
					// Custom OAuth2 handlers have no CLI overrides; re-use the
					// handler resolved at the start of this command.
					bridgeHandler = handler
				}
				if bridgeErr != nil {
					// Fall back to the pre-login handler rather than failing outright.
					bridgeHandler = handler
				}
				return bridgeAuthToRegistryPostLogin(ctx, w, bridgeHandler, handlerName, registry, registryScope, writeRegistryAuth)
			}

			return nil
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
	cmd.Flags().StringVar(&registry, "registry", "", "OCI registry to bridge auth credentials to after login (e.g. ghcr.io)")
	cmd.Flags().StringVar(&registryScope, "registry-scope", "", "OAuth scope for registry credential bridging")
	cmd.Flags().BoolVar(&writeRegistryAuth, "write-registry-auth", false, "Also write bridged credentials to container auth file (Docker/Podman interop)")

	return cmd
}

// loginGitHub handles the login flow for the GitHub auth handler.
func loginGitHub(ctx context.Context, w *writer.Writer, binaryName string, flow auth.Flow, hostname, clientID string, callbackPort int, timeout time.Duration, scopes []string, force, skipIfAuthenticated bool) error {
	// Auto-detect flow from available credentials.
	// Skip PAT auto-detection when user provides --scope flags, since scopes
	// only apply to the device code / interactive flows (PAT scopes are fixed at creation).
	var detectors []auth.CredentialDetector
	if len(scopes) == 0 {
		detectors = append(detectors, auth.CredentialDetector{
			HasCredentials: ghauth.HasPATCredentials,
			Flow:           auth.FlowPAT,
			Description:    "Detected GitHub token in environment variables",
		})
	}
	detection := auth.DetectFlow(flow, detectors, auth.FlowInteractive)
	flow = detection.Flow
	if detection.Description != "" {
		w.Info(detection.Description)
	}

	// Get or create handler
	handler, err := getGitHubHandlerWithOverrides(ctx, hostname, clientID)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Check pre-login state
	preLogin, err := auth.PreLoginCheck(ctx, handler, flow, force, skipIfAuthenticated, auth.FlowPAT)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	switch preLogin.Action {
	case auth.PreLoginProceed:
		// Continue with login
	case auth.PreLoginSkip:
		w.Infof("Already authenticated as %s — skipping login.", preLogin.Identity)
		return nil
	case auth.PreLoginAlreadyAuthenticated:
		w.Warningf("Already authenticated as %s.", preLogin.Identity)
		w.Warningf("Use '%s auth logout github' to sign out first, or use --force to re-authenticate.", binaryName)
		w.Info("")
	}

	return executeLogin(ctx, w, binaryName, handler, flow, "", callbackPort, timeout, scopes)
}

// loginGCP handles the login flow for the GCP auth handler.
func loginGCP(ctx context.Context, w *writer.Writer, binaryName string, flow auth.Flow, clientID, impersonateServiceAccount string, callbackPort int, timeout time.Duration, scopes []string, force, skipIfAuthenticated bool) error {
	// Auto-detect flow based on available credentials (highest priority first)
	detection := auth.DetectFlow(flow, []auth.CredentialDetector{
		{
			HasCredentials: gcpauth.HasWorkloadIdentityCredentials,
			Flow:           auth.FlowWorkloadIdentity,
			Description:    "Detected workload identity credentials in environment",
		},
		{
			HasCredentials: gcpauth.HasServiceAccountCredentials,
			Flow:           auth.FlowServicePrincipal,
			Description:    "Detected service account key in environment",
		},
	}, auth.FlowInteractive)
	flow = detection.Flow
	if detection.Description != "" {
		w.Info(detection.Description)
	}

	// Get or create handler with overrides
	handler, err := getGCPHandlerWithOverrides(ctx, clientID, impersonateServiceAccount)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Check pre-login state (skip check for non-interactive flows except interactive and gcloud_adc)
	preLogin, err := auth.PreLoginCheck(ctx, handler, flow, force, skipIfAuthenticated,
		auth.FlowWorkloadIdentity, auth.FlowServicePrincipal, auth.FlowPAT, auth.FlowMetadata)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	switch preLogin.Action {
	case auth.PreLoginProceed:
		// Continue with login
	case auth.PreLoginSkip:
		w.Infof("Already authenticated as %s — skipping login.", preLogin.Identity)
		return nil
	case auth.PreLoginAlreadyAuthenticated:
		w.Warningf("Already authenticated as %s.", preLogin.Identity)
		w.Warningf("Use '%s auth logout gcp' to sign out first, or use --force to re-authenticate.", binaryName)
		w.Info("")
	}

	return executeLogin(ctx, w, binaryName, handler, flow, "", callbackPort, timeout, scopes)
}

// loginEntra handles the login flow for the Entra auth handler.
func loginEntra(ctx context.Context, w *writer.Writer, binaryName string, flow auth.Flow, tenantID, clientID string, callbackPort int, timeout time.Duration, federatedToken, flowStr string, scopes []string, force, skipIfAuthenticated bool) error {
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

	// Auto-detect flow from available credentials
	detection := auth.DetectFlow(flow, []auth.CredentialDetector{
		{
			HasCredentials: entra.HasWorkloadIdentityCredentials,
			Flow:           auth.FlowWorkloadIdentity,
			Description:    "Detected workload identity credentials in environment",
		},
		{
			HasCredentials: entra.HasServicePrincipalCredentials,
			Flow:           auth.FlowServicePrincipal,
			Description:    "Detected service principal credentials in environment variables",
		},
	}, auth.FlowInteractive)
	flow = detection.Flow
	if detection.Description != "" {
		w.Info(detection.Description)
	}

	// Get or create handler
	handler, err := getEntraHandlerWithOverrides(ctx, tenantID, clientID)
	if err != nil {
		err = fmt.Errorf("failed to initialize auth handler: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Check pre-login state (skip check for service principal)
	preLogin, err := auth.PreLoginCheck(ctx, handler, flow, force, skipIfAuthenticated, auth.FlowServicePrincipal)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	switch preLogin.Action {
	case auth.PreLoginProceed:
		// Continue with login
	case auth.PreLoginSkip:
		w.Infof("Already authenticated as %s — skipping login.", preLogin.Identity)
		return nil
	case auth.PreLoginAlreadyAuthenticated:
		w.Warningf("Already authenticated as %s.", preLogin.Identity)
		w.Warningf("Use '%s auth logout entra' to sign out first, or use --force to re-authenticate.", binaryName)
		w.Info("")
	}

	return executeLogin(ctx, w, binaryName, handler, flow, tenantID, callbackPort, timeout, scopes)
}

// loginGeneric handles the login flow for custom (non-built-in) OAuth2 handlers.
// It uses the handler already resolved from the auth registry so provider-specific
// overrides (tenantID, hostname, etc.) do not apply.
func loginGeneric(ctx context.Context, w *writer.Writer, binaryName string, handler auth.Handler, handlerName string, flow auth.Flow, callbackPort int, timeout time.Duration, scopes []string, force, skipIfAuthenticated bool) error {
	// Pre-login check; skip for client_credentials since it is non-interactive.
	preLogin, err := auth.PreLoginCheck(ctx, handler, flow, force, skipIfAuthenticated, auth.FlowClientCredentials)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}
	switch preLogin.Action {
	case auth.PreLoginProceed:
		// Continue with login
	case auth.PreLoginSkip:
		w.Infof("Already authenticated as %s — skipping login.", preLogin.Identity)
		return nil
	case auth.PreLoginAlreadyAuthenticated:
		w.Warningf("Already authenticated as %s.", preLogin.Identity)
		w.Warningf("Use '%s auth logout %s' to sign out first, or use --force to re-authenticate.", binaryName, handlerName)
		w.Info("")
	}

	return executeLogin(ctx, w, binaryName, handler, flow, "", callbackPort, timeout, scopes)
}

// executeLogin runs the common login logic for any auth handler.
// For device-code flows on a terminal, it uses the kvx status screen TUI.
// All other flows (and non-terminal output) use plain text output.
func executeLogin(ctx context.Context, w *writer.Writer, binaryName string, handler auth.Handler, flow auth.Flow, tenantID string, callbackPort int, timeout time.Duration, scopes []string) error {
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
		return executeLoginWithStatusTUI(ctx, w, binaryName, handler, flow, tenantID, callbackPort, timeout, scopes, ioStreams)
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
	binaryName string,
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
	cfg.AppName = binaryName
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

// bridgeAuthToRegistryPostLogin bridges the authenticated handler's token
// to OCI registry credentials and stores them in the native credential store.
func bridgeAuthToRegistryPostLogin(ctx context.Context, w *writer.Writer, handler auth.Handler, handlerName, registry, scope string, writeRegistryAuth bool) error {
	username, password, err := catalog.BridgeAuthToRegistry(ctx, handler, registry, scope)
	if err != nil {
		err = fmt.Errorf("failed to bridge %s auth to registry %s: %w", handlerName, registry, err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Explicitly initialise a secrets store so password encryption honours the
	// same backend as the CLI's shared store. Errors are non-fatal.
	var nativeStore *catalog.NativeCredentialStore
	if ss, ssErr := secrets.New(); ssErr == nil {
		nativeStore = catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	} else {
		nativeStore = catalog.NewNativeCredentialStore()
	}

	containerAuthFile := ""
	if writeRegistryAuth {
		if writtenPath, containerErr := nativeStore.WriteContainerAuth(registry, username, password); containerErr != nil {
			w.Warningf("Failed to write container auth file: %v", containerErr)
			w.Warning("Docker/Podman interop may not work.")
		} else {
			containerAuthFile = writtenPath
		}
	}

	if err := nativeStore.SetCredential(registry, username, password, containerAuthFile); err != nil {
		err = fmt.Errorf("failed to store registry credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Infof("Registry credentials stored for %s (via %s handler)", registry, handlerName)
	return nil
}
