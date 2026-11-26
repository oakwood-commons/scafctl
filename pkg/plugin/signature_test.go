// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"net/url"
	"regexp"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSignatureMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SignatureMode
		wantErr bool
	}{
		{name: "empty defaults to off", input: "", want: SignatureModeOff},
		{name: "off", input: "off", want: SignatureModeOff},
		{name: "warn", input: "warn", want: SignatureModeWarn},
		{name: "enforce", input: "enforce", want: SignatureModeEnforce},
		{name: "unknown returns error", input: "strict", wantErr: true},
		{name: "case sensitive", input: "Warn", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSignatureMode(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignaturePolicy_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		policy *SignaturePolicy
		want   bool
	}{
		{name: "nil policy", policy: nil, want: false},
		{name: "empty mode treated as off", policy: &SignaturePolicy{Mode: ""}, want: false},
		{name: "off mode", policy: &SignaturePolicy{Mode: SignatureModeOff}, want: false},
		{name: "warn mode", policy: &SignaturePolicy{Mode: SignatureModeWarn}, want: true},
		{name: "enforce mode", policy: &SignaturePolicy{Mode: SignatureModeEnforce}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.policy.IsEnabled())
		})
	}
}

func TestStubVerifier_NilPolicy(t *testing.T) {
	verifier := NewSignatureVerifier()
	result, err := verifier.VerifySignature(context.Background(), "ghcr.io/test/plugin@sha256:abc123", nil)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestStubVerifier_DisabledPolicy(t *testing.T) {
	verifier := NewSignatureVerifier()
	policy := &SignaturePolicy{Mode: SignatureModeOff}
	result, err := verifier.VerifySignature(context.Background(), "ghcr.io/test/plugin@sha256:abc123", policy)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// mockVerifier is a test helper that implements SignatureVerifier.
type mockVerifier struct {
	mu        sync.Mutex
	result    *SignatureResult
	err       error
	imageRefs []string
	policies  []*SignaturePolicy
}

func (m *mockVerifier) VerifySignature(_ context.Context, imageRef string, policy *SignaturePolicy) (*SignatureResult, error) {
	m.mu.Lock()
	m.imageRefs = append(m.imageRefs, imageRef)
	m.policies = append(m.policies, policy)
	m.mu.Unlock()
	return m.result, m.err
}

func TestFetcher_verifySignature_WarnMode_LogsAndContinues(t *testing.T) {
	f := &Fetcher{
		sigPolicy: &SignaturePolicy{
			Mode:              SignatureModeWarn,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		},
		sigVerifier: &mockVerifier{err: ErrSignatureNotFound},
		logger:      logr.Discard(),
	}

	result, err := f.verifySignature(context.Background(), catalog.ArtifactKindProvider, "test-plugin", "1.0.0", "ghcr.io/org/plugin@sha256:abc")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestFetcher_verifySignature_EnforceMode_ReturnsError(t *testing.T) {
	f := &Fetcher{
		sigPolicy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		},
		sigVerifier: &mockVerifier{err: ErrSignatureNotFound},
		logger:      logr.Discard(),
	}

	result, err := f.verifySignature(context.Background(), catalog.ArtifactKindProvider, "test-plugin", "1.0.0", "ghcr.io/org/plugin@sha256:abc")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no cosign signature found")
}

func TestFetcher_verifySignature_Success(t *testing.T) {
	expected := &SignatureResult{
		Verified: true,
		Issuer:   "https://token.actions.githubusercontent.com",
		Identity: "https://github.com/oakwood-commons/plugin/.github/workflows/release.yaml@refs/tags/v1.0.0",
		SignedAt: "2025-01-15T10:00:00Z",
	}
	f := &Fetcher{
		sigPolicy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
		},
		sigVerifier: &mockVerifier{result: expected},
		logger:      logr.Discard(),
	}

	result, err := f.verifySignature(context.Background(), catalog.ArtifactKindProvider, "test-plugin", "1.0.0", "ghcr.io/org/plugin@sha256:abc")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Verified)
	assert.Equal(t, expected.Issuer, result.Issuer)
	assert.Equal(t, expected.Identity, result.Identity)
}

