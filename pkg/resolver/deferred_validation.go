// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"fmt"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// DeferredValidationSummary reports the outcome of the post-resolution deferred
// (cross-resolver) validation phase. It is attached to the execution context so
// collect-errors-mode callers (for example `run resolver`) can inspect and gate
// on deferred validation without treating it as a fatal error.
type DeferredValidationSummary struct {
	// Evaluated is the number of deferred rules whose provider was invoked.
	Evaluated int
	// Failed is the number of deferred rules that failed.
	Failed int
	// Skipped is the number of deferred rules that did not run (owning resolver
	// had no value, or a phase/rule when condition evaluated false).
	Skipped int
	// Results holds the per-resolver failures, ordered root-cause-first.
	Results []DeferredValidationFailure
	// Suppressed lists resolvers whose deferred rules were suppressed because a
	// referenced resolver already failed its own deferred validation.
	Suppressed []string
	// Cycles lists cross-validation cycles detected in the reporting graph. These
	// are informational (tolerated), not errors.
	Cycles [][]string
}

// HasFailures reports whether any deferred validation rule failed.
func (s *DeferredValidationSummary) HasFailures() bool {
	return s != nil && s.Failed > 0
}

// DeferredValidationResultFromContext returns the deferred validation summary
// attached to ctx by Execute, if any. Callers use it to gate state persistence
// and actions on deferred validation results in non-fatal modes.
func DeferredValidationResultFromContext(ctx context.Context) (*DeferredValidationSummary, bool) {
	rc, ok := FromContext(ctx)
	if !ok {
		return nil, false
	}
	s := rc.DeferredValidation()
	return s, s != nil
}

