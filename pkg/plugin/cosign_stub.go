// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !cosign

package plugin

import "context"

// stubVerifier is the no-op signature verifier compiled into binaries without
// the "cosign" build tag. It returns (nil, nil) when the policy is nil or
// disabled, and ErrCosignNotAvailable when verification is actually requested.
type stubVerifier struct{}

// NewSignatureVerifier returns a stub verifier when the cosign build tag is
// not present. The stub returns ErrCosignNotAvailable only when verification
// is requested (policy is non-nil and enabled).
func NewSignatureVerifier() SignatureVerifier {
	return &stubVerifier{}
}

func (s *stubVerifier) VerifySignature(_ context.Context, _ string, policy *SignaturePolicy) (*SignatureResult, error) {
	if policy == nil || !policy.IsEnabled() {
		return nil, nil
	}
	return nil, ErrCosignNotAvailable
}
