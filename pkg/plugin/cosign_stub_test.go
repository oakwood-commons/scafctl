// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !cosign

package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubVerifier_ReturnsErrCosignNotAvailable(t *testing.T) {
	t.Parallel()
	verifier := NewSignatureVerifier()
	policy := &SignaturePolicy{
		Mode:              SignatureModeEnforce,
		TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
		TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
	}

	result, err := verifier.VerifySignature(context.Background(), "ghcr.io/test/plugin@sha256:abc123", policy)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCosignNotAvailable))
}
