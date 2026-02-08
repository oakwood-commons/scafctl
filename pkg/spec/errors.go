// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

// OnErrorBehavior defines how to handle errors during execution.
// It is used by both resolvers and actions to control error propagation.
type OnErrorBehavior string

const (
	// OnErrorFail stops execution and returns the error (default behavior).
	OnErrorFail OnErrorBehavior = "fail"

	// OnErrorContinue continues execution despite errors.
	// For resolvers: tries the next source/step.
	// For actions: continues with remaining iterations or actions.
	OnErrorContinue OnErrorBehavior = "continue"
)

// IsValid returns true if the behavior is a valid OnErrorBehavior value.
func (b OnErrorBehavior) IsValid() bool {
	switch b {
	case OnErrorFail, OnErrorContinue, "":
		return true
	default:
		return false
	}
}

// OrDefault returns the behavior or the default (OnErrorFail) if empty.
func (b OnErrorBehavior) OrDefault() OnErrorBehavior {
	if b == "" {
		return OnErrorFail
	}
	return b
}
