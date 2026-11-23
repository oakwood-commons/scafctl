// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type fakeCredResolver struct {
	secrets map[string]string
	tokens  map[string]string
	lastReq TokenRef
}

func (f *fakeCredResolver) ResolveSecret(_ context.Context, name string) (string, error) {
	return f.secrets[name], nil
}

func (f *fakeCredResolver) ResolveToken(_ context.Context, req TokenRef) (string, error) {
	f.lastReq = req
	return f.tokens[req.Handler], nil
}

func TestValueRef_CredentialsYAMLAndJSON(t *testing.T) {
	t.Run("scrt YAML and JSON round-trip", func(t *testing.T) {
		var vr ValueRef
		require.NoError(t, yaml.Unmarshal([]byte("scrt: npm-token\n"), &vr))
		require.NotNil(t, vr.Secret)
		assert.Equal(t, "npm-token", *vr.Secret)
		assert.True(t, vr.ContainsCredentialRef())
		assert.Equal(t, "scrt:npm-token", vr.CredentialSummary())

		yb, err := yaml.Marshal(vr)
		require.NoError(t, err)
		var vr2 ValueRef
		require.NoError(t, yaml.Unmarshal(yb, &vr2))
		require.NotNil(t, vr2.Secret)
		assert.Equal(t, "npm-token", *vr2.Secret)

		jb, err := json.Marshal(vr)
		require.NoError(t, err)
		var vr3 ValueRef
		require.NoError(t, json.Unmarshal(jb, &vr3))
		require.NotNil(t, vr3.Secret)
		assert.Equal(t, "npm-token", *vr3.Secret)
	})

	t.Run("tkn shorthand and object YAML and JSON round-trip", func(t *testing.T) {
		var shortVR ValueRef
		require.NoError(t, yaml.Unmarshal([]byte("tkn: github\n"), &shortVR))
		require.NotNil(t, shortVR.Token)
		assert.Equal(t, TokenRef{Handler: "github"}, *shortVR.Token)
		assert.Equal(t, "tkn:github", shortVR.CredentialSummary())
		assert.Equal(t, []string{"github"}, shortVR.ReferencedAuthHandlers())

		yb, err := yaml.Marshal(shortVR)
		require.NoError(t, err)
		assert.Contains(t, string(yb), "tkn: github")

		fullYAML := `
tkn:
  handler: gcp
  scope: https://www.googleapis.com/auth/cloud-platform
  profile: work
  hostname: prod-east
  minValidFor: 5m
`
		var fullVR ValueRef
		require.NoError(t, yaml.Unmarshal([]byte(fullYAML), &fullVR))
		require.NotNil(t, fullVR.Token)
		assert.Equal(t, "gcp", fullVR.Token.Handler)
		assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", fullVR.Token.Scope)
		assert.Equal(t, "work", fullVR.Token.Profile)
		assert.Equal(t, "prod-east", fullVR.Token.Hostname)
		assert.Equal(t, "5m", fullVR.Token.MinValidFor)

		jb, err := json.Marshal(fullVR)
		require.NoError(t, err)
		var fromJSON ValueRef
		require.NoError(t, json.Unmarshal(jb, &fromJSON))
		assert.Equal(t, *fullVR.Token, *fromJSON.Token)
	})

	t.Run("rejects invalid scrt and tkn forms", func(t *testing.T) {
		badYAML := []string{
			"scrt: \"\"\n",
			"scrt: null\n",
			"scrt: 123\n",
			"tkn: \"\"\n",
			"tkn: null\n",
			"tkn: 42\n",
			"tkn:\n  scope: read\n",
			"tkn:\n  handler: github\n  unknownField: foo\n",
			"scrt: npm-token\ntkn: github\n",
		}
		for _, sample := range badYAML {
			var vr ValueRef
			err := yaml.Unmarshal([]byte(sample), &vr)
			require.Error(t, err, "expected error for YAML: %s", sample)
		}

		badJSON := []string{
			`{"scrt": ""}`,
			`{"scrt": null}`,
			`{"tkn": ""}`,
			`{"tkn": null}`,
			`{"tkn": {"scope": "read"}}`,
			`{"tkn": {"handler": "github", "extra": "bad"}}`,
			`{"tkn": {"handler": "github", "scope": 123}}`,
			`{"scrt": "npm-token", "tkn": "github"}`,
		}
		for _, sample := range badJSON {
			var vr ValueRef
			err := json.Unmarshal([]byte(sample), &vr)
			require.Error(t, err, "expected error for JSON: %s", sample)
		}
	})

	t.Run("Kubernetes pod secret literal does not collide with scrt", func(t *testing.T) {
		k8sYAML := `
volumes:
  - name: tls
    secret:
      secretName: my-tls-cert
`
		var vr ValueRef
		require.NoError(t, yaml.Unmarshal([]byte(k8sYAML), &vr))
		assert.False(t, vr.ContainsCredentialRef())
		res, err := vr.Resolve(context.Background(), nil, nil)
		require.NoError(t, err)
		m, ok := res.(map[string]any)
		require.True(t, ok)
		assert.Contains(t, m, "volumes")
	})
}

func TestValueRef_ResolveCredentialsAndTrackRedaction(t *testing.T) {
	fake := &fakeCredResolver{
		secrets: map[string]string{"npm-token": "npm_super_secret_9876"},
		tokens:  map[string]string{"gcp": "ya29.gcp_access_token_5432"},
	}
	tr := redact.NewTracker()
	ctx := WithCredentialResolver(context.Background(), fake)
	ctx = redact.WithTracker(ctx, tr)

	envSpec := `
CLOUDSDK_AUTH_ACCESS_TOKEN:
  tkn:
    handler: gcp
    scope: https://www.googleapis.com/auth/cloud-platform
NPM_TOKEN:
  scrt: npm-token
PLAIN: hello
`
	var vr ValueRef
	require.NoError(t, yaml.Unmarshal([]byte(envSpec), &vr))
	assert.True(t, vr.ContainsCredentialRef())
	assert.Equal(t, "scrt:npm-token, tkn:gcp", vr.CredentialSummary())
	assert.Equal(t, []string{"gcp"}, vr.ReferencedAuthHandlers())

	resolved, err := vr.Resolve(ctx, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"CLOUDSDK_AUTH_ACCESS_TOKEN": "ya29.gcp_access_token_5432",
		"NPM_TOKEN":                  "npm_super_secret_9876",
		"PLAIN":                      "hello",
	}, resolved)

	// Both resolved credentials must now be registered in the redaction tracker.
	assert.Equal(t, "deploying [REDACTED] with [REDACTED]", tr.RedactString("deploying npm_super_secret_9876 with ya29.gcp_access_token_5432"))

	// PlaceholderCredentialResolver emits safe symbolic placeholders without touching stores.
	placeholderCtx := WithCredentialResolver(context.Background(), PlaceholderCredentialResolver())
	preview, err := vr.Resolve(placeholderCtx, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"CLOUDSDK_AUTH_ACCESS_TOKEN": "<redacted:tkn:gcp>",
		"NPM_TOKEN":                  "<redacted:scrt:npm-token>",
		"PLAIN":                      "hello",
	}, preview)
}
