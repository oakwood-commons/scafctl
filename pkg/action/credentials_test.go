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
