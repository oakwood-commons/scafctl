// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnvExecProvider struct{}

func (fakeEnvExecProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         "exec",
		DisplayName:  "Exec",
		Description:  "Execute commands",
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{provider.CapabilityAction, provider.CapabilityFrom},
		Schema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"command": {Type: "string"},
				"env": {
					Type:                 "object",
					AdditionalProperties: &jsonschema.Schema{Type: "string"},
				},
			},
		},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityAction: {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"success": {Type: "boolean"},
				},
			},
			provider.CapabilityFrom: {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"stdout": {Type: "string"},
				},
			},
		},
	}
}

func (fakeEnvExecProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{}, nil
}

func TestLintCredentials_RulesAndNestedInputPlaceholders(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(staticprovider.New()))
	require.NoError(t, reg.Register(fakeEnvExecProvider{}))

	t.Run("valid nested scrt and tkn on exec env and sensitive resolver pass cleanly", func(t *testing.T) {
		raw := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: valid-creds
  version: 1.0.0
spec:
  resolvers:
    authHeader:
      description: Bearer token for internal call
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: github
  workflow:
    actions:
      publish:
        description: Publish package
        provider: exec
        inputs:
          command: npm publish
          env:
            NPM_TOKEN:
              scrt: npm-token
            GCP_TOKEN:
              tkn:
                handler: gcp
                scope: https://www.googleapis.com/auth/cloud-platform
                profile: work
                minValidFor: 5m
            HEADER:
              rslvr: authHeader
`
		var sol solution.Solution
		require.NoError(t, sol.FromYAML([]byte(raw)))

		res := Solution(&sol, "solution.yaml", reg)
		for _, f := range res.Findings {
			assert.NotEqual(t, "invalid-provider-input-type", f.RuleName, "unexpected finding: %+v", f)
			assert.NotEqual(t, "invalid-credential-ref", f.RuleName, "unexpected finding: %+v", f)
			assert.NotEqual(t, "unmarked-sensitive-credential-resolver", f.RuleName, "unexpected finding: %+v", f)
		}
		assert.Equal(t, 0, res.ErrorCount)
	})

	t.Run("detects invalid-credential-ref and unmarked-sensitive-credential-resolver", func(t *testing.T) {
		raw := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-creds
  version: 1.0.0
spec:
  resolvers:
    leakyToken:
      description: Unmarked credential resolver
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: scafctl.auth.github.token
  workflow:
    actions:
      deploy:
        description: Deploy with bad tkn options
        provider: exec
        inputs:
          command: echo ok
          env:
            BAD_SECRET:
              scrt: ../traversal
            BAD_TOKEN:
              tkn:
                handler: INVALID_UPPER
                profile: bad@profile
                minValidFor: -10s
`
		var sol solution.Solution
		require.NoError(t, sol.FromYAML([]byte(raw)))

		res := Solution(&sol, "solution.yaml", reg)
		var invalidCredCount, unmarkedCount int
		for _, f := range res.Findings {
			switch f.RuleName {
			case "invalid-credential-ref":
				invalidCredCount++
			case "unmarked-sensitive-credential-resolver":
				unmarkedCount++
			}
		}
		assert.GreaterOrEqual(t, invalidCredCount, 4, "expected findings for internal prefix, traversal, handler, profile, and minValidFor")
		assert.Equal(t, 1, unmarkedCount)
	})

	t.Run("flags persist and immutable combined with sensitive or credential refs", func(t *testing.T) {
		raw := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: persisted-secret-lint
  version: 1.0.0
state:
  load:
    provider: file
    inputs:
      path: state.json
  save:
    - extends: load
spec:
  resolvers:
    persistedSens:
      type: string
      persist: true
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: secret
    immutableCred:
      type: string
      immutable: true
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: deploy-key
`
		var sol solution.Solution
		require.NoError(t, sol.FromYAML([]byte(raw)))

		res := Solution(&sol, "solution.yaml", reg)
		var persistedSensCount int
		for _, f := range res.Findings {
			if f.RuleName == "persisted-sensitive-resolver" {
				persistedSensCount++
			}
		}
		assert.Equal(t, 2, persistedSensCount)
	})
}
