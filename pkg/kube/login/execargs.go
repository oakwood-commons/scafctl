// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// buildExecArgs returns the static arguments baked into the kubeconfig exec
// block. The runtime command becomes:
//
//	<binary> auth token <handler> --exec-credential [--scope <audience>] [--profile <profile>]
//
// The cluster audience is passed as a scope only for handlers that accept
// per-request scopes; other handlers fix their scopes at login time.
func buildExecArgs(h Authenticator, info kube.ClusterInfo, profile string) []string {
	args := []string{execAuthCmd, execTokenCmd, h.Name(), execCredentialFlag}
	if info.OIDCAudience != "" && auth.HasCapability(h.Capabilities(), auth.CapScopesOnTokenRequest) {
		args = append(args, execScopeFlag, info.OIDCAudience)
	}
	if profile != "" {
		args = append(args, execProfileFlag, profile)
	}
	return args
}

// interactiveModeFor chooses the kubeconfig exec interactiveMode. OAuth
// implicit-grant handlers return no refresh token, so the user must re-login on
// expiry: allow an interactive prompt when a terminal is attached. Refresh-
// capable handlers (OIDC, and the auto default) refresh silently, so never
// prompt from inside a kubectl subprocess.
func interactiveModeFor(authType kube.AuthType) string {
	if authType == kube.AuthTypeOAuth {
		return kubeconfig.InteractiveModeIfAvailable
	}
	return kubeconfig.InteractiveModeNever
}

// buildInstallHint returns the message kubectl shows when the host binary is
// missing from PATH. It is embedder-aware via the binary name.
func buildInstallHint(binaryName string) string {
	return fmt.Sprintf("%s not found in PATH; install it and run %q to refresh credentials", binaryName, binaryName+" kube login")
}
