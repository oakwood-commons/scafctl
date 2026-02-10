// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package terminal

import (
	"bytes"
	"io"
	"testing"
)

func TestNewTestIOStreams(t *testing.T) {
	got, gotOut, gotErr := NewTestIOStreams()

	if got == nil {
		t.Fatal("NewTestIOStreams() returned nil IOStreams")
	}
	if gotOut == nil {
		t.Fatal("NewTestIOStreams() returned nil Out buffer")
	}
	if gotErr == nil {
		t.Fatal("NewTestIOStreams() returned nil ErrOut buffer")
	}

	// Check that Out and ErrOut in IOStreams are the same as returned buffers
	if got.Out != gotOut {
		t.Errorf("IOStreams.Out does not match returned out buffer")
	}
	if got.ErrOut != gotErr {
		t.Errorf("IOStreams.ErrOut does not match returned errOut buffer")
	}

	// Check ColorEnabled is false
	if got.ColorEnabled != false {
		t.Errorf("IOStreams.ColorEnabled = %v, want false", got.ColorEnabled)
	}

	// Check In is not nil and is empty
	buf := make([]byte, 1)
	n, err := got.In.Read(buf)
	if n != 0 && err == nil {
		t.Errorf("IOStreams.In should be empty, got n=%d, err=%v", n, err)
	}
}

func TestNewIOStreams(t *testing.T) {
	r := io.NopCloser(bytes.NewReader([]byte("input")))
	outBuf := &bytes.Buffer{}
	errOutBuf := &bytes.Buffer{}
	color := true

	streams := NewIOStreams(r, outBuf, errOutBuf, color)
	if streams == nil {
		t.Fatal("NewIOStreams() returned nil")
	}
	if streams.In != r {
		t.Errorf("NewIOStreams().In = %v, want %v", streams.In, r)
	}
	if streams.Out != outBuf {
		t.Errorf("NewIOStreams().Out = %v, want %v", streams.Out, outBuf)
	}
	if streams.ErrOut != errOutBuf {
		t.Errorf("NewIOStreams().ErrOut = %v, want %v", streams.ErrOut, errOutBuf)
	}
	if streams.ColorEnabled != color {
		t.Errorf("NewIOStreams().ColorEnabled = %v, want %v", streams.ColorEnabled, color)
	}
}
