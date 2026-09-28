// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// interactiveLineSupported reports whether this platform implements
// cancellation-safe terminal reads.
const interactiveLineSupported = true

// readPollInterval bounds how long each wait-for-input cycle blocks before
// re-checking ctx, so a canceled prompt stops within this delay.
const readPollInterval = 100 * time.Millisecond

// interactiveLineMaxLen caps accepted input so a runaway paste cannot grow
// the buffer unboundedly.
const interactiveLineMaxLen = 64 * 1024

// linePoller abstracts the low-level input primitives of an interactive read
// so the read loop can be tested without a real TTY.
type linePoller struct {
	// waitInput blocks until a byte may be readable (for at most
	// readPollInterval) and returns whether one is available now.
	waitInput func() (bool, error)
	// readByte reads one available byte.
	readByte func() (byte, error)
	// maxLen caps the accepted line length.
	maxLen int
}

// readInteractiveLineLoop reads one line via the poller, echoing typed
// characters to echo. It returns when the user presses Enter or ctx is
// canceled; between every wait/read step ctx is re-checked, so no input is
// consumed once the context is done (the caller's next reader sees whatever
// the terminal still holds).
func readInteractiveLineLoop(ctx context.Context, p linePoller, echo io.Writer) (string, error) {
	var line strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		ready, err := p.waitInput()
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return "", fmt.Errorf("waiting for input: %w", err)
		}
		if !ready {
			continue // re-check ctx before waiting again
		}
		if ctx.Err() != nil {
			// Canceled while input became ready: do not consume it.
			return "", ctx.Err()
		}
		b, err := p.readByte()
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
				continue
			}
			return "", fmt.Errorf("reading input: %w", err)
		}
		switch b {
		case '\n', '\r':
			_, _ = echo.Write([]byte{'\n'})
			return line.String(), nil
		case 0x7f, 0x08:
			// DEL / backspace: erase one byte and one visual cell. This is
			// deliberately ASCII-oriented -- redirect URLs are ASCII, and a
			// multi-byte rune erasure would only matter for non-ASCII pastes.
			if s := line.String(); len(s) > 0 {
				s = s[:len(s)-1]
				line.Reset()
				line.WriteString(s)
				_, _ = echo.Write([]byte{'\b', ' ', '\b'})
			}
		default:
			if line.Len() >= p.maxLen {
				return "", fmt.Errorf("input exceeds the maximum length of %d characters", p.maxLen)
			}
			line.WriteByte(b)
			_, _ = echo.Write([]byte{b})
		}
	}
}

// ReadInteractiveLine reads one line of terminal input from in (which must
// be an *os.File attached to a TTY), echoing typed characters to echo, and
// returns the line (without the newline) when the user presses Enter or
// ctx.Err() when ctx is canceled.
//
// Reads never block uninterruptibly: the terminal is placed in
// non-canonical mode with VMIN=0 (a read with no data returns immediately)
// and input availability is awaited with poll, so the loop checks ctx at
// readPollInterval cadence. Signal characters are left intact, so Ctrl+C
// still raises SIGINT through the host's existing login signal handler. On
// return -- success, error, or cancellation -- the original terminal mode is
// restored and any queued (unread) input is discarded, so no pending read
// survives a canceled prompt to steal the next reader's input.
func ReadInteractiveLine(ctx context.Context, in io.Reader, echo io.Writer) (string, error) {
	f, ok := in.(*os.File)
	if !ok {
		return "", errors.New("interactive input requires a terminal file")
	}
	fd := int(f.Fd()) //nolint:gosec // Fd() fits in int on all supported platforms

	oldState, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return "", fmt.Errorf("preparing interactive input: %w", err)
	}
	// Non-canonical, no kernel echo: byte-at-a-time input so ctx is checked
	// between reads, with the caller performing echo (which also makes
	// backspace erasure possible).
	rawState := *oldState
	rawState.Lflag &^= unix.ICANON | unix.ECHO
	rawState.Lflag |= unix.ISIG
	rawState.Cc[unix.VMIN] = 0
	rawState.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &rawState); err != nil {
		return "", fmt.Errorf("preparing interactive input: %w", err)
	}
	defer func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, oldState)
		// Discard queued input so an abandoned prompt cannot leak its bytes
		// into the next reader's line.
		_ = flushInput(fd)
	}()

	poller := linePoller{
		waitInput: func() (bool, error) {
			nReady, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, int(readPollInterval.Milliseconds())) //nolint:gosec // fd fits in int32 on all supported platforms
			if err != nil {
				return false, err
			}
			return nReady > 0, nil
		},
		readByte: func() (byte, error) {
			buf := make([]byte, 1)
			n, err := unix.Read(fd, buf)
			if err != nil {
				return 0, err
			}
			if n == 0 {
				return 0, io.EOF
			}
			return buf[0], nil
		},
		maxLen: interactiveLineMaxLen,
	}
	line, err := readInteractiveLineLoop(ctx, poller, echo)
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return line, err
}
