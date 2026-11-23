// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type credTestProvider struct {
	mu             sync.Mutex
	receivedInputs []map[string]any
	streamChunks   []string
	failWithSecret bool
}

func (p *credTestProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         "cred-exec",
		Version:      semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{provider.CapabilityAction},
	}
}

func (p *credTestProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	m, _ := input.(map[string]any)
	p.mu.Lock()
	p.receivedInputs = append(p.receivedInputs, m)
	p.mu.Unlock()

	if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil && streams.Out != nil {
		for _, chunk := range p.streamChunks {
			_, _ = streams.Out.Write([]byte(chunk))
		}
	}

	envMap, _ := m["env"].(map[string]any)
	tok, _ := envMap["GH_TOKEN"].(string)
	if p.failWithSecret {
		return nil, fmt.Errorf("command failed with Authorization: Bearer %s", tok)
	}

	return &provider.Output{
		Data: map[string]any{
			"stdout": "published with " + tok,
		},
		Warnings: []string{"used token " + tok},
		Metadata: map[string]any{"header": "Bearer " + tok},
		Streamed: len(p.streamChunks) > 0,
	}, nil
}

type credTestRegistry struct {
	providers map[string]provider.Provider
}

func (r *credTestRegistry) Get(name string) (provider.Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *credTestRegistry) Has(name string) bool {
	_, ok := r.providers[name]
	return ok
}

type rotatingCredResolver struct {
	mu      sync.Mutex
	secrets map[string]string
	tokens  map[string]string
	calls   int
}

func (r *rotatingCredResolver) ResolveSecret(_ context.Context, name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.secrets[name], nil
}

func (r *rotatingCredResolver) ResolveToken(_ context.Context, req spec.TokenRef) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.tokens[req.Handler], nil
}

func TestActionCredentials_DeferralRedactionAndFingerprints(t *testing.T) {
	workflowYAML := `
actions:
  publish:
    provider: cred-exec
    sources:
      - "src.txt"
    inputs:
      command: npm publish
      env:
        NPM_TOKEN:
          scrt: npm-token
        GH_TOKEN:
          tkn: github
`
	var wf Workflow
	require.NoError(t, yaml.Unmarshal([]byte(workflowYAML), &wf))

	// 1. BuildGraph + Render must NOT touch CredentialResolver or serialize secrets.
	graph, err := BuildGraph(context.Background(), &wf, map[string]any{}, nil)
	require.NoError(t, err)
	ea := graph.Actions["publish"]
	require.NotNil(t, ea)
	require.Contains(t, ea.DeferredInputs, "env")
	assert.Equal(t, "scrt:npm-token, tkn:github", ea.DeferredInputs["env"].Summary())

	rendered, err := Render(graph, &RenderOptions{Format: "json", PrettyPrint: true})
	require.NoError(t, err)
	assert.Contains(t, string(rendered), "scrt:npm-token, tkn:github")

	// 2. Runtime execution: provider gets raw secrets, while stream chunks,
	// ActionResult.Inputs, ActionResult.Results, and fingerprints get [REDACTED]!
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src.txt"), []byte("v1"), 0o600))

	credProv := &credTestProvider{
		streamChunks: []string{
			"echoing ghp_live_secret_",
			"token_111 and npm_secret_",
			"val_222!\n",
		},
	}
	reg := &credTestRegistry{providers: map[string]provider.Provider{"cred-exec": credProv}}

	credRes := &rotatingCredResolver{
		secrets: map[string]string{"npm-token": "npm_secret_val_222"},
		tokens:  map[string]string{"github": "ghp_live_secret_token_111"},
	}

	t.Setenv("GITHUB_ACTIONS", "true")
	var outBuf, errBuf bytes.Buffer
	ctx := spec.WithCredentialResolver(context.Background(), credRes)
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: &outBuf, ErrOut: &errBuf})

	stateData := state.NewData()
	fpChecker := fingerprint.NewChecker(stateData)

	exec := NewExecutor(
		WithRegistry(reg),
		WithCwd(tmpDir),
		WithFingerprintChecker(fpChecker),
	)
	res, err := exec.Execute(ctx, &wf)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, res.FinalStatus)

	// Provider received the real unredacted credentials.
	require.Len(t, credProv.receivedInputs, 1)
	gotEnv, _ := credProv.receivedInputs[0]["env"].(map[string]any)
	assert.Equal(t, "npm_secret_val_222", gotEnv["NPM_TOKEN"])
	assert.Equal(t, "ghp_live_secret_token_111", gotEnv["GH_TOKEN"])

	// GitHub Actions ::add-mask:: emitted before stream, and split stream chunks redacted!
	streamOut := outBuf.String()
	assert.Contains(t, streamOut, "::add-mask::ghp_live_secret_token_111\n")
	assert.Contains(t, streamOut, "::add-mask::npm_secret_val_222\n")
	assert.Contains(t, streamOut, "echoing [REDACTED] and [REDACTED]!\n")

	// ActionResult.Inputs and Results are scrubbed.
	actRes := res.Actions["publish"]
	require.NotNil(t, actRes)
	recordedEnv, _ := actRes.Inputs["env"].(map[string]any)
	assert.Equal(t, "[REDACTED]", recordedEnv["NPM_TOKEN"])
	assert.Equal(t, "[REDACTED]", recordedEnv["GH_TOKEN"])
	recordedResults, _ := actRes.Results.(map[string]any)
	assert.Equal(t, "published with [REDACTED]", recordedResults["stdout"])

	// 3. Second run with a ROTATED JWT token (different iat/exp) still hits fingerprint up-to-date!
	credRes.tokens["github"] = "ghp_rotated_jwt_token_999"
	exec2 := NewExecutor(
		WithRegistry(reg),
		WithCwd(tmpDir),
		WithFingerprintChecker(fingerprint.NewChecker(stateData)),
	)
	ctx2 := spec.WithCredentialResolver(context.Background(), credRes)
	ctx2 = redact.WithTracker(ctx2, redact.NewTracker())
	res2, err := exec2.Execute(ctx2, &wf)
	require.NoError(t, err)
	assert.Equal(t, StatusSkipped, res2.Actions["publish"].Status)
	assert.Equal(t, SkipReasonUpToDate, res2.Actions["publish"].SkipReason)
}

