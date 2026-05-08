// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
)

// SignatureMode controls how signature verification failures are handled.
type SignatureMode string

const (
	// SignatureModeOff disables signature verification entirely (digest-only).
	SignatureModeOff SignatureMode = "off"

	// SignatureModeWarn verifies signatures but logs a warning on failure
	// instead of blocking execution.
	SignatureModeWarn SignatureMode = "warn"

	// SignatureModeEnforce requires valid signatures; missing or invalid
	// signatures cause a hard failure.
	SignatureModeEnforce SignatureMode = "enforce"
)

// ParseSignatureMode converts a string to a SignatureMode, returning an error
// for unrecognized values.
func ParseSignatureMode(s string) (SignatureMode, error) {
	switch s {
	case "", "off":
		return SignatureModeOff, nil
	case "warn":
		return SignatureModeWarn, nil
	case "enforce":
		return SignatureModeEnforce, nil
	default:
		return "", fmt.Errorf("unknown signature mode %q: must be off, warn, or enforce", s)
	}
}

// SignaturePolicy defines the verification policy for plugin binary signatures.
type SignaturePolicy struct {
	// Mode controls behavior on verification failure.
	Mode SignatureMode `json:"mode" yaml:"mode" doc:"Signature verification mode" enum:"off,warn,enforce" example:"warn"`

	// TrustedIssuers are OIDC issuers whose signing certificates are trusted
	// (e.g., "https://token.actions.githubusercontent.com").
	TrustedIssuers []string `json:"trustedIssuers,omitempty" yaml:"trustedIssuers,omitempty" doc:"Trusted OIDC certificate issuers" maxItems:"20"`

	// TrustedIdentities are glob patterns matching the certificate
	// subject/identity (e.g., "https://github.com/oakwood-commons/*").
	TrustedIdentities []string `json:"trustedIdentities,omitempty" yaml:"trustedIdentities,omitempty" doc:"Trusted signing identity patterns (glob)" maxItems:"50"`
}

// IsEnabled reports whether signature verification is active (mode != off).
// An empty Mode is treated as off (consistent with ParseSignatureMode).
func (p *SignaturePolicy) IsEnabled() bool {
	return p != nil && p.Mode != SignatureModeOff && p.Mode != ""
}

// Validate checks the policy for configuration errors. It returns an error
// when the mode requires verification but no trusted issuer/identity pairs
// are configured.
func (p *SignaturePolicy) Validate() error {
	if p == nil || !p.IsEnabled() {
		return nil
	}
	// Reject unknown modes early so callers don't silently fall through to
	// enforce behaviour with a typo in the config.
	if _, err := ParseSignatureMode(string(p.Mode)); err != nil {
		return err
	}
	if len(p.TrustedIssuers) == 0 || len(p.TrustedIdentities) == 0 {
		return fmt.Errorf(
			"signature mode %q requires at least one trustedIssuers and one trustedIdentities entry",
			p.Mode,
		)
	}
	return nil
}

// HandleVerificationError applies the policy's mode to a signature
// verification error. In warn mode the error is logged and nil is returned;
// in enforce mode (and any unrecognised mode) the error is propagated.
// A nil policy is treated as enforce (fail-closed).
func HandleVerificationError(policy *SignaturePolicy, err error, lgr logr.Logger, labels ...any) error {
	if err == nil {
		return nil
	}
	if policy == nil {
		return err
	}
	switch policy.Mode {
	case SignatureModeWarn:
		lgr.Info("plugin signature verification failed (warn mode, continuing)",
			append(labels, "error", err)...)
		return nil
	case SignatureModeEnforce:
		return err
	case SignatureModeOff:
		// Unreachable when callers guard with IsEnabled(); propagate the error.
		return err
	default:
		return err
	}
}

// SignaturePolicyFromRaw constructs a SignaturePolicy from raw configuration
// values. Returns (nil, nil) when mode is "off" or empty. Returns an error
// for unrecognized mode values.
func SignaturePolicyFromRaw(mode string, trustedIssuers, trustedIdentities []string) (*SignaturePolicy, error) {
	parsed, err := ParseSignatureMode(mode)
	if err != nil {
		return nil, err
	}
	if parsed == SignatureModeOff {
		return nil, nil
	}
	return &SignaturePolicy{
		Mode:              parsed,
		TrustedIssuers:    trustedIssuers,
		TrustedIdentities: trustedIdentities,
	}, nil
}

type signaturePolicyContextKey struct{}

