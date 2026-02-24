// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	logGlobal "go.opentelemetry.io/otel/log/global"
)

// Define an unexported custom type for the context key to prevent collisions.
type loggerContextKey struct{}

const (
	RootCommandKey = "root_command"
	SubCommandKey  = "sub_command"
	CommitKey      = "commit"
	VersionKey     = "version"
	BuildTimeKey   = "build_time"
	GoVersionKey   = "go_version"
	TimeStampKey   = "timestamp"
	MessageKey     = "message"
	// DurationKey    = "duration"
	// UrlKey         = "url"
	// EnvKey         = "environment"
)

var (
	once sync.Once // Ensures Setup is called only once

	// globalLogrLogger is the logr.Logger instance that application code will primarily use
	// if not retrieving from context, or as a default for context.
	// It's package-private to prevent direct modification.
	globalLogrLogger *logr.Logger

	// defaultNoopLogger is a logger that does nothing, used as a fallback.
	defaultNoopLogger logr.Logger = logr.Discard()
)

// LogFormat represents the output format for logs.
type LogFormat string

const (
	// FormatJSON outputs logs in JSON format.
	FormatJSON LogFormat = "json"
	// FormatConsole outputs logs in human-readable text format.
	FormatConsole LogFormat = "console"
	// FormatText is an alias for FormatConsole for backward compatibility with config files.
	FormatText LogFormat = "text"
)

// Named log level constants used in config and CLI flags.
const (
	// LevelNone disables all structured log output.
	LevelNone = "none"
	// LevelError shows only error-level logs.
	LevelError = "error"
	// LevelWarn shows warning and error logs.
	LevelWarn = "warn"
	// LevelInfo shows info, warning, and error logs.
	LevelInfo = "info"
	// LevelDebug shows debug (V(1)) and above logs.
	LevelDebug = "debug"
	// LevelTrace shows trace (V(2)) and above logs.
	LevelTrace = "trace"
)

// LogLevelNone is the sentinel slog.Level value that silences all log output.
// It is set to math.MaxInt32 so no log entry can reach it.
const LogLevelNone = slog.Level(math.MaxInt32)

// ParseLogLevel converts a named or numeric log level string to a slog.Level.
// Named levels: none, error, warn, info, debug, trace.
// Numeric levels: any integer string (e.g., "3", "5") is treated as a logr V-level
// and negated to produce the corresponding slog level (user's 3 → slog.Level(-3) → V(3) visible).
//
// The logr/slog bridge maps logr V-level n to slog.Level(-n), so the handler
// threshold must also be -n to enable that level.
func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LevelNone, "":
		return LogLevelNone, nil
	case LevelError:
		return slog.LevelError, nil
	case LevelWarn:
		return slog.LevelWarn, nil
	case LevelInfo:
		return slog.LevelInfo, nil
	case LevelDebug:
		return slog.Level(-1), nil // V(1)
	case LevelTrace:
		return slog.Level(-2), nil // V(2)
	default:
		// Try numeric V-level
		n, err := strconv.Atoi(s)
		if err != nil {
			return slog.LevelInfo, fmt.Errorf("invalid log level %q: must be one of none, error, warn, info, debug, trace, or a numeric V-level", s)
		}
		// Numeric values are treated as V-levels: user provides positive number,
		// we negate to get the slog level (e.g., 3 → slog.Level(-3) → V(3) visible).
		return slog.Level(-n), nil
	}
}

// IsDebugLevel returns true if the given log level string resolves to debug or deeper.
// This is used by the writer package to gate debug output.
func IsDebugLevel(level string) bool {
	l, err := ParseLogLevel(level)
	if err != nil {
		return false
	}
	return l <= slog.Level(-1)
}

// Options configures the logger behavior.
type Options struct {
	// Level is the minimum slog log level. Use ParseLogLevel to obtain from a string.
	Level slog.Level
	// Format is the output format (json, console, or text).
	Format LogFormat
	// Timestamps controls whether timestamps are included in log output.
	Timestamps bool
	// FilePath is an optional file path to write logs to. When set, logs are
	// written to this file. If empty, logs go to stderr.
	FilePath string
	// AlsoStderr controls whether logs are also written to stderr when FilePath
	// is set. When FilePath is empty, this has no effect (stderr is always used).
	AlsoStderr bool
}

// DefaultOptions returns the default logger options.
func DefaultOptions() Options {
	return Options{
		Level:      LogLevelNone,
		Format:     FormatConsole,
		Timestamps: true,
	}
}

// Get initializes the global logger with default options and the given slog level.
// It can only be called once. Subsequent calls will have no effect.
// This function must be called before using FromContext or any logging operations.
func Get(logLevel slog.Level) *logr.Logger {
	return GetWithOptions(Options{
		Level:      logLevel,
		Format:     FormatJSON,
		Timestamps: true,
	})
}

