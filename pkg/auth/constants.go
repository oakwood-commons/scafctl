// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

// Environment variable constants used by auth handlers.
// These are defined here so that CLI commands can reference them without
// importing handler-specific packages (e.g., pkg/auth/entra).
const (
	// EnvAzureFederatedToken is the raw federated token (for testing/debugging).
	// Takes precedence over EnvAzureFederatedTokenFile if both are set.
	EnvAzureFederatedToken = "AZURE_FEDERATED_TOKEN" //nolint:gosec // This is the env var name, not a credential

	// EnvAzureFederatedTokenFile is the path to the projected service account token.
	EnvAzureFederatedTokenFile = "AZURE_FEDERATED_TOKEN_FILE" //nolint:gosec // This is the env var name, not a credential
)