// WithSignaturePolicy stores a SignaturePolicy in the context.
func WithSignaturePolicy(ctx context.Context, policy *SignaturePolicy) context.Context {
	return context.WithValue(ctx, signaturePolicyContextKey{}, policy)
}

// SignaturePolicyFromContext retrieves the SignaturePolicy from the context.
// Returns nil if none is set.
func SignaturePolicyFromContext(ctx context.Context) *SignaturePolicy {
	val, _ := ctx.Value(signaturePolicyContextKey{}).(*SignaturePolicy)
	return val
}

// SignatureResult holds the outcome of a signature verification attempt.
type SignatureResult struct {
	// Verified is true when a valid signature was confirmed.
	Verified bool `json:"verified" yaml:"verified" doc:"Whether a valid signature was confirmed"`

	// Issuer is the OIDC issuer from the signing certificate.
	Issuer string `json:"issuer,omitempty" yaml:"issuer,omitempty" doc:"OIDC certificate issuer" example:"https://token.actions.githubusercontent.com" maxLength:"255"`

	// Identity is the certificate subject/identity.
	Identity string `json:"identity,omitempty" yaml:"identity,omitempty" doc:"Certificate subject identity" example:"https://github.com/oakwood-commons/scafctl-plugin-auth-entra/.github/workflows/release.yaml@refs/tags/v1.0.0" maxLength:"500"`

	// SignedAt is the signature timestamp in RFC 3339 format (empty if unknown).
	SignedAt string `json:"signedAt,omitempty" yaml:"signedAt,omitempty" doc:"Signature timestamp (RFC 3339)" example:"2026-01-15T10:30:00Z" pattern:"^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(Z|[+-]\\d{2}:\\d{2})$" patternDescription:"RFC 3339 timestamp" maxLength:"30"`
}

// SignatureVerifier verifies OCI artifact signatures.
type SignatureVerifier interface {
	// VerifySignature checks the Sigstore/cosign signature for an OCI artifact
	// identified by its image reference (registry/repo@digest).
	// Returns the verification result or an error if verification could not
	// be performed (network failure, missing cosign support, etc.).
	VerifySignature(ctx context.Context, imageRef string, policy *SignaturePolicy) (*SignatureResult, error)
}

// ErrCosignNotAvailable indicates that the binary was compiled without cosign
// support (missing the "cosign" build tag).
var ErrCosignNotAvailable = errors.New("cosign signature verification not available: binary compiled without 'cosign' build tag")

// ErrSignatureNotFound indicates that no signature was found for the artifact.
var ErrSignatureNotFound = errors.New("no cosign signature found for artifact")

// ErrSignatureInvalid indicates that a signature exists but failed validation
// (wrong issuer, identity mismatch, expired certificate, etc.).
var ErrSignatureInvalid = errors.New("cosign signature verification failed")

// fulcioOIDCIssuerOIDs are the ASN.1 OIDs for the Fulcio OIDC issuer
// certificate extension. v1 is 1.3.6.1.4.1.57264.1.1; v2 is
// 1.3.6.1.4.1.57264.1.8.
var fulcioOIDCIssuerOIDs = []string{
	"1.3.6.1.4.1.57264.1.8", // v2 (preferred)
	"1.3.6.1.4.1.57264.1.1", // v1 (legacy)
}

// extractCertMetadata extracts the OIDC issuer and identity from an x509
// certificate's extensions and URI SANs. This is used to populate
// SignatureResult from Fulcio-issued signing certificates.
func extractCertMetadata(cert *x509.Certificate) (issuer, identity string) {
	if cert == nil {
		return "", ""
	}
	if len(cert.URIs) > 0 {
		identity = cert.URIs[0].String()
	}
	for _, oid := range fulcioOIDCIssuerOIDs {
		for _, ext := range cert.Extensions {
			if ext.Id.String() == oid {
				var parsed string
				if _, err := asn1.Unmarshal(ext.Value, &parsed); err == nil {
					issuer = parsed
				}
				return issuer, identity
			}
		}
	}
	return issuer, identity
}

// globToRegexp converts a simple glob pattern (with * wildcards) to a regexp
// string suitable for cosign identity matching. It escapes all regex
// metacharacters except *, which is converted to .*, and anchors the result.
func globToRegexp(pattern string) string {
	var result []rune
	for _, ch := range pattern {
		switch ch {
		case '*':
			result = append(result, '.', '*')
		case '.', '+', '?', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			result = append(result, '\\', ch)
		default:
			result = append(result, ch)
		}
	}
	return "^" + string(result) + "$"
}
