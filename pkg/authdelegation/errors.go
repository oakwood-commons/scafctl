// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import "fmt"

var EntraNoTenantID = fmt.Errorf("tenant ID is required")
var EntraNoClientID = fmt.Errorf("client ID is required")
var EntraNoCredential = fmt.Errorf("at least one credential is required (federatedTokenFile or clientSecret)")
var EntraInvalidCredentialType = fmt.Errorf("credentialType must be %q or %q", CredentialTypeWIF, CredentialTypeSecret)
var EntraWIFMissingTokenFile = fmt.Errorf("federatedTokenFile is required when credentialType is %q", CredentialTypeWIF)
var EntraSecretMissing = fmt.Errorf("clientSecret is required when credentialType is %q", CredentialTypeSecret)
