// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build cosign

package plugin

import (
	"context"
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/fulcio"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/rekor"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	"github.com/sigstore/rekor/pkg/generated/client"
)

// cosignVerifier implements SignatureVerifier using the Sigstore cosign library
// for keyless OIDC-based signature verification against Fulcio and Rekor.
type cosignVerifier struct {
	initOnce          sync.Once
	rekorClient       *client.Rekor
	rootCerts         *x509.CertPool
	intermediateCerts *x509.CertPool
	initErr           error
}

// NewSignatureVerifier returns a cosign-backed signature verifier.
func NewSignatureVerifier() SignatureVerifier {
	return &cosignVerifier{}
}

// initClients lazily initialises the Rekor client and Fulcio certificate
// pools. Repeated calls are a no-op thanks to sync.Once.
func (v *cosignVerifier) initClients() error {
	v.initOnce.Do(func() {
		v.rekorClient, v.initErr = rekor.NewClient(options.DefaultRekorURL)
		if v.initErr != nil {
			v.initErr = fmt.Errorf("creating rekor client: %w", v.initErr)
			return
		}
		v.rootCerts, v.initErr = fulcio.GetRoots()
		if v.initErr != nil {
			v.initErr = fmt.Errorf("getting fulcio root certificates: %w", v.initErr)
			return
		}
		v.intermediateCerts, v.initErr = fulcio.GetIntermediates()
		if v.initErr != nil {
			v.initErr = fmt.Errorf("getting fulcio intermediate certificates: %w", v.initErr)
		}
	})
	return v.initErr
}

func (v *cosignVerifier) VerifySignature(ctx context.Context, imageRef string, policy *SignaturePolicy) (*SignatureResult, error) {
	if policy == nil || !policy.IsEnabled() {
		return nil, nil
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parsing image reference %q: %w", imageRef, err)
	}

	// Build certificate identity matchers from policy.
	identities := make([]cosign.Identity, 0, len(policy.TrustedIssuers)*len(policy.TrustedIdentities))
	for _, issuer := range policy.TrustedIssuers {
		for _, identity := range policy.TrustedIdentities {
			identities = append(identities, cosign.Identity{
				IssuerRegExp:  globToRegexp(issuer),
				SubjectRegExp: globToRegexp(identity),
			})
		}
	}

	if len(identities) == 0 {
		return nil, fmt.Errorf("signature policy has no trusted issuer/identity pairs configured")
	}

	if err := v.initClients(); err != nil {
		return nil, err
	}

	checkOpts := &cosign.CheckOpts{
		RekorClient:       v.rekorClient,
		RootCerts:         v.rootCerts,
		IntermediateCerts: v.intermediateCerts,
		Identities:        identities,
		IgnoreSCT:         false,
		IgnoreTlog:        false,
		ClaimVerifier:     cosign.SimpleClaimVerifier,
	}

	signatures, _, err := cosign.VerifyImageSignatures(ctx, ref, checkOpts)
	if err != nil {
		return nil, fmt.Errorf("verifying image signatures: %w", err)
	}

	if len(signatures) == 0 {
		return nil, ErrSignatureNotFound
	}

	// Extract metadata from the first verified signature.
	result := &SignatureResult{
		Verified: true,
	}

	sig := signatures[0]

	// Prefer Rekor transparency log IntegratedTime (when the signature was
	// actually recorded) over the certificate's NotBefore timestamp.
	if b, bErr := sig.Bundle(); bErr == nil && b != nil && b.Payload.IntegratedTime > 0 {
		result.SignedAt = time.Unix(b.Payload.IntegratedTime, 0).UTC().Format(time.RFC3339)
	}

	cert, certErr := sig.Cert()
	if certErr != nil {
		logger := logr.FromContextOrDiscard(ctx)
		logger.V(1).Info("failed to extract certificate from verified signature", "error", certErr)
	} else if cert != nil {
		result.Issuer, result.Identity = extractCertMetadata(cert)
		// Fall back to cert.NotBefore only when no Rekor timestamp is available.
		if result.SignedAt == "" {
			result.SignedAt = cert.NotBefore.Format(time.RFC3339)
		}
	}

	return result, nil
}