// runDeferredValidation evaluates every resolver's deferred (cross-resolver)
// validation rules after all resolver phases have completed. Rules are evaluated
// in root-cause-first order so upstream failures surface before, and suppress,
// the cascaded failures they cause.
//
// Semantics (decision D2):
//   - If the owning resolver was skipped or produced no usable value, its own
//     deferred rules are skipped.
//   - If a deferred rule references a resolver that produced no usable value
//     (skipped/errored/absent), every deferred rule for that owner fails closed
//     with a clear message.
//   - If a referenced resolver already failed its own deferred validation, the
//     owner's deferred rules are suppressed (the referenced resolver is the root
//     cause) rather than emitting duplicate failures.
func (e *Executor) runDeferredValidation(
	ctx context.Context,
	units []DeferredValidationUnit,
	resolverMap map[string]*Resolver,
	failed *sync.Map,
) *DeferredValidationSummary {
	lgr := logger.FromContext(ctx)
	resolverCtx, _ := FromContext(ctx)

	plan := buildDeferredReportingPlan(units)
	unitByName := make(map[string]DeferredValidationUnit, len(units))
	for _, u := range units {
		unitByName[u.ResolverName] = u
	}

	summary := &DeferredValidationSummary{Cycles: plan.Cycles}
	deferredFailed := make(map[string]bool) // resolvers that failed their own deferred validation

	for _, name := range plan.Order {
		unit := unitByName[name]
		r := resolverMap[name]
		if r == nil || r.Validate == nil {
			continue
		}
		ruleCount := len(unit.RuleIndices)

		// The owning resolver must itself have produced a usable value.
		ownResult, ok := resolverCtx.GetResult(name)
		if !ok || ownResult == nil || !resolverHasUsableValue(ownResult, failed, name) {
			summary.Skipped += ruleCount
			lgr.V(1).Info("skipping deferred validation: owning resolver produced no value", "resolver", name)
			continue
		}
		value := ownResult.Value

		// Cascade suppression: if a referenced resolver already failed its own
		// deferred validation, that is the root cause -- suppress this resolver's
		// rules rather than reporting a duplicate downstream failure.
		if upstream := firstDeferredFailure(unit.DependsOn, deferredFailed); upstream != "" {
			summary.Suppressed = append(summary.Suppressed, name)
			summary.Skipped += ruleCount
			lgr.V(1).Info("suppressing deferred validation due to upstream failure",
				"resolver", name, "upstream", upstream)
			continue
		}

		// Strict fail-closed: a referenced resolver with no usable value makes the
		// cross-resolver rule unevaluable.
		if missing := firstValuelessRef(unit.DependsOn, resolverCtx, failed); missing != "" {
			rvf := DeferredValidationFailure{ResolverName: name, Sensitive: r.Sensitive}
			for _, idx := range unit.RuleIndices {
				// The referenced resolver produced no value, so no provider is
				// invoked -- this is a failure, not an evaluation.
				summary.Failed++
				rvf.Failures = append(rvf.Failures, ValidationFailure{
					Rule:      idx,
					Provider:  r.Validate.With[idx].Provider,
					Message:   fmt.Sprintf("deferred validation references resolver %q which did not produce a value", missing),
					Sensitive: r.Sensitive,
				})
			}
			summary.Results = append(summary.Results, rvf)
			deferredFailed[name] = true
			continue
		}

		// Phase-level when gates the deferred rules too. It is evaluated here (not
		// inline) so it can reference foreign resolvers when needed.
		if r.Validate.When != nil {
			shouldExecute, err := e.evaluateConditionWithSelf(ctx, r.Validate.When, value)
			if err != nil {
				// The phase-level when failed to evaluate, so no provider ran for any
				// rule in this block. Record one failure per affected rule so the
				// summary counters stay consistent with the detailed Results and each
				// failure is attributable to a specific rule/provider.
				rvf := DeferredValidationFailure{ResolverName: name, Sensitive: r.Sensitive}
				for _, idx := range unit.RuleIndices {
					summary.Failed++
					rvf.Failures = append(rvf.Failures, ValidationFailure{
						Rule:      idx,
						Provider:  r.Validate.With[idx].Provider,
						Message:   fmt.Sprintf("failed to evaluate deferred validate phase when condition: %v", err),
						Cause:     err,
						Sensitive: r.Sensitive,
					})
				}
				summary.Results = append(summary.Results, rvf)
				deferredFailed[name] = true
				continue
			}
			if !shouldExecute {
				summary.Skipped += ruleCount
				lgr.V(1).Info("skipping deferred validation due to phase when condition", "resolver", name)
				continue
			}
		}

		rvf := DeferredValidationFailure{ResolverName: name, Sensitive: r.Sensitive}
		for _, idx := range unit.RuleIndices {
			validation := r.Validate.With[idx]
			calls, failure, err := e.evaluateValidationRule(ctx, idx, r.Sensitive, validation, value)
			if err != nil {
				// A fatal rule-evaluation error (for example a bad when condition)
				// is recorded as a failure so it is surfaced to the author. No
				// provider was invoked, so it is not counted as evaluated.
				failure = &ValidationFailure{
					Rule:      idx,
					Provider:  validation.Provider,
					Message:   err.Error(),
					Cause:     err,
					Sensitive: r.Sensitive,
				}
			}
			switch {
			case calls > 0:
				// The provider was invoked for this rule.
				summary.Evaluated++
			case failure == nil:
				// The rule's own when condition evaluated false; nothing ran.
				summary.Skipped++
			}
			if failure == nil {
				continue
			}
			summary.Failed++
			rvf.Failures = append(rvf.Failures, *failure)
			lgr.V(1).Info("deferred validation rule failed",
				"resolver", name, "rule", idx+1,
				logKeyProvider, validation.Provider,
				"message", redactForLog(failure.Message, r.Sensitive))
		}
		if len(rvf.Failures) > 0 {
			summary.Results = append(summary.Results, rvf)
			deferredFailed[name] = true
		}
	}

	return summary
}

// resolverHasUsableValue reports whether a resolver produced a value that a
// deferred validation rule can read. A skipped resolver has no value; a failed
// resolver has a usable value only when the failure was a (partial-emission)
// validation failure rather than a resolve/transform failure.
func resolverHasUsableValue(result *ExecutionResult, failed *sync.Map, name string) bool {
	switch result.Status {
	case ExecutionStatusSkipped:
		return false
	case ExecutionStatusSuccess:
		return true
	case ExecutionStatusFailed:
		if kindVal, ok := failed.Load(name); ok {
			if kind, ok := kindVal.(resolverFailureKind); ok {
				return kind == failureValidation
			}
		}
		return false
	default:
		return false
	}
}

// firstDeferredFailure returns the first referenced resolver (in the given order)
// that has already failed its own deferred validation, or "" if none.
func firstDeferredFailure(refs []string, deferredFailed map[string]bool) string {
	for _, ref := range refs {
		if deferredFailed[ref] {
			return ref
		}
	}
	return ""
}

// firstValuelessRef returns the first referenced resolver that did not produce a
// usable value, or "" if every reference has one.
func firstValuelessRef(refs []string, resolverCtx *Context, failed *sync.Map) string {
	for _, ref := range refs {
		res, ok := resolverCtx.GetResult(ref)
		if !ok || res == nil || !resolverHasUsableValue(res, failed, ref) {
			return ref
		}
	}
	return ""
}
