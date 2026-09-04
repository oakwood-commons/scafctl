// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolCond(expr string) *spec.Condition {
	e := celexp.Expression(expr)
	return &spec.Condition{Expr: &e}
}

func TestEffectiveOnErrorPolicy(t *testing.T) {
	cond := boolCond("true")
	feCond := boolCond("false")

	t.Run("non-forEach uses action-level fields", func(t *testing.T) {
		action := &ExpandedAction{Action: &Action{ContinueOnError: cond, OnError: spec.OnErrorContinue}}
		gotCond, gotFallback := effectiveOnErrorPolicy(action)
		assert.Same(t, cond, gotCond)
		assert.Equal(t, spec.OnErrorContinue, gotFallback)
	})

	t.Run("forEach continueOnError wins over action-level", func(t *testing.T) {
		action := &ExpandedAction{
			Action:          &Action{ContinueOnError: cond, ForEach: &spec.ForEachClause{ContinueOnError: feCond}},
			ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 0},
		}
		gotCond, _ := effectiveOnErrorPolicy(action)
		assert.Same(t, feCond, gotCond)
	})

	t.Run("forEach deprecated onError wins over action-level", func(t *testing.T) {
		action := &ExpandedAction{
			Action:          &Action{ContinueOnError: cond, ForEach: &spec.ForEachClause{OnError: spec.OnErrorContinue}}, //nolint:staticcheck // intentionally exercises the deprecated field's precedence behavior
			ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 1},
		}
		gotCond, gotFallback := effectiveOnErrorPolicy(action)
		assert.Nil(t, gotCond)
		assert.Equal(t, spec.OnErrorContinue, gotFallback)
	})

	t.Run("forEach without policy falls back to action-level", func(t *testing.T) {
		action := &ExpandedAction{
			Action:          &Action{ContinueOnError: cond, ForEach: &spec.ForEachClause{}},
			ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 2},
		}
		gotCond, _ := effectiveOnErrorPolicy(action)
		assert.Same(t, cond, gotCond)
	})
}

func TestContinueOnErrorVars(t *testing.T) {
	e := &Executor{}

	t.Run("binds real attempt and maxAttempts", func(t *testing.T) {
		action := &ExpandedAction{Action: &Action{}}
		vars := e.continueOnErrorVars(action, errors.New("boom"), 3, 5)
		errMap, ok := vars[celexp.VarError].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, 3, errMap["attempt"])
		assert.Equal(t, 5, errMap["maxAttempts"])
		assert.NotContains(t, vars, celexp.VarItem)
	})

	t.Run("includes forEach iteration vars and aliases", func(t *testing.T) {
		action := &ExpandedAction{
			Action: &Action{ForEach: &spec.ForEachClause{Item: "region", Index: "i"}},
			ForEachMetadata: &ForEachExpansionMetadata{
				ExpandedFrom: "deploy",
				Index:        2,
				Item:         "us-east",
			},
		}
		vars := e.continueOnErrorVars(action, errors.New("boom"), 1, 1)
		assert.Equal(t, "us-east", vars[celexp.VarItem])
		assert.Equal(t, 2, vars[celexp.VarIndex])
		assert.Equal(t, "us-east", vars["region"])
		assert.Equal(t, 2, vars["i"])
	})

	t.Run("nil error omits error context", func(t *testing.T) {
		action := &ExpandedAction{Action: &Action{}}
		vars := e.continueOnErrorVars(action, nil, 1, 1)
		assert.NotContains(t, vars, celexp.VarError)
	})
}

func TestActionShouldContinue_ForEachCEL(t *testing.T) {
	e := &Executor{resolverData: map[string]any{}}
	action := &ExpandedAction{
		Action: &Action{
			ForEach: &spec.ForEachClause{ContinueOnError: boolCond(`__item == "skip" && __error.attempt == 1`)},
		},
		ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 0, Item: "skip"},
	}

	t.Run("matching item and attempt continues", func(t *testing.T) {
		cont, err := e.actionShouldContinue(context.Background(), action, errors.New("boom"), 1, 1)
		require.NoError(t, err)
		assert.True(t, cont)
	})

	t.Run("non-matching item aborts", func(t *testing.T) {
		other := &ExpandedAction{
			Action:          action.Action,
			ForEachMetadata: &ForEachExpansionMetadata{ExpandedFrom: "deploy", Index: 1, Item: "keep"},
		}
		cont, err := e.actionShouldContinue(context.Background(), other, errors.New("boom"), 1, 1)
		require.NoError(t, err)
		assert.False(t, cont)
	})
}