func TestFetcher_verifySignature_UnexpectedMode_ReturnsError(t *testing.T) {
	f := &Fetcher{
		sigPolicy: &SignaturePolicy{
			Mode: SignatureModeOff,
		},
		sigVerifier: &mockVerifier{err: errors.New("unexpected call")},
		logger:      logr.Discard(),
	}

	// With off mode, IsEnabled() is false so verifySignature won't be called
	// in the fetcher flow. If called directly with an error, the default case
	// in HandleVerificationError propagates the error.
	result, err := f.verifySignature(context.Background(), catalog.ArtifactKindProvider, "test-plugin", "1.0.0", "ghcr.io/org/plugin@sha256:abc")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unexpected call")
}

func TestGlobToRegexp(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "simple wildcard", pattern: "https://github.com/org/*", want: `^https://github\.com/org/.*$`},
		{name: "no wildcard", pattern: "https://token.actions.githubusercontent.com", want: `^https://token\.actions\.githubusercontent\.com$`},
		{name: "multiple wildcards", pattern: "*/workflows/*.yaml@*", want: `^.*/workflows/.*\.yaml@.*$`},
		{name: "special chars", pattern: "a+b?c[d]", want: `^a\+b\?c\[d\]$`},
		{name: "empty", pattern: "", want: "^$"},
		{name: "backslash", pattern: `foo\bar`, want: `^foo\\bar$`},
		{name: "unicode characters", pattern: "org/日本語/*", want: `^org/日本語/.*$`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := globToRegexp(tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignaturePolicyContext(t *testing.T) {
	t.Run("nil when not set", func(t *testing.T) {
		ctx := context.Background()
		assert.Nil(t, SignaturePolicyFromContext(ctx))
	})

	t.Run("round-trips value", func(t *testing.T) {
		policy := &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		}
		ctx := WithSignaturePolicy(context.Background(), policy)
		got := SignaturePolicyFromContext(ctx)
		require.NotNil(t, got)
		assert.Equal(t, SignatureModeEnforce, got.Mode)
		assert.Equal(t, policy.TrustedIssuers, got.TrustedIssuers)
	})
}

func TestSignaturePolicyFromRaw(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		issuers    []string
		identities []string
		wantNil    bool
		wantMode   SignatureMode
		wantErr    bool
	}{
		{name: "empty mode returns nil", mode: "", wantNil: true},
		{name: "off mode returns nil", mode: "off", wantNil: true},
		{name: "warn mode returns policy", mode: "warn", issuers: []string{"i"}, identities: []string{"id"}, wantMode: SignatureModeWarn},
		{name: "enforce mode returns policy", mode: "enforce", issuers: []string{"i"}, identities: []string{"id"}, wantMode: SignatureModeEnforce},
		{name: "unknown mode returns error", mode: "strict", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SignaturePolicyFromRaw(tt.mode, tt.issuers, tt.identities)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantMode, got.Mode)
			assert.Equal(t, tt.issuers, got.TrustedIssuers)
			assert.Equal(t, tt.identities, got.TrustedIdentities)
		})
	}
}

func BenchmarkGlobToRegexp(b *testing.B) {
	patterns := []string{
		"https://github.com/oakwood-commons/*",
		"https://token.actions.githubusercontent.com",
		"*/workflows/*.yaml@*",
	}
	for b.Loop() {
		for _, p := range patterns {
			_ = globToRegexp(p)
		}
	}
}

func BenchmarkParseSignatureMode(b *testing.B) {
	modes := []string{"off", "warn", "enforce", ""}
	for b.Loop() {
		for _, m := range modes {
			_, _ = ParseSignatureMode(m)
		}
	}
}

