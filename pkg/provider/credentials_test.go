// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubProviderCredResolver struct {
	secrets map[string]string
	tokens  map[string]string
}

func (s *stubProviderCredResolver) ResolveSecret(_ context.Context, name string) (string, error) {
	v, ok := s.secrets[name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", name)
	}
	return v, nil
}

func (s *stubProviderCredResolver) ResolveToken(_ context.Context, req spec.TokenRef) (string, error) {
	v, ok := s.tokens[req.Handler]
	if !ok {
		return "", fmt.Errorf("handler %q not found", req.Handler)
	}
	return v, nil
}

type echoingTestProvider struct {
	failWithSecret bool
	received       map[string]any
}

func (p *echoingTestProvider) Descriptor() *Descriptor {
	return &Descriptor{
		Name:         "echo-cred",
		DisplayName:  "Echo Cred",
		Description:  "Test provider for credential resolution and stream redaction",
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Capabilities: []Capability{CapabilityFrom, CapabilityAction},
		Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
			"token": schemahelper.StringProp("Token input"),
			"env":   schemahelper.AnyProp("Nested env map"),
		}),
		OutputSchemas: map[Capability]*jsonschema.Schema{
			CapabilityFrom: {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"echo": {Type: "string"},
				},
			},
			CapabilityAction: {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"success": {Type: "boolean"},
				},
			},
		},
	}
}

func (p *echoingTestProvider) Execute(ctx context.Context, input any) (*Output, error) {
	m, _ := input.(map[string]any)
	p.received = m
	tok, _ := m["token"].(string)

	if streams, ok := IOStreamsFromContext(ctx); ok && streams != nil && streams.Out != nil {
		_, _ = streams.Out.Write([]byte("streamed=" + tok[:6]))
		_, _ = streams.Out.Write([]byte(tok[6:] + "\n"))
	}

	if p.failWithSecret {
		return nil, fmt.Errorf("failed authenticating with %s", tok)
	}

	return &Output{
		Data: map[string]any{
			"echo":    "used " + tok,
			"success": true,
		},
		Warnings: []string{"warn " + tok},
		Metadata: map[string]any{"metaTok": tok},
	}, nil
}

