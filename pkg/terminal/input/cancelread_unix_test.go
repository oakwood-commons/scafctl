// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package input

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptPoller scripts waitInput/readByte behavior from a stream of input
// bytes: each byte becomes available one waitInput cycle at a time.
func scriptPoller(input string, maxLen int, readCalls *int32) linePoller {
	ch := make(chan byte, len(input))
	for i := 0; i < len(input); i++ {
		ch <- input[i]
	}
	return linePoller{
		waitInput: func() (bool, error) {
			return len(ch) > 0, nil
		},
		readByte: func() (byte, error) {
			atomic.AddInt32(readCalls, 1)
			select {
			case b := <-ch:
				return b, nil
			default:
				return 0, errors.New("no input")
			}
		},
		maxLen: maxLen,
	}
}

func TestReadInteractiveLineLoop_ReadsLineWithEcho(t *testing.T) {
	t.Parallel()

	var readCalls int32
	echo := &bytes.Buffer{}
	poller := scriptPoller("http://localhost:8400/callback?code=abc\n", 1024, &readCalls)

	line, err := readInteractiveLineLoop(context.Background(), poller, echo)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8400/callback?code=abc", line)
	assert.Equal(t, "http://localhost:8400/callback?code=abc\n", echo.String(), "typed characters echo, Enter emits a newline")
}

func TestReadInteractiveLineLoop_CRAndTrailingCRHandled(t *testing.T) {
	t.Parallel()

	var readCalls int32
	echo := &bytes.Buffer{}
	poller := scriptPoller("\r", 1024, &readCalls)
	line, err := readInteractiveLineLoop(context.Background(), poller, echo)
	require.NoError(t, err)
	assert.Empty(t, line)

	var readCalls2 int32
	echo2 := &bytes.Buffer{}
	// ICRNL usually translates Enter to \n; both line endings are accepted
	// and a trailing \r is stripped.
	poller2 := scriptPoller("value\r\n", 1024, &readCalls2)
	line, err = readInteractiveLineLoop(context.Background(), poller2, echo2)
	require.NoError(t, err)
	assert.Equal(t, "value", line)
}

func TestReadInteractiveLineLoop_BackspaceErases(t *testing.T) {
	t.Parallel()

	var readCalls int32
	echo := &bytes.Buffer{}
	poller := scriptPoller("ab\x7f\n", 64, &readCalls) // type "ab", backspace, Enter

	line, err := readInteractiveLineLoop(context.Background(), poller, echo)
	require.NoError(t, err)
	assert.Equal(t, "a", line)
	assert.Contains(t, echo.String(), "\b \b", "backspace visually erases one character")
}

func TestReadInteractiveLineLoop_MaxLengthEnforced(t *testing.T) {
	t.Parallel()

	var readCalls int32
	poller := scriptPoller(strings.Repeat("a", 6)+"\n", 4, &readCalls)
	_, err := readInteractiveLineLoop(context.Background(), poller, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
}

func TestReadInteractiveLineLoop_CancellationStopsRead(t *testing.T) {
	t.Parallel()

	t.Run("cancel while waiting consumes no input", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())

		var readCalls int32
		poller := linePoller{
			waitInput: func() (bool, error) {
				cancel() // the localhost callback arrives mid-wait
				time.Sleep(2 * time.Millisecond)
				return true, nil
			},
			readByte: func() (byte, error) {
				atomic.AddInt32(&readCalls, 1)
				return 'x', nil
			},
			maxLen: 64,
		}

		_, err := readInteractiveLineLoop(ctx, poller, &bytes.Buffer{})
		assert.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, atomic.LoadInt32(&readCalls), "input became ready after cancel and must not be consumed")
	})

	t.Run("cancel before first wait consumes no input", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var readCalls int32
		poller := scriptPoller("pasted-value\n", 64, &readCalls)
		_, err := readInteractiveLineLoop(ctx, poller, &bytes.Buffer{})
		assert.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, atomic.LoadInt32(&readCalls), "no byte read after cancellation")
	})
}

func TestReadInteractiveLineLoop_WaitErrorSurfaces(t *testing.T) {
	t.Parallel()

	poller := linePoller{
		waitInput: func() (bool, error) { return false, errors.New("poll exploded") },
		readByte:  func() (byte, error) { return 0, nil },
		maxLen:    64,
	}
	_, err := readInteractiveLineLoop(context.Background(), poller, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "waiting for input")
}

func TestReadInteractiveLineLoop_ReadErrorSurfaces(t *testing.T) {
	t.Parallel()

	poller := linePoller{
		waitInput: func() (bool, error) { return true, nil },
		readByte:  func() (byte, error) { return 0, errors.New("read exploded") },
		maxLen:    64,
	}
	_, err := readInteractiveLineLoop(context.Background(), poller, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading input")
}

// TestReadInteractiveLine_NonTTY asserts the non-terminal guard on the real
// entry point (a bytes.Reader is not an *os.File).
func TestReadInteractiveLine_NonTTY(t *testing.T) {
	t.Parallel()

	_, err := ReadInteractiveLine(context.Background(), strings.NewReader("x\n"), &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminal file")
}

// TestReadInteractiveLine_NonTerminalFile asserts the termios guard: stdin
// wired to a regular file (piped stdin) cannot host the paste-back prompt.
func TestReadInteractiveLine_NonTerminalFile(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Skip("temp file unavailable")
	}
	defer f.Close()
	_, err = ReadInteractiveLine(context.Background(), f, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive input")
}