// GetWithOptions initializes the global logr logger with custom options using
// a slog handler for local output and an otellogr sink for OTel export.
// It can only be called once. Subsequent calls will have no effect.
// This function must be called before using FromContext or any logging operations.
//
// If telemetry.Setup has been called before this function, the otellogr sink
// will forward log records to the configured OTel LoggerProvider. Otherwise
// the noop provider is used, which is safe for unit tests.
func GetWithOptions(opts Options) *logr.Logger {
	once.Do(func() {
		// If level is set to LogLevelNone, return a noop logger
		if opts.Level >= LogLevelNone {
			gl := logr.Discard()
			globalLogrLogger = &gl
			return
		}

		buildInfo, _ := debug.ReadBuildInfo()
		goVersion := ""
		if buildInfo != nil {
			goVersion = buildInfo.GoVersion
		}

		// ── Sink 1: slog handler for local console/file output ───────────────
		slogLevel := &slog.LevelVar{}
		slogLevel.Set(opts.Level)

		// Determine output destination
		var slogWriter io.Writer = os.Stderr
		if opts.FilePath != "" {
			f, err := os.OpenFile(opts.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "WARNING: failed to open log file %q: %v, falling back to stderr\n", opts.FilePath, err)
			case opts.AlsoStderr:
				slogWriter = io.MultiWriter(os.Stderr, f)
			default:
				slogWriter = f
			}
		}

		handlerOpts := &slog.HandlerOptions{
			Level:     slogLevel,
			AddSource: true,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey && !opts.Timestamps {
					return slog.Attr{} // drop timestamp
				}
				if a.Key == slog.TimeKey {
					a.Key = TimeStampKey
				}
				if a.Key == slog.MessageKey {
					a.Key = MessageKey
				}
				return a
			},
		}

		var slogHandler slog.Handler
		if opts.Format == FormatJSON {
			slogHandler = slog.NewJSONHandler(slogWriter, handlerOpts)
		} else {
			slogHandler = slog.NewTextHandler(slogWriter, handlerOpts)
		}

		// Add static fields (commit, version, build time, go version)
		slogHandler = slogHandler.WithAttrs([]slog.Attr{
			slog.String(CommitKey, settings.VersionInformation.Commit),
			slog.String(VersionKey, settings.VersionInformation.BuildVersion),
			slog.String(BuildTimeKey, settings.VersionInformation.BuildTime),
			slog.String(GoVersionKey, goVersion),
		})

		consoleSink := logr.FromSlogHandler(slogHandler).GetSink()

		// ── Sink 2: otellogr forwarding to global OTel LoggerProvider ────────
		// Uses logGlobal.GetLoggerProvider() which returns the provider set by
		// telemetry.Setup(). If Setup has not been called, this is the noop provider
		// and nothing is exported — safe for unit tests.
		otelSink := otellogr.NewLogSink(settings.CliBinaryName,
			otellogr.WithLoggerProvider(logGlobal.GetLoggerProvider()),
		)

		gl := logr.New(newMultiSink(consoleSink, otelSink))
		globalLogrLogger = &gl
	})
	if globalLogrLogger == nil {
		return &defaultNoopLogger
	}
	return globalLogrLogger
}

// WithLogger returns a new context with the provided logr.Logger attached.
// If the context already contains the same logger instance, it returns the original context.
// This allows logger propagation through context for structured logging.
func WithLogger(ctx context.Context, log *logr.Logger) context.Context {
	if lp, ok := ctx.Value(loggerContextKey{}).(*logr.Logger); ok {
		if lp == log {
			return ctx
		}
	}
	return context.WithValue(ctx, loggerContextKey{}, log)
}

// FromContext retrieves the logr.Logger from the context.
// If no logger is found in the context, it returns the globally configured logger.
// If Setup has not been called, it returns a no-op logger to prevent panics.
func FromContext(ctx context.Context) *logr.Logger {
	if log, ok := ctx.Value(loggerContextKey{}).(*logr.Logger); ok {
		return log
	} else if log := globalLogrLogger; log != nil {
		// If no logger in context, return the global logger.
		return log
	}
	// Fallback to a no-op logger if Setup hasn't been called at all.
	return &defaultNoopLogger
}

// GetGlobalLogger returns the globally configured logr.Logger.
// This is useful for top-level logging in main where context might not be readily available,
// or as a fallback. It will return a no-op logger if Setup has not been called.
func GetGlobalLogger() *logr.Logger {
	if globalLogrLogger != nil {
		return globalLogrLogger
	}
	return &defaultNoopLogger
}

func GetNoopLogger() *logr.Logger {
	return &defaultNoopLogger
}

// WithValues returns a new logr.Logger with additional key-value pairs for structured logging.
// The provided keysAndValues are added to the logger's context, allowing for richer log output.
// lgr: The base logger to augment.
// keysAndValues: Variadic list of key-value pairs to associate with the logger.
// Returns a pointer to the new logger with the added values.
func WithValues(lgr *logr.Logger, keysAndValues ...any) *logr.Logger {
	nlgr := lgr.WithValues(keysAndValues...)
	return &nlgr
}
