// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ServerCredential provides the server's proof-of-identity for the token endpoint.
// This is the variability point for WIF vs. secret vs. cert.
type ServerCredential interface {
	Apply(params url.Values) error
}

// WIFCredential implements ServerCredential by reading a projected token file.
type WIFCredential struct {
	TokenFile           string // path to federated token (re-read per request)
	ClientAssertionType string // "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
}

// SecretCredential implements ServerCredential using a static client secret.
type SecretCredential struct {
	Secret string
}

// Apply sets the client_secret on the token request params.
func (c *SecretCredential) Apply(params url.Values) error {
	params.Set("client_secret", c.Secret)
	return nil
}

// Apply reads the projected federated token file and sets the WIF client assertion params.
// The file is re-read on every call because projected service account tokens rotate.
func (c *WIFCredential) Apply(params url.Values) error {
	raw, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return fmt.Errorf("reading federated token file %q: %w", c.TokenFile, err)
	}
	assertion := strings.TrimSpace(string(raw))
	if assertion == "" {
		return fmt.Errorf("federated token file %q is empty", c.TokenFile)
	}
	params.Set("client_assertion_type", c.ClientAssertionType)
	params.Set("client_assertion", assertion)
	return nil
}
