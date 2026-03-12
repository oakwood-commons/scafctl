// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import "time"

// Observer receives lifecycle events for individual actions.
// Implement this interface when you only need to track action-level events
// (start, complete, fail, skip, timeout, cancel).
type Observer interface {
	// OnActionStart is called when an action begins execution.
	OnActionStart(actionName string)

	// OnActionComplete is called when an action completes successfully.
	OnActionComplete(actionName string, results any)

	// OnActionFailed is called when an action fails.
	OnActionFailed(actionName string, err error)

	// OnActionSkipped is called when an action is skipped.
	OnActionSkipped(actionName, reason string)

	// OnActionTimeout is called when an action times out.
	OnActionTimeout(actionName string, timeout time.Duration)

	// OnActionCancelled is called when an action is cancelled.
	OnActionCancelled(actionName string)
}

// PhaseObserver receives lifecycle events for execution phases.
// Implement this interface when you only need to track phase boundaries
// and the finally section.
type PhaseObserver interface {
	// OnPhaseStart is called when a new execution phase begins.
	OnPhaseStart(phase int, actionNames []string)

	// OnPhaseComplete is called when an execution phase completes.
	OnPhaseComplete(phase int)

	// OnFinallyStart is called when the finally section begins.
	OnFinallyStart()

	// OnFinallyComplete is called when the finally section completes.
	OnFinallyComplete()
}

// RetryObserver receives retry and iteration progress events.
// Implement this interface when you only need to track retries and forEach progress.
type RetryObserver interface {
	// OnRetryAttempt is called before each retry attempt.
	OnRetryAttempt(actionName string, attempt, maxAttempts int, err error)

	// OnForEachProgress is called during forEach execution.
	OnForEachProgress(actionName string, completed, total int)
}
