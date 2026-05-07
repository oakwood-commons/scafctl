// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build cosign

package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosignVerifier_NilPolicy(t *testing.T) {
	v := &cosignVerifier{}
	result, err := v.VerifySignature(context.Background(), "ghcr.io/org/plugin@sha256:abc", nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestCosignVerifier_DisabledPolicy(t *testing.T) {
	v := &cosignVerifier{}
	policy := &SignaturePolicy{Mode: SignatureModeOff}
	result, err := v.VerifySignature(context.Background(), "ghcr.io/org/plugin@sha256:abc", policy)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestCosignVerifier_InvalidImageRef(t *testing.T) {
	v := &cosignVerifier{}
	policy := &SignaturePolicy{
		Mode:              SignatureModeEnforce,
		TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	}
	_, err := v.VerifySignature(context.Background(), ":::invalid", policy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing image reference")
}

func TestCosignVerifier_EmptyIdentities(t *testing.T) {
	v := &cosignVerifier{}
	policy := &SignaturePolicy{
		Mode: SignatureModeEnforce,
		// No issuers or identities — the cross product is empty.
	}
	_, err := v.VerifySignature(context.Background(), "ghcr.io/org/plugin@sha256:abc123", policy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no trusted issuer/identity pairs")
}

func BenchmarkCosignVerifier_DisabledPolicy(b *testing.B) {
	v := &cosignVerifier{}
	policy := &SignaturePolicy{Mode: SignatureModeOff}
	ctx := context.Background()
	for b.Loop() {
		_, _ = v.VerifySignature(ctx, "ghcr.io/org/plugin@sha256:abc", policy)
	}
}
