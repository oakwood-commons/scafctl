// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package logger

import (
	"github.com/go-logr/logr"
)

// multiSink fans structured log records out to multiple logr.LogSink
// implementations simultaneously. Enabled returns true when any child
// sink is enabled so the most-verbose configured sink wins.
type multiSink struct {
	sinks []logr.LogSink
}

func newMultiSink(sinks ...logr.LogSink) *multiSink {
	return &multiSink{sinks: sinks}
}

func (m *multiSink) Init(info logr.RuntimeInfo) {
	for _, s := range m.sinks {
		s.Init(info)
	}
}

func (m *multiSink) Enabled(level int) bool {
	for _, s := range m.sinks {
		if s.Enabled(level) {
			return true
		}
	}
	return false
}

func (m *multiSink) Info(level int, msg string, keysAndValues ...any) {
	for _, s := range m.sinks {
		if s.Enabled(level) {
			s.Info(level, msg, keysAndValues...)
		}
	}
}

func (m *multiSink) Error(err error, msg string, keysAndValues ...any) {
	for _, s := range m.sinks {
		s.Error(err, msg, keysAndValues...)
	}
}

func (m *multiSink) WithValues(keysAndValues ...any) logr.LogSink {
	next := make([]logr.LogSink, len(m.sinks))
	for i, s := range m.sinks {
		next[i] = s.WithValues(keysAndValues...)
	}
	return &multiSink{sinks: next}
}

func (m *multiSink) WithName(name string) logr.LogSink {
	next := make([]logr.LogSink, len(m.sinks))
	for i, s := range m.sinks {
		next[i] = s.WithName(name)
	}
	return &multiSink{sinks: next}
}
