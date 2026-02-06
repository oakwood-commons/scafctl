// Package writer provides a centralized CLI output writer for scafctl.
// It handles message formatting, respects global flags like --quiet and --no-color,
// and provides a consistent interface for all CLI commands to write output.
package writer

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
)

// Writer provides a centralized interface for writing CLI output.
// It respects global settings like --quiet and --no-color automatically.
type Writer struct {
	ioStreams *terminal.IOStreams
	cliParams *settings.Run
	exitFunc  func(code int)
}

// New creates a new Writer with the given IOStreams and CLI params.
// Functional options can be provided to customize behavior.
func New(ioStreams *terminal.IOStreams, cliParams *settings.Run, opts ...Option) *Writer {
	w := &Writer{
		ioStreams: ioStreams,
		cliParams: cliParams,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Success writes a success message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Success(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.Out, output.SuccessMessage(msg, w.cliParams.NoColor))
}

// Successf writes a formatted success message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Successf(format string, args ...any) {
	w.Success(fmt.Sprintf(format, args...))
}

// Warning writes a warning message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Warning(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.Out, output.WarningMessage(msg, w.cliParams.NoColor))
}

// Warningf writes a formatted warning message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Warningf(format string, args ...any) {
	w.Warning(fmt.Sprintf(format, args...))
}

// Error writes an error message to stderr.
// Does NOT respect --quiet (errors should always be visible).
// Respects --no-color flag.
func (w *Writer) Error(msg string) {
	fmt.Fprintln(w.ioStreams.ErrOut, output.ErrorMessage(msg, w.cliParams.NoColor))
}

// Errorf writes a formatted error message to stderr.
// Does NOT respect --quiet (errors should always be visible).
// Respects --no-color flag.
func (w *Writer) Errorf(format string, args ...any) {
	w.Error(fmt.Sprintf(format, args...))
}

// ErrorWithExit writes an error message and exits with code 1.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithExit(msg string) {
	w.ErrorWithCode(1, msg)
}

// ErrorWithExitf writes a formatted error message and exits with code 1.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithExitf(format string, args ...any) {
	w.ErrorWithExit(fmt.Sprintf(format, args...))
}

// ErrorWithCode writes an error message and exits with the specified code.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithCode(code int, msg string) {
	w.Error(msg)
	if w.exitFunc != nil {
		w.exitFunc(code)
	} else {
		os.Exit(code)
	}
}

// ErrorWithCodef writes a formatted error message and exits with the specified code.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithCodef(code int, format string, args ...any) {
	w.ErrorWithCode(code, fmt.Sprintf(format, args...))
}

// Info writes an informational message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Info(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.Out, output.InfoMessage(msg, w.cliParams.NoColor))
}

// Infof writes a formatted informational message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Infof(format string, args ...any) {
	w.Info(fmt.Sprintf(format, args...))
}

// Debug writes a debug message to stdout.
// Respects --quiet and --no-color flags.
// Only writes if log level indicates debug output is enabled (MinLogLevel <= -1).
func (w *Writer) Debug(msg string) {
	if w.cliParams.IsQuiet || w.cliParams.MinLogLevel > -1 {
		return
	}
	fmt.Fprintln(w.ioStreams.Out, output.DebugMessage(msg, w.cliParams.NoColor))
}

// Debugf writes a formatted debug message to stdout.
// Respects --quiet and --no-color flags.
// Only writes if log level indicates debug output is enabled (MinLogLevel <= -1).
func (w *Writer) Debugf(format string, args ...any) {
	w.Debug(fmt.Sprintf(format, args...))
}

// DebugOut writes a debug output message to stderr.
// This is intended for explicit user debugging (e.g., debug.out CEL function).
// Respects --quiet flag but ignores log level (always prints when not quiet).
// Writes to stderr so it doesn't pollute piped stdout output.
func (w *Writer) DebugOut(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.ErrOut, output.DebugMessage(msg, w.cliParams.NoColor))
}

// DebugOutf writes a formatted debug output message to stderr.
// This is intended for explicit user debugging (e.g., debug.out CEL function).
// Respects --quiet flag but ignores log level (always prints when not quiet).
// Writes to stderr so it doesn't pollute piped stdout output.
func (w *Writer) DebugOutf(format string, args ...any) {
	w.DebugOut(fmt.Sprintf(format, args...))
}

// Plain writes a plain message to stdout without any styling or newline.
// Respects --quiet flag only.
func (w *Writer) Plain(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprint(w.ioStreams.Out, msg)
}

// Plainf writes a formatted plain message to stdout without any styling or newline.
// Respects --quiet flag only.
func (w *Writer) Plainf(format string, args ...any) {
	w.Plain(fmt.Sprintf(format, args...))
}

// Plainln writes a plain message to stdout with a newline, without any styling.
// Respects --quiet flag only.
func (w *Writer) Plainln(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.Out, msg)
}

// Plainlnf writes a formatted plain message to stdout with a newline, without any styling.
// Respects --quiet flag only.
func (w *Writer) Plainlnf(format string, args ...any) {
	w.Plainln(fmt.Sprintf(format, args...))
}

// IOStreams returns the underlying IOStreams.
// Useful when you need direct access to the streams for structured output.
func (w *Writer) IOStreams() *terminal.IOStreams {
	return w.ioStreams
}

// CliParams returns the underlying CLI parameters.
// Useful when you need to check flags like NoColor or IsQuiet.
func (w *Writer) CliParams() *settings.Run {
	return w.cliParams
}

// NoColor returns true if color output is disabled.
func (w *Writer) NoColor() bool {
	return w.cliParams.NoColor
}

// IsQuiet returns true if quiet mode is enabled.
func (w *Writer) IsQuiet() bool {
	return w.cliParams.IsQuiet
}