func TestSignaturePolicy_Validate(t *testing.T) {
	tests := []struct {
		name      string
		policy    *SignaturePolicy
		wantErr   bool
		wantErrIs error
	}{
		{name: "nil policy", policy: nil, wantErr: false},
		{name: "off mode", policy: &SignaturePolicy{Mode: SignatureModeOff}, wantErr: false},
		{name: "off mode with valid template", policy: &SignaturePolicy{
			Mode:              SignatureModeOff,
			TrustedIdentities: []string{"https://github.com/my-org/sln-{name}/.github/workflows/*"},
		}, wantErr: false},
		{name: "off mode with malformed template rejected", policy: &SignaturePolicy{
			Mode:              SignatureModeOff,
			TrustedIdentities: []string{"https://github.com/my-org/{repo}/*"},
		}, wantErr: true, wantErrIs: ErrInvalidIdentityPattern},
		{name: "warn with both", policy: &SignaturePolicy{
			Mode:              SignatureModeWarn,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		}, wantErr: false},
		{name: "enforce with both", policy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		}, wantErr: false},
		{name: "enforce with {name} and {kind} templates", policy: &SignaturePolicy{
			Mode:           SignatureModeEnforce,
			TrustedIssuers: []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{
				"https://github.com/my-org/sln-{name}/.github/workflows/*",
				"https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*",
			},
		}, wantErr: false},
		{name: "warn missing issuers", policy: &SignaturePolicy{
			Mode:              SignatureModeWarn,
			TrustedIdentities: []string{"https://github.com/org/*"},
		}, wantErr: true},
		{name: "enforce blank issuer", policy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"   "},
			TrustedIdentities: []string{"https://github.com/org/*"},
		}, wantErr: true},
		{name: "enforce missing identities", policy: &SignaturePolicy{
			Mode:           SignatureModeEnforce,
			TrustedIssuers: []string{"https://issuer.example.com"},
		}, wantErr: true},
		{name: "enforce missing both", policy: &SignaturePolicy{
			Mode: SignatureModeEnforce,
		}, wantErr: true},
		{name: "unknown mode", policy: &SignaturePolicy{
			Mode:              SignatureMode("bogus"),
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		}, wantErr: true},
		{name: "unknown placeholder rejected", policy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/{repo}/*"},
		}, wantErr: true, wantErrIs: ErrInvalidIdentityPattern},
		{name: "unbalanced brace rejected", policy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{"https://github.com/org/{name/*"},
		}, wantErr: true, wantErrIs: ErrInvalidIdentityPattern},
		{name: "empty identity pattern rejected", policy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://issuer.example.com"},
			TrustedIdentities: []string{""},
		}, wantErr: true, wantErrIs: ErrInvalidIdentityPattern},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					assert.ErrorIs(t, err, tt.wantErrIs)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSignaturePolicy_HasPlaceholders(t *testing.T) {
	t.Parallel()

	var nilPolicy *SignaturePolicy
	assert.False(t, nilPolicy.HasPlaceholders())
	assert.False(t, (&SignaturePolicy{TrustedIdentities: []string{"https://github.com/org/*"}}).HasPlaceholders())
	assert.True(t, (&SignaturePolicy{TrustedIdentities: []string{
		"https://github.com/oakwood-commons/*",
		"https://github.com/my-org/sln-{name}/.github/workflows/*",
	}}).HasPlaceholders())
	assert.True(t, (&SignaturePolicy{TrustedIdentities: []string{
		"https://github.com/my-org/{kind}/.github/workflows/*",
	}}).HasPlaceholders())
}

func TestSignaturePolicy_ForArtifact(t *testing.T) {
	t.Parallel()

	t.Run("nil and disabled policies return unchanged", func(t *testing.T) {
		t.Parallel()
		var nilPolicy *SignaturePolicy
		got, err := nilPolicy.ForArtifact(catalog.ArtifactKindProvider, "github")
		require.NoError(t, err)
		assert.Nil(t, got)

		offPolicy := &SignaturePolicy{Mode: SignatureModeOff, TrustedIdentities: []string{"{invalid"}}
		got, err = offPolicy.ForArtifact(catalog.ArtifactKindProvider, "../bad")
		require.NoError(t, err)
		assert.Same(t, offPolicy, got)
	})

	t.Run("static policy returns exact pointer without allocating", func(t *testing.T) {
		t.Parallel()
		static := &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
		}
		got, err := static.ForArtifact(catalog.ArtifactKindProvider, "github")
		require.NoError(t, err)
		assert.Same(t, static, got)
	})

	t.Run("expands {name} and {kind} and blocks mismatched repo spoofing", func(t *testing.T) {
		t.Parallel()
		base := &SignaturePolicy{
			Mode:           SignatureModeEnforce,
			TrustedIssuers: []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{
				"https://github.com/my-org/sln-{name}/.github/workflows/*",
				"https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*",
				"https://github.com/oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@refs/tags/sign-plugin/v*",
			},
		}

		scoped, err := base.ForArtifact(catalog.ArtifactKindSolution, "prod-deploy")
		require.NoError(t, err)
		require.NotSame(t, base, scoped)
		assert.Equal(t, []string{
			"https://github.com/my-org/sln-prod-deploy/.github/workflows/*",
			"https://github.com/my-org/scafctl-solution-prod-deploy/.github/workflows/*",
			"https://github.com/oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@refs/tags/sign-plugin/v*",
		}, scoped.TrustedIdentities)
		assert.Equal(t, "https://github.com/my-org/sln-{name}/.github/workflows/*", base.TrustedIdentities[0], "original policy must not be mutated")

		matchingCertSAN := "https://github.com/my-org/sln-prod-deploy/.github/workflows/release.yml@refs/tags/v1.0.0"
		spoofingDocsSAN := "https://github.com/my-org/sln-docs/.github/workflows/release.yml@refs/tags/v1.0.0"
		re := regexp.MustCompile(globToRegexp(scoped.TrustedIdentities[0]))
		assert.True(t, re.MatchString(matchingCertSAN))
		assert.False(t, re.MatchString(spoofingDocsSAN), "compromised sibling repo must not match expanded identity")
	})

	t.Run("safe identifier validation blocks injection attempts", func(t *testing.T) {
		t.Parallel()
		policy := &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*"},
		}

		badInputs := []struct {
			label string
			kind  catalog.ArtifactKind
			name  string
		}{
			{label: "empty kind", kind: "", name: "github"},
			{label: "unknown kind", kind: "plugin", name: "github"},
			{label: "wildcard kind", kind: "*", name: "github"},
			{label: "empty name", kind: catalog.ArtifactKindProvider, name: ""},
			{label: "wildcard in name", kind: catalog.ArtifactKindProvider, name: "git*"},
			{label: "bare star name", kind: catalog.ArtifactKindProvider, name: "*"},
			{label: "slash path injection", kind: catalog.ArtifactKindProvider, name: "a/b"},
			{label: "parent traversal", kind: catalog.ArtifactKindProvider, name: "../other"},
			{label: "dot metacharacter", kind: catalog.ArtifactKindProvider, name: "my.plugin"},
			{label: "regex alternation", kind: catalog.ArtifactKindProvider, name: "good|evil"},
			{label: "brace injection", kind: catalog.ArtifactKindProvider, name: "{kind}"},
			{label: "uppercase letters", kind: catalog.ArtifactKindProvider, name: "GitHub"},
			{label: "double hyphen", kind: catalog.ArtifactKindProvider, name: "my--plugin"},
		}

		for _, tc := range badInputs {
			t.Run(tc.label, func(t *testing.T) {
				t.Parallel()
				got, err := policy.ForArtifact(tc.kind, tc.name)
				assert.Nil(t, got)
				require.ErrorIs(t, err, ErrInvalidArtifactIdentity)
			})
		}
	})

	t.Run("malformed pattern fails closed even without supported placeholders", func(t *testing.T) {
		t.Parallel()
		policy := &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/my-org/{repo}/*"},
		}
		got, err := policy.ForArtifact(catalog.ArtifactKindProvider, "github")
		assert.Nil(t, got)
		require.ErrorIs(t, err, ErrInvalidIdentityPattern)
	})

	t.Run("concurrent ForArtifact calls are race-free", func(t *testing.T) {
		t.Parallel()
		policy := &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/my-org/{kind}-{name}/.github/workflows/*"},
		}
		var wg sync.WaitGroup
		for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				got, err := policy.ForArtifact(catalog.ArtifactKindProvider, n)
				require.NoError(t, err)
				assert.Equal(t, []string{"https://github.com/my-org/provider-" + n + "/.github/workflows/*"}, got.TrustedIdentities)
			}(name)
		}
		wg.Wait()
	})
}

func BenchmarkSignaturePolicy_ForArtifact_Static(b *testing.B) {
	policy := &SignaturePolicy{
		Mode:              SignatureModeEnforce,
		TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
		TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := policy.ForArtifact(catalog.ArtifactKindProvider, "github"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignaturePolicy_ForArtifact_Placeholders(b *testing.B) {
	policy := &SignaturePolicy{
		Mode:           SignatureModeEnforce,
		TrustedIssuers: []string{"https://token.actions.githubusercontent.com"},
		TrustedIdentities: []string{
			"https://github.com/my-org/scafctl-{kind}-{name}/.github/workflows/*",
			"https://github.com/oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@refs/tags/sign-plugin/v*",
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := policy.ForArtifact(catalog.ArtifactKindProvider, "github"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestHandleVerificationError(t *testing.T) {
	tests := []struct {
		name    string
		policy  *SignaturePolicy
		err     error
		wantNil bool
	}{
		{name: "nil error", policy: &SignaturePolicy{Mode: SignatureModeEnforce}, err: nil, wantNil: true},
		{name: "warn mode swallows error", policy: &SignaturePolicy{Mode: SignatureModeWarn}, err: ErrSignatureNotFound, wantNil: true},
		{name: "enforce mode propagates", policy: &SignaturePolicy{Mode: SignatureModeEnforce}, err: ErrSignatureNotFound, wantNil: false},
		{name: "unknown mode propagates", policy: &SignaturePolicy{Mode: SignatureMode("custom")}, err: ErrSignatureNotFound, wantNil: false},
		{name: "nil policy propagates error", policy: nil, err: ErrSignatureNotFound, wantNil: false},
		{name: "nil policy nil error", policy: nil, err: nil, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HandleVerificationError(tt.policy, tt.err, logr.Discard(), "test", "value")
			if tt.wantNil {
				assert.NoError(t, got)
			} else {
				assert.Error(t, got)
			}
		})
	}
}

func mustMarshalASN1String(s string) []byte {
	data, err := asn1.Marshal(s)
	if err != nil {
		panic(err)
	}
	return data
}

func TestExtractCertMetadata(t *testing.T) {
	// OID for Fulcio v1 issuer extension.
	oidV1 := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	// OID for Fulcio v2 issuer extension.
	oidV2 := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}

	tests := []struct {
		name         string
		cert         *x509.Certificate
		wantIssuer   string
		wantIdentity string
	}{
		{name: "nil cert", cert: nil, wantIssuer: "", wantIdentity: ""},
		{
			name: "v1 OID with URI SAN",
			cert: &x509.Certificate{
				URIs: []*url.URL{{Scheme: "https", Host: "github.com", Path: "/org/repo/.github/workflows/release.yaml@refs/tags/v1.0.0"}},
				Extensions: []pkix.Extension{
					{Id: oidV1, Value: mustMarshalASN1String("https://token.actions.githubusercontent.com")},
				},
			},
			wantIssuer:   "https://token.actions.githubusercontent.com",
			wantIdentity: "https://github.com/org/repo/.github/workflows/release.yaml@refs/tags/v1.0.0",
		},
		{
			name: "v2 OID preferred over v1",
			cert: &x509.Certificate{
				URIs: []*url.URL{{Scheme: "https", Host: "github.com", Path: "/org/repo"}},
				Extensions: []pkix.Extension{
					{Id: oidV1, Value: mustMarshalASN1String("v1-issuer")},
					{Id: oidV2, Value: mustMarshalASN1String("v2-issuer")},
				},
			},
			wantIssuer:   "v2-issuer",
			wantIdentity: "https://github.com/org/repo",
		},
		{
			name: "no matching OID",
			cert: &x509.Certificate{
				URIs: []*url.URL{{Scheme: "https", Host: "example.com", Path: "/identity"}},
				Extensions: []pkix.Extension{
					{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Value: []byte("irrelevant")},
				},
			},
			wantIssuer:   "",
			wantIdentity: "https://example.com/identity",
		},
		{
			name:         "no URIs no extensions",
			cert:         &x509.Certificate{},
			wantIssuer:   "",
			wantIdentity: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer, identity := extractCertMetadata(tt.cert)
			assert.Equal(t, tt.wantIssuer, issuer)
			assert.Equal(t, tt.wantIdentity, identity)
		})
	}
}

func BenchmarkExtractCertMetadata(b *testing.B) {
	oidV1 := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	cert := &x509.Certificate{
		URIs: []*url.URL{{Scheme: "https", Host: "github.com", Path: "/org/repo"}},
		Extensions: []pkix.Extension{
			{Id: oidV1, Value: mustMarshalASN1String("https://token.actions.githubusercontent.com")},
		},
	}
	for b.Loop() {
		extractCertMetadata(cert)
	}
}
