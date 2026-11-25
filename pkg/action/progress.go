// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

type inheritedExecutorConfigKey struct{}

// InheritedExecutorConfig carries execution settings from a parent Executor
// across context boundaries so nested action executors (such as the solution
// provider running a child workflow) inherit timeouts, concurrency limits,
// terminal streams, working directory, and progress reporting.
type InheritedExecutorConfig struct {
	DefaultTimeout   time.Duration
	GracePeriod      time.Duration
	MaxConcurrency   int
	ProgressCallback ProgressCallback
	IOStreams        *provider.IOStreams
	Cwd              string
}

// WithInheritedExecutorConfig returns a derived context carrying cfg.
func WithInheritedExecutorConfig(ctx context.Context, cfg InheritedExecutorConfig) context.Context {
	return context.WithValue(ctx, inheritedExecutorConfigKey{}, cfg)
}

// InheritedExecutorConfigFromContext retrieves the parent executor configuration
// stored in ctx, if present.
func InheritedExecutorConfigFromContext(ctx context.Context) (InheritedExecutorConfig, bool) {
	cfg, ok := ctx.Value(inheritedExecutorConfigKey{}).(InheritedExecutorConfig)
	return cfg, ok
}

// synchronizedProgressCallback serializes all ProgressCallback invocations
// through a single mutex so concurrent calls from parallel parent actions and
// nested sub-solution executors never race on unsynchronized observers.
type synchronizedProgressCallback struct {
	mu    sync.Mutex
	inner ProgressCallback
}

// SynchronizeProgressCallback wraps cb with a mutex so concurrent invocations
// across goroutines and nested executors are serialized. Returns nil when cb is
// nil, and returns cb unchanged when it is already synchronized.
func SynchronizeProgressCallback(cb ProgressCallback) ProgressCallback {
	if cb == nil {
		return nil
	}
	switch cb.(type) {
	case *synchronizedProgressCallback, *PrefixedProgressCallback:
		return cb
	default:
		return &synchronizedProgressCallback{inner: cb}
	}
}

func (s *synchronizedProgressCallback) OnActionStart(actionName, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionStart(actionName, description)
}

func (s *synchronizedProgressCallback) OnActionComplete(actionName string, results any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionComplete(actionName, results)
}

func (s *synchronizedProgressCallback) OnActionFailed(actionName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionFailed(actionName, err)
}

func (s *synchronizedProgressCallback) OnActionSkipped(actionName, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionSkipped(actionName, reason)
}

func (s *synchronizedProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionTimeout(actionName, timeout)
}

func (s *synchronizedProgressCallback) OnActionCancelled(actionName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnActionCancelled(actionName)
}

func (s *synchronizedProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnRetryAttempt(actionName, attempt, maxAttempts, err)
}

func (s *synchronizedProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnForEachProgress(actionName, completed, total)
}

func (s *synchronizedProgressCallback) OnPhaseStart(phase int, actionNames []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnPhaseStart(phase, actionNames)
}

func (s *synchronizedProgressCallback) OnPhaseComplete(phase int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnPhaseComplete(phase)
}

func (s *synchronizedProgressCallback) OnFinallyStart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnFinallyStart()
}

func (s *synchronizedProgressCallback) OnFinallyComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.OnFinallyComplete()
}

// PrefixedProgressCallback forwards action-scoped progress events to an inner
// ProgressCallback with each action name prefixed by "[solution:<name>] ".
// Phase and finally boundary callbacks are no-ops because phase indices and
// finally boundaries are local to the child DAG and carry no action name to
// distinguish them from parent phase boundaries.
type PrefixedProgressCallback struct {
	inner  ProgressCallback
	prefix string
}

// NewSolutionProgressCallback wraps parent so child action events are reported
// with a "[solution:<solutionName>] " prefix and serialized through a mutex.
// Returns nil when parent is nil.
func NewSolutionProgressCallback(parent ProgressCallback, solutionName string) ProgressCallback {
	if parent == nil {
		return nil
	}
	var prefix string
	if solutionName != "" {
		prefix = fmt.Sprintf("[solution:%s] ", solutionName)
	}
	return &PrefixedProgressCallback{
		inner:  SynchronizeProgressCallback(parent),
		prefix: prefix,
	}
}

func (p *PrefixedProgressCallback) formatName(actionName string) string {
	return p.prefix + actionName
}

func (p *PrefixedProgressCallback) OnActionStart(actionName, description string) {
	p.inner.OnActionStart(p.formatName(actionName), description)
}

func (p *PrefixedProgressCallback) OnActionComplete(actionName string, results any) {
	p.inner.OnActionComplete(p.formatName(actionName), results)
}

func (p *PrefixedProgressCallback) OnActionFailed(actionName string, err error) {
	p.inner.OnActionFailed(p.formatName(actionName), err)
}

func (p *PrefixedProgressCallback) OnActionSkipped(actionName, reason string) {
	p.inner.OnActionSkipped(p.formatName(actionName), reason)
}

func (p *PrefixedProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	p.inner.OnActionTimeout(p.formatName(actionName), timeout)
}

func (p *PrefixedProgressCallback) OnActionCancelled(actionName string) {
	p.inner.OnActionCancelled(p.formatName(actionName))
}

func (p *PrefixedProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	p.inner.OnRetryAttempt(p.formatName(actionName), attempt, maxAttempts, err)
}

func (p *PrefixedProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	p.inner.OnForEachProgress(p.formatName(actionName), completed, total)
}

func (*PrefixedProgressCallback) OnPhaseStart(_ int, _ []string) {}
func (*PrefixedProgressCallback) OnPhaseComplete(_ int)          {}
func (*PrefixedProgressCallback) OnFinallyStart()                {}
func (*PrefixedProgressCallback) OnFinallyComplete()             {}