func TestActionCredentials_ErrorScrubbingAndForEach(t *testing.T) {
	workflowYAML := `
actions:
  deploy:
    provider: cred-exec
    forEach:
      in: ["east", "west"]
      item: region
    inputs:
      env:
        REGION:
          expr: region
        GH_TOKEN:
          tkn: github
`
	var wf Workflow
	require.NoError(t, yaml.Unmarshal([]byte(workflowYAML), &wf))

	credProv := &credTestProvider{failWithSecret: true}
	reg := &credTestRegistry{providers: map[string]provider.Provider{"cred-exec": credProv}}
	credRes := &rotatingCredResolver{
		tokens: map[string]string{"github": "ghp_failing_secret_777"},
	}

	ctx := spec.WithCredentialResolver(context.Background(), credRes)
	exec := NewExecutor(WithRegistry(reg))
	res, err := exec.Execute(ctx, &wf)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "ghp_failing_secret_777")
	assert.Contains(t, err.Error(), "[REDACTED]")
	for _, ar := range res.Actions {
		assert.NotContains(t, ar.Error, "ghp_failing_secret_777")
	}
}

func TestActionCredentials_FingerprintInvalidatesOnReferenceOrTokenOptionChange(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "src.txt"), []byte("v1"), 0o600))

	credProv := &credTestProvider{}
	reg := &credTestRegistry{providers: map[string]provider.Provider{"cred-exec": credProv}}
	credRes := &rotatingCredResolver{
		secrets: map[string]string{
			"staging-key":    "scrt_staging_bytes_111",
			"production-key": "scrt_production_bytes_222",
		},
		tokens: map[string]string{"github": "ghp_token_v1"},
	}

	stateData := state.NewData()

	runWithYAML := func(wfYAML string) *ExecutionResult {
		var wf Workflow
		require.NoError(t, yaml.Unmarshal([]byte(wfYAML), &wf))
		ctx := spec.WithCredentialResolver(context.Background(), credRes)
		ctx = redact.WithTracker(ctx, redact.NewTracker())
		exec := NewExecutor(
			WithRegistry(reg),
			WithCwd(tmpDir),
			WithFingerprintChecker(fingerprint.NewChecker(stateData)),
		)
		res, err := exec.Execute(ctx, &wf)
		require.NoError(t, err)
		return res
	}

	baseYAML := `
actions:
  publish:
    provider: cred-exec
    sources:
      - "src.txt"
    inputs:
      env:
        API_KEY:
          scrt: staging-key
        GH_TOKEN:
          tkn:
            handler: github
            scope: read:packages
`
	res1 := runWithYAML(baseYAML)
	assert.Equal(t, StatusSucceeded, res1.Actions["publish"].Status)

	// Identical credential references with rotated token bytes still skip as up-to-date.
	credRes.tokens["github"] = "ghp_token_v2_rotated"
	res2 := runWithYAML(baseYAML)
	assert.Equal(t, StatusSkipped, res2.Actions["publish"].Status)

	// Changing scrt: staging-key -> scrt: production-key must invalidate fingerprint and re-run.
	prodKeyYAML := `
actions:
  publish:
    provider: cred-exec
    sources:
      - "src.txt"
    inputs:
      env:
        API_KEY:
          scrt: production-key
        GH_TOKEN:
          tkn:
            handler: github
            scope: read:packages
`
	res3 := runWithYAML(prodKeyYAML)
	assert.Equal(t, StatusSucceeded, res3.Actions["publish"].Status)

	// Changing tkn.scope must also invalidate fingerprint and re-run.
	writeScopeYAML := `
actions:
  publish:
    provider: cred-exec
    sources:
      - "src.txt"
    inputs:
      env:
        API_KEY:
          scrt: production-key
        GH_TOKEN:
          tkn:
            handler: github
            scope: write:packages
`
	res4 := runWithYAML(writeScopeYAML)
	assert.Equal(t, StatusSucceeded, res4.Actions["publish"].Status)
}