func TestInputResolver_CredentialsAndExclusivity(t *testing.T) {
	t.Parallel()

	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"secretVal": schemahelper.StringProp("secret"),
		"tokenVal":  schemahelper.StringProp("token"),
		"nested":    schemahelper.AnyProp("nested"),
	})

	credRes := &stubProviderCredResolver{
		secrets: map[string]string{"api-key": "scrt_val_123456"},
		tokens:  map[string]string{"github": "gho_val_654321"},
	}
	tracker := redact.NewTracker()
	ctx := spec.WithCredentialResolver(context.Background(), credRes)
	ctx = redact.WithTracker(ctx, tracker)

	ir := NewInputResolver(ctx, schema, nil)
	resolved, err := ir.ResolveInputs(map[string]any{
		"secretVal": map[string]any{"scrt": "api-key"},
		"tokenVal": map[string]any{
			"tkn": map[string]any{
				"handler":     "github",
				"minValidFor": "5m",
			},
		},
		"nested": map[string]any{
			"headers": map[string]any{
				"X-Key": map[string]any{"scrt": "api-key"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "scrt_val_123456", resolved["secretVal"])
	assert.Equal(t, "gho_val_654321", resolved["tokenVal"])
	nestedMap := resolved["nested"].(map[string]any)
	headersMap := nestedMap["headers"].(map[string]any)
	assert.Equal(t, "scrt_val_123456", headersMap["X-Key"])
	assert.Equal(t, "REDACTED", tracker.RedactString("scrt_val_123456")[1:len("[REDACTED]")-1])

	// Reject mixed keys alongside scrt or tkn, and multiple form keys.
	_, err = ir.ResolveInputs(map[string]any{
		"secretVal": map[string]any{"scrt": "api-key", "extra": "bad"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "single key")

	_, err = ir.ResolveInputs(map[string]any{
		"secretVal": map[string]any{"scrt": "api-key", "tkn": "github"},
	})
	require.Error(t, err)
}

func TestExecutorAndRunProvider_CredentialsRedaction(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	credRes := &stubProviderCredResolver{
		secrets: map[string]string{"deploy-key": "super_secret_prov_token_777"},
		tokens:  map[string]string{"github": "gho_prov_token_888"},
	}

	t.Run("RunProvider live scrubs Data Warnings Metadata and streams", func(t *testing.T) {
		prov := &echoingTestProvider{}
		var outBuf, errBuf bytes.Buffer
		ctx := spec.WithCredentialResolver(context.Background(), credRes)
		ctx = WithIOStreams(ctx, &IOStreams{Out: &outBuf, ErrOut: &errBuf})

		res, err := RunProvider(ctx, RunOptions{
			Provider: prov,
			Inputs: map[string]any{
				"token": map[string]any{"scrt": "deploy-key"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "super_secret_prov_token_777", prov.received["token"])

		// Streamed output has ::add-mask:: + redacted stream!
		assert.Contains(t, outBuf.String(), "::add-mask::super_secret_prov_token_777\n")
		assert.Contains(t, outBuf.String(), "streamed=[REDACTED]\n")

		// RunResult fields are scrubbed!
		dataMap := res.Data.(map[string]any)
		assert.Equal(t, "used [REDACTED]", dataMap["echo"])
		assert.Equal(t, []string{"warn [REDACTED]"}, res.Warnings)
		assert.Equal(t, "[REDACTED]", res.Metadata["metaTok"])
	})

	t.Run("RunProvider DryRun uses PlaceholderCredentialResolver", func(t *testing.T) {
		prov := &echoingTestProvider{}
		// Deliberately pass NO CredentialResolver in context -- DryRun must install placeholder!
		res, err := RunProvider(context.Background(), RunOptions{
			Provider: prov,
			DryRun:   true,
			Inputs: map[string]any{
				"token": map[string]any{"tkn": "github"},
			},
		})
		require.NoError(t, err)
		assert.True(t, res.DryRun)
		assert.Equal(t, "<redacted:tkn:github>", prov.received["token"])
	})

	t.Run("RunProvider scrubs returned error messages", func(t *testing.T) {
		prov := &echoingTestProvider{failWithSecret: true}
		ctx := spec.WithCredentialResolver(context.Background(), credRes)
		_, err := RunProvider(ctx, RunOptions{
			Provider: prov,
			Inputs: map[string]any{
				"token": map[string]any{"scrt": "deploy-key"},
			},
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "super_secret_prov_token_777")
		assert.Contains(t, err.Error(), "[REDACTED]")
	})

	t.Run("Fallback streams scrub stdout and stderr when IOStreams absent or partially nil outside CI", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "false")
		prov := &echoingTestProvider{}

		// Case 1: No IOStreams in context at all.
		var fbOut, fbErr bytes.Buffer
		ctxNoStreams := spec.WithCredentialResolver(context.Background(), credRes)
		ctxNoStreams = WithFallbackWriters(ctxNoStreams, &fbOut, &fbErr)
		_, err := RunProvider(ctxNoStreams, RunOptions{
			Provider: prov,
			Inputs: map[string]any{
				"token": map[string]any{"scrt": "deploy-key"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "streamed=[REDACTED]\n", fbOut.String())
		assert.NotContains(t, fbOut.String(), "super_secret_prov_token_777")

		// Case 2: IOStreams present with nil Out destination.
		fbOut.Reset()
		fbErr.Reset()
		ctxNilOut := spec.WithCredentialResolver(context.Background(), credRes)
		ctxNilOut = WithFallbackWriters(ctxNilOut, &fbOut, &fbErr)
		ctxNilOut = WithIOStreams(ctxNilOut, &IOStreams{Out: nil, ErrOut: &fbErr})
		_, err = RunProvider(ctxNilOut, RunOptions{
			Provider: prov,
			Inputs: map[string]any{
				"token": map[string]any{"scrt": "deploy-key"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "streamed=[REDACTED]\n", fbOut.String())
		assert.NotContains(t, fbOut.String(), "super_secret_prov_token_777")
	})
}

func TestInputResolver_PreResolvedInputsBlocksSecondOrderInjectionAndMasksShortSecretCoercion(t *testing.T) {
	t.Parallel()

	credRes := &stubProviderCredResolver{
		secrets: map[string]string{
			"host-secret": "EXFILTRATED_HOST_SECRET_9999",
			"tiny-pin":    "xy",
		},
		tokens: map[string]string{
			"github": "EXFILTRATED_GH_TOKEN_8888",
		},
	}

	schema := schemahelper.ObjectSchema([]string{"payload"}, map[string]*jsonschema.Schema{
		"payload":  schemahelper.AnyProp("untrusted object"),
		"items":    schemahelper.AnyProp("untrusted array"),
		"multiKey": schemahelper.AnyProp("scaffolded YAML map containing ValueRef keys"),
		"port":     schemahelper.IntProp("coerced port"),
		"withDef":  {Type: "string", Default: []byte(`"default-val"`)},
	})

	baseCtx := spec.WithCredentialResolver(context.Background(), credRes)
	baseCtx = WithResolverContext(baseCtx, map[string]any{"internal": "INTERNAL_RESOLVER_DATA"})
	preCtx := WithPreResolvedInputs(baseCtx)

	untrustedPayload := map[string]any{"scrt": "host-secret"}
	untrustedItems := []any{
		map[string]any{"tkn": "github"},
		map[string]any{"expr": "1 + 1"},
		map[string]any{"tmpl": "{{ .internal }}"},
		map[string]any{"rslvr": "internal"},
	}
	scaffoldedMap := map[string]any{
		"scrt":  "not-a-valueref",
		"expr":  "also-inert",
		"other": "allowed-together",
	}

	ir := NewInputResolver(preCtx, schema, nil)
	resolved, err := ir.ResolveInputs(map[string]any{
		"payload":  untrustedPayload,
		"items":    untrustedItems,
		"multiKey": scaffoldedMap,
		"port":     "8080",
	})
	require.NoError(t, err)
	assert.Equal(t, untrustedPayload, resolved["payload"], "untrusted {scrt:...} map must remain inert literal data")
	assert.Equal(t, untrustedItems, resolved["items"], "nested ValueRef-shaped maps must remain inert literal data")
	assert.Equal(t, scaffoldedMap, resolved["multiKey"], "maps with ValueRef keys plus sibling keys must pass through")
	assert.Equal(t, 8080, resolved["port"], "schema type coercion must still apply")
	assert.Equal(t, "default-val", resolved["withDef"], "schema defaults must still apply")

	// Non-pre-resolved InputResolver masks <4-byte scrt: coercion errors without leaking the short secret.
	rawIR := NewInputResolver(baseCtx, schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"pin": schemahelper.IntProp("numeric pin"),
	}), nil)
	_, err = rawIR.ResolveInputs(map[string]any{
		"pin": map[string]any{"scrt": "tiny-pin"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coercion failed for secret value")
	assert.NotContains(t, err.Error(), "xy")
}
