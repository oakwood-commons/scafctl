// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import "fmt"

var (
	ErrEntraNoTenantID            = fmt.Errorf("tenant ID is required")
	ErrEntraNoClientID            = fmt.Errorf("client ID is required")
	ErrEntraNoCredential          = fmt.Errorf("at least one credential is required (federatedTokenFile or clientSecret)")
	ErrEntraInvalidCredentialType = fmt.Errorf("credentialType must be %q or %q", CredentialTypeWIF, CredentialTypeSecret)
	ErrEntraWIFMissingTokenFile   = fmt.Errorf("federatedTokenFile is required when credentialType is %q", CredentialTypeWIF)
	ErrEntraSecretMissing         = fmt.Errorf("clientSecret is required when credentialType is %q", CredentialTypeSecret)
	ErrNoCallerToken              = fmt.Errorf("no caller token in context")
	ErrNoScope                    = fmt.Errorf("scope is required for token delegation")
)
