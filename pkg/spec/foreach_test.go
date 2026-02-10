// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestForEachClause_Defaults(t *testing.T) {
	f := ForEachClause{}

	assert.Equal(t, "", f.Item)
	assert.Equal(t, "", f.Index)
	assert.Nil(t, f.In)
	assert.Equal(t, 0, f.Concurrency)
	assert.Equal(t, OnErrorBehavior(""), f.OnError)
}

func TestForEachClause_WithValues(t *testing.T) {
	inRef := &ValueRef{Literal: []string{"a", "b", "c"}}

	f := ForEachClause{
		Item:        "element",
		Index:       "i",
		In:          inRef,
		Concurrency: 5,
		OnError:     OnErrorContinue,
	}

	assert.Equal(t, "element", f.Item)
	assert.Equal(t, "i", f.Index)
	assert.Equal(t, inRef, f.In)
	assert.Equal(t, 5, f.Concurrency)
	assert.Equal(t, OnErrorContinue, f.OnError)
}
