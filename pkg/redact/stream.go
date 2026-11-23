// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package redact

import (
	"bytes"
	"io"
	"sync"
	"unicode/utf8"
)

// StreamWriter wraps an io.Writer and scrubs tracked secrets across arbitrary
// Write chunk boundaries by retaining a rolling holdback tail of maxSecretLen-1
// bytes. Call Flush when the producer finishes to emit any remaining buffered
// bytes after final redaction.
type StreamWriter struct {
	mu          sync.Mutex
	w           io.Writer
	tracker     *Tracker
	emitMasks   bool
	atLineStart bool
	buf         []byte
}

// NewStreamWriter wraps w with chunk-boundary-safe secret redaction backed by tracker.
// If w is nil, NewStreamWriter returns nil.
func NewStreamWriter(w io.Writer, tracker *Tracker) *StreamWriter {
	return NewStreamWriterWithMasks(w, tracker, false)
}

// NewStreamWriterWithMasks wraps w with chunk-boundary-safe secret redaction and,
// when emitMasks is true, lazily emits GitHub Actions ::add-mask:: commands on w
// before forwarding streamed bytes whenever tracked secrets exist.
func NewStreamWriterWithMasks(w io.Writer, tracker *Tracker, emitMasks bool) *StreamWriter {
	if w == nil {
		return nil
	}
	return &StreamWriter{
		w:           w,
		tracker:     tracker.Root(),
		emitMasks:   emitMasks,
		atLineStart: true,
	}
}

func (s *StreamWriter) writeBytesLocked(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	s.atLineStart = b[len(b)-1] == '\n'
	return nil
}

func (s *StreamWriter) writeStringLocked(str string) error {
	if str == "" {
		return nil
	}
	if _, err := io.WriteString(s.w, str); err != nil {
		return err
	}
	s.atLineStart = str[len(str)-1] == '\n'
	return nil
}

// Write buffers incoming bytes enough to detect secrets split across Write
// calls, scrubs complete matches, and forwards safe prefixes to the underlying
// writer. It always returns len(p) on success to satisfy io.Writer.
func (s *StreamWriter) Write(p []byte) (int, error) {
	origLen := len(p)
	if origLen == 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tracker == nil || s.tracker.MaxSecretLen() == 0 {
		if len(s.buf) > 0 {
			if err := s.writeBytesLocked(s.buf); err != nil {
				return 0, err
			}
			s.buf = s.buf[:0]
		}
		if err := s.writeBytesLocked(p); err != nil {
			return 0, err
		}
		return origLen, nil
	}

	if s.emitMasks {
		var err error
		s.atLineStart, err = s.tracker.emitGitHubActionsMasks(s.w, s.atLineStart)
		if err != nil {
			return 0, err
		}
	}

	s.buf = append(s.buf, p...)

	// Fast line-flush path: when no tracked secret contains a newline, any
	// bytes up to and including the last '\n' cannot span into future chunks.
	if !s.tracker.HasMultiline() {
		if nl := bytes.LastIndexByte(s.buf, '\n'); nl >= 0 {
			complete := string(s.buf[:nl+1])
			scrubbed := s.tracker.RedactString(complete)
			if err := s.writeStringLocked(scrubbed); err != nil {
				return 0, err
			}
			rest := copy(s.buf, s.buf[nl+1:])
			s.buf = s.buf[:rest]
		}
	}

	if err := s.drainSafePrefixLocked(); err != nil {
		return 0, err
	}

	return origLen, nil
}

// drainSafePrefixLocked scans s.buf up to len(s.buf)-(maxSecretLen-1), where
// every candidate start index has at least maxSecretLen bytes available in
// s.buf so longest-first matching cannot prematurely replace a shorter prefix
// of a longer tracked secret.
func (s *StreamWriter) drainSafePrefixLocked() error {
	s.tracker.mu.RLock()
	maxLen := s.tracker.maxLen
	if maxLen <= 0 || len(s.buf) < maxLen {
		s.tracker.mu.RUnlock()
		return nil
	}

	holdback := maxLen - 1
	safeEnd := len(s.buf) - holdback
	bufStr := string(s.buf)

	var out bytes.Buffer
	cursor := 0
	for cursor < safeEnd {
		if matchLen := s.tracker.matchLongestAt(bufStr[cursor:]); matchLen > 0 {
			out.WriteString(RedactedPlaceholder)
			cursor += matchLen
			continue
		}
		out.WriteByte(bufStr[cursor])
		cursor++
	}
	s.tracker.mu.RUnlock()

	// Avoid splitting a multi-byte UTF-8 sequence when advancing by single bytes
	// without a match right at safeEnd.
	for cursor > 0 && cursor < len(s.buf) && !utf8.RuneStart(s.buf[cursor]) {
		// Only step back into literal bytes we just copied (never into a
		// replacement placeholder).
		if out.Len() == 0 {
			break
		}
		raw := out.Bytes()
		if bytes.HasSuffix(raw, []byte(RedactedPlaceholder)) {
			break
		}
		out.Truncate(len(raw) - 1)
		cursor--
	}

	if out.Len() > 0 {
		if err := s.writeBytesLocked(out.Bytes()); err != nil {
			return err
		}
	}
	if cursor > 0 {
		rest := copy(s.buf, s.buf[cursor:])
		s.buf = s.buf[:rest]
	}
	return nil
}

// Flush scrubs and writes any remaining held-back bytes to the underlying writer.
func (s *StreamWriter) Flush() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.buf) == 0 {
		return nil
	}
	data := string(s.buf)
	s.buf = s.buf[:0]
	if s.tracker != nil {
		data = s.tracker.RedactString(data)
	}
	return s.writeStringLocked(data)
}
