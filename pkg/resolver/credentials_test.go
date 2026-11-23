// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type stubCredResolver struct {
	secrets map[string]string
	tokens  map[string]string
}

func (s *stubCredResolver) ResolveSecret(_ context.Context, name string) (string, error) {
	return s.secrets[name], nil
}

func (s *stubCredResolver) ResolveToken(_ context.Context, req spec.TokenRef) (string, error) {
	return s.tokens[req.Handler], nil
}

func TestResolverCredentials_SensitiveVsNonSensitiveScrubbing(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(celprovider.NewCelProvider()))

	var sensitiveRes, derivedNonSensitive, directNonSensitive Resolver
	require.NoError(t, yaml.Unmarshal([]byte(`
name: rawToken
sensitive: true
resolve:
  with:
    - provider: static
      inputs:
        value:
          tkn: github
transform:
  with:
    - provider: cel
      inputs:
        expression: "'Bearer ' + __self"
`), &sensitiveRes))

	require.NoError(t, yaml.Unmarshal([]byte(`
name: accidentalEcho
sensitive: false
resolve:
  with:
    - provider: cel
      inputs:
        expression: "'header=' + _.rawToken"
`), &derivedNonSensitive))

	require.NoError(t, yaml.Unmarshal([]byte(`
name: directUnmarked
sensitive: false
resolve:
  with:
    - provider: static
      inputs:
        value:
          scrt: npm-token
`), &directNonSensitive))

	ctx := spec.WithCredentialResolver(context.Background(), &stubCredResolver{
		secrets: map[string]string{"npm-token": "npm_secret_token_4444"},
		tokens:  map[string]string{"github": "gho_secret_token_5555"},
	})

	exec := NewExecutor(reg)
	outCtx, err := exec.Execute(ctx, []*Resolver{&sensitiveRes, &derivedNonSensitive, &directNonSensitive}, nil)
	require.NoError(t, err)

	rctx, ok := FromContext(outCtx)
	require.True(t, ok)

	// sensitive: true resolver retains full unredacted derived value in memory for downstream consumers.
	rawRes, ok := rctx.GetResult("rawToken")
	require.True(t, ok)
	assert.Equal(t, "Bearer gho_secret_token_5555", rawRes.Value)

	// Downstream non-sensitive resolver echoing the derived secret has it scrubbed!
	echoRes, ok := rctx.GetResult("accidentalEcho")
	require.True(t, ok)
	assert.Equal(t, "header=[REDACTED]", echoRes.Value)

	// Non-sensitive resolver reading scrt: directly has its output scrubbed!
	directRes, ok := rctx.GetResult("directUnmarked")
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", directRes.Value)
}

func TestResolverCredentials_PartialEmissionAndValidationErrorsScrubbed(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))

	var badCoercion Resolver
	require.NoError(t, yaml.Unmarshal([]byte(`
name: badCoerce
type: int
sensitive: false
resolve:
  with:
    - provider: static
      inputs:
        value:
          scrt: secret-non-int
`), &badCoercion))

	ctx := spec.WithCredentialResolver(context.Background(), &stubCredResolver{
		secrets: map[string]string{"secret-non-int": "not_an_int_secret_999"},
	})

	exec := NewExecutor(reg)
	outCtx, err := exec.Execute(ctx, []*Resolver{&badCoercion}, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not_an_int_secret_999")

	rctx, ok := FromContext(outCtx)
	require.True(t, ok)
	res, ok := rctx.GetResult("badCoerce")
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", res.Value)
	if res.Error != nil {
		assert.NotContains(t, res.Error.Error(), "not_an_int_secret_999")
	}
}

func TestResolverCredentials_DebugProviderFallbackStreamsScrubbed(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(debugprovider.NewDebugProvider()))

	var stdoutRes, stderrRes Resolver
	require.NoError(t, yaml.Unmarshal([]byte(`
name: debugOut
resolve:
  with:
    - provider: debug
      inputs:
        destination: stdout
        label:
          scrt: fallback-secret
`), &stdoutRes))
	require.NoError(t, yaml.Unmarshal([]byte(`
name: debugErr
resolve:
  with:
    - provider: debug
      inputs:
        destination: stderr
        label:
          tkn: github
`), &stderrRes))

	var fbOut, fbErr bytes.Buffer
	ctx := spec.WithCredentialResolver(context.Background(), &stubCredResolver{
		secrets: map[string]string{"fallback-secret": "scrt_fallback_out_112233"},
		tokens:  map[string]string{"github": "gho_fallback_err_445566"},
	})
	ctx = provider.WithFallbackWriters(ctx, &fbOut, &fbErr)

	// Case 1: No IOStreams in context at all.
	_, err := NewExecutor(reg).Execute(ctx, []*Resolver{&stdoutRes, &stderrRes}, nil)
	require.NoError(t, err)
	assert.Contains(t, fbOut.String(), "[REDACTED]")
	assert.NotContains(t, fbOut.String(), "scrt_fallback_out_112233")
	assert.Contains(t, fbErr.String(), "[REDACTED]")
	assert.NotContains(t, fbErr.String(), "gho_fallback_err_445566")

	// Case 2: IOStreams present with nil individual destinations.
	fbOut.Reset()
	fbErr.Reset()
	ctxPartial := provider.WithIOStreams(ctx, &provider.IOStreams{Out: nil, ErrOut: nil})
	_, err = NewExecutor(reg).Execute(ctxPartial, []*Resolver{&stdoutRes, &stderrRes}, nil)
	require.NoError(t, err)
	assert.NotContains(t, fbOut.String(), "scrt_fallback_out_112233")
	assert.NotContains(t, fbErr.String(), "gho_fallback_err_445566")
}
