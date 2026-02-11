// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package writer

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
)

func newTestWriter(opts ...Option) (*Writer, *bytes.Buffer, *bytes.Buffer) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:           io.NopCloser(bytes.NewReader([]byte{})),
		Out:          outBuf,
		ErrOut:       errBuf,
		ColorEnabled: false,
	}
	cliParams := settings.NewCliParams()
	w := New(ioStreams, cliParams, opts...)
	return w, outBuf, errBuf
}

func TestNew(t *testing.T) {
	w, _, _ := newTestWriter()
	assert.NotNil(t, w)
	assert.NotNil(t, w.ioStreams)
	assert.NotNil(t, w.cliParams)
}

func TestSuccess(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Success("Operation completed")
	assert.Contains(t, outBuf.String(), "Operation completed")
	assert.Contains(t, outBuf.String(), "✅")
}

func TestSuccessf(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.NoColor = true

	w.Successf("Processed %d items in %s", 42, "5s")
	assert.Contains(t, outBuf.String(), "Processed 42 items in 5s")
}

func TestSuccess_NoColor(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.NoColor = true

	w.Success("Operation completed")
	assert.Contains(t, outBuf.String(), "Operation completed")
	assert.NotContains(t, outBuf.String(), "✅")
}

func TestSuccess_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Success("Operation completed")
	assert.Empty(t, outBuf.String())
}

func TestWarning(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Warning("Something might be wrong")
	assert.Contains(t, outBuf.String(), "Something might be wrong")
	assert.Contains(t, outBuf.String(), "⚠️")
}

func TestWarningf(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.NoColor = true

	w.Warningf("Item %s not found", "foo")
	assert.Contains(t, outBuf.String(), "Item foo not found")
}

func TestWarning_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Warning("Something might be wrong")
	assert.Empty(t, outBuf.String())
}

func TestError(t *testing.T) {
	w, _, errBuf := newTestWriter()

	w.Error("Something went wrong")
	assert.Contains(t, errBuf.String(), "Something went wrong")
	assert.Contains(t, errBuf.String(), "❌")
}

func TestErrorf(t *testing.T) {
	w, _, errBuf := newTestWriter()
	w.cliParams.NoColor = true

	w.Errorf("Failed to open %s: %v", "file.txt", "not found")
	assert.Contains(t, errBuf.String(), "Failed to open file.txt: not found")
}

func TestError_NoColor(t *testing.T) {
	w, _, errBuf := newTestWriter()
	w.cliParams.NoColor = true

	w.Error("Something went wrong")
	assert.Contains(t, errBuf.String(), "Something went wrong")
	assert.NotContains(t, errBuf.String(), "❌")
}

func TestError_NotSuppressedByQuiet(t *testing.T) {
	w, _, errBuf := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Error("Critical error")
	assert.Contains(t, errBuf.String(), "Critical error")
}

func TestErrorWithExit(t *testing.T) {
	var exitCode int
	w, _, errBuf := newTestWriter(WithExitFunc(func(code int) {
		exitCode = code
	}))

	w.ErrorWithExit("Fatal error")
	assert.Contains(t, errBuf.String(), "Fatal error")
	assert.Equal(t, 1, exitCode)
}

func TestErrorWithExitf(t *testing.T) {
	var exitCode int
	w, _, errBuf := newTestWriter(WithExitFunc(func(code int) {
		exitCode = code
	}))

	w.ErrorWithExitf("Fatal error: %s", "reason")
	assert.Contains(t, errBuf.String(), "Fatal error: reason")
	assert.Equal(t, 1, exitCode)
}

func TestErrorWithCode(t *testing.T) {
	var exitCode int
	w, _, errBuf := newTestWriter(WithExitFunc(func(code int) {
		exitCode = code
	}))

	w.ErrorWithCode(42, "Custom exit code error")
	assert.Contains(t, errBuf.String(), "Custom exit code error")
	assert.Equal(t, 42, exitCode)
}

func TestErrorWithCodef(t *testing.T) {
	var exitCode int
	w, _, errBuf := newTestWriter(WithExitFunc(func(code int) {
		exitCode = code
	}))

	w.ErrorWithCodef(42, "Exit %d: %s", 42, "reason")
	assert.Contains(t, errBuf.String(), "Exit 42: reason")
	assert.Equal(t, 42, exitCode)
}

