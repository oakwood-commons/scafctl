// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package mockexec provides a configurable mock for shell command execution in
// functional tests. It is designed to run as a test service alongside the mock HTTP
// server — configure it via `spec.testing.config.services` with `type: exec`.
//
// Mock rules match commands by exact string or regex and return predefined
// stdout, stderr, and exit codes. Unmatched commands can fall through to real
// execution or fail with an error (configurable via Passthrough).
package mockexec

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/shellexec"
)

// Rule defines a single mock command response.
type Rule struct {
	// Command is an exact command string to match. Mutually exclusive with Pattern.
	Command string `json:"command,omitempty" yaml:"command,omitempty" doc:"Exact command string to match" maxLength:"1000"`

	// Pattern is a regex pattern to match against the command. Mutually exclusive with Command.
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty" doc:"Regex pattern to match against command" maxLength:"500"`

	// Stdout is the mock standard output to return.
	Stdout string `json:"stdout,omitempty" yaml:"stdout,omitempty" doc:"Mock stdout response" maxLength:"100000"`

	// Stderr is the mock standard error to return.
	Stderr string `json:"stderr,omitempty" yaml:"stderr,omitempty" doc:"Mock stderr response" maxLength:"100000"`

	// ExitCode is the mock exit code (default: 0).
	ExitCode int `json:"exitCode,omitempty" yaml:"exitCode,omitempty" doc:"Mock exit code" maximum:"255"`

	// compiled is the compiled regex pattern (populated by Compile).
	compiled *regexp.Regexp
}

// Compile pre-compiles the regex pattern if set. Returns an error for invalid patterns.
func (r *Rule) Compile() error {
	if r.Pattern != "" {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return fmt.Errorf("invalid mock exec pattern %q: %w", r.Pattern, err)
		}
		r.compiled = re
	}
	return nil
}

// Matches returns true if the rule matches the given command string.
func (r *Rule) Matches(command string) bool {
	if r.Command != "" {
		return command == r.Command || strings.Contains(command, r.Command)
	}
	if r.compiled != nil {
		return r.compiled.MatchString(command)
	}
	return false
}

// MockExec intercepts shell commands and returns predefined responses.
type MockExec struct {
	mu          sync.Mutex
	rules       []Rule
	calls       []CallRecord
	passthrough bool
}

// CallRecord records a command that was intercepted.
type CallRecord struct {
	Command string
	Args    []string
	Matched bool
	Rule    *Rule
}

// Option configures a MockExec.
type Option func(*MockExec)

// WithPassthrough allows unmatched commands to execute normally via the real shellexec.Run.
// When false (default), unmatched commands return an error.
func WithPassthrough(allow bool) Option {
	return func(m *MockExec) {
		m.passthrough = allow
	}
}

// New creates a new MockExec with the given rules.
// Rules are matched in order — the first matching rule wins.
func New(rules []Rule, opts ...Option) (*MockExec, error) {
	for i := range rules {
		if err := rules[i].Compile(); err != nil {
			return nil, err
		}
	}
	m := &MockExec{
		rules: rules,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// RunFunc returns a shellexec.RunFunc that can be injected into context via
// shellexec.WithRunFunc. Matched commands return mock responses; unmatched
// commands either pass through to real execution or fail, depending on config.
func (m *MockExec) RunFunc() shellexec.RunFunc {
	return func(ctx context.Context, opts *shellexec.RunOptions) (*shellexec.RunResult, error) {
		fullCmd := shellexec.BuildFullCommand(opts.Command, opts.Args)

		m.mu.Lock()
		for i := range m.rules {
			if m.rules[i].Matches(fullCmd) {
				rule := &m.rules[i]
				m.calls = append(m.calls, CallRecord{
					Command: fullCmd,
					Args:    opts.Args,
					Matched: true,
					Rule:    rule,
				})
				m.mu.Unlock()

				// Write mock output to the provided writers
				if opts.Stdout != nil && rule.Stdout != "" {
					_, _ = fmt.Fprint(opts.Stdout, rule.Stdout)
				}
				if opts.Stderr != nil && rule.Stderr != "" {
					_, _ = fmt.Fprint(opts.Stderr, rule.Stderr)
				}

				return &shellexec.RunResult{
					ExitCode: rule.ExitCode,
					Shell:    opts.Shell,
				}, nil
			}
		}

		m.calls = append(m.calls, CallRecord{
			Command: fullCmd,
			Args:    opts.Args,
			Matched: false,
		})
		m.mu.Unlock()

		if m.passthrough {
			return shellexec.Run(ctx, opts)
		}

		return nil, fmt.Errorf("mockexec: no matching rule for command %q (add a rule or enable passthrough)", fullCmd)
	}
}

// Calls returns a copy of all recorded call records.
func (m *MockExec) Calls() []CallRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CallRecord, len(m.calls))
	copy(out, m.calls)
	return out
}

// Reset clears recorded calls.
func (m *MockExec) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
}

// Rules returns the configured rules (read-only slice).
func (m *MockExec) Rules() []Rule {
	return m.rules
}
