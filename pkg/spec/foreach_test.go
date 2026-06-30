// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
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

func condExpr(expr string) *Condition {
	e := celexp.Expression(expr)
	return &Condition{Expr: &e}
}

func TestForEachClause_EffectiveOnError(t *testing.T) {
	tests := []struct {
		name string
		fe   *ForEachClause
		want OnErrorBehavior
	}{
		{"nil clause defaults to fail", nil, OnErrorFail},
		{"empty clause defaults to fail", &ForEachClause{}, OnErrorFail},
		{"literal true continueOnError", &ForEachClause{ContinueOnError: condExpr("true")}, OnErrorContinue},
		{"literal false continueOnError", &ForEachClause{ContinueOnError: condExpr("false")}, OnErrorFail},
		{
			"non-literal CEL falls back to onError",
			&ForEachClause{ContinueOnError: condExpr(`__item != ""`), OnError: OnErrorContinue},
			OnErrorContinue,
		},
		{"deprecated onError continue", &ForEachClause{OnError: OnErrorContinue}, OnErrorContinue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.fe.EffectiveOnError())
		})
	}
}
