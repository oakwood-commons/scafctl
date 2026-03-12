// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Compile-time checks: ensure the observer interfaces are subsets of ProgressCallback.
var (
	_ Observer      = (ProgressCallback)(nil)
	_ PhaseObserver = (ProgressCallback)(nil)
	_ RetryObserver = (ProgressCallback)(nil)
)

// Compile-time checks: ensure NoOpProgressCallback satisfies all observer interfaces.
var (
	_ Observer         = NoOpProgressCallback{}
	_ PhaseObserver    = NoOpProgressCallback{}
	_ RetryObserver    = NoOpProgressCallback{}
	_ ProgressCallback = NoOpProgressCallback{}
)

// testActionObserver only implements Observer.
type testActionObserver struct {
	started   []string
	completed []string
	failed    []string
	skipped   []string
	timedOut  []string
	cancelled []string
}

func (o *testActionObserver) OnActionStart(name string) { o.started = append(o.started, name) }
func (o *testActionObserver) OnActionComplete(name string, _ any) {
	o.completed = append(o.completed, name)
}
func (o *testActionObserver) OnActionFailed(name string, _ error) { o.failed = append(o.failed, name) }
func (o *testActionObserver) OnActionSkipped(name, _ string)      { o.skipped = append(o.skipped, name) }
func (o *testActionObserver) OnActionTimeout(name string, _ time.Duration) {
	o.timedOut = append(o.timedOut, name)
}
func (o *testActionObserver) OnActionCancelled(name string) { o.cancelled = append(o.cancelled, name) }

// testPhaseObserver only implements PhaseObserver.
type testPhaseObserver struct {
	phases         []int
	phasesComplete []int
	finallyStarted bool
	finallyDone    bool
}

func (o *testPhaseObserver) OnPhaseStart(phase int, _ []string) { o.phases = append(o.phases, phase) }
func (o *testPhaseObserver) OnPhaseComplete(phase int) {
	o.phasesComplete = append(o.phasesComplete, phase)
}
func (o *testPhaseObserver) OnFinallyStart()    { o.finallyStarted = true }
func (o *testPhaseObserver) OnFinallyComplete() { o.finallyDone = true }

// testRetryObserver only implements RetryObserver.
type testRetryObserver struct {
	retries  int
	progress []int
}

func (o *testRetryObserver) OnRetryAttempt(_ string, _, _ int, _ error) { o.retries++ }
func (o *testRetryObserver) OnForEachProgress(_ string, completed, _ int) {
	o.progress = append(o.progress, completed)
}

func TestActionObserverInterface(t *testing.T) {
	obs := &testActionObserver{}
	var iface Observer = obs // compile-time check

	iface.OnActionStart("deploy")
	iface.OnActionComplete("deploy", nil)
	iface.OnActionFailed("build", nil)
	iface.OnActionSkipped("optional", "condition")
	iface.OnActionTimeout("slow", 5*time.Second)
	iface.OnActionCancelled("aborted")

	assert.Equal(t, []string{"deploy"}, obs.started)
	assert.Equal(t, []string{"deploy"}, obs.completed)
	assert.Equal(t, []string{"build"}, obs.failed)
	assert.Equal(t, []string{"optional"}, obs.skipped)
	assert.Equal(t, []string{"slow"}, obs.timedOut)
	assert.Equal(t, []string{"aborted"}, obs.cancelled)
}

func TestPhaseObserverInterface(t *testing.T) {
	obs := &testPhaseObserver{}
	var iface PhaseObserver = obs

	iface.OnPhaseStart(0, []string{"build"})
	iface.OnPhaseComplete(0)
	iface.OnFinallyStart()
	iface.OnFinallyComplete()

	assert.Equal(t, []int{0}, obs.phases)
	assert.Equal(t, []int{0}, obs.phasesComplete)
	assert.True(t, obs.finallyStarted)
	assert.True(t, obs.finallyDone)
}

func TestRetryObserverInterface(t *testing.T) {
	obs := &testRetryObserver{}
	var iface RetryObserver = obs

	iface.OnRetryAttempt("deploy", 1, 3, nil)
	iface.OnRetryAttempt("deploy", 2, 3, nil)
	iface.OnForEachProgress("batch", 5, 10)

	assert.Equal(t, 2, obs.retries)
	assert.Equal(t, []int{5}, obs.progress)
}

func TestProgressCallbackSatisfiesAllObservers(t *testing.T) {
	// NoOpProgressCallback satisfies all three observer interfaces
	var noop ProgressCallback = NoOpProgressCallback{}

	// Should be assignable to all narrower interfaces
	var _ Observer = noop
	var _ PhaseObserver = noop
	var _ RetryObserver = noop

	// Call all methods without panic
	noop.OnActionStart("a")
	noop.OnActionComplete("a", nil)
	noop.OnActionFailed("a", nil)
	noop.OnActionSkipped("a", "reason")
	noop.OnActionTimeout("a", time.Second)
	noop.OnActionCancelled("a")
	noop.OnRetryAttempt("a", 1, 3, nil)
	noop.OnForEachProgress("a", 1, 10)
	noop.OnPhaseStart(0, nil)
	noop.OnPhaseComplete(0)
	noop.OnFinallyStart()
	noop.OnFinallyComplete()
}
