// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
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

// PreLoginCheck checks whether a handler is already authenticated and determines
// the appropriate action. If force is true, the handler is logged out first and
// PreLoginProceed is returned. If the flow is in skipCheckFlows, no check is performed.
//
// Parameters:
//   - handler: the auth handler to check
//   - flow: the selected auth flow
//   - force: if true, force logout and proceed
//   - skipIfAuthenticated: if true and already authenticated, return PreLoginSkip
//   - skipCheckFlows: flows for which no auth-status check is needed (e.g., FlowPAT)
func PreLoginCheck(ctx context.Context, handler Handler, flow Flow, force, skipIfAuthenticated bool, skipCheckFlows ...Flow) (*PreLoginResult, error) {
	// Force re-auth: log out first.
	if force {
		_ = handler.Logout(ctx) // best-effort
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	// Skip check for certain flows
	for _, skipFlow := range skipCheckFlows {
		if flow == skipFlow {
			return &PreLoginResult{Action: PreLoginProceed}, nil
		}
	}

	// Check current auth status
	status, err := handler.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check auth status: %w", err)
	}

	if !status.Authenticated {
		return &PreLoginResult{Action: PreLoginProceed}, nil
	}

	identity := status.Claims.DisplayIdentity()

	if skipIfAuthenticated {
		return &PreLoginResult{
			Action:   PreLoginSkip,
			Identity: identity,
		}, nil
	}

	return &PreLoginResult{
		Action:   PreLoginAlreadyAuthenticated,
		Identity: identity,
	}, nil
}
