// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// This package defines only function types (StatFunc, ReadFileFunc).
// These tests verify the types compile and can be assigned real functions.

func TestStatFunc_AssignableFromOsStat(t *testing.T) {
	var fn StatFunc = os.Stat
	assert.NotNil(t, fn)
}

func TestReadFileFunc_AssignableFromOsReadFile(t *testing.T) {
	var fn ReadFileFunc = os.ReadFile
	assert.NotNil(t, fn)
}
