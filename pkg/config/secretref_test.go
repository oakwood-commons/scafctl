// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSecretGetter struct {
	data map[string][]byte
	err  error
}

func (s *stubSecretGetter) Get(_ context.Context, name string) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	v, ok := s.data[name]
	if !ok {
		return nil, errors.New("secret not found")
	}
	return v, nil
}

func TestSecretRef_Validate_ValidEnv(t *testing.T) {
	t.Parallel()
	s := SecretRef("env://MY_SECRET")
	assert.NoError(t, s.Validate())
}

func TestSecretRef_Validate_ValidFile(t *testing.T) {
	t.Parallel()
	s := SecretRef("file:///run/secrets/token")
	assert.NoError(t, s.Validate())
}

func TestSecretRef_Validate_ValidSecret(t *testing.T) {
	t.Parallel()
	for _, ref := range []SecretRef{
		"secret://quay-robot-token",
		"secret://my_api.key-1",
		"secret://scafctl_user_secret", // underscore, not reserved dot prefix
	} {
		assert.NoError(t, ref.Validate(), "expected %q to be valid", ref)
	}
}

func TestSecretRef_Validate_EmptyEnvName(t *testing.T) {
	t.Parallel()
	s := SecretRef("env://")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a variable name")
}

func TestSecretRef_Validate_InvalidEnvChars(t *testing.T) {
	t.Parallel()
	s := SecretRef("env://BAD VAR")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whitespace or control characters")
}

func TestSecretRef_Validate_EmptyFilePath(t *testing.T) {
	t.Parallel()
	s := SecretRef("file://")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a path")
}

func TestSecretRef_Validate_SecretInvalidCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     SecretRef
		binName string
		wantErr string
	}{
		{
			name:    "empty secret name",
			ref:     "secret://",
			wantErr: "requires a secret name",
		},
		{
			name:    "whitespace in name",
			ref:     "secret://my secret",
			wantErr: "whitespace or control characters",
		},
		{
			name:    "control char in name",
			ref:     "secret://my\nsecret",
			wantErr: "whitespace or control characters",
		},
		{
			name:    "forward slash path traversal",
			ref:     "secret://../other",
			wantErr: "path separators or '..'",
		},
		{
			name:    "slash separator",
			ref:     "secret://org/token",
			wantErr: "path separators or '..'",
		},
		{
			name:    "backslash separator",
			ref:     `secret://org\token`,
			wantErr: "path separators or '..'",
		},
		{
			name:    "consecutive dots",
			ref:     "secret://foo..bar",
			wantErr: "path separators or '..'",
		},
		{
			name:    "leading dot",
			ref:     "secret://.hidden",
			wantErr: "must start with an alphanumeric character",
		},
		{
			name:    "leading dash",
			ref:     "secret://-flag",
			wantErr: "must start with an alphanumeric character",
		},
		{
			name:    "leading underscore",
			ref:     "secret://_hidden",
			wantErr: "must start with an alphanumeric character",
		},
		{
			name:    "disallowed punctuation",
			ref:     "secret://bad@name",
			wantErr: "invalid characters",
		},
		{
			name:    "exceeds max length",
			ref:     SecretRef("secret://" + strings.Repeat("a", 256)),
			wantErr: "exceeds maximum length",
		},
		{
			name:    "reserved default internal prefix",
			ref:     "secret://scafctl.auth.oauth2.quay.pat_token",
			wantErr: "reserved internal secret prefix",
		},
		{
			name:    "reserved custom embedder binary prefix",
			ref:     "secret://mycli.auth.token",
			binName: "mycli",
			wantErr: "reserved internal secret prefix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tc.binName != "" {
				err = tc.ref.ValidateFor(tc.binName)
			} else {
				err = tc.ref.Validate()
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSecretRef_Validate_UnknownScheme(t *testing.T) {
	t.Parallel()
	s := SecretRef("vault://secret/data/foo")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported secret ref scheme")
}

func TestSecretRef_Validate_NoScheme(t *testing.T) {
	t.Parallel()
	s := SecretRef("just-a-string")
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported secret ref scheme")
}

func TestSecretRef_Resolve_Env(t *testing.T) {
	t.Setenv("TEST_SECRET_REF_VAL", "s3cr3t")
	s := SecretRef("env://TEST_SECRET_REF_VAL")
	val, err := s.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", val)
}

func TestSecretRef_Resolve_EnvEmpty(t *testing.T) {
	t.Setenv("TEST_SECRET_REF_EMPTY", "")
	s := SecretRef("env://TEST_SECRET_REF_EMPTY")
	_, err := s.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty or not set")
}

func TestSecretRef_Resolve_FileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(path, []byte("file-secret\n"), 0o600))
	s := SecretRef("file://" + path)
	val, err := s.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "file-secret", val)
}

