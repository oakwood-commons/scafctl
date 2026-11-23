// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/concepts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSchemaAndKinds_Credentials(t *testing.T) {
	t.Run("valueref and tokenref kinds registered", func(t *testing.T) {
		vrKind, err := GetKind("valueref")
		require.NoError(t, err)
		require.NotNil(t, vrKind)
		assert.Contains(t, vrKind.Description, "scrt")
		assert.Contains(t, vrKind.Description, "tkn")

		tknKind, err := GetKind("tokenref")
		require.NoError(t, err)
		require.NotNil(t, tknKind)
		assert.Contains(t, tknKind.Description, "hostname")
	})

	t.Run("credentials concept registered", func(t *testing.T) {
		c, ok := concepts.Get("credentials")
		require.True(t, ok)
		assert.Equal(t, "security", c.Category)
		assert.Contains(t, c.Explanation, "scrt:")
		assert.Contains(t, c.Explanation, "tkn:")
	})

	t.Run("ValidateSolutionAgainstSchema accepts scrt tkn and literal maps", func(t *testing.T) {
		validYAML := []byte(`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cred-schema-valid
  version: 1.0.0
spec:
  resolvers:
    k8sSpec:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                secret:
                  secretName: my-tls
  workflow:
    actions:
      publish:
        provider: exec
        inputs:
          command: npm publish
          password:
            scrt: registry-token
          token:
            tkn: github
          gcpToken:
            tkn:
              handler: gcp
              scope: https://www.googleapis.com/auth/cloud-platform
              hostname: prod-east
              minValidFor: 5m
          env:
            NPM_TOKEN:
              scrt: npm-token
            GH_TOKEN:
              tkn: github
`)
		var validDoc map[string]any
		require.NoError(t, yaml.Unmarshal(validYAML, &validDoc))
		violations, err := ValidateSolutionAgainstSchema(validDoc)
		require.NoError(t, err)
		assert.Empty(t, violations)
	})

	t.Run("ValidateSolutionAgainstSchema rejects malformed scrt and tkn", func(t *testing.T) {
		invalidCases := []string{
			`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-empty-scrt
spec:
  workflow:
    actions:
      a:
        provider: exec
        inputs:
          password:
            scrt: ""
`,
			`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-tkn-unknown-field
spec:
  workflow:
    actions:
      a:
        provider: exec
        inputs:
          token:
            tkn:
              handler: github
              bogus: nope
`,
			`
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-multi-key-valueref
spec:
  workflow:
    actions:
      a:
        provider: exec
        inputs:
          token:
            scrt: foo
            tkn: github
`,
		}

		for _, sample := range invalidCases {
			var doc map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(sample), &doc))
			violations, err := ValidateSolutionAgainstSchema(doc)
			require.NoError(t, err)
			assert.NotEmpty(t, violations, "expected schema violations for:\n%s", sample)
		}
	})
}
