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
	_, err := v.VerifySignature(context.Background(), "ghcr.io/org/plugin@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", policy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no trusted issuer/identity pairs")
}

func TestCosignVerifier_UnexpandedOrInvalidIdentityFailsClosed(t *testing.T) {
	t.Parallel()
	const ref = "ghcr.io/org/providers/github@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	tests := []struct {
		name       string
		identities []string
		wantText   string
	}{
		{
			name:       "unexpanded {name} placeholder",
			identities: []string{"https://github.com/my-org/scafctl-plugin-{name}/.github/workflows/*"},
			wantText:   "unexpanded placeholder",
		},
		{
			name:       "unexpanded {kind} placeholder",
			identities: []string{"https://github.com/my-org/{kind}/.github/workflows/*"},
			wantText:   "unexpanded placeholder",
		},
		{
			name:       "unknown placeholder {repo}",
			identities: []string{"https://github.com/my-org/{repo}/.github/workflows/*"},
			wantText:   "unknown placeholder {repo}",
		},
		{
			name:       "unclosed brace",
			identities: []string{"https://github.com/my-org/{name/*"},
			wantText:   "unclosed '{'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := &cosignVerifier{}
			policy := &SignaturePolicy{
				Mode:              SignatureModeEnforce,
				TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
				TrustedIdentities: tt.identities,
			}
			res, err := v.VerifySignature(context.Background(), ref, policy)
			assert.Nil(t, res)
			require.ErrorIs(t, err, ErrInvalidIdentityPattern)
			assert.Contains(t, err.Error(), tt.wantText)
		})
	}
}

func BenchmarkCosignVerifier_DisabledPolicy(b *testing.B) {
	v := &cosignVerifier{}
	policy := &SignaturePolicy{Mode: SignatureModeOff}
	ctx := context.Background()
	for b.Loop() {
		_, _ = v.VerifySignature(ctx, "ghcr.io/org/plugin@sha256:abc", policy)
	}
}
