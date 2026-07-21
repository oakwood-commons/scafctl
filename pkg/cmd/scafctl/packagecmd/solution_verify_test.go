// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPackageVerifyDecision exercises the producer verification policy applied
// to a built bundle: fail on completeness errors by default, and (under
// --strict) also fail on completeness warnings.
func TestPackageVerifyDecision(t *testing.T) {
	clean := &bundler.VerifyResult{Successes: []string{"a.yaml"}}
	incomplete := &bundler.VerifyResult{Errors: []bundler.VerifyError{{Path: "a.yaml", Reason: "missing"}}}
	warned := &bundler.VerifyResult{Warnings: []string{"pattern \"x/*\" matches no bundled files"}}

	tests := []struct {
		name     string
		vr       *bundler.VerifyResult
		strict   bool
		wantFail bool
		wantIn   string
	}{
		{
			name:   "clean pass, not strict",
			vr:     clean,
			strict: false,
		},
		{
			name:   "clean pass, strict",
			vr:     clean,
			strict: true,
		},
		{
			name:     "errors -> fail (not strict)",
			vr:       incomplete,
			strict:   false,
			wantFail: true,
			wantIn:   "incomplete: 1 error(s)",
		},
		{
			name:     "errors -> fail (strict)",
			vr:       incomplete,
			strict:   true,
			wantFail: true,
			wantIn:   "incomplete: 1 error(s)",
		},
		{
			name:   "warnings, not strict -> pass",
			vr:     warned,
			strict: false,
		},
		{
			name:     "warnings, strict -> fail",
			vr:       warned,
			strict:   true,
			wantFail: true,
			wantIn:   "1 warning(s) (strict)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := packageVerifyDecision(tc.vr, tc.strict)
			if tc.wantFail {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantIn)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