func TestSecretRef_Resolve_FileMissing(t *testing.T) {
	t.Parallel()
	s := SecretRef("file:///nonexistent/path/secret")
	_, err := s.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading secret file")
}

func TestSecretRef_Resolve_SecretWithoutStoreErrors(t *testing.T) {
	t.Parallel()
	s := SecretRef("secret://my-token")
	_, err := s.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResolveWithStore")
}

func TestSecretRef_ResolveWithStore(t *testing.T) {
	t.Parallel()

	t.Run("resolves secret from store and trims whitespace", func(t *testing.T) {
		t.Parallel()
		store := &stubSecretGetter{data: map[string][]byte{"quay-token": []byte("  robot-secret\n")}}
		val, err := SecretRef("secret://quay-token").ResolveWithStore(context.Background(), store)
		require.NoError(t, err)
		assert.Equal(t, "robot-secret", val)
	})

	t.Run("nil store returns actionable error", func(t *testing.T) {
		t.Parallel()
		_, err := SecretRef("secret://quay-token").ResolveWithStore(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret store is unavailable")
	})

	t.Run("empty secret value returns error", func(t *testing.T) {
		t.Parallel()
		store := &stubSecretGetter{data: map[string][]byte{"empty-key": []byte("   \n")}}
		_, err := SecretRef("secret://empty-key").ResolveWithStore(context.Background(), store)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is empty")
	})

	t.Run("missing secret propagates store error", func(t *testing.T) {
		t.Parallel()
		store := &stubSecretGetter{data: map[string][]byte{}}
		_, err := SecretRef("secret://missing-key").ResolveWithStore(context.Background(), store)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret not found")
	})

	t.Run("rejects context binary name internal prefix", func(t *testing.T) {
		t.Parallel()
		ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: "mycli"})
		store := &stubSecretGetter{data: map[string][]byte{"mycli.auth.token": []byte("internal")}}
		_, err := SecretRef("secret://mycli.auth.token").ResolveWithStore(ctx, store)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved internal secret prefix")
	})

	t.Run("delegates file scheme", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "tok")
		require.NoError(t, os.WriteFile(path, []byte("jwt-val"), 0o600))
		val, err := SecretRef("file://"+path).ResolveWithStore(context.Background(), nil)
		require.NoError(t, err)
		assert.Equal(t, "jwt-val", val)
	})
}

