// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package terminal

import (
	"fmt"
	"io"
	"sync"
)

// PrefixedWriter wraps an io.Writer and prepends a prefix to each line of output.
// It is safe for concurrent use. When multiple actions run in parallel, each action
// gets a PrefixedWriter with its name, so output lines are clearly attributed.
type PrefixedWriter struct {
	mu          sync.Mutex
	out         io.Writer
	prefix      string
	needsPrefix bool
}

// NewPrefixedWriter creates a writer that prefixes each line with the given string.
// The prefix is formatted as "[name] " and prepended to the start of each line.
func NewPrefixedWriter(out io.Writer, name string) *PrefixedWriter {
	return &PrefixedWriter{
		out:         out,
		prefix:      fmt.Sprintf("[%s] ", name),
		needsPrefix: true,
	}
}

// Write implements io.Writer. It scans each byte for newlines and inserts
// the prefix at the beginning of each new line.
func (pw *PrefixedWriter) Write(p []byte) (int, error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	written := 0
	for _, b := range p {
		if pw.needsPrefix {
			if _, err := io.WriteString(pw.out, pw.prefix); err != nil {
				return written, err
			}
			pw.needsPrefix = false
		}

		if _, err := pw.out.Write([]byte{b}); err != nil {
			return written, err
		}
		written++

		if b == '\n' {
			pw.needsPrefix = true
		}
	}

	return written, nil
}
