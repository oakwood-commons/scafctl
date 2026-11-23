// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolution_ReferencedAuthHandlerNames_AllCredentialLocations(t *testing.T) {
	t.Parallel()

	rawYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auth-refs-all
  version: 1.0.0
bundle:
  plugins:
    - name: oci
      kind: provider
      defaults:
        token:
          tkn: bundle-auth
state:
  load:
    provider: http
    inputs:
      headers:
        Authorization:
          tkn:
            handler: state-load-auth
  save:
    - provider: http
      inputs:
        token:
          tkn: state-save-auth
spec:
  calls:
    push_artifact:
      provider: http
      inputs:
        headers:
          X-Call-Token:
            tkn: call-auth
  resolvers:
    res_direct:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: res-auth
            forEach:
              in:
                tkn: foreach-res-auth
      transform:
        with:
          - provider: static
            inputs:
              value:
                tkn:
                  handler: transform-auth
      validate:
        with:
          - provider: validation
            inputs:
              expression: "true"
            message:
              tkn: validate-msg-auth
      messages:
        error:
          tkn: error-msg-auth
  workflow:
    actions:
      deploy:
        provider: exec
        forEach:
          in:
            tkn: foreach-act-auth
        inputs:
          env:
            ACT_TOKEN:
              tkn: action-auth
      invoke_call:
        call: push_artifact
        args:
          extra:
            tkn: call-arg-auth
    finally:
      cleanup:
        provider: exec
        inputs:
          token:
            tkn: finally-auth
`

	var sol Solution
	require.NoError(t, sol.FromYAML([]byte(rawYAML)))

	specHandlers := sol.Spec.ReferencedAuthHandlerNames()
	assert.Equal(t, []string{
		"action-auth",
		"call-arg-auth",
		"call-auth",
		"error-msg-auth",
		"finally-auth",
		"foreach-act-auth",
		"foreach-res-auth",
		"res-auth",
		"transform-auth",
		"validate-msg-auth",
	}, specHandlers)

	solHandlers := sol.ReferencedAuthHandlerNames()
	assert.Equal(t, []string{
		"action-auth",
		"bundle-auth",
		"call-arg-auth",
		"call-auth",
		"error-msg-auth",
		"finally-auth",
		"foreach-act-auth",
		"foreach-res-auth",
		"res-auth",
		"state-load-auth",
		"state-save-auth",
		"transform-auth",
		"validate-msg-auth",
	}, solHandlers)

	var nilSol *Solution
	assert.Nil(t, nilSol.ReferencedAuthHandlerNames())
}
