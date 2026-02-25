// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	require.NoError(t, err)

	// 32 bytes = 43 base64url chars
	assert.Len(t, verifier, 43)

	// Must be valid base64url
	_, err = base64.RawURLEncoding.DecodeString(verifier)
	assert.NoError(t, err)
}

func TestGenerateCodeVerifier_Unique(t *testing.T) {
	v1, err := GenerateCodeVerifier()
	require.NoError(t, err)
	v2, err := GenerateCodeVerifier()
	require.NoError(t, err)
	assert.NotEqual(t, v1, v2, "successive verifiers must be unique")
}

func TestGenerateCodeChallenge(t *testing.T) {
	// RFC 7636 Appendix B test vector
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := GenerateCodeChallenge(verifier)

	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	assert.Equal(t, expected, challenge)
}

func TestGenerateCodeChallenge_DeterministicForSameInput(t *testing.T) {
	verifier := "test-verifier-12345"
	c1 := GenerateCodeChallenge(verifier)
	c2 := GenerateCodeChallenge(verifier)
	assert.Equal(t, c1, c2)
}
