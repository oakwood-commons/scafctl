// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

// ForEachClause defines iteration over an array.
// When present, the associated operation is executed once per array element
// and results are collected into an output array preserving order.
type ForEachClause struct {
	// Item is the variable name alias for the current element.
	// Creates both __item (always) and this custom name.
	// Optional - if not specified, only __item is available.
	Item string `json:"item,omitempty" yaml:"item,omitempty" doc:"Variable name alias for current array element" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"region"`

	// Index is the variable name alias for the current 0-based index.
	// Creates both __index (always) and this custom name.
	// Optional - if not specified, only __index is available.
	Index string `json:"index,omitempty" yaml:"index,omitempty" doc:"Variable name alias for current index" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"i"`

	// In specifies the array to iterate over.
	// Optional - defaults to __self (current transform value) for resolvers.
	In *ValueRef `json:"in,omitempty" yaml:"in,omitempty" doc:"Array to iterate over (default: __self for resolvers)"`

	// Concurrency limits parallel execution.
	// 0 (default) means unlimited parallelism.
	Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty" doc:"Maximum parallel iterations (0=unlimited)" minimum:"0" example:"5"`

	// ContinueOnError controls whether a failed iteration allows execution to
	// continue. It accepts a boolean or a CEL expression evaluated per iteration
	// with the structured error bound as __error and the iteration variables
	// (__item/__index plus any configured aliases) in scope. Truthy continues
	// with the remaining iterations; falsy aborts. Overrides the deprecated
	// OnError field. This field is only used by actions; resolvers ignore it.
	ContinueOnError *Condition `json:"continueOnError,omitempty" yaml:"continueOnError,omitempty" doc:"Whether to continue when an iteration fails (actions only). Accepts a boolean or a CEL expression evaluated per iteration with __error and the iteration variables in scope. Overrides the deprecated onError field."`

	// OnError defines behavior when an iteration fails.
	// This field is only used by actions; resolvers ignore it.
	//
	// Deprecated: use ContinueOnError instead.
	OnError OnErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" deprecated:"true" deprecatedReplacement:"continueOnError" doc:"DEPRECATED: use continueOnError instead. Error handling behavior (actions only)" example:"fail" default:"fail"`

	// KeepSkipped controls whether items skipped by a when condition are
	// retained as nil entries in the output array.
	// By default (false), skipped items are removed so the output contains
	// only the items that were actually processed. Set to true when you need
	// the output array to remain index-aligned with the input array.
	KeepSkipped bool `json:"keepSkipped,omitempty" yaml:"keepSkipped,omitempty" doc:"Retain nil entries for items skipped by when condition (default: false)"`
}

// EffectiveOnError returns the static, render-time error-handling behavior for
// the clause. A literal boolean continueOnError maps directly (true =>
// OnErrorContinue, false => OnErrorFail); a non-literal CEL expression cannot be
// reduced to a static behavior here, so the deprecated OnError enum is used as
// the display value. This method is only a rendering approximation: the
// authoritative per-iteration decision -- including non-literal CEL
// continueOnError expressions evaluated against the iteration's
// __error/__item/__index context -- is made at runtime by the action executor
// (see Executor.actionShouldContinue / effectiveOnErrorPolicy).
func (f *ForEachClause) EffectiveOnError() OnErrorBehavior {
	if f == nil {
		return OnErrorFail
	}
	if f.ContinueOnError != nil && f.ContinueOnError.Expr != nil {
		switch string(*f.ContinueOnError.Expr) {
		case "true":
			return OnErrorContinue
		case "false":
			return OnErrorFail
		}
	}
	return f.OnError.OrDefault()
}
