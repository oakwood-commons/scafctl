// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyResult_Passed_NoErrors(t *testing.T) {
	r := &VerifyResult{}
	assert.True(t, r.Passed())
}

func TestVerifyResult_Passed_WithErrors(t *testing.T) {
	r := &VerifyResult{
		Errors: []VerifyError{{Path: "file.yaml", Reason: "missing"}},
	}
	assert.False(t, r.Passed())
}

func TestVerifyResult_Passed_OnlyWarnings(t *testing.T) {
	r := &VerifyResult{Warnings: []string{"some warning"}}
	assert.True(t, r.Passed())
}
