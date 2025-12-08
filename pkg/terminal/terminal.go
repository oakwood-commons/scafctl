package terminal

import (
	"bytes"
	"io"
)

// IOStreams provides a set of input/output streams for terminal interactions.
// It encapsulates standard input, output, error output, and a flag indicating
// whether color output is enabled.
type IOStreams struct {
	In           io.ReadCloser
	Out          io.Writer
	ErrOut       io.Writer
	ColorEnabled bool
}

// NewIOStreams creates and returns a new IOStreams instance with the provided input, output, error output streams,
// and a flag indicating whether color output is enabled.
func NewIOStreams(in io.ReadCloser, out, errOut io.Writer, colorEnabled bool) *IOStreams {
	return &IOStreams{
		In:           in,
		Out:          out,
		ErrOut:       errOut,
		ColorEnabled: colorEnabled,
	}
}

// NewTestIOStreams creates an IOStreams instance for testing with buffers
// that can be read and validated. Returns the IOStreams, out buffer, and errOut buffer.
func NewTestIOStreams() (*IOStreams, *bytes.Buffer, *bytes.Buffer) {
	outBuf := &bytes.Buffer{}
	errOutBuf := &bytes.Buffer{}
	return &IOStreams{
		In:           io.NopCloser(bytes.NewReader([]byte{})),
		Out:          outBuf,
		ErrOut:       errOutBuf,
		ColorEnabled: false,
	}, outBuf, errOutBuf
}