func TestResolveCredentialValue(t *testing.T) {
	t.Setenv("TEST_CRED_ENV_VAR", "from-env")
	dir := t.TempDir()
	filePath := filepath.Join(dir, "cred")
	require.NoError(t, os.WriteFile(filePath, []byte("from-file\n"), 0o600))
	store := &stubSecretGetter{data: map[string][]byte{"my-secret": []byte("from-store")}}
	ctx := context.Background()

	val, err := ResolveCredentialValue(ctx, "", store)
	require.NoError(t, err)
	assert.Empty(t, val)

	val, err = ResolveCredentialValue(ctx, "bare-literal-secret", store)
	require.NoError(t, err)
	assert.Equal(t, "bare-literal-secret", val)

	val, err = ResolveCredentialValue(ctx, "env://TEST_CRED_ENV_VAR", store)
	require.NoError(t, err)
	assert.Equal(t, "from-env", val)

	val, err = ResolveCredentialValue(ctx, "file://"+filePath, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-file", val)

	val, err = ResolveCredentialValue(ctx, "secret://my-secret", store)
	require.NoError(t, err)
	assert.Equal(t, "from-store", val)

	_, err = ResolveCredentialValue(ctx, "secret://scafctl.auth.internal", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved internal secret prefix")
}

func TestValidateCredentialValue(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateCredentialValue(""))
	assert.NoError(t, ValidateCredentialValue("literal-value"))
	assert.NoError(t, ValidateCredentialValue("secret://valid-secret"))
	assert.Error(t, ValidateCredentialValue("secret://scafctl.internal"))
	assert.Error(t, ValidateCredentialValue("secret://mycli.internal", "mycli"))
	assert.Error(t, ValidateCredentialValue("env://"))
}

func TestCustomOAuth2Config_ValidateCredentials(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver is valid", func(t *testing.T) {
		t.Parallel()
		var oc *CustomOAuth2Config
		assert.NoError(t, oc.ValidateCredentials())
	})

	t.Run("valid SecretRef and literal fields", func(t *testing.T) {
		t.Parallel()
		oc := CustomOAuth2Config{
			Name:                      "valid",
			FederatedToken:            "file:///var/run/secrets/tokens/sa-token",
			Token:                     "secret://quay-robot-token",
			ClientSecret:              "secret://oauth-client-secret",
			Username:                  "myorg+ci_robot",
			WorkloadIdentityGrantType: WorkloadIdentityGrantTokenExchange,
			DynamicClientRegistration: &DynamicClientRegistrationConfig{
				RegistrationEndpoint: "https://idp.example.com/register",
				InitialAccessToken:   "env://DCR_INITIAL_TOKEN",
			},
		}
		assert.NoError(t, oc.ValidateCredentials())
	})

	t.Run("mutually exclusive federatedToken and federatedTokenFile", func(t *testing.T) {
		t.Parallel()
		oc := CustomOAuth2Config{
			Name:               "wif",
			FederatedToken:     "env://OIDC_TOKEN",
			FederatedTokenFile: "/var/run/secrets/token",
		}
		err := oc.ValidateCredentials()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("rejects whitespace-only and NUL federatedTokenFile", func(t *testing.T) {
		t.Parallel()
		ws := CustomOAuth2Config{Name: "wif", FederatedTokenFile: "   "}
		require.ErrorContains(t, ws.ValidateCredentials(), "whitespace-only")

		nul := CustomOAuth2Config{Name: "wif", FederatedTokenFile: "/tmp/bad\x00path"}
		require.ErrorContains(t, nul.ValidateCredentials(), "NUL bytes")
	})

	t.Run("rejects plaintext non-SecretRef on token and federatedToken", func(t *testing.T) {
		t.Parallel()
		oc := CustomOAuth2Config{Name: "pat", Token: "plaintext-pat"}
		require.Error(t, oc.ValidateCredentials())

		oc2 := CustomOAuth2Config{Name: "wif", FederatedToken: "eyJhbGciOi..."}
		require.Error(t, oc2.ValidateCredentials())
	})

	t.Run("rejects invalid clientSecret username dcr token and grantType", func(t *testing.T) {
		t.Parallel()
		badSecret := CustomOAuth2Config{Name: "cc", ClientSecret: "secret://scafctl.auth.internal"}
		require.ErrorContains(t, badSecret.ValidateCredentials(), "invalid clientSecret")

		badEmbedder := CustomOAuth2Config{Name: "cc", ClientSecret: "secret://mycli.auth.internal"}
		require.ErrorContains(t, badEmbedder.ValidateCredentials("mycli"), "reserved internal secret prefix")

		badUser := CustomOAuth2Config{Name: "pat", Username: "env://"}
		require.ErrorContains(t, badUser.ValidateCredentials(), "invalid username")

		badDCR := CustomOAuth2Config{
			Name: "dcr",
			DynamicClientRegistration: &DynamicClientRegistrationConfig{
				RegistrationEndpoint: "https://idp.example.com/register",
				InitialAccessToken:   "secret://_invalid",
			},
		}
		require.ErrorContains(t, badDCR.ValidateCredentials(), "invalid dynamicClientRegistration.initialAccessToken")

		badGrant := CustomOAuth2Config{Name: "wif", WorkloadIdentityGrantType: "implicit"}
		require.ErrorContains(t, badGrant.ValidateCredentials(), "unknown workloadIdentityGrantType")
	})
}

func BenchmarkSecretRef_ResolveWithStore(b *testing.B) {
	store := &stubSecretGetter{data: map[string][]byte{"quay-robot": []byte("robot-token-value")}}
	ref := SecretRef("secret://quay-robot")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ref.ResolveWithStore(ctx, store)
	}
}

func BenchmarkResolveCredentialValue(b *testing.B) {
	store := &stubSecretGetter{data: map[string][]byte{"client-sec": []byte("secret-val")}}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ResolveCredentialValue(ctx, "secret://client-sec", store)
	}
}

func TestSecretRef_Scheme(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "env", SecretRef("env://FOO").Scheme())
	assert.Equal(t, "file", SecretRef("file:///bar").Scheme())
	assert.Equal(t, "secret", SecretRef("secret://baz").Scheme())
	assert.Equal(t, "", SecretRef("noscheme").Scheme())
}
