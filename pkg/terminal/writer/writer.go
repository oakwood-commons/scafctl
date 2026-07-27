// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package writer provides a centralized CLI output writer for scafctl.
// It handles message formatting, respects global flags like --quiet and --no-color,
// and provides a consistent interface for all CLI commands to write output.
package writer

import (
	"fmt"
	"io"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/logger"
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

	// humanToStderr routes human-facing messages (success/info/warning/
	// section-header/debug/plain) to stderr instead of stdout. It is set via
	// WithHumanToStderr for commands that emit structured data on stdout
	// (e.g., -o json) and must keep progress noise off that stream.
	humanToStderr bool
}

// humanOut returns the stream for human-facing (non-error) messages. It is
// stderr when humanToStderr is set, otherwise stdout.
func (w *Writer) humanOut() io.Writer {
	if w.humanToStderr {
		return w.ioStreams.ErrOut
	}
	return w.ioStreams.Out
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

// Success writes a success message to stdout (or stderr when WithHumanToStderr is set).
// Respects --quiet and --no-color flags.
func (w *Writer) Success(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.humanOut(), output.SuccessMessage(msg, w.cliParams.NoColor))
}

// Successf writes a formatted success message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Successf(format string, args ...any) {
	w.Success(fmt.Sprintf(format, args...))
}

// SuccessStderr writes a success message to stderr instead of stdout.
// Use this for status confirmations that must not corrupt structured command
// output (e.g., -o json). Respects --quiet and --no-color flags.
func (w *Writer) SuccessStderr(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.ErrOut, output.SuccessMessage(msg, w.cliParams.NoColor))
}

// SuccessStderrf writes a formatted success message to stderr.
// Use this for status confirmations that must not corrupt structured command
// output (e.g., -o json). Respects --quiet and --no-color flags.
func (w *Writer) SuccessStderrf(format string, args ...any) {
	w.SuccessStderr(fmt.Sprintf(format, args...))
}

// Warning writes a warning message to stdout (or stderr when WithHumanToStderr is set).
// Respects --quiet and --no-color flags.
func (w *Writer) Warning(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.humanOut(), output.WarningMessage(msg, w.cliParams.NoColor))
}

// Warningf writes a formatted warning message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Warningf(format string, args ...any) {
	w.Warning(fmt.Sprintf(format, args...))
}

// WarnStderr writes a warning message to stderr instead of stdout.
// Use this for system-level diagnostic warnings that must not corrupt
// structured command output (e.g., -o json).
// Respects --quiet and --no-color flags.
func (w *Writer) WarnStderr(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.ErrOut, output.WarningMessage(msg, w.cliParams.NoColor))
}

// WarnStderrf writes a formatted warning message to stderr.
// Use this for system-level diagnostic warnings that must not corrupt
// structured command output (e.g., -o json).
// Respects --quiet and --no-color flags.
func (w *Writer) WarnStderrf(format string, args ...any) {
	w.WarnStderr(fmt.Sprintf(format, args...))
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

// Info writes an informational message to stdout (or stderr when WithHumanToStderr is set).
// Respects --quiet and --no-color flags.
func (w *Writer) Info(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.humanOut(), output.InfoMessage(msg, w.cliParams.NoColor))
}

// Infof writes a formatted informational message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Infof(format string, args ...any) {
	w.Info(fmt.Sprintf(format, args...))
}

// SectionHeader writes a bold section header to stdout (or stderr when WithHumanToStderr is set).
// Respects --quiet and --no-color flags.
func (w *Writer) SectionHeader(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.humanOut(), output.SectionHeaderMessage(msg, w.cliParams.NoColor))
}

// Debug writes a debug message to stdout (or stderr when WithHumanToStderr is set).
// Respects --quiet and --no-color flags.
// Only writes if log level indicates debug output is enabled (debug, trace, or numeric V-level).
func (w *Writer) Debug(msg string) {
	if w.cliParams.IsQuiet || !logger.IsDebugLevel(w.cliParams.MinLogLevel) {
		return
	}
	fmt.Fprintln(w.humanOut(), output.DebugMessage(msg, w.cliParams.NoColor))
}

// Debugf writes a formatted debug message to stdout.
// Respects --quiet and --no-color flags.
// Only writes if log level indicates debug output is enabled (debug, trace, or numeric V-level).
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

// VerboseEnabled returns true if verbose output is active (--verbose is set and --quiet is not).
func (w *Writer) VerboseEnabled() bool {
	return w.cliParams.Verbose && !w.cliParams.IsQuiet
}

// Verbose writes a verbose diagnostic message to stderr.
// Only prints when --verbose is enabled. Respects --quiet.
// Writes to stderr so it doesn't pollute piped stdout output (e.g., -o json).
func (w *Writer) Verbose(msg string) {
	if w.cliParams.IsQuiet || !w.cliParams.Verbose {
		return
	}
	fmt.Fprintln(w.ioStreams.ErrOut, output.VerboseMessage(msg, w.cliParams.NoColor))
}

// Verbosef writes a formatted verbose diagnostic message to stderr.
// Only prints when --verbose is enabled. Respects --quiet.
// Writes to stderr so it doesn't pollute piped stdout output (e.g., -o json).
func (w *Writer) Verbosef(format string, args ...any) {
	w.Verbose(fmt.Sprintf(format, args...))
}

// Plain writes a plain message to stdout (or stderr when WithHumanToStderr is set) without any styling or newline.
// Respects --quiet flag only.
func (w *Writer) Plain(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprint(w.humanOut(), msg)
}

// Plainf writes a formatted plain message to stdout without any styling or newline.
// Respects --quiet flag only.
func (w *Writer) Plainf(format string, args ...any) {
	w.Plain(fmt.Sprintf(format, args...))
}

// Plainln writes a plain message to stdout (or stderr when WithHumanToStderr is set) with a newline, without any styling.
// Respects --quiet flag only.
func (w *Writer) Plainln(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.humanOut(), msg)
}

// Plainlnf writes a formatted plain message to stdout with a newline, without any styling.
// Respects --quiet flag only.
func (w *Writer) Plainlnf(format string, args ...any) {
	w.Plainln(fmt.Sprintf(format, args...))
}

// PlainStderr writes a plain message to stderr without any styling.
// Use this for diagnostic output that must not corrupt structured stdout
// but also does not warrant warning/error formatting.
// Respects --quiet flag only.
func (w *Writer) PlainStderr(msg string) {
	if w.cliParams.IsQuiet {
		return
	}
	fmt.Fprintln(w.ioStreams.ErrOut, msg)
}

// PlainStderrf writes a formatted plain message to stderr without any styling.
// Respects --quiet flag only.
func (w *Writer) PlainStderrf(format string, args ...any) {
	w.PlainStderr(fmt.Sprintf(format, args...))
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