func TestInfo(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Info("Here's some information")
	assert.Contains(t, outBuf.String(), "Here's some information")
	assert.Contains(t, outBuf.String(), "💡")
}

func TestInfof(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.NoColor = true

	w.Infof("Processing %d files", 10)
	assert.Contains(t, outBuf.String(), "Processing 10 files")
}

func TestInfo_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Info("Here's some information")
	assert.Empty(t, outBuf.String())
}

func TestDebug(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.MinLogLevel = "debug" // Enable debug

	w.Debug("Debug information")
	assert.Contains(t, outBuf.String(), "Debug information")
	assert.Contains(t, outBuf.String(), "🐛")
}

func TestDebugf(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.MinLogLevel = "debug" // Enable debug
	w.cliParams.NoColor = true

	w.Debugf("Variable x = %d", 42)
	assert.Contains(t, outBuf.String(), "Variable x = 42")
}

func TestDebug_NotEnabled(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.MinLogLevel = "none" // Logs disabled, debug not enabled

	w.Debug("Debug information")
	assert.Empty(t, outBuf.String())
}

func TestDebug_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.MinLogLevel = "debug" // Enable debug
	w.cliParams.IsQuiet = true

	w.Debug("Debug information")
	assert.Empty(t, outBuf.String())
}

func TestPlain(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Plain("Plain text without newline")
	assert.Equal(t, "Plain text without newline", outBuf.String())
}

func TestPlainf(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Plainf("Value: %d", 100)
	assert.Equal(t, "Value: 100", outBuf.String())
}

func TestPlain_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Plain("Plain text")
	assert.Empty(t, outBuf.String())
}

func TestPlainln(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Plainln("Plain text with newline")
	assert.Equal(t, "Plain text with newline\n", outBuf.String())
}

func TestPlainlnf(t *testing.T) {
	w, outBuf, _ := newTestWriter()

	w.Plainlnf("Result: %s", "success")
	assert.Equal(t, "Result: success\n", outBuf.String())
}

func TestPlainln_Quiet(t *testing.T) {
	w, outBuf, _ := newTestWriter()
	w.cliParams.IsQuiet = true

	w.Plainln("Plain text")
	assert.Empty(t, outBuf.String())
}

func TestIOStreams(t *testing.T) {
	w, _, _ := newTestWriter()
	assert.NotNil(t, w.IOStreams())
}

func TestCliParams(t *testing.T) {
	w, _, _ := newTestWriter()
	assert.NotNil(t, w.CliParams())
}

func TestNoColor(t *testing.T) {
	w, _, _ := newTestWriter()
	assert.False(t, w.NoColor())

	w.cliParams.NoColor = true
	assert.True(t, w.NoColor())
}

func TestIsQuiet(t *testing.T) {
	w, _, _ := newTestWriter()
	assert.False(t, w.IsQuiet())

	w.cliParams.IsQuiet = true
	assert.True(t, w.IsQuiet())
}

// Context tests

func TestWithWriter_FromContext(t *testing.T) {
	w, _, _ := newTestWriter()
	ctx := context.Background()

	ctx = WithWriter(ctx, w)
	retrieved := FromContext(ctx)

	assert.Equal(t, w, retrieved)
}

func TestFromContext_NoWriter(t *testing.T) {
	ctx := context.Background()
	retrieved := FromContext(ctx)
	assert.Nil(t, retrieved)
}

func TestMustFromContext(t *testing.T) {
	w, _, _ := newTestWriter()
	ctx := WithWriter(context.Background(), w)

	retrieved := MustFromContext(ctx)
	assert.Equal(t, w, retrieved)
}

func TestMustFromContext_Panics(t *testing.T) {
	ctx := context.Background()

	assert.Panics(t, func() {
		MustFromContext(ctx)
	})
}

// Integration-style tests

func TestMultipleMessages(t *testing.T) {
	w, outBuf, errBuf := newTestWriter()
	w.cliParams.NoColor = true

	w.Info("Starting process")
	w.Warning("This is a warning")
	w.Success("Process completed")
	w.Error("But there was an error")

	out := outBuf.String()
	assert.True(t, strings.Contains(out, "Starting process"))
	assert.True(t, strings.Contains(out, "This is a warning"))
	assert.True(t, strings.Contains(out, "Process completed"))
	assert.True(t, strings.Contains(errBuf.String(), "But there was an error"))
}
