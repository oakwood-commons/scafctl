// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package terminal

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefixedWriter_SingleLine(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "action-1")

	n, err := pw.Write([]byte("hello world\n"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)
	assert.Equal(t, "[action-1] hello world\n", buf.String())
}

func TestPrefixedWriter_MultipleLines(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "greet")

	_, err := pw.Write([]byte("line one\nline two\n"))
	require.NoError(t, err)
	assert.Equal(t, "[greet] line one\n[greet] line two\n", buf.String())
}

func TestPrefixedWriter_NoTrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "test")

	_, err := pw.Write([]byte("no newline"))
	require.NoError(t, err)
	assert.Equal(t, "[test] no newline", buf.String())
}

func TestPrefixedWriter_EmptyWrite(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "test")

	n, err := pw.Write([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, "", buf.String())
}

func TestPrefixedWriter_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "step")

	_, err := pw.Write([]byte("first\n"))
	require.NoError(t, err)
	_, err = pw.Write([]byte("second\n"))
	require.NoError(t, err)

	assert.Equal(t, "[step] first\n[step] second\n", buf.String())
}

func TestPrefixedWriter_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "concurrent")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pw.Write([]byte("test line\n"))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Each write should produce a prefixed line
	output := buf.String()
	count := 0
	for i := 0; i < len(output); i++ {
		if output[i] == '\n' {
			count++
		}
	}
	assert.Equal(t, 10, count, "expected 10 lines of output")
}

func TestPrefixedWriter_PartialLinesThenNewline(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixedWriter(&buf, "partial")

	_, err := pw.Write([]byte("hello "))
	require.NoError(t, err)
	_, err = pw.Write([]byte("world\n"))
	require.NoError(t, err)

	// Prefix was written at the start of the first write,
	// second write continues the same line until newline
	assert.Equal(t, "[partial] hello world\n", buf.String())
}
