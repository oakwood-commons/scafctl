// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"slices"
)

// FlowDetectionResult contains the auto-detected flow and a description.
type FlowDetectionResult struct {
	// Flow is the selected authentication flow.
	Flow Flow
	// Description is a human-readable message about what was detected,
	// or empty if the default flow was used.
	Description string
}

// CredentialDetector checks for available credentials in the environment.
type CredentialDetector struct {
	// HasCredentials returns true if this credential type is available.
	HasCredentials func() bool
	// Flow is the auth flow to use when these credentials are detected.
	Flow Flow
	// Description is a human-readable message shown when this credential is detected.
	Description string
}

// DetectFlow determines the best authentication flow given an explicit preference,
// a prioritized list of credential detectors, and a default fallback flow.
//
// Resolution order:
//  1. If explicitFlow is non-empty, use it.
//  2. Check detectors in order; use the first one with available credentials.
//  3. Fall back to defaultFlow.
func DetectFlow(explicitFlow Flow, detectors []CredentialDetector, defaultFlow Flow) FlowDetectionResult {
	if explicitFlow != "" {
		return FlowDetectionResult{Flow: explicitFlow}
	}

	for _, d := range detectors {
		if d.HasCredentials() {
			return FlowDetectionResult{
				Flow:        d.Flow,
				Description: d.Description,
			}
		}
	}

	return FlowDetectionResult{Flow: defaultFlow}
}

// PreLoginAction describes what the caller should do after a pre-login check.
type PreLoginAction int

const (
	// PreLoginProceed means the login should proceed normally.
	PreLoginProceed PreLoginAction = iota
	// PreLoginSkip means the user is already authenticated and login should be skipped.
	PreLoginSkip
	// PreLoginAlreadyAuthenticated means the user is already authenticated but login was not skipped.
	PreLoginAlreadyAuthenticated
)

// PreLoginResult contains the result of a pre-login check.
type PreLoginResult struct {
	// Action indicates what the caller should do.
	Action PreLoginAction
	// Identity is the display name of the authenticated identity (if any).
	Identity string
}

// isAmbientFlow reports whether f is an ambient or non-logout-managed
// credential flow (such as local ADC files, environment variables, or metadata
// endpoints) that 'auth logout' does not clear.
func isAmbientFlow(f Flow) bool {
	switch f {
	case FlowGcloudADC, FlowMetadata, FlowPAT, FlowServicePrincipal, FlowWorkloadIdentity, FlowClientCredentials:
		return true
	case FlowInteractive, FlowDeviceCode, FlowGitHubApp, FlowOnBehalfOf:
		return false
	default:
		return false
	}
}

// PreLoginCheck checks whether a handler is already authenticated and determines
// the appropriate action. If force is true, the handler is logged out first and
// PreLoginProceed is returned. When skipIfAuthenticated is false and flow is in
// skipCheckFlows, no status check is performed and PreLoginProceed is returned.
// When skipIfAuthenticated is true, Status is checked (except on explicit
// host-aware overrides) so already-authenticated handlers return PreLoginSkip.
//
// Parameters:
//   - handler: the auth handler to check
//   - flow: the selected auth flow
//   - force: if true, force logout and proceed
//   - skipIfAuthenticated: if true and already authenticated, return PreLoginSkip
//   - hostname: the explicitly requested host (empty if none). For host-aware
//     handlers, a non-empty hostname bypasses the already-authenticated check so
//     the user can log in to a different host without --force.
//   - skipCheckFlows: non-interactive/ambient flows for which no status check is
//     performed unless skipIfAuthenticated is true (e.g., FlowPAT, FlowGcloudADC)
func PreLoginCheck(ctx context.Context, handler Handler, flow Flow, force, skipIfAuthenticated bool, hostname string, skipCheckFlows ...Flow) (*PreLoginResult, error) {
	// Force re-auth: log out first.
	if force {
		_ = handler.Logout(ctx) // best-effort
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	// Skip status check for non-interactive/ambient flows unless idempotent
	// skip-if-authenticated behavior was requested.
	if !skipIfAuthenticated && slices.Contains(skipCheckFlows, flow) {
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	// When an explicit hostname is requested for a host-aware handler, the
	// generic Status() check cannot tell which host the cached credentials
	// belong to. Proceed with login so per-host handlers (e.g. OpenShift) can
	// authenticate to a different host without requiring --force.
	if hostname != "" && HasCapability(handler.Capabilities(), CapHostname) {
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	// Check current auth status
	status, err := handler.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check auth status: %w", err)
	}

	if status == nil || !status.Authenticated {
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	var identity string
	if status.Claims != nil {
		identity = status.Claims.DisplayIdentity()
	}

	if skipIfAuthenticated {
		return &PreLoginResult{
			Action:   PreLoginSkip,
			Identity: identity,
		}, nil
	}

	activeFlow := status.Flow
	if activeFlow == "" {
		if reporter, ok := handler.(FlowReporter); ok {
			activeFlow = reporter.ActiveFlow(ctx)
		}
	}

	// When authentication is backed only by an ambient or non-logout-managed
	// flow (such as gcloud ADC, metadata, PAT, service principal, workload
	// identity, or client credentials), proceed so an explicit login can
	// establish a managed session instead of instructing the user to run a
	// logout command that does not clear external credentials.
	if activeFlow != "" && (isAmbientFlow(activeFlow) || slices.Contains(skipCheckFlows, activeFlow)) {
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	return &PreLoginResult{
		Action:   PreLoginAlreadyAuthenticated,
		Identity: identity,
	}, nil
}
