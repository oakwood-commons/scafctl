// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullVerifyDecision exercises the consumer verification policy applied to a
// pulled bundle: warn-by-default on incompleteness, only fatal under --strict,
// and warnings become fatal only under --strict.
func TestPullVerifyDecision(t *testing.T) {
	clean := &bundler.VerifyResult{Successes: []string{"a.yaml"}}
	incomplete := &bundler.VerifyResult{Errors: []bundler.VerifyError{{Path: "a.yaml", Reason: "missing"}}}
	warned := &bundler.VerifyResult{Warnings: []string{"pattern \"x/*\" matches no bundled files"}}

	tests := []struct {
		name       string
		vr         *bundler.VerifyResult
		strict     bool
		wantFail   bool
		wantFailIn string
		wantWarn   bool
		wantWarnIn string
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
			name:       "errors, not strict -> warn",
			vr:         incomplete,
			strict:     false,
			wantWarn:   true,
			wantWarnIn: "incomplete: 1 error(s) -- run with --strict to fail",
		},
		{
			name:       "errors, strict -> fail",
			vr:         incomplete,
			strict:     true,
			wantFail:   true,
			wantFailIn: "incomplete: 1 error(s) (strict)",
		},
		{
			name:   "warnings, not strict -> silent",
			vr:     warned,
			strict: false,
		},
		{
			name:       "warnings, strict -> fail",
			vr:         warned,
			strict:     true,
			wantFail:   true,
			wantFailIn: "1 warning(s) (strict)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warnMsg, failErr := pullVerifyDecision(tc.vr, tc.strict)

			if tc.wantFail {
				require.Error(t, failErr)
				assert.Contains(t, failErr.Error(), tc.wantFailIn)
				assert.Empty(t, warnMsg, "fail and warn are mutually exclusive")
			} else {
				assert.NoError(t, failErr)
			}

			if tc.wantWarn {
				assert.Contains(t, warnMsg, tc.wantWarnIn)
			} else if !tc.wantFail {
				assert.Empty(t, warnMsg)
			}
		})
	}
}

// TestPullVerifyDecisionNoEmDash ensures the warn message uses ASCII "--" and
// not an em dash, per repo conventions.
func TestPullVerifyDecisionNoEmDash(t *testing.T) {
	vr := &bundler.VerifyResult{Errors: []bundler.VerifyError{{Path: "a.yaml", Reason: "missing"}}}
	warnMsg, _ := pullVerifyDecision(vr, false)
	assert.NotContains(t, warnMsg, "\u2014")
	assert.True(t, strings.Contains(warnMsg, " -- "))
}