func TestActionCredentials_ResultSchemaValidatesRawDataAndScrubsDiagnostics(t *testing.T) {
	reg := &credTestRegistry{providers: map[string]provider.Provider{"cred-exec": &credTestProvider{}}}
	const longToken = "ghp_valid_twenty_plus_chars_secret_token"
	credRes := &rotatingCredResolver{
		tokens: map[string]string{"github": longToken},
	}

	minLen := 25
	validWF := &Workflow{
		Actions: map[string]*Action{
			"mint": {
				Provider: "cred-exec",
				Inputs: map[string]*spec.ValueRef{
					"env": {
						Literal: map[string]any{
							"GH_TOKEN": map[string]any{"tkn": "github"},
						},
					},
				},
				ResultSchema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"stdout": {
							Type:      "string",
							MinLength: &minLen,
						},
					},
					Required: []string{"stdout"},
				},
			},
		},
	}

	ctx := spec.WithCredentialResolver(context.Background(), credRes)
	exec := NewExecutor(WithRegistry(reg))
	res, err := exec.Execute(ctx, validWF)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, res.Actions["mint"].Status)
	published, _ := res.Actions["mint"].Results.(map[string]any)
	assert.Equal(t, "published with [REDACTED]", published["stdout"])

	// When ResultSchema rejects the raw output (e.g. enum mismatch echoing value),
	// the failure is reported and any echoed credential bytes are scrubbed.
	invalidWF := &Workflow{
		Actions: map[string]*Action{
			"mint_bad": {
				Provider: "cred-exec",
				Inputs: map[string]*spec.ValueRef{
					"env": {
						Literal: map[string]any{
							"GH_TOKEN": map[string]any{"tkn": "github"},
						},
					},
				},
				ResultSchema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"stdout": {
							Type: "string",
							Enum: []any{"expected-constant"},
						},
					},
				},
			},
		},
	}

	ctxBad := spec.WithCredentialResolver(context.Background(), credRes)
	resBad, errBad := NewExecutor(WithRegistry(reg)).Execute(ctxBad, invalidWF)
	require.Error(t, errBad)
	assert.NotContains(t, errBad.Error(), longToken)
	assert.NotContains(t, resBad.Actions["mint_bad"].Error, longToken)
}

func TestActionCredentials_NestedDeferredDependenciesAndCallArgs(t *testing.T) {
	workflowYAML := `
actions:
  build:
    provider: cred-exec
    inputs:
      env:
        LITERAL_LOOKALIKE: "__actions.nonexistent.results.val"
  publish:
    provider: cred-exec
    inputs:
      env:
        GH_TOKEN:
          tkn: github
        TAG:
          expr: __actions.build.results.stdout
        TMPL_TAG:
          tmpl: "{{ .__actions.build.results.stdout }}"
        PLAIN_TEXT: "__actions.phantom.results.val"
  audit:
    call: send_audit
    args:
      payload:
        nested:
          tag:
            expr: __actions.publish.results.stdout
          literalIgnored: "__actions.phantom2.results.val"
`
	var wf Workflow
	require.NoError(t, yaml.Unmarshal([]byte(workflowYAML), &wf))

	reg := &credTestRegistry{providers: map[string]provider.Provider{"cred-exec": &credTestProvider{}}}
	require.NoError(t, ValidateWorkflow(&wf, reg))

	graph, err := BuildGraph(context.Background(), &wf, map[string]any{}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"build"}, graph.Actions["publish"].Dependencies)
	assert.Equal(t, []string{"publish"}, graph.Actions["audit"].Dependencies)
	require.Len(t, graph.ExecutionOrder, 3)
	assert.Equal(t, []string{"build"}, graph.ExecutionOrder[0])
	assert.Equal(t, []string{"publish"}, graph.ExecutionOrder[1])
	assert.Equal(t, []string{"audit"}, graph.ExecutionOrder[2])

	// End-to-end execution with reusable call verifies ordering and resolution.
	calls := map[string]*spec.Call{
		"send_audit": {
			Provider: "cred-exec",
			Args: map[string]*spec.ArgDef{
				"payload": {Type: "any"},
			},
			Inputs: map[string]*spec.ValueRef{
				"env": {
					Literal: map[string]any{
						"GH_TOKEN": map[string]any{"tkn": "github"},
					},
				},
			},
		},
	}
	ctx := spec.WithCredentialResolver(context.Background(), &rotatingCredResolver{
		tokens: map[string]string{"github": "ghp_nested_dep_token_555"},
	})
	exec := NewExecutor(WithRegistry(reg), WithCalls(calls))
	res, err := exec.Execute(ctx, &wf)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, res.FinalStatus)

	// Unknown __actions reference inside nested inline {expr: ...} is caught by ValidateWorkflow.
	var brokenWF Workflow
	require.NoError(t, yaml.Unmarshal([]byte(`
actions:
  publish:
    provider: cred-exec
    inputs:
      env:
        GH_TOKEN:
          tkn: github
        TAG:
          expr: __actions.missing_step.results.stdout
`), &brokenWF))
	require.Error(t, ValidateWorkflow(&brokenWF, reg))
}
